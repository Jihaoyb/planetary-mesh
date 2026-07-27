package pmctl

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"planetary-mesh/internal/protocol"
	"planetary-mesh/internal/security"
)

const (
	doctorSchemaVersion = 1
	doctorMinTimeout    = 100 * time.Millisecond
	doctorMaxTimeout    = 60 * time.Second
	knownSchemaVersion  = 2

	doctorExitDiagnosticFailure = 1
	doctorExitUsage             = 2
	doctorExitInterrupted       = 3
	doctorExitInternal          = 4
	doctorExitStrictWarning     = 5
)

type doctorStatus string

const (
	doctorStatusPass doctorStatus = "PASS"
	doctorStatusWarn doctorStatus = "WARN"
	doctorStatusFail doctorStatus = "FAIL"
)

type doctorOptions struct {
	Strict  bool
	Timeout time.Duration
}

type doctorClient interface {
	Status(context.Context) (protocol.CoordinatorStatusResponse, error)
	ListNodes(context.Context) ([]Node, error)
}

type doctorReport struct {
	SchemaVersion int           `json:"schema_version"`
	OverallStatus doctorStatus  `json:"overall_status"`
	Strict        bool          `json:"strict"`
	Timeout       string        `json:"timeout"`
	Summary       doctorSummary `json:"summary"`
	Facts         doctorFacts   `json:"facts"`
	Checks        []doctorCheck `json:"checks"`
	Scope         doctorScope   `json:"scope"`
	Limitations   []string      `json:"limitations"`
}

type doctorSummary struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
}

type doctorFacts struct {
	CoordinatorReachable *bool                      `json:"coordinator_reachable"`
	ProtocolCompatible   *bool                      `json:"protocol_compatible"`
	CoordinatorHealthy   *bool                      `json:"coordinator_healthy"`
	JobSubmissionReady   *bool                      `json:"job_submission_ready"`
	StorageBackend       *string                    `json:"storage_backend"`
	Schema               *doctorSchemaFacts         `json:"schema"`
	SecureMode           *bool                      `json:"secure_mode"`
	NodeAllowlistEnabled *bool                      `json:"node_allowlist_enabled"`
	Dispatch             *doctorDispatchFacts       `json:"dispatch"`
	Reconciliation       *doctorReconciliationFacts `json:"reconciliation"`
	Nodes                *doctorNodeFacts           `json:"nodes"`
}

type doctorSchemaFacts struct {
	Ready           bool `json:"ready"`
	Version         int  `json:"version"`
	ExpectedVersion int  `json:"expected_version"`
}

type doctorDispatchFacts struct {
	Timeout     string `json:"timeout"`
	MaxAttempts int    `json:"max_attempts"`
	BaseBackoff string `json:"base_backoff"`
}

type doctorReconciliationFacts struct {
	Grace              string `json:"grace"`
	PendingRunningJobs uint64 `json:"pending_running_jobs"`
}

type doctorNodeFacts struct {
	Total   int `json:"total"`
	Healthy int `json:"healthy"`
	Suspect int `json:"suspect"`
	Offline int `json:"offline"`
}

type doctorCheck struct {
	Name        string       `json:"name"`
	Status      doctorStatus `json:"status"`
	Code        string       `json:"code"`
	Summary     string       `json:"summary"`
	Remediation []string     `json:"remediation"`
}

type doctorScope struct {
	EndpointsUsed             []string `json:"endpoints_used"`
	CreatesJobs               bool     `json:"creates_jobs"`
	ExecutesCommands          bool     `json:"executes_commands"`
	ContactsAgentsDirectly    bool     `json:"contacts_agents_directly"`
	ReadsMetrics              bool     `json:"reads_metrics"`
	ChecksAgentAllowlists     bool     `json:"checks_agent_allowlists"`
	ChecksWorkloadExecutables bool     `json:"checks_workload_executables"`
	ChecksAgentLocalFiles     bool     `json:"checks_agent_local_files"`
	ModifiesConfiguration     bool     `json:"modifies_configuration"`
	ManagesServices           bool     `json:"manages_services"`
	ManagesCertificates       bool     `json:"manages_certificates"`
}

type doctorCommandError struct {
	code     int
	message  string
	reported bool
}

func (e *doctorCommandError) Error() string {
	return e.message
}

type doctorRunResult struct {
	interrupted bool
}

