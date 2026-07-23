package pmctl

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"planetary-mesh/internal/protocol"
	"planetary-mesh/internal/security"
)

type fakeDoctorClient struct {
	status    protocol.CoordinatorStatusResponse
	statusErr error
	nodes     []Node
	nodesErr  error
}

func (f *fakeDoctorClient) Status(context.Context) (protocol.CoordinatorStatusResponse, error) {
	return f.status, f.statusErr
}

func (f *fakeDoctorClient) ListNodes(context.Context) ([]Node, error) {
	return f.nodes, f.nodesErr
}

func TestDoctorHealthyMeshPassesInStableOrder(t *testing.T) {
	report := newDoctorReport(doctorOptions{Timeout: DefaultTimeout})
	report.addCheck(validConfigurationCheck())
	client := &fakeDoctorClient{
		status: healthyDoctorStatus("in_memory"),
		nodes:  []Node{healthyDoctorNode("agent-private", "http://private-agent.local:8081")},
	}
	validated := validatedDoctorConfig{Config: Config{CoordinatorURL: DefaultCoordinatorURL}}

	result := runDoctorChecks(context.Background(), client, validated, &report)
	if result.interrupted {
		t.Fatal("healthy diagnostics unexpectedly interrupted")
	}
	report.aggregate()

	if report.OverallStatus != doctorStatusPass {
		t.Fatalf("expected PASS, got %s: %+v", report.OverallStatus, report.Checks)
	}
	if report.Facts.JobSubmissionReady == nil || !*report.Facts.JobSubmissionReady {
		t.Fatalf("expected job submission readiness, got %+v", report.Facts.JobSubmissionReady)
	}
	gotNames := make([]string, 0, len(report.Checks))
	for _, check := range report.Checks {
		gotNames = append(gotNames, check.Name)
	}
	wantNames := []string{
		"client_configuration",
		"coordinator_connectivity",
		"status_endpoint",
		"protocol_compatibility",
		"coordinator_health",
		"storage_readiness",
		"transport_security",
		"reconciliation",
		"node_readiness",
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("unexpected check order\nwant: %v\n got: %v", wantNames, gotNames)
	}
	if report.Facts.Nodes == nil || report.Facts.Nodes.Total != 1 || report.Facts.Nodes.Healthy != 1 {
		t.Fatalf("unexpected node facts: %+v", report.Facts.Nodes)
	}
}

func TestDoctorNodeReadinessWarnings(t *testing.T) {
	tests := []struct {
		name      string
		nodes     []Node
		wantCode  string
		wantReady bool
	}{
		{name: "no nodes", nodes: []Node{}, wantCode: "no_nodes"},
		{
			name: "no healthy nodes",
			nodes: []Node{
				doctorNodeWithState("agent-1", "SUSPECT"),
				doctorNodeWithState("agent-2", "OFFLINE"),
			},
			wantCode: "no_healthy_nodes",
		},
		{
			name: "degraded but runnable",
			nodes: []Node{
				doctorNodeWithState("agent-1", "HEALTHY"),
				doctorNodeWithState("agent-2", "OFFLINE"),
			},
			wantCode:  "nodes_degraded",
			wantReady: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := newDoctorReport(doctorOptions{Timeout: DefaultTimeout})
			report.addCheck(validConfigurationCheck())
			runDoctorChecks(context.Background(), &fakeDoctorClient{
				status: healthyDoctorStatus("in_memory"),
				nodes:  tc.nodes,
			}, validatedDoctorConfig{}, &report)
			report.aggregate()

			if report.OverallStatus != doctorStatusWarn {
				t.Fatalf("expected WARN, got %s", report.OverallStatus)
			}
			check := report.Checks[len(report.Checks)-1]
			if check.Code != tc.wantCode || check.Status != doctorStatusWarn {
				t.Fatalf("unexpected node check: %+v", check)
			}
			if report.Facts.JobSubmissionReady == nil || *report.Facts.JobSubmissionReady != tc.wantReady {
				t.Fatalf("unexpected readiness: %+v", report.Facts.JobSubmissionReady)
			}
		})
	}
}

