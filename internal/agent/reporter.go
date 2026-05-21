package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"planetary-mesh/internal/protocol"
)

const (
	DefaultResultReportCacheEntries = 128
	DefaultResultReportTTL          = 5 * time.Minute
	defaultResultReportInterval     = 2 * time.Second
)

type ResultReport struct {
	JobID           string
	Status          string
	ExitCode        *int
	Stdout          string
	Stderr          string
	StdoutTruncated bool
	StderrTruncated bool
	LastError       string
}

type ResultReporter struct {
	client       *http.Client
	coordBaseURL string
	nodeID       string
	maxEntries   int
	ttl          time.Duration

	mu      sync.Mutex
	entries map[string]cachedResultReport
}

type cachedResultReport struct {
	report    ResultReport
	createdAt time.Time
}

func NewResultReporter(client *http.Client, coordBaseURL, nodeID string) *ResultReporter {
	return NewResultReporterWithConfig(client, coordBaseURL, nodeID, DefaultResultReportCacheEntries, DefaultResultReportTTL)
}

func NewResultReporterWithConfig(client *http.Client, coordBaseURL, nodeID string, maxEntries int, ttl time.Duration) *ResultReporter {
	if client == nil {
		client = http.DefaultClient
	}
	if maxEntries <= 0 {
		maxEntries = DefaultResultReportCacheEntries
	}
	if ttl <= 0 {
		ttl = DefaultResultReportTTL
	}
	return &ResultReporter{
		client:       client,
		coordBaseURL: strings.TrimRight(strings.TrimSpace(coordBaseURL), "/"),
		nodeID:       nodeID,
		maxEntries:   maxEntries,
		ttl:          ttl,
		entries:      make(map[string]cachedResultReport),
	}
}

func (r *ResultReporter) Start(stopCh <-chan struct{}) {
	if r == nil {
		return
	}
	ticker := time.NewTicker(defaultResultReportInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.FlushOnce(context.Background(), time.Now().UTC())
			case <-stopCh:
				return
			}
		}
	}()
}

func (r *ResultReporter) Record(report ResultReport) {
	if r == nil || strings.TrimSpace(report.JobID) == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UTC()
	r.dropExpiredLocked(now)
	if _, exists := r.entries[report.JobID]; !exists && len(r.entries) >= r.maxEntries {
		r.evictOldestLocked()
	}
	r.entries[report.JobID] = cachedResultReport{
		report:    report,
		createdAt: now,
	}
}

func (r *ResultReporter) FlushOnce(ctx context.Context, now time.Time) {
	if r == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	entries := r.snapshot(now.UTC())
	for _, entry := range entries {
		drop, err := r.send(ctx, entry.report)
		if err != nil {
			slog.Warn("report job result failed", "job_id", entry.report.JobID, "err", err)
			continue
		}
		if drop {
			r.drop(entry.report.JobID)
		}
	}
}

func (r *ResultReporter) snapshot(now time.Time) []cachedResultReport {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.dropExpiredLocked(now)
	entries := make([]cachedResultReport, 0, len(r.entries))
	for _, entry := range r.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].createdAt.Equal(entries[j].createdAt) {
			return entries[i].report.JobID < entries[j].report.JobID
		}
		return entries[i].createdAt.Before(entries[j].createdAt)
	})
	return entries
}

func (r *ResultReporter) drop(jobID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, jobID)
}

func (r *ResultReporter) dropExpiredLocked(now time.Time) {
	for jobID, entry := range r.entries {
		if !entry.createdAt.Add(r.ttl).After(now) {
			delete(r.entries, jobID)
		}
	}
}

func (r *ResultReporter) evictOldestLocked() {
	var oldestJobID string
	var oldest time.Time
	for jobID, entry := range r.entries {
		if oldestJobID == "" || entry.createdAt.Before(oldest) || (entry.createdAt.Equal(oldest) && jobID < oldestJobID) {
			oldestJobID = jobID
			oldest = entry.createdAt
		}
	}
	if oldestJobID != "" {
		delete(r.entries, oldestJobID)
	}
}

func (r *ResultReporter) send(ctx context.Context, report ResultReport) (bool, error) {
	if r.coordBaseURL == "" {
		return false, fmt.Errorf("coordinator URL is empty")
	}
	payload := protocol.JobResultReportRequest{
		NodeID:          r.nodeID,
		Status:          report.Status,
		ExitCode:        report.ExitCode,
		Stdout:          report.Stdout,
		Stderr:          report.Stderr,
		StdoutTruncated: report.StdoutTruncated,
		StderrTruncated: report.StderrTruncated,
		LastError:       report.LastError,
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return true, fmt.Errorf("encode result report: %w", err)
	}

	endpoint := r.coordBaseURL + "/jobs/" + url.PathEscape(report.JobID) + "/result"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return true, fmt.Errorf("build result report request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	protocol.SetVersionHeader(req.Header)

	resp, err := r.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}
	if resp.StatusCode >= 500 {
		return false, fmt.Errorf("coordinator returned %s", resp.Status)
	}
	return true, nil
}

func (r *ResultReporter) CachedCount() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}
