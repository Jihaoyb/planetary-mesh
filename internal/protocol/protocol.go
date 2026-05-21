package protocol

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

const (
	HeaderName = "X-Planetary-Protocol-Version"
	Version    = "1"

	MaxNodeCapabilities     = 32
	MaxNodeCapabilityLength = 64
)

func SetVersionHeader(h http.Header) {
	h.Set(HeaderName, Version)
}

func HasExpectedVersion(h http.Header) bool {
	return h.Get(HeaderName) == Version
}

type ExecuteRequest struct {
	JobID   string   `json:"job_id"`
	Type    string   `json:"type"`
	Payload string   `json:"payload,omitempty"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

type ExecuteResponse struct {
	Status          string `json:"status"`
	ExitCode        *int   `json:"exit_code,omitempty"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	LastError       string `json:"last_error"`
}

type JobResultReportRequest struct {
	NodeID          string `json:"node_id"`
	Status          string `json:"status"`
	ExitCode        *int   `json:"exit_code,omitempty"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	LastError       string `json:"last_error"`
}

type NodeLoad struct {
	ActiveExecutions int `json:"active_executions"`
}

func NormalizeNodeCapabilities(in []string) ([]string, error) {
	if len(in) == 0 {
		return []string{}, nil
	}

	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		label := strings.TrimSpace(raw)
		if label == "" {
			return nil, fmt.Errorf("node capability label cannot be empty")
		}
		if len(label) > MaxNodeCapabilityLength {
			return nil, fmt.Errorf("node capability %q exceeds %d characters", label, MaxNodeCapabilityLength)
		}
		if !isValidNodeCapability(label) {
			return nil, fmt.Errorf("invalid node capability %q", label)
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	if len(out) > MaxNodeCapabilities {
		return nil, fmt.Errorf("node capabilities exceed maximum of %d", MaxNodeCapabilities)
	}
	sort.Strings(out)
	return out, nil
}

func ValidateNodeLoad(load NodeLoad) error {
	if load.ActiveExecutions < 0 {
		return fmt.Errorf("active_executions cannot be negative")
	}
	return nil
}

func isValidNodeCapability(label string) bool {
	for i := 0; i < len(label); i++ {
		c := label[i]
		ok := (c >= 'A' && c <= 'Z') ||
			(c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') ||
			(i > 0 && (c == '.' || c == '_' || c == ':' || c == '-'))
		if !ok {
			return false
		}
	}
	return true
}

type CoordinatorStatusResponse struct {
	Status               string         `json:"status"`
	ProtocolVersion      string         `json:"protocol_version"`
	StorageBackend       string         `json:"storage_backend"`
	Schema               *SchemaStatus  `json:"schema,omitempty"`
	SecureMode           bool           `json:"secure_mode"`
	NodeAllowlistEnabled bool           `json:"node_allowlist_enabled"`
	Dispatch             DispatchStatus `json:"dispatch"`
}

type SchemaStatus struct {
	Ready           bool `json:"ready"`
	Version         int  `json:"version"`
	ExpectedVersion int  `json:"expected_version"`
}

type DispatchStatus struct {
	Timeout     string `json:"timeout"`
	MaxAttempts int    `json:"max_attempts"`
	BaseBackoff string `json:"base_backoff"`
}