func parseDoctorFlags(args []string) (doctorOptions, error) {
	fs := flag.NewFlagSet("pmctl doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	opts := doctorOptions{Timeout: DefaultTimeout}
	fs.BoolVar(&opts.Strict, "strict", false, "return non-zero when diagnostics contain warnings")
	fs.DurationVar(&opts.Timeout, "timeout", DefaultTimeout, "total diagnostic network timeout")
	if err := fs.Parse(args); err != nil {
		return doctorOptions{}, usageError("usage: pmctl doctor [--strict] [--timeout <duration>]")
	}
	if fs.NArg() != 0 {
		return doctorOptions{}, usageError("usage: pmctl doctor [--strict] [--timeout <duration>]")
	}
	if opts.Timeout < doctorMinTimeout || opts.Timeout > doctorMaxTimeout {
		return doctorOptions{}, usageError("doctor --timeout must be between 100ms and 60s")
	}
	return opts, nil
}

func runDoctorFromSources(ctx context.Context, allArgs, doctorArgs []string, stdout io.Writer, jsonOut bool) error {
	opts, err := parseDoctorFlags(doctorArgs)
	if err != nil {
		return &doctorCommandError{code: doctorExitUsage, message: err.Error()}
	}
	report := newDoctorReport(opts)

	cfg, err := ConfigFromSources(allArgs)
	if err == nil {
		_, _, err = parseGlobalFlags(allArgs, &cfg)
	}
	if err != nil {
		report.addCheck(doctorCheck{
			Name:    "client_configuration",
			Status:  doctorStatusFail,
			Code:    "config_file_invalid",
			Summary: "The pmctl configuration file could not be loaded or parsed.",
			Remediation: []string{
				"Check --config or PMCTL_CONFIG_FILE and use a valid env-style configuration file.",
			},
		})
		return writeDoctorResult(stdout, report, jsonOut, doctorRunResult{})
	}

	validated, check := validateDoctorConfig(cfg)
	report.addCheck(check)
	if check.Status == doctorStatusFail {
		return writeDoctorResult(stdout, report, jsonOut, doctorRunResult{})
	}

	client, err := newDoctorClient(validated, opts.Timeout)
	if err != nil {
		report.addCheck(doctorCheck{
			Name:    "coordinator_connectivity",
			Status:  doctorStatusFail,
			Code:    "client_initialization_failed",
			Summary: "The diagnostic HTTP client could not be initialized.",
			Remediation: []string{
				"Recheck the pmctl URL and TLS configuration, then rerun pmctl doctor.",
			},
		})
		return writeDoctorResult(stdout, report, jsonOut, doctorRunResult{})
	}

	networkCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	result := runDoctorChecks(networkCtx, client, &report)
	return writeDoctorResult(stdout, report, jsonOut, result)
}

func newDoctorReport(opts doctorOptions) doctorReport {
	return doctorReport{
		SchemaVersion: doctorSchemaVersion,
		OverallStatus: doctorStatusPass,
		Strict:        opts.Strict,
		Timeout:       opts.Timeout.String(),
		Checks:        []doctorCheck{},
		Scope: doctorScope{
			EndpointsUsed: []string{"/status", "/nodes"},
		},
		Limitations: []string{
			"Node health is a coordinator-reported heartbeat snapshot, not a direct agent probe.",
			"A PASS does not prove workload safety, strong isolation, production readiness, or agent-local file availability.",
		},
	}
}

func validateDoctorConfig(cfg Config) (Config, doctorCheck) {
	rawURL := strings.TrimSpace(cfg.CoordinatorURL)
	if rawURL == "" {
		rawURL = DefaultCoordinatorURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" ||
		parsed.Hostname() == "" ||
		parsed.Opaque != "" ||
		parsed.User != nil ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return Config{}, doctorCheck{
			Name:    "client_configuration",
			Status:  doctorStatusFail,
			Code:    "coordinator_url_invalid",
			Summary: "The coordinator URL is invalid or contains unsupported sensitive components.",
			Remediation: []string{
				"Set PMCTL_COORDINATOR_URL or --coordinator-url to an absolute http:// or https:// base URL without credentials, query, fragment, or path.",
			},
		}
	}

	if err := cfg.TLSFiles.ValidateComplete("PMCTL"); err != nil {
		return Config{}, doctorCheck{
			Name:    "client_configuration",
			Status:  doctorStatusFail,
			Code:    "tls_config_partial",
			Summary: "The pmctl TLS configuration is incomplete.",
			Remediation: []string{
				"Configure PMCTL_TLS_CA_FILE, PMCTL_TLS_CERT_FILE, and PMCTL_TLS_KEY_FILE together, or configure none.",
			},
		}
	}
	if cfg.TLSFiles.Configured() && parsed.Scheme != "https" {
		return Config{}, doctorCheck{
			Name:    "client_configuration",
			Status:  doctorStatusFail,
			Code:    "tls_requires_https",
			Summary: "TLS client files are configured for a plain HTTP coordinator URL.",
			Remediation: []string{
				"Use an https:// coordinator URL with the configured TLS files, or remove the TLS file settings for supported local plain-HTTP mode.",
			},
		}
	}
	if cfg.TLSFiles.Configured() {
		for _, path := range []string{cfg.TLSFiles.CAFile, cfg.TLSFiles.CertFile, cfg.TLSFiles.KeyFile} {
			file, err := os.Open(path)
			if err != nil {
				return Config{}, doctorCheck{
					Name:    "client_configuration",
					Status:  doctorStatusFail,
					Code:    "tls_file_unreadable",
					Summary: "One or more pmctl TLS files cannot be read.",
					Remediation: []string{
						"Check the configured PMCTL TLS files and their permissions without sharing their contents.",
					},
				}
			}
			_ = file.Close()
		}
		if _, err := security.LoadCAPool(cfg.TLSFiles.CAFile); err != nil {
			return Config{}, doctorCheck{
				Name:    "client_configuration",
				Status:  doctorStatusFail,
				Code:    "tls_ca_invalid",
				Summary: "The configured pmctl CA file does not contain a usable CA certificate.",
				Remediation: []string{
					"Provide a valid CA certificate through PMCTL_TLS_CA_FILE.",
				},
			}
		}
		if _, err := security.LoadKeyPair(cfg.TLSFiles.CertFile, cfg.TLSFiles.KeyFile); err != nil {
			return Config{}, doctorCheck{
				Name:    "client_configuration",
				Status:  doctorStatusFail,
				Code:    "tls_keypair_invalid",
				Summary: "The configured pmctl client certificate and key are invalid or do not match.",
				Remediation: []string{
					"Provide a matching client certificate and private key through the PMCTL TLS settings.",
				},
			}
		}
	}

	cfg.CoordinatorURL = strings.TrimRight(rawURL, "/")
	return cfg, doctorCheck{
		Name:        "client_configuration",
		Status:      doctorStatusPass,
		Code:        "configuration_valid",
		Summary:     "Local pmctl configuration is valid.",
		Remediation: []string{},
	}
}

func runDoctorChecks(ctx context.Context, client doctorClient, report *doctorReport) doctorRunResult {
	status, err := client.Status(ctx)
	if err != nil {
		return classifyStatusFailure(ctx, err, report)
	}

	report.Facts.CoordinatorReachable = boolPointer(true)
	report.addCheck(doctorCheck{
		Name:        "coordinator_connectivity",
		Status:      doctorStatusPass,
		Code:        "http_response_received",
		Summary:     "The configured coordinator returned an HTTP response.",
		Remediation: []string{},
	})
	report.addCheck(doctorCheck{
		Name:        "status_endpoint",
		Status:      doctorStatusPass,
		Code:        "status_response_valid",
		Summary:     "The coordinator status endpoint returned one valid JSON document.",
		Remediation: []string{},
	})

	protocolCompatible := status.ProtocolVersion == protocol.Version
	report.Facts.ProtocolCompatible = boolPointer(protocolCompatible)
	if !protocolCompatible {
		code := "protocol_mismatch"
		summary := "The coordinator protocol version does not match pmctl protocol version 1."
		if status.ProtocolVersion == "" {
			code = "protocol_missing"
			summary = "The coordinator status response does not include a protocol version."
		}
		report.addCheck(doctorCheck{
			Name:    "protocol_compatibility",
			Status:  doctorStatusFail,
			Code:    code,
			Summary: summary,
			Remediation: []string{
				"Use coordinator and pmctl binaries compatible with X-Planetary-Protocol-Version: 1.",
			},
		})
		return doctorRunResult{}
	}
	report.addCheck(doctorCheck{
		Name:        "protocol_compatibility",
		Status:      doctorStatusPass,
		Code:        "protocol_version_1",
		Summary:     "The coordinator and pmctl use protocol version 1.",
		Remediation: []string{},
	})

	dispatch, dispatchValid := validateDispatchStatus(status.Dispatch)
	healthy := status.Status == "ok" && dispatchValid
	report.Facts.CoordinatorHealthy = boolPointer(healthy)
	if !healthy {
		code := "coordinator_not_ok"
		summary := "The coordinator did not report status ok."
		if status.Status == "ok" {
			code = "runtime_metadata_invalid"
			summary = "The coordinator returned invalid dispatch runtime metadata."
		}
		report.addCheck(doctorCheck{
			Name:    "coordinator_health",
			Status:  doctorStatusFail,
			Code:    code,
			Summary: summary,
			Remediation: []string{
				"Inspect the coordinator service and logs, correct its runtime configuration, and rerun pmctl doctor.",
			},
		})
		return doctorRunResult{}
	}
	report.Facts.Dispatch = dispatch
	report.addCheck(doctorCheck{
		Name:        "coordinator_health",
		Status:      doctorStatusPass,
		Code:        "coordinator_ok",
		Summary:     "The coordinator reports status ok with valid dispatch metadata.",
		Remediation: []string{},
	})

	classifyStorage(status, report)
	classifySecurity(status, report)
	classifyReconciliation(status, report)

	nodes, err := client.ListNodes(ctx)
	if err != nil {
		return classifyNodesFailure(ctx, err, report)
	}
	counts, check := classifyNodes(nodes)
	report.Facts.Nodes = counts
	report.addCheck(check)
	if counts == nil {
		return doctorRunResult{}
	}

	ready := counts.Healthy > 0 && !report.hasFailure()
	report.Facts.JobSubmissionReady = boolPointer(ready)
	return doctorRunResult{}
}

func classifyStatusFailure(ctx context.Context, err error, report *doctorReport) doctorRunResult {
	if interruptedCode(ctx, err) != "" {
		code := interruptedCode(ctx, err)
		report.Facts.CoordinatorReachable = boolPointer(false)
		report.addCheck(doctorCheck{
			Name:    "coordinator_connectivity",
			Status:  doctorStatusFail,
			Code:    code,
			Summary: interruptionSummary(code),
			Remediation: []string{
				"Retry with a reachable coordinator and, for slow trusted networks, choose a doctor --timeout between 100ms and 60s.",
			},
		})
		return doctorRunResult{interrupted: true}
	}

	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		report.Facts.CoordinatorReachable = boolPointer(true)
		report.addCheck(doctorCheck{
			Name:        "coordinator_connectivity",
			Status:      doctorStatusPass,
			Code:        "http_response_received",
			Summary:     "The configured coordinator returned an HTTP response.",
			Remediation: []string{},
		})
		if httpErr.StatusCode == http.StatusConflict {
			report.Facts.ProtocolCompatible = boolPointer(false)
			report.addCheck(doctorCheck{
				Name:    "protocol_compatibility",
				Status:  doctorStatusFail,
				Code:    "protocol_rejected",
				Summary: "The coordinator rejected pmctl protocol version 1.",
				Remediation: []string{
					"Use coordinator and pmctl binaries compatible with X-Planetary-Protocol-Version: 1.",
				},
			})
			return doctorRunResult{}
		}
		report.addCheck(statusHTTPFailure(httpErr.StatusCode))
		return doctorRunResult{}
	}

	var decodeErr *DecodeError
	if errors.As(err, &decodeErr) {
		report.Facts.CoordinatorReachable = boolPointer(true)
		report.addCheck(doctorCheck{
			Name:        "coordinator_connectivity",
			Status:      doctorStatusPass,
			Code:        "http_response_received",
			Summary:     "The configured coordinator returned an HTTP response.",
			Remediation: []string{},
		})
		report.addCheck(doctorCheck{
			Name:    "status_endpoint",
			Status:  doctorStatusFail,
			Code:    "status_invalid_json",
			Summary: "The coordinator status endpoint did not return exactly one valid JSON document.",
			Remediation: []string{
				"Verify the coordinator base URL and use a compatible coordinator binary.",
			},
		})
		return doctorRunResult{}
	}

	report.Facts.CoordinatorReachable = boolPointer(false)
	code := "coordinator_unreachable"
	summary := "The coordinator could not be reached."
	if isTLSFailure(err) {
		code = "tls_handshake_failed"
		summary = "The TLS handshake with the coordinator failed."
	}
	report.addCheck(doctorCheck{
		Name:    "coordinator_connectivity",
		Status:  doctorStatusFail,
		Code:    code,
		Summary: summary,
		Remediation: []string{
			connectivityRemediation(code),
		},
	})
	return doctorRunResult{}
}

