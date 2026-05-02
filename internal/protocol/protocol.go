package protocol

import "net/http"

const (
	HeaderName = "X-Planetary-Protocol-Version"
	Version    = "1"
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

type CoordinatorStatusResponse struct {
	Status               string         `json:"status"`
	ProtocolVersion      string         `json:"protocol_version"`
	StorageBackend       string         `json:"storage_backend"`
	SecureMode           bool           `json:"secure_mode"`
	NodeAllowlistEnabled bool           `json:"node_allowlist_enabled"`
	Dispatch             DispatchStatus `json:"dispatch"`
}

type DispatchStatus struct {
	Timeout     string `json:"timeout"`
	MaxAttempts int    `json:"max_attempts"`
	BaseBackoff string `json:"base_backoff"`
}
