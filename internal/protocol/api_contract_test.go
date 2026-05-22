package protocol

import (
	"encoding/json"
	"testing"
)

func TestAPIContractExecuteRequestJSONFields(t *testing.T) {
	got := protocolMarshalMap(t, ExecuteRequest{
		JobID:   "job-1",
		Type:    "command",
		Payload: "legacy",
		Command: "echo",
		Args:    []string{"hello"},
	})

	protocolAssertJSONKeys(t, got, "job_id", "type", "payload", "command", "args")
}

func TestAPIContractExecuteResponseJSONFields(t *testing.T) {
	exitCode := 0
	got := protocolMarshalMap(t, ExecuteResponse{
		Status:          "ok",
		ExitCode:        &exitCode,
		Stdout:          "hello\n",
		Stderr:          "",
		StdoutTruncated: false,
		StderrTruncated: false,
		LastError:       "",
	})

	protocolAssertJSONKeys(t, got, "status", "exit_code", "stdout", "stderr", "stdout_truncated", "stderr_truncated", "last_error")
}

func TestAPIContractJobResultReportRequestJSONFields(t *testing.T) {
	exitCode := 0
	got := protocolMarshalMap(t, JobResultReportRequest{
		NodeID:          "agent-1",
		Status:          JobResultStatusCompleted,
		ExitCode:        &exitCode,
		Stdout:          "hello\n",
		Stderr:          "",
		StdoutTruncated: false,
		StderrTruncated: false,
		LastError:       "",
	})

	protocolAssertJSONKeys(t, got, "node_id", "status", "exit_code", "stdout", "stderr", "stdout_truncated", "stderr_truncated", "last_error")
}

func TestAPIContractCoordinatorStatusResponseJSONFields(t *testing.T) {
	got := protocolMarshalMap(t, CoordinatorStatusResponse{
		Status:               "ok",
		ProtocolVersion:      Version,
		StorageBackend:       "postgres",
		Schema:               &SchemaStatus{Ready: true, Version: 2, ExpectedVersion: 2},
		SecureMode:           true,
		NodeAllowlistEnabled: true,
		Dispatch:             DispatchStatus{Timeout: "10s", MaxAttempts: 3, BaseBackoff: "500ms"},
		Reconciliation:       &ReconciliationStatus{Grace: "30s", PendingRunningJobs: 1},
	})

	protocolAssertJSONKeys(t, got, "status", "protocol_version", "storage_backend", "schema", "secure_mode", "node_allowlist_enabled", "dispatch", "reconciliation")
	protocolAssertJSONKeys(t, got["schema"].(map[string]any), "ready", "version", "expected_version")
	protocolAssertJSONKeys(t, got["dispatch"].(map[string]any), "timeout", "max_attempts", "base_backoff")
	protocolAssertJSONKeys(t, got["reconciliation"].(map[string]any), "grace", "pending_running_jobs")
}

func TestAPIContractNodeLoadJSONFields(t *testing.T) {
	got := protocolMarshalMap(t, NodeLoad{ActiveExecutions: 2})

	protocolAssertJSONKeys(t, got, "active_executions")
}

func protocolMarshalMap(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal JSON map: %v", err)
	}
	return out
}

func protocolAssertJSONKeys(t *testing.T, got map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := got[key]; !ok {
			t.Fatalf("expected JSON key %q in %+v", key, got)
		}
	}
}