func statusHTTPFailure(statusCode int) doctorCheck {
	check := doctorCheck{
		Name:    "status_endpoint",
		Status:  doctorStatusFail,
		Code:    "status_unexpected_http_status",
		Summary: "The coordinator status endpoint returned an unexpected HTTP status.",
		Remediation: []string{
			"Verify the coordinator base URL and inspect the coordinator service and logs.",
		},
	}
	switch {
	case statusCode == http.StatusUnauthorized:
		check.Code = "status_unauthorized"
		check.Summary = "The coordinator status request was unauthorized."
		check.Remediation = []string{"Verify operator access and pmctl TLS client configuration."}
	case statusCode == http.StatusForbidden:
		check.Code = "status_forbidden"
		check.Summary = "The coordinator status request was forbidden."
		check.Remediation = []string{"Verify the operator certificate and coordinator access policy."}
	case statusCode == http.StatusNotFound:
		check.Code = "status_not_found"
		check.Summary = "The coordinator status endpoint was not found."
		check.Remediation = []string{"Verify the coordinator base URL and coordinator binary version."}
	case statusCode >= 300 && statusCode < 400:
		check.Code = "status_redirect_rejected"
		check.Summary = "The coordinator status endpoint returned a redirect, which doctor does not follow."
		check.Remediation = []string{"Configure the final coordinator base URL directly without a redirect."}
	case statusCode >= 400 && statusCode < 500:
		check.Code = "status_client_error"
		check.Summary = "The coordinator status endpoint rejected the request."
	case statusCode >= 500 && statusCode < 600:
		check.Code = "status_server_error"
		check.Summary = "The coordinator status endpoint returned a server error."
	}
	return check
}

