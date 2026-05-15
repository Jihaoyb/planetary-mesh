package coordinator

import (
	"fmt"
	"io"
	"sync/atomic"

	"planetary-mesh/internal/protocol"
)

// Metrics holds simple counters for the coordinator.
// All counters are safe for concurrent use via sync/atomic.
type Metrics struct {
	JobsCreated          atomic.Uint64
	JobsCompleted        atomic.Uint64
	JobsFailed           atomic.Uint64
	StartupRecoveredJobs atomic.Uint64
	DispatchAttempts     atomic.Uint64
	DispatchErrors       atomic.Uint64
}

// NewMetrics constructs a zeroed Metrics.
func NewMetrics() *Metrics {
	return &Metrics{}
}

// WriteProm writes metrics in a minimal Prometheus text exposition format.
// It also includes node-state gauges through the store aggregate contract.
func (m *Metrics) WriteProm(w io.Writer, registry NodeStore, schema *protocol.SchemaStatus) {
	fmt.Fprintf(w, "# HELP planetary_jobs_created_total Total jobs created.\n")
	fmt.Fprintf(w, "# TYPE planetary_jobs_created_total counter\n")
	fmt.Fprintf(w, "planetary_jobs_created_total %d\n", m.JobsCreated.Load())

	fmt.Fprintf(w, "# HELP planetary_jobs_completed_total Total jobs completed successfully.\n")
	fmt.Fprintf(w, "# TYPE planetary_jobs_completed_total counter\n")
	fmt.Fprintf(w, "planetary_jobs_completed_total %d\n", m.JobsCompleted.Load())

	fmt.Fprintf(w, "# HELP planetary_jobs_failed_total Total jobs that ended in FAILED.\n")
	fmt.Fprintf(w, "# TYPE planetary_jobs_failed_total counter\n")
	fmt.Fprintf(w, "planetary_jobs_failed_total %d\n", m.JobsFailed.Load())

	fmt.Fprintf(w, "# HELP planetary_jobs_recovered_on_startup_total Total persisted RUNNING jobs marked FAILED during coordinator startup recovery.\n")
	fmt.Fprintf(w, "# TYPE planetary_jobs_recovered_on_startup_total counter\n")
	fmt.Fprintf(w, "planetary_jobs_recovered_on_startup_total %d\n", m.StartupRecoveredJobs.Load())

	fmt.Fprintf(w, "# HELP planetary_dispatch_attempts_total Total dispatch attempts (including retries).\n")
	fmt.Fprintf(w, "# TYPE planetary_dispatch_attempts_total counter\n")
	fmt.Fprintf(w, "planetary_dispatch_attempts_total %d\n", m.DispatchAttempts.Load())

	fmt.Fprintf(w, "# HELP planetary_dispatch_errors_total Total dispatch attempts that returned an error.\n")
	fmt.Fprintf(w, "# TYPE planetary_dispatch_errors_total counter\n")
	fmt.Fprintf(w, "planetary_dispatch_errors_total %d\n", m.DispatchErrors.Load())

	if registry != nil {
		counts, _ := registry.CountByState()
		fmt.Fprintf(w, "# HELP planetary_nodes Number of nodes by state.\n")
		fmt.Fprintf(w, "# TYPE planetary_nodes gauge\n")
		fmt.Fprintf(w, "planetary_nodes{state=\"HEALTHY\"} %d\n", counts.Healthy)
		fmt.Fprintf(w, "planetary_nodes{state=\"SUSPECT\"} %d\n", counts.Suspect)
		fmt.Fprintf(w, "planetary_nodes{state=\"OFFLINE\"} %d\n", counts.Offline)
	}
	if schema != nil {
		ready := 0
		if schema.Ready {
			ready = 1
		}
		fmt.Fprintf(w, "# HELP planetary_postgres_schema_ready Whether the Postgres schema is ready for this coordinator version.\n")
		fmt.Fprintf(w, "# TYPE planetary_postgres_schema_ready gauge\n")
		fmt.Fprintf(w, "planetary_postgres_schema_ready %d\n", ready)
		fmt.Fprintf(w, "# HELP planetary_postgres_schema_version Current Postgres schema version recorded in schema_version.\n")
		fmt.Fprintf(w, "# TYPE planetary_postgres_schema_version gauge\n")
		fmt.Fprintf(w, "planetary_postgres_schema_version %d\n", schema.Version)
		fmt.Fprintf(w, "# HELP planetary_postgres_schema_expected_version Postgres schema version expected by this coordinator binary.\n")
		fmt.Fprintf(w, "# TYPE planetary_postgres_schema_expected_version gauge\n")
		fmt.Fprintf(w, "planetary_postgres_schema_expected_version %d\n", schema.ExpectedVersion)
	}
}
