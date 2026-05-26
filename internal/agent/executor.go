package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"planetary-mesh/internal/protocol"
)

const streamLimit = 1 << 20

type executor struct {
	cfg      ExecutorConfig
	tracker  *LoadTracker
	reporter *ResultReporter
}

// HealthHandler is a basic health check.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// ExecuteHandler implements POST /execute on the agent using the default config.
func ExecuteHandler(w http.ResponseWriter, r *http.Request) {
	NewExecuteHandler(DefaultExecutorConfig())(w, r)
}

func NewExecuteHandler(cfg ExecutorConfig) http.HandlerFunc {
	return NewExecuteHandlerWithLoadTracker(cfg, nil)
}

func NewExecuteHandlerWithLoadTracker(cfg ExecutorConfig, tracker *LoadTracker) http.HandlerFunc {
	return NewExecuteHandlerWithLoadTrackerAndReporter(cfg, tracker, nil)
}

func NewExecuteHandlerWithLoadTrackerAndReporter(cfg ExecutorConfig, tracker *LoadTracker, reporter *ResultReporter) http.HandlerFunc {
	ex := &executor{cfg: cfg, tracker: tracker, reporter: reporter}
	return ex.handleExecute
}

func (e *executor) handleExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !protocol.HasExpectedVersion(r.Header) {
		http.Error(w, "protocol version mismatch", http.StatusConflict)
		return
	}

	var req protocol.ExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.JobID == "" {
		http.Error(w, "job_id is required", http.StatusBadRequest)
		return
	}

	if req.Type != "command" {
		slog.Info("legacy execute stub", "job_id", req.JobID, "type", req.Type)
		e.writeJSON(w, http.StatusOK, protocol.ExecuteResponse{Status: "ok"})
		return
	}
	if req.Command == "" {
		resp := protocol.ExecuteResponse{
			Status:    "error",
			LastError: "command is required for type=command",
		}
		e.recordResult(req.JobID, protocol.JobResultStatusFailed, resp)
		e.writeJSON(w, http.StatusBadRequest, resp)
		return
	}

	target, ok := e.cfg.Allowlist[req.Command]
	if !ok {
		resp := protocol.ExecuteResponse{
			Status:    "error",
			LastError: fmt.Sprintf("command %q is not allowlisted", req.Command),
		}
		e.recordResult(req.JobID, protocol.JobResultStatusFailed, resp)
		e.writeJSON(w, http.StatusBadRequest, resp)
		return
	}

	done := e.tracker.BeginExecution()
	defer done()

	slog.Info("execute start", "job_id", req.JobID, "type", req.Type, "command", req.Command, "args", req.Args)

	ctx, cancel := context.WithTimeout(r.Context(), e.cfg.Timeout)
	defer cancel()

	stdoutBuf := &limitedBuffer{limit: streamLimit}
	stderrBuf := &limitedBuffer{limit: streamLimit}

	err := runAllowlistedTarget(ctx, target, req.Args, stdoutBuf, stderrBuf)
	resp := protocol.ExecuteResponse{
		Status:          "ok",
		Stdout:          stdoutBuf.String(),
		Stderr:          stderrBuf.String(),
		StdoutTruncated: stdoutBuf.truncated,
		StderrTruncated: stderrBuf.truncated,
	}

	switch {
	case err == nil:
		slog.Info("execute done", "job_id", req.JobID, "command", req.Command)
		e.recordResult(req.JobID, protocol.JobResultStatusCompleted, resp)
		e.writeJSON(w, http.StatusOK, resp)
		return
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		resp.Status = "error"
		resp.LastError = fmt.Sprintf("command timed out after %s", e.cfg.Timeout)
		slog.Warn("execute timeout", "job_id", req.JobID, "command", req.Command, "timeout", e.cfg.Timeout)
		e.recordResult(req.JobID, protocol.JobResultStatusFailed, resp)
		e.writeJSON(w, http.StatusGatewayTimeout, resp)
		return
	case ctx.Err() != nil:
		resp.Status = "error"
		resp.LastError = ctx.Err().Error()
		slog.Warn("execute context canceled", "job_id", req.JobID, "command", req.Command, "err", ctx.Err())
		e.writeJSON(w, http.StatusInternalServerError, resp)
		return
	default:
		if code, ok := commandExitCode(err); ok {
			resp.Status = "error"
			resp.ExitCode = &code
			resp.LastError = fmt.Sprintf("command exited with code %d", code)
			slog.Warn("execute failed", "job_id", req.JobID, "command", req.Command, "exit_code", code)
			e.recordResult(req.JobID, protocol.JobResultStatusFailed, resp)
			e.writeJSON(w, http.StatusUnprocessableEntity, resp)
			return
		}

		resp.Status = "error"
		resp.LastError = err.Error()
		slog.Error("execute internal error", "job_id", req.JobID, "command", req.Command, "err", err)
		e.writeJSON(w, http.StatusInternalServerError, resp)
	}
}

// Mux returns an http.ServeMux with all agent routes wired up.
func Mux() *http.ServeMux {
	return MuxWithConfig(DefaultExecutorConfig())
}

func MuxWithConfig(cfg ExecutorConfig) *http.ServeMux {
	return MuxWithConfigAndLoadTracker(cfg, nil)
}

func MuxWithConfigAndLoadTracker(cfg ExecutorConfig, tracker *LoadTracker) *http.ServeMux {
	return MuxWithConfigLoadTrackerAndReporter(cfg, tracker, nil)
}

func MuxWithConfigLoadTrackerAndReporter(cfg ExecutorConfig, tracker *LoadTracker, reporter *ResultReporter) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", HealthHandler)
	mux.HandleFunc("/execute", NewExecuteHandlerWithLoadTrackerAndReporter(cfg, tracker, reporter))
	return mux
}

func (e *executor) writeJSON(w http.ResponseWriter, status int, resp protocol.ExecuteResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Warn("encode /execute response failed", "err", err)
	}
}

func (e *executor) recordResult(jobID, status string, resp protocol.ExecuteResponse) {
	if e.reporter == nil {
		return
	}
	e.reporter.Record(ResultReport{
		JobID:           jobID,
		Status:          status,
		ExitCode:        resp.ExitCode,
		Stdout:          resp.Stdout,
		Stderr:          resp.Stderr,
		StdoutTruncated: resp.StdoutTruncated,
		StderrTruncated: resp.StderrTruncated,
		LastError:       resp.LastError,
	})
}

type limitedBuffer struct {
	mu        sync.Mutex
	buf       []byte
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.buf) < b.limit {
		remaining := b.limit - len(b.buf)
		if len(p) > remaining {
			b.truncated = true
		} else {
			remaining = len(p)
		}
		b.buf = append(b.buf, p[:remaining]...)
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