func TestDoctorStorageSecurityAndReconciliationPolicies(t *testing.T) {
	tests := []struct {
		name           string
		mutate         func(*protocol.CoordinatorStatusResponse)
		checkName      string
		wantStatus     doctorStatus
		wantCode       string
		wantReady      bool
		wantSchemaFact bool
	}{
		{
			name: "postgres ready",
			mutate: func(status *protocol.CoordinatorStatusResponse) {
				status.StorageBackend = "postgres"
				status.Schema = &protocol.SchemaStatus{Ready: true, Version: 2, ExpectedVersion: 2}
				status.Reconciliation = &protocol.ReconciliationStatus{Grace: "30s"}
			},
			checkName:      "storage_readiness",
			wantStatus:     doctorStatusPass,
			wantCode:       "postgres_schema_ready",
			wantReady:      true,
			wantSchemaFact: true,
		},
		{
			name: "pending reconciliation warns",
			mutate: func(status *protocol.CoordinatorStatusResponse) {
				status.StorageBackend = "postgres"
				status.Schema = &protocol.SchemaStatus{Ready: true, Version: 2, ExpectedVersion: 2}
				status.Reconciliation = &protocol.ReconciliationStatus{Grace: "30s", PendingRunningJobs: 2}
			},
			checkName:      "reconciliation",
			wantStatus:     doctorStatusWarn,
			wantCode:       "reconciliation_pending",
			wantReady:      true,
			wantSchemaFact: true,
		},
		{
			name: "unknown ready schema warns",
			mutate: func(status *protocol.CoordinatorStatusResponse) {
				status.StorageBackend = "postgres"
				status.Schema = &protocol.SchemaStatus{Ready: true, Version: 3, ExpectedVersion: 3}
				status.Reconciliation = &protocol.ReconciliationStatus{Grace: "30s"}
			},
			checkName:      "storage_readiness",
			wantStatus:     doctorStatusWarn,
			wantCode:       "schema_version_unknown",
			wantReady:      true,
			wantSchemaFact: true,
		},
		{
			name: "schema mismatch fails",
			mutate: func(status *protocol.CoordinatorStatusResponse) {
				status.StorageBackend = "postgres"
				status.Schema = &protocol.SchemaStatus{Ready: true, Version: 1, ExpectedVersion: 2}
				status.Reconciliation = &protocol.ReconciliationStatus{Grace: "30s"}
			},
			checkName:      "storage_readiness",
			wantStatus:     doctorStatusFail,
			wantCode:       "schema_version_mismatch",
			wantSchemaFact: true,
		},
		{
			name: "security mismatch fails",
			mutate: func(status *protocol.CoordinatorStatusResponse) {
				status.SecureMode = true
				status.NodeAllowlistEnabled = false
			},
			checkName:  "transport_security",
			wantStatus: doctorStatusFail,
			wantCode:   "security_metadata_inconsistent",
		},
		{
			name: "plain mode is supported",
			mutate: func(*protocol.CoordinatorStatusResponse) {
			},
			checkName:  "transport_security",
			wantStatus: doctorStatusPass,
			wantCode:   "plain_mode_supported",
			wantReady:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status := healthyDoctorStatus("in_memory")
			tc.mutate(&status)
			report := newDoctorReport(doctorOptions{Timeout: DefaultTimeout})
			report.addCheck(validConfigurationCheck())
			runDoctorChecks(context.Background(), &fakeDoctorClient{
				status: status,
				nodes:  []Node{healthyDoctorNode("agent-1", "http://agent.local:8081")},
			}, validatedDoctorConfig{}, &report)
			report.aggregate()

			check, ok := findDoctorCheck(report, tc.checkName)
			if !ok {
				t.Fatalf("missing check %q: %+v", tc.checkName, report.Checks)
			}
			if check.Status != tc.wantStatus || check.Code != tc.wantCode {
				t.Fatalf("unexpected check: %+v", check)
			}
			if report.Facts.JobSubmissionReady == nil || *report.Facts.JobSubmissionReady != tc.wantReady {
				t.Fatalf("unexpected readiness: %+v", report.Facts.JobSubmissionReady)
			}
			if (report.Facts.Schema != nil) != tc.wantSchemaFact {
				t.Fatalf("unexpected schema facts: %+v", report.Facts.Schema)
			}
		})
	}
}