func validateDispatchStatus(status protocol.DispatchStatus) (*doctorDispatchFacts, bool) {
	timeout, err := time.ParseDuration(status.Timeout)
	if err != nil || timeout <= 0 || status.MaxAttempts < 1 {
		return nil, false
	}
	backoff, err := time.ParseDuration(status.BaseBackoff)
	if err != nil || backoff < 0 {
		return nil, false
	}
	return &doctorDispatchFacts{
		Timeout:     timeout.String(),
		MaxAttempts: status.MaxAttempts,
		BaseBackoff: backoff.String(),
	}, true
}

func classifyStorage(status protocol.CoordinatorStatusResponse, report *doctorReport) {
	switch status.StorageBackend {
	case "in_memory":
		backend := "in_memory"
		report.Facts.StorageBackend = &backend
		if status.Schema != nil {
			report.addCheck(doctorCheck{
				Name:    "storage_readiness",
				Status:  doctorStatusFail,
				Code:    "unexpected_schema_metadata",
				Summary: "The in-memory coordinator returned unexpected Postgres schema metadata.",
				Remediation: []string{
					"Inspect the coordinator runtime configuration and use a compatible coordinator binary.",
				},
			})
			return
		}
		report.addCheck(doctorCheck{
			Name:        "storage_readiness",
			Status:      doctorStatusPass,
			Code:        "in_memory_supported",
			Summary:     "In-memory storage is active; it is supported but non-durable.",
			Remediation: []string{},
		})
	case "postgres":
		backend := "postgres"
		report.Facts.StorageBackend = &backend
		if status.Schema == nil {
			report.addCheck(doctorCheck{
				Name:    "storage_readiness",
				Status:  doctorStatusFail,
				Code:    "schema_metadata_missing",
				Summary: "The Postgres coordinator did not return schema readiness metadata.",
				Remediation: []string{
					"Inspect Postgres initialization and coordinator logs.",
				},
			})
			return
		}
		schema := &doctorSchemaFacts{
			Ready:           status.Schema.Ready,
			Version:         status.Schema.Version,
			ExpectedVersion: status.Schema.ExpectedVersion,
		}
		report.Facts.Schema = schema
		switch {
		case !schema.Ready:
			report.addCheck(doctorCheck{
				Name:    "storage_readiness",
				Status:  doctorStatusFail,
				Code:    "schema_not_ready",
				Summary: "Postgres schema readiness is false.",
				Remediation: []string{
					"Inspect Postgres initialization, schema metadata, and coordinator logs.",
				},
			})
		case schema.Version <= 0 || schema.ExpectedVersion <= 0:
			report.addCheck(doctorCheck{
				Name:    "storage_readiness",
				Status:  doctorStatusFail,
				Code:    "schema_version_invalid",
				Summary: "Postgres schema version metadata is invalid.",
				Remediation: []string{
					"Inspect Postgres schema metadata and use a compatible coordinator binary.",
				},
			})
		case schema.Version != schema.ExpectedVersion:
			report.addCheck(doctorCheck{
				Name:    "storage_readiness",
				Status:  doctorStatusFail,
				Code:    "schema_version_mismatch",
				Summary: "Postgres schema version does not match the coordinator expectation.",
				Remediation: []string{
					"Use the expected coordinator/database schema combination before accepting jobs.",
				},
			})
		case schema.ExpectedVersion != knownSchemaVersion:
			report.addCheck(doctorCheck{
				Name:    "storage_readiness",
				Status:  doctorStatusWarn,
				Code:    "schema_version_unknown",
				Summary: "Postgres reports ready with a schema version this pmctl does not recognize.",
				Remediation: []string{
					"Confirm coordinator and pmctl release compatibility before using this result as an automation gate.",
				},
			})
		default:
			report.addCheck(doctorCheck{
				Name:        "storage_readiness",
				Status:      doctorStatusPass,
				Code:        "postgres_schema_ready",
				Summary:     "Postgres schema readiness version 2 is valid.",
				Remediation: []string{},
			})
		}
	default:
		report.addCheck(doctorCheck{
			Name:    "storage_readiness",
			Status:  doctorStatusFail,
			Code:    "storage_backend_unknown",
			Summary: "The coordinator reported an unknown storage backend.",
			Remediation: []string{
				"Use a coordinator configured for supported in-memory or Postgres storage.",
			},
		})
	}
}

