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