func TestDoctorStatusFailuresAreSanitizedAndDistinct(t *testing.T) {
	tests := []struct {
		name            string
		err             error
		wantCheck       string
		wantCode        string
		wantReachable   bool
		wantInterrupted bool
	}{
		{
			name:          "unauthorized",
			err:           &HTTPError{StatusCode: http.StatusUnauthorized, Status: "401 Unauthorized", Body: "secret-body"},
			wantCheck:     "status_endpoint",
			wantCode:      "status_unauthorized",
			wantReachable: true,
		},
		{
			name:          "forbidden",
			err:           &HTTPError{StatusCode: http.StatusForbidden, Status: "403 Forbidden"},
			wantCheck:     "status_endpoint",
			wantCode:      "status_forbidden",
			wantReachable: true,
		},
		{
			name:          "not found",
			err:           &HTTPError{StatusCode: http.StatusNotFound, Status: "404 Not Found"},
			wantCheck:     "status_endpoint",
			wantCode:      "status_not_found",
			wantReachable: true,
		},
		{
			name:          "redirect",
			err:           &HTTPError{StatusCode: http.StatusTemporaryRedirect, Status: "307 Temporary Redirect"},
			wantCheck:     "status_endpoint",
			wantCode:      "status_redirect_rejected",
			wantReachable: true,
		},
		{
			name:          "protocol conflict",
			err:           &HTTPError{StatusCode: http.StatusConflict, Status: "409 Conflict"},
			wantCheck:     "protocol_compatibility",
			wantCode:      "protocol_rejected",
			wantReachable: true,
		},
		{
			name:          "server error",
			err:           &HTTPError{StatusCode: http.StatusServiceUnavailable, Status: "503 Service Unavailable"},
			wantCheck:     "status_endpoint",
			wantCode:      "status_server_error",
			wantReachable: true,
		},
		{
			name:          "invalid JSON",
			err:           &DecodeError{Err: errors.New("secret-decode-details")},
			wantCheck:     "status_endpoint",
			wantCode:      "status_invalid_json",
			wantReachable: true,
		},
		{
			name:          "unreachable",
			err:           &RequestError{Err: errors.New("private-host-secret")},
			wantCheck:     "coordinator_connectivity",
			wantCode:      "coordinator_unreachable",
			wantReachable: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := newDoctorReport(doctorOptions{Timeout: DefaultTimeout})
			report.addCheck(validConfigurationCheck())
			result := runDoctorChecks(context.Background(), &fakeDoctorClient{statusErr: tc.err}, validatedDoctorConfig{}, &report)
			if result.interrupted != tc.wantInterrupted {
				t.Fatalf("unexpected interruption: %t", result.interrupted)
			}
			check := report.Checks[len(report.Checks)-1]
			if check.Name != tc.wantCheck || check.Code != tc.wantCode {
				t.Fatalf("unexpected check: %+v", check)
			}
			if report.Facts.CoordinatorReachable == nil || *report.Facts.CoordinatorReachable != tc.wantReachable {
				t.Fatalf("unexpected reachability: %+v", report.Facts.CoordinatorReachable)
			}
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatalf("marshal report: %v", err)
			}
			for _, secret := range []string{"secret-body", "secret-decode-details", "private-host-secret"} {
				if strings.Contains(string(data), secret) {
					t.Fatalf("report exposed %q: %s", secret, data)
				}
			}
		})
	}
}

func TestDoctorProtocolAndRuntimeFailures(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*protocol.CoordinatorStatusResponse)
		wantCheck string
		wantCode  string
	}{
		{
			name: "missing protocol",
			mutate: func(status *protocol.CoordinatorStatusResponse) {
				status.ProtocolVersion = ""
			},
			wantCheck: "protocol_compatibility",
			wantCode:  "protocol_missing",
		},
		{
			name: "mismatched protocol",
			mutate: func(status *protocol.CoordinatorStatusResponse) {
				status.ProtocolVersion = "2"
			},
			wantCheck: "protocol_compatibility",
			wantCode:  "protocol_mismatch",
		},
		{
			name: "coordinator not ok",
			mutate: func(status *protocol.CoordinatorStatusResponse) {
				status.Status = "degraded-private-value"
			},
			wantCheck: "coordinator_health",
			wantCode:  "coordinator_not_ok",
		},
		{
			name: "invalid dispatch",
			mutate: func(status *protocol.CoordinatorStatusResponse) {
				status.Dispatch.Timeout = "not-a-duration-private-value"
			},
			wantCheck: "coordinator_health",
			wantCode:  "runtime_metadata_invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status := healthyDoctorStatus("in_memory")
			tc.mutate(&status)
			report := newDoctorReport(doctorOptions{Timeout: DefaultTimeout})
			report.addCheck(validConfigurationCheck())
			runDoctorChecks(context.Background(), &fakeDoctorClient{status: status}, validatedDoctorConfig{}, &report)
			report.aggregate()

			check := report.Checks[len(report.Checks)-1]
			if check.Name != tc.wantCheck || check.Code != tc.wantCode || check.Status != doctorStatusFail {
				t.Fatalf("unexpected check: %+v", check)
			}
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatalf("marshal report: %v", err)
			}
			if strings.Contains(string(data), "private-value") {
				t.Fatalf("report exposed arbitrary status value: %s", data)
			}
		})
	}
}