func classifySecurity(status protocol.CoordinatorStatusResponse, report *doctorReport) {
	report.Facts.SecureMode = boolPointer(status.SecureMode)
	report.Facts.NodeAllowlistEnabled = boolPointer(status.NodeAllowlistEnabled)
	switch {
	case status.SecureMode && status.NodeAllowlistEnabled:
		report.addCheck(doctorCheck{
			Name:        "transport_security",
			Status:      doctorStatusPass,
			Code:        "secure_mode_enabled",
			Summary:     "Coordinator mTLS secure mode and node allowlisting are enabled.",
			Remediation: []string{},
		})
	case !status.SecureMode && !status.NodeAllowlistEnabled:
		report.addCheck(doctorCheck{
			Name:        "transport_security",
			Status:      doctorStatusPass,
			Code:        "plain_mode_supported",
			Summary:     "Plain coordinator mode is active; it is supported for local or trusted-network use.",
			Remediation: []string{},
		})
	default:
		report.addCheck(doctorCheck{
			Name:    "transport_security",
			Status:  doctorStatusFail,
			Code:    "security_metadata_inconsistent",
			Summary: "Coordinator secure-mode and node-allowlist metadata are inconsistent.",
			Remediation: []string{
				"Inspect coordinator TLS and node allowlist configuration before accepting jobs.",
			},
		})
	}
}

