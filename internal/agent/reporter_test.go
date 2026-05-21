package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"planetary-mesh/internal/protocol"
)

func TestResultReporterSendsAndDropsAcceptedReport(t *testing.T) {
	var gotPath string
	var gotHeader string
	var gotPayload protocol.JobResultReportRequest
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get(protocol.HeaderName)
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return textResponse(http.StatusOK, `{}`), nil
	})}
	reporter := NewResultReporterWithConfig(client, "http://coordinator.test", "node-1", 4, time.Minute)

	exitCode := 0
	reporter.Record(ResultReport{
		JobID:    "job-1",
		Status:   protocol.JobResultStatusCompleted,
		ExitCode: &exitCode,
		Stdout:   "hello\n",
	})
	reporter.FlushOnce(context.Background(), time.Now())

	if gotPath != "/jobs/job-1/result" {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if gotHeader != protocol.Version {
		t.Fatalf("expected protocol header %q, got %q", protocol.Version, gotHeader)
	}
	if gotPayload.NodeID != "node-1" || gotPayload.Status != protocol.JobResultStatusCompleted || gotPayload.Stdout != "hello\n" {
		t.Fatalf("unexpected payload: %+v", gotPayload)
	}
	if gotPayload.ExitCode == nil || *gotPayload.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %#v", gotPayload.ExitCode)
	}
	if got := reporter.CachedCount(); got != 0 {
		t.Fatalf("expected accepted report to be dropped, got %d cached", got)
	}
}

func TestResultReporterRetriesNetworkAnd5xx(t *testing.T) {
	networkReporter := NewResultReporterWithConfig(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})}, "http://coordinator.test", "node-1", 4, time.Minute)
	networkReporter.Record(ResultReport{JobID: "job-network", Status: protocol.JobResultStatusCompleted})
	networkReporter.FlushOnce(context.Background(), time.Now())
	if got := networkReporter.CachedCount(); got != 1 {
		t.Fatalf("expected network failure to remain cached, got %d", got)
	}

	serverReporter := NewResultReporterWithConfig(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return textResponse(http.StatusInternalServerError, `server error`), nil
	})}, "http://coordinator.test", "node-1", 4, time.Minute)
	serverReporter.Record(ResultReport{JobID: "job-500", Status: protocol.JobResultStatusCompleted})
	serverReporter.FlushOnce(context.Background(), time.Now())
	if got := serverReporter.CachedCount(); got != 1 {
		t.Fatalf("expected 5xx response to remain cached, got %d", got)
	}
}

func TestResultReporterDropsPermanentResponses(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusConflict} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			reporter := NewResultReporterWithConfig(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return textResponse(status, `permanent`), nil
			})}, "http://coordinator.test", "node-1", 4, time.Minute)
			reporter.Record(ResultReport{JobID: "job-drop", Status: protocol.JobResultStatusCompleted})
			reporter.FlushOnce(context.Background(), time.Now())
			if got := reporter.CachedCount(); got != 0 {
				t.Fatalf("expected permanent response %d to drop report, got %d cached", status, got)
			}
		})
	}
}

func TestResultReporterCacheBoundsAndTTL(t *testing.T) {
	reporter := NewResultReporterWithConfig(http.DefaultClient, "http://coordinator.test", "node-1", 2, time.Minute)
	reporter.Record(ResultReport{JobID: "job-1", Status: protocol.JobResultStatusCompleted})
	reporter.Record(ResultReport{JobID: "job-2", Status: protocol.JobResultStatusCompleted})
	reporter.Record(ResultReport{JobID: "job-3", Status: protocol.JobResultStatusCompleted})
	if got := reporter.CachedCount(); got != 2 {
		t.Fatalf("expected bounded cache count 2, got %d", got)
	}
	reporter.mu.Lock()
	if _, ok := reporter.entries["job-1"]; ok {
		t.Fatalf("expected oldest entry to be evicted")
	}
	reporter.entries["job-2"] = cachedResultReport{
		report:    reporter.entries["job-2"].report,
		createdAt: time.Now().Add(-2 * time.Minute),
	}
	reporter.mu.Unlock()

	var sent []string
	reporter.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		sent = append(sent, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/jobs/"), "/result"))
		return textResponse(http.StatusOK, `{}`), nil
	})}
	reporter.ttl = time.Minute
	reporter.FlushOnce(context.Background(), time.Now())

	if len(sent) != 1 || sent[0] != "job-3" {
		t.Fatalf("expected only unexpired job-3 to be sent, got %v", sent)
	}
	if got := reporter.CachedCount(); got != 0 {
		t.Fatalf("expected cache to be empty after accepted unexpired report, got %d", got)
	}
}

func TestResultReporterRestartLosesCache(t *testing.T) {
	first := NewResultReporterWithConfig(http.DefaultClient, "http://coordinator.test", "node-1", 4, time.Minute)
	first.Record(ResultReport{JobID: "job-1", Status: protocol.JobResultStatusCompleted})
	if got := first.CachedCount(); got != 1 {
		t.Fatalf("expected first reporter to cache result, got %d", got)
	}

	restarted := NewResultReporterWithConfig(http.DefaultClient, "http://coordinator.test", "node-1", 4, time.Minute)
	if got := restarted.CachedCount(); got != 0 {
		t.Fatalf("expected restarted reporter to have empty cache, got %d", got)
	}
}