func TestDoctorConfigurationValidation(t *testing.T) {
	validTLS := writeTestTLSFiles(t)
	invalidCA := filepath.Join(t.TempDir(), "invalid-ca.pem")
	if err := os.WriteFile(invalidCA, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("write invalid CA: %v", err)
	}
	invalidKey := filepath.Join(t.TempDir(), "invalid-key.pem")
	if err := os.WriteFile(invalidKey, []byte("not a private key"), 0o600); err != nil {
		t.Fatalf("write invalid key: %v", err)
	}
	tests := []struct {
		name     string
		cfg      Config
		wantCode string
	}{
		{
			name:     "default URL",
			cfg:      Config{},
			wantCode: "configuration_valid",
		},
		{
			name:     "valid HTTPS with TLS",
			cfg:      Config{CoordinatorURL: "https://coordinator.test", TLSFiles: validTLS},
			wantCode: "configuration_valid",
		},
		{
			name:     "credentials rejected",
			cfg:      Config{CoordinatorURL: "http://user:secret@coordinator.test"},
			wantCode: "coordinator_url_invalid",
		},
		{
			name:     "query rejected",
			cfg:      Config{CoordinatorURL: "http://coordinator.test?token=secret-query"},
			wantCode: "coordinator_url_invalid",
		},
		{
			name:     "fragment rejected",
			cfg:      Config{CoordinatorURL: "http://coordinator.test#secret-fragment"},
			wantCode: "coordinator_url_invalid",
		},
		{
			name:     "path rejected",
			cfg:      Config{CoordinatorURL: "http://coordinator.test/private-path"},
			wantCode: "coordinator_url_invalid",
		},
		{
			name: "partial TLS",
			cfg: Config{CoordinatorURL: "https://coordinator.test", TLSFiles: security.TLSFiles{
				CAFile: "secret-ca-path",
			}},
			wantCode: "tls_config_partial",
		},
		{
			name:     "TLS over HTTP",
			cfg:      Config{CoordinatorURL: "http://coordinator.test", TLSFiles: validTLS},
			wantCode: "tls_requires_https",
		},
		{
			name: "unreadable TLS",
			cfg: Config{CoordinatorURL: "https://coordinator.test", TLSFiles: security.TLSFiles{
				CAFile:   filepath.Join(t.TempDir(), "secret-missing-ca"),
				CertFile: validTLS.CertFile,
				KeyFile:  validTLS.KeyFile,
			}},
			wantCode: "tls_file_unreadable",
		},
		{
			name: "invalid CA",
			cfg: Config{CoordinatorURL: "https://coordinator.test", TLSFiles: security.TLSFiles{
				CAFile:   invalidCA,
				CertFile: validTLS.CertFile,
				KeyFile:  validTLS.KeyFile,
			}},
			wantCode: "tls_ca_invalid",
		},
		{
			name: "invalid key pair",
			cfg: Config{CoordinatorURL: "https://coordinator.test", TLSFiles: security.TLSFiles{
				CAFile:   validTLS.CAFile,
				CertFile: validTLS.CertFile,
				KeyFile:  invalidKey,
			}},
			wantCode: "tls_keypair_invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, check := validateDoctorConfig(tc.cfg)
			if check.Code != tc.wantCode {
				t.Fatalf("expected %q, got %+v", tc.wantCode, check)
			}
			data, err := json.Marshal(check)
			if err != nil {
				t.Fatalf("marshal check: %v", err)
			}
			for _, secret := range []string{
				"user:secret",
				"secret-query",
				"secret-fragment",
				"private-path",
				"secret-ca-path",
				"secret-missing-ca",
			} {
				if strings.Contains(string(data), secret) {
					t.Fatalf("check exposed %q: %s", secret, data)
				}
			}
		})
	}
}