func classifyReconciliation(status protocol.CoordinatorStatusResponse, report *doctorReport) {
	switch status.StorageBackend {
	case "in_memory":
		if status.Reconciliation != nil {
			report.addCheck(doctorCheck{
				Name:    "reconciliation",
				Status:  doctorStatusFail,
				Code:    "reconciliation_unexpected",
				Summary: "The in-memory coordinator returned unexpected reconciliation metadata.",
				Remediation: []string{
					"Inspect the coordinator storage configuration and use a compatible coordinator binary.",
				},
			})
			return
		}
		report.addCheck(doctorCheck{
			Name:        "reconciliation",
			Status:      doctorStatusPass,
			Code:        "reconciliation_not_applicable",
			Summary:     "Startup reconciliation is not applicable to in-memory storage.",
			Remediation: []string{},
		})
	case "postgres":
		if status.Reconciliation == nil {
			report.addCheck(doctorCheck{
				Name:    "reconciliation",
				Status:  doctorStatusFail,
				Code:    "reconciliation_metadata_missing",
				Summary: "The Postgres coordinator did not return reconciliation metadata.",
				Remediation: []string{
					"Inspect coordinator startup and use a compatible coordinator binary.",
				},
			})
			return
		}
		grace, err := time.ParseDuration(status.Reconciliation.Grace)
		if err != nil || grace < 0 {
			report.addCheck(doctorCheck{
				Name:    "reconciliation",
				Status:  doctorStatusFail,
				Code:    "reconciliation_metadata_invalid",
				Summary: "The coordinator returned invalid reconciliation metadata.",
				Remediation: []string{
					"Inspect COORDINATOR_RECONCILIATION_GRACE and coordinator startup logs.",
				},
			})
			return
		}
		report.Facts.Reconciliation = &doctorReconciliationFacts{
			Grace:              grace.String(),
			PendingRunningJobs: status.Reconciliation.PendingRunningJobs,
		}
		if status.Reconciliation.PendingRunningJobs > 0 {
			report.addCheck(doctorCheck{
				Name:    "reconciliation",
				Status:  doctorStatusWarn,
				Code:    "reconciliation_pending",
				Summary: "Startup-running jobs are still pending reconciliation.",
				Remediation: []string{
					"Wait for agent reports or reconciliation grace expiry, rerun pmctl doctor, and inspect jobs with pmctl jobs list or pmctl jobs inspect.",
				},
			})
			return
		}
		report.addCheck(doctorCheck{
			Name:        "reconciliation",
			Status:      doctorStatusPass,
			Code:        "reconciliation_clear",
			Summary:     "No startup-running jobs are pending reconciliation.",
			Remediation: []string{},
		})
	}
}

