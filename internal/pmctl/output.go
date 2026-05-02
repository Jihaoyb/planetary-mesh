package pmctl

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"planetary-mesh/internal/protocol"
)

func writeStatus(w io.Writer, s protocol.CoordinatorStatusResponse) error {
	tw := newTabWriter(w)
	fmt.Fprintln(tw, "STATUS\tPROTOCOL\tSTORAGE\tSECURE\tNODE_ALLOWLIST\tDISPATCH")
	fmt.Fprintf(tw, "%s\t%s\t%s\t%t\t%t\tattempts=%d timeout=%s backoff=%s\n",
		s.Status,
		s.ProtocolVersion,
		s.StorageBackend,
		s.SecureMode,
		s.NodeAllowlistEnabled,
		s.Dispatch.MaxAttempts,
		s.Dispatch.Timeout,
		s.Dispatch.BaseBackoff,
	)
	return tw.Flush()
}

func writeNodes(w io.Writer, nodes []Node) error {
	tw := newTabWriter(w)
	fmt.Fprintln(tw, "ID\tSTATE\tADDRESS\tLAST_SEEN\tCERTIFICATE")
	for _, node := range nodes {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			node.ID,
			node.State,
			node.Address,
			formatTime(node.LastSeen),
			shortFingerprint(node.Certificate.SHA256Fingerprint),
		)
	}
	return tw.Flush()
}

func writeJobs(w io.Writer, jobs []Job) error {
	tw := newTabWriter(w)
	fmt.Fprintln(tw, "ID\tSTATUS\tTYPE\tCOMMAND\tNODE\tATTEMPTS\tUPDATED")
	for _, job := range jobs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			job.ID,
			job.Status,
			job.Type,
			joinCommand(job.Command, job.Args),
			dash(job.NodeID),
			job.Attempts,
			formatTime(job.UpdatedAt),
		)
	}
	return tw.Flush()
}

func writeJobDetail(w io.Writer, job Job) error {
	tw := newTabWriter(w)
	fmt.Fprintf(tw, "ID\t%s\n", job.ID)
	fmt.Fprintf(tw, "Status\t%s\n", job.Status)
	fmt.Fprintf(tw, "Type\t%s\n", job.Type)
	fmt.Fprintf(tw, "Command\t%s\n", joinCommand(job.Command, job.Args))
	fmt.Fprintf(tw, "Node\t%s\n", dash(job.NodeID))
	fmt.Fprintf(tw, "Attempts\t%d\n", job.Attempts)
	if job.ExitCode != nil {
		fmt.Fprintf(tw, "Exit Code\t%d\n", *job.ExitCode)
	}
	if job.LastError != "" {
		fmt.Fprintf(tw, "Last Error\t%s\n", job.LastError)
	}
	fmt.Fprintf(tw, "Created\t%s\n", formatTime(job.CreatedAt))
	fmt.Fprintf(tw, "Updated\t%s\n", formatTime(job.UpdatedAt))
	if job.StartedAt != nil {
		fmt.Fprintf(tw, "Started\t%s\n", formatTime(*job.StartedAt))
	}
	if job.CompletedAt != nil {
		fmt.Fprintf(tw, "Completed\t%s\n", formatTime(*job.CompletedAt))
	}
	if job.Stdout != "" {
		fmt.Fprintf(tw, "Stdout\t%s\n", trimTrailingNewline(job.Stdout))
	}
	if job.Stderr != "" {
		fmt.Fprintf(tw, "Stderr\t%s\n", trimTrailingNewline(job.Stderr))
	}
	if job.StdoutTruncated || job.StderrTruncated {
		fmt.Fprintf(tw, "Truncated\tstdout=%t stderr=%t\n", job.StdoutTruncated, job.StderrTruncated)
	}
	return tw.Flush()
}

func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func shortFingerprint(fingerprint string) string {
	if fingerprint == "" {
		return "-"
	}
	if len(fingerprint) <= 12 {
		return fingerprint
	}
	return fingerprint[:12]
}

func trimTrailingNewline(s string) string {
	return strings.TrimRight(s, "\r\n")
}