func TestDoctorClassifiesLocalTLSHandshakeFailure(t *testing.T) {
	report := newDoctorReport(doctorOptions{Timeout: time.Second})
	report.addCheck(validConfigurationCheck())
	runDoctorChecks(context.Background(), &fakeDoctorClient{
		statusErr: &RequestError{Err: x509.UnknownAuthorityError{Cert: &x509.Certificate{}}},
	}, validatedDoctorConfig{}, &report)

	check := report.Checks[len(report.Checks)-1]
	if check.Code != "tls_handshake_failed" || check.Status != doctorStatusFail {
		t.Fatalf("unexpected TLS check: %+v", check)
	}
}

func TestDoctorCancellationAndTimeoutAreInterrupted(t *testing.T) {
	tests := []struct {
		name     string
		ctx      func() context.Context
		err      error
		wantCode string
	}{
		{
			name: "canceled",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			err:      context.Canceled,
			wantCode: "canceled",
		},
		{
			name: "deadline",
			ctx: func() context.Context {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				t.Cleanup(cancel)
				return ctx
			},
			err:      context.DeadlineExceeded,
			wantCode: "timeout",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := newDoctorReport(doctorOptions{Timeout: DefaultTimeout})
			report.addCheck(validConfigurationCheck())
			result := runDoctorChecks(tc.ctx(), &fakeDoctorClient{statusErr: &RequestError{Err: tc.err}}, validatedDoctorConfig{}, &report)
			if !result.interrupted {
				t.Fatal("expected interrupted result")
			}
			check := report.Checks[len(report.Checks)-1]
			if check.Code != tc.wantCode {
				t.Fatalf("expected %q, got %+v", tc.wantCode, check)
			}
		})
	}
}

func TestDoctorRejectsMalformedNodeMetadata(t *testing.T) {
	tests := []struct {
		name  string
		nodes []Node
	}{
		{name: "missing id", nodes: []Node{{Address: "http://agent", LastSeen: time.Now(), State: "HEALTHY"}}},
		{name: "missing address", nodes: []Node{{ID: "agent", LastSeen: time.Now(), State: "HEALTHY"}}},
		{name: "missing last seen", nodes: []Node{{ID: "agent", Address: "http://agent", State: "HEALTHY"}}},
		{name: "unknown state", nodes: []Node{{ID: "agent", Address: "http://agent", LastSeen: time.Now(), State: "UNKNOWN"}}},
		{
			name: "duplicate ids",
			nodes: []Node{
				healthyDoctorNode("agent", "http://agent-1"),
				healthyDoctorNode("agent", "http://agent-2"),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := newDoctorReport(doctorOptions{Timeout: DefaultTimeout})
			report.addCheck(validConfigurationCheck())
			runDoctorChecks(context.Background(), &fakeDoctorClient{
				status: healthyDoctorStatus("in_memory"),
				nodes:  tc.nodes,
			}, validatedDoctorConfig{}, &report)
			report.aggregate()

			check := report.Checks[len(report.Checks)-1]
			if check.Status != doctorStatusFail || check.Code != "nodes_invalid_metadata" {
				t.Fatalf("unexpected check: %+v", check)
			}
			if report.Facts.Nodes != nil || report.Facts.JobSubmissionReady != nil {
				t.Fatalf("malformed metadata should leave node/readiness facts unknown: %+v", report.Facts)
			}
		})
	}
}