func classifyNodes(nodes []Node) (*doctorNodeFacts, doctorCheck) {
	counts := &doctorNodeFacts{}
	seen := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if strings.TrimSpace(node.ID) == "" ||
			strings.TrimSpace(node.Address) == "" ||
			node.LastSeen.IsZero() {
			return nil, invalidNodeMetadataCheck()
		}
		if _, ok := seen[node.ID]; ok {
			return nil, invalidNodeMetadataCheck()
		}
		seen[node.ID] = struct{}{}
		if _, err := protocol.NormalizeNodeCapabilities(node.Capabilities); err != nil {
			return nil, invalidNodeMetadataCheck()
		}
		if err := protocol.ValidateNodeLoad(node.Load); err != nil {
			return nil, invalidNodeMetadataCheck()
		}
		switch node.State {
		case "HEALTHY":
			counts.Healthy++
		case "SUSPECT":
			counts.Suspect++
		case "OFFLINE":
			counts.Offline++
		default:
			return nil, invalidNodeMetadataCheck()
		}
		counts.Total++
	}

	switch {
	case counts.Total == 0:
		return counts, doctorCheck{
			Name:    "node_readiness",
			Status:  doctorStatusWarn,
			Code:    "no_nodes",
			Summary: "No agents are registered.",
			Remediation: []string{
				"Start and register at least one agent, then rerun pmctl doctor.",
			},
		}
	case counts.Healthy == 0:
		return counts, doctorCheck{
			Name:    "node_readiness",
			Status:  doctorStatusWarn,
			Code:    "no_healthy_nodes",
			Summary: "Agents are registered, but none are coordinator-reported HEALTHY.",
			Remediation: []string{
				"Inspect agent processes, registration, coordinator URL, firewall, protocol, and mTLS configuration.",
			},
		}
	case counts.Suspect > 0 || counts.Offline > 0:
		return counts, doctorCheck{
			Name:    "node_readiness",
			Status:  doctorStatusWarn,
			Code:    "nodes_degraded",
			Summary: "At least one agent is healthy, but suspect or offline agents are also registered.",
			Remediation: []string{
				"Inspect unhealthy agents and stale heartbeat state; jobs can use currently healthy agents.",
			},
		}
	default:
		return counts, doctorCheck{
			Name:        "node_readiness",
			Status:      doctorStatusPass,
			Code:        "healthy_nodes_available",
			Summary:     "At least one coordinator-reported HEALTHY agent is available.",
			Remediation: []string{},
		}
	}
}

func classifyNodesFailure(ctx context.Context, err error, report *doctorReport) doctorRunResult {
	if code := interruptedCode(ctx, err); code != "" {
		report.addCheck(doctorCheck{
			Name:    "node_readiness",
			Status:  doctorStatusFail,
			Code:    code,
			Summary: interruptionSummary(code),
			Remediation: []string{
				"Retry with a reachable coordinator and, for slow trusted networks, choose a doctor --timeout between 100ms and 60s.",
			},
		})
		return doctorRunResult{interrupted: true}
	}

	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		check := statusHTTPFailure(httpErr.StatusCode)
		check.Name = "node_readiness"
		check.Code = strings.Replace(check.Code, "status_", "nodes_", 1)
		check.Summary = "The coordinator nodes endpoint could not be inspected."
		check.Remediation = []string{"Verify coordinator access and inspect the coordinator service and logs."}
		report.addCheck(check)
		return doctorRunResult{}
	}
	var decodeErr *DecodeError
	if errors.As(err, &decodeErr) {
		report.addCheck(doctorCheck{
			Name:    "node_readiness",
			Status:  doctorStatusFail,
			Code:    "nodes_invalid_json",
			Summary: "The coordinator nodes endpoint did not return exactly one valid JSON document.",
			Remediation: []string{
				"Verify coordinator compatibility and inspect the coordinator service and logs.",
			},
		})
		return doctorRunResult{}
	}

	report.addCheck(doctorCheck{
		Name:    "node_readiness",
		Status:  doctorStatusFail,
		Code:    "nodes_invalid_metadata",
		Summary: "The coordinator returned malformed node metadata.",
		Remediation: []string{
			"Inspect the coordinator nodes response and use compatible coordinator and agent binaries.",
		},
	})
	return doctorRunResult{}
}

func invalidNodeMetadataCheck() doctorCheck {
	return doctorCheck{
		Name:    "node_readiness",
		Status:  doctorStatusFail,
		Code:    "nodes_invalid_metadata",
		Summary: "The coordinator returned malformed node metadata.",
		Remediation: []string{
			"Inspect the coordinator nodes response and use compatible coordinator and agent binaries.",
		},
	}
}

func writeDoctorResult(w io.Writer, report doctorReport, jsonOut bool, result doctorRunResult) error {
	report.aggregate()
	if err := writeValue(w, report, jsonOut, writeDoctorReport); err != nil {
		return &doctorCommandError{
			code:    doctorExitInternal,
			message: "write diagnostic output failed",
		}
	}

	switch {
	case result.interrupted:
		return &doctorCommandError{code: doctorExitInterrupted, reported: true}
	case report.OverallStatus == doctorStatusFail:
		return &doctorCommandError{code: doctorExitDiagnosticFailure, reported: true}
	case report.OverallStatus == doctorStatusWarn && report.Strict:
		return &doctorCommandError{code: doctorExitStrictWarning, reported: true}
	default:
		return nil
	}
}

func (r *doctorReport) addCheck(check doctorCheck) {
	if check.Remediation == nil {
		check.Remediation = []string{}
	}
	r.Checks = append(r.Checks, check)
}

func (r *doctorReport) aggregate() {
	r.Summary = doctorSummary{}
	r.OverallStatus = doctorStatusPass
	for _, check := range r.Checks {
		switch check.Status {
		case doctorStatusPass:
			r.Summary.Pass++
		case doctorStatusWarn:
			r.Summary.Warn++
			if r.OverallStatus == doctorStatusPass {
				r.OverallStatus = doctorStatusWarn
			}
		case doctorStatusFail:
			r.Summary.Fail++
			r.OverallStatus = doctorStatusFail
		}
	}
}

func (r doctorReport) hasFailure() bool {
	for _, check := range r.Checks {
		if check.Status == doctorStatusFail {
			return true
		}
	}
	return false
}

func boolPointer(value bool) *bool {
	return &value
}

func interruptedCode(ctx context.Context, err error) string {
	switch {
	case errors.Is(ctx.Err(), context.Canceled), errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return ""
	}
}

func interruptionSummary(code string) string {
	if code == "canceled" {
		return "The diagnostic request was canceled."
	}
	return "The diagnostic network timeout expired."
}

func connectivityRemediation(code string) string {
	if code == "tls_handshake_failed" {
		return "Check the CA, client certificate/key, hostname, expiry, and coordinator mTLS configuration without sharing certificate contents."
	}
	return "Check that the coordinator is running and reachable from this host, then rerun pmctl doctor."
}

func isTLSFailure(err error) bool {
	var unknownAuthority x509.UnknownAuthorityError
	var certificateInvalid x509.CertificateInvalidError
	var hostnameError x509.HostnameError
	var recordHeader tls.RecordHeaderError
	if errors.As(err, &unknownAuthority) ||
		errors.As(err, &certificateInvalid) ||
		errors.As(err, &hostnameError) ||
		errors.As(err, &recordHeader) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "tls:") ||
		strings.Contains(text, "x509:") ||
		strings.Contains(text, "server gave http response to https client")
}

func isDoctorCommand(args []string) bool {
	return len(args) > 0 && args[0] == "doctor"
}

func doctorExit(err error) (*doctorCommandError, bool) {
	var exitErr *doctorCommandError
	if !errors.As(err, &exitErr) {
		return nil, false
	}
	return exitErr, true
}

func safeDoctorError(err error) string {
	if exitErr, ok := doctorExit(err); ok && exitErr.message != "" {
		return exitErr.message
	}
	return fmt.Sprint(err)
}