func TestDoctorCommandExitCodesAndOutputStreams(t *testing.T) {
	clearPMCTLEnv(t)
	tests := []struct {
		name       string
		args       []string
		wantExit   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "usage",
			args:       []string{"doctor", "--timeout", "1ms"},
			wantExit:   doctorExitUsage,
			wantStderr: "pmctl: doctor --timeout must be between 100ms and 60s",
		},
		{
			name:       "global flag after doctor is usage",
			args:       []string{"doctor", "--json"},
			wantExit:   doctorExitUsage,
			wantStderr: "pmctl: usage: pmctl doctor",
		},
		{
			name:       "invalid URL diagnostic failure",
			args:       []string{"--json", "--coordinator-url", "not-a-url", "doctor"},
			wantExit:   doctorExitDiagnosticFailure,
			wantStdout: `"overall_status": "FAIL"`,
		},
		{
			name:       "strict invalid URL remains diagnostic failure",
			args:       []string{"--json", "--coordinator-url", "http://user:secret@coordinator.test", "doctor", "--strict"},
			wantExit:   doctorExitDiagnosticFailure,
			wantStdout: `"code": "coordinator_url_invalid"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exit := Run(context.Background(), tc.args, &stdout, &stderr)
			if exit != tc.wantExit {
				t.Fatalf("expected exit %d, got %d\nstdout:\n%s\nstderr:\n%s", tc.wantExit, exit, stdout.String(), stderr.String())
			}
			if tc.wantStdout != "" && !strings.Contains(stdout.String(), tc.wantStdout) {
				t.Fatalf("stdout missing %q:\n%s", tc.wantStdout, stdout.String())
			}
			if tc.wantStderr != "" && !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Fatalf("stderr missing %q:\n%s", tc.wantStderr, stderr.String())
			}
			if tc.wantExit == doctorExitDiagnosticFailure && stderr.Len() != 0 {
				t.Fatalf("diagnostic failure wrote stderr: %s", stderr.String())
			}
			if strings.Contains(stdout.String()+stderr.String(), "user:secret") {
				t.Fatalf("output exposed URL credentials:\n%s%s", stdout.String(), stderr.String())
			}
		})
	}
}

func TestDoctorMalformedConfigStillProducesValidJSON(t *testing.T) {
	clearPMCTLEnv(t)
	path := filepath.Join(t.TempDir(), "private-config.env")
	if err := os.WriteFile(path, []byte("BROKEN CONFIG secret-config-value\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exit := Run(context.Background(), []string{"--json", "--config", path, "doctor"}, &stdout, &stderr)
	if exit != doctorExitDiagnosticFailure {
		t.Fatalf("expected diagnostic failure, got %d: %s", exit, stderr.String())
	}
	var report doctorReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, stdout.String())
	}
	if report.OverallStatus != doctorStatusFail || len(report.Checks) != 1 || report.Checks[0].Code != "config_file_invalid" {
		t.Fatalf("unexpected report: %+v", report)
	}
	for _, secret := range []string{path, "secret-config-value"} {
		if strings.Contains(stdout.String()+stderr.String(), secret) {
			t.Fatalf("output exposed %q:\n%s%s", secret, stdout.String(), stderr.String())
		}
	}
}

func TestDoctorJSONUsesNullFactsAndEmptyArrays(t *testing.T) {
	report := newDoctorReport(doctorOptions{Timeout: DefaultTimeout})
	report.addCheck(doctorCheck{
		Name:        "client_configuration",
		Status:      doctorStatusFail,
		Code:        "test_failure",
		Summary:     "Safe failure.",
		Remediation: []string{},
	})
	report.aggregate()

	var out bytes.Buffer
	if err := writeValue(&out, report, true, writeDoctorReport); err != nil {
		t.Fatalf("write JSON: %v", err)
	}
	text := out.String()
	for _, want := range []string{
		`"coordinator_reachable": null`,
		`"job_submission_ready": null`,
		`"schema": null`,
		`"dispatch": null`,
		`"nodes": null`,
		`"remediation": []`,
		`"endpoints_used": [`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("JSON missing %q:\n%s", want, text)
		}
	}
}

func TestDoctorHumanOutputDoesNotExposeNodeDetails(t *testing.T) {
	report := newDoctorReport(doctorOptions{Timeout: DefaultTimeout})
	report.addCheck(validConfigurationCheck())
	runDoctorChecks(context.Background(), &fakeDoctorClient{
		status: healthyDoctorStatus("in_memory"),
		nodes:  []Node{healthyDoctorNode("private-node-secret", "http://10.20.30.40:8081")},
	}, validatedDoctorConfig{}, &report)
	report.aggregate()

	var out bytes.Buffer
	if err := writeDoctorReport(&out, report); err != nil {
		t.Fatalf("write human report: %v", err)
	}
	text := out.String()
	for _, want := range []string{
		"Planetary Mesh doctor",
		"Overall: PASS",
		"Ready for job submission: yes",
		"CHECK",
		"STATUS",
		"SUMMARY",
		"Remediation: none",
		"Limitations",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("human output missing %q:\n%s", want, text)
		}
	}
	for _, secret := range []string{"private-node-secret", "10.20.30.40"} {
		if strings.Contains(text, secret) {
			t.Fatalf("human output exposed %q:\n%s", secret, text)
		}
	}
}

func TestDoctorOutputFailureUsesExitFour(t *testing.T) {
	report := newDoctorReport(doctorOptions{Timeout: DefaultTimeout})
	report.addCheck(validConfigurationCheck())
	err := writeDoctorResult(failingWriter{}, report, false, doctorRunResult{})
	exitErr, ok := doctorExit(err)
	if !ok || exitErr.code != doctorExitInternal || exitErr.reported {
		t.Fatalf("unexpected output failure: %#v", err)
	}
	if exitErr.message != "write diagnostic output failed" {
		t.Fatalf("unexpected safe message: %q", exitErr.message)
	}
}

func TestDoctorInterruptedResultUsesExitThree(t *testing.T) {
	report := newDoctorReport(doctorOptions{Timeout: DefaultTimeout})
	report.addCheck(doctorCheck{
		Name:        "coordinator_connectivity",
		Status:      doctorStatusFail,
		Code:        "timeout",
		Summary:     "The diagnostic network timeout expired.",
		Remediation: []string{},
	})
	err := writeDoctorResult(io.Discard, report, true, doctorRunResult{interrupted: true})
	exitErr, ok := doctorExit(err)
	if !ok || exitErr.code != doctorExitInterrupted || !exitErr.reported {
		t.Fatalf("unexpected interrupted result: %#v", err)
	}
}

func TestDoctorStrictWarningExitFive(t *testing.T) {
	report := newDoctorReport(doctorOptions{Strict: true, Timeout: DefaultTimeout})
	report.addCheck(doctorCheck{
		Name:        "node_readiness",
		Status:      doctorStatusWarn,
		Code:        "no_nodes",
		Summary:     "No agents are registered.",
		Remediation: []string{},
	})
	var out bytes.Buffer
	err := writeDoctorResult(&out, report, true, doctorRunResult{})
	exitErr, ok := doctorExit(err)
	if !ok || exitErr.code != doctorExitStrictWarning || !exitErr.reported {
		t.Fatalf("unexpected strict warning result: %#v", err)
	}

	report.Strict = false
	if err := writeDoctorResult(io.Discard, report, true, doctorRunResult{}); err != nil {
		t.Fatalf("normal warning should exit successfully: %v", err)
	}
}

func TestParseDoctorFlags(t *testing.T) {
	opts, err := parseDoctorFlags([]string{"--strict", "--timeout", "250ms"})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if !opts.Strict || opts.Timeout != 250*time.Millisecond {
		t.Fatalf("unexpected options: %+v", opts)
	}
	for _, args := range [][]string{
		{"--timeout", "99ms"},
		{"--timeout", "61s"},
		{"--timeout", "invalid"},
		{"unexpected"},
	} {
		if _, err := parseDoctorFlags(args); err == nil {
			t.Fatalf("expected usage error for %q", args)
		}
	}
}

func validConfigurationCheck() doctorCheck {
	return doctorCheck{
		Name:        "client_configuration",
		Status:      doctorStatusPass,
		Code:        "configuration_valid",
		Summary:     "Local pmctl configuration is valid.",
		Remediation: []string{},
	}
}

func healthyDoctorStatus(storage string) protocol.CoordinatorStatusResponse {
	return protocol.CoordinatorStatusResponse{
		Status:          "ok",
		ProtocolVersion: protocol.Version,
		StorageBackend:  storage,
		Dispatch: protocol.DispatchStatus{
			Timeout:     "10s",
			MaxAttempts: 3,
			BaseBackoff: "500ms",
		},
	}
}

func healthyDoctorNode(id, address string) Node {
	return Node{
		ID:       id,
		Address:  address,
		LastSeen: time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC),
		State:    "HEALTHY",
	}
}

func doctorNodeWithState(id, state string) Node {
	node := healthyDoctorNode(id, "http://"+id+".local:8081")
	node.State = state
	return node
}

func findDoctorCheck(report doctorReport, name string) (doctorCheck, bool) {
	for _, check := range report.Checks {
		if check.Name == name {
			return check, true
		}
	}
	return doctorCheck{}, false
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("writer-secret")
}
