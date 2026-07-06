package pmctl

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"planetary-mesh/internal/protocol"
	"planetary-mesh/internal/workflowtemplate"
)

func writeStatus(w io.Writer, s protocol.CoordinatorStatusResponse) error {
	tw := newTabWriter(w)
	fmt.Fprintln(tw, "STATUS\tPROTOCOL\tSTORAGE\tSECURE\tNODE_ALLOWLIST\tDISPATCH\tRECONCILIATION")
	fmt.Fprintf(tw, "%s\t%s\t%s\t%t\t%t\tattempts=%d timeout=%s backoff=%s\t%s\n",
		s.Status,
		s.ProtocolVersion,
		s.StorageBackend,
		s.SecureMode,
		s.NodeAllowlistEnabled,
		s.Dispatch.MaxAttempts,
		s.Dispatch.Timeout,
		s.Dispatch.BaseBackoff,
		formatReconciliation(s.Reconciliation),
	)
	return tw.Flush()
}

func writeNodes(w io.Writer, nodes []Node) error {
	tw := newTabWriter(w)
	fmt.Fprintln(tw, "ID\tSTATE\tACTIVE\tCAPABILITIES\tADDRESS\tLAST_SEEN\tCERTIFICATE")
	for _, node := range nodes {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\t%s\n",
			node.ID,
			node.State,
			node.Load.ActiveExecutions,
			formatCapabilities(node.Capabilities),
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

func writeTemplateValidation(w io.Writer, out templateValidationOutput) error {
	tw := newTabWriter(w)
	fmt.Fprintf(tw, "Template:\t%s\n", out.Path)
	fmt.Fprintln(tw, "Status:\tvalid")
	fmt.Fprintf(tw, "Name:\t%s\n", out.Template.Name)
	fmt.Fprintf(tw, "Version:\t%d\n", out.Template.Version)
	fmt.Fprintf(tw, "Command:\t%s\n", out.Template.Command)
	fmt.Fprintf(tw, "Parameters:\t%s\n", formatTemplateParameters(out.Template.Parameters))
	fmt.Fprintf(tw, "Args:\t%d\n", len(out.Template.Args))
	return tw.Flush()
}

func writeTemplateInspection(w io.Writer, out templateInspectionOutput) error {
	tw := newTabWriter(w)
	fmt.Fprintf(tw, "Template:\t%s\n", out.Path)
	fmt.Fprintln(tw, "Status:\tvalid")
	fmt.Fprintf(tw, "Name:\t%s\n", out.Name)
	fmt.Fprintf(tw, "Description:\t%s\n", out.Description)
	fmt.Fprintf(tw, "Version:\t%d\n", out.Version)
	fmt.Fprintf(tw, "Command:\t%s\n", out.Command)
	fmt.Fprintln(tw)
	fmt.Fprintln(tw, "Parameters:")
	fmt.Fprintln(tw, "NAME\tREQUIRED\tDEFAULT\tDESCRIPTION")
	for _, param := range out.Parameters {
		fmt.Fprintf(tw, "%s\t%t\t%s\t%s\n",
			param.Name,
			param.Required,
			formatTemplateDefault(param.Default),
			param.Description,
		)
	}
	fmt.Fprintln(tw)
	fmt.Fprintln(tw, "Args:")
	fmt.Fprintln(tw, "INDEX\tTYPE\tVALUE")
	for _, arg := range out.Args {
		fmt.Fprintf(tw, "%d\t%s\t%s\n", arg.Index, arg.Type, formatTemplateArgDescription(arg))
	}
	return tw.Flush()
}

func writeTemplatePreview(w io.Writer, out templatePreviewOutput) error {
	tw := newTabWriter(w)
	fmt.Fprintf(tw, "Template:\t%s\n", out.Path)
	fmt.Fprintln(tw, "Status:\tpreview")
	fmt.Fprintf(tw, "Name:\t%s\n", out.Name)
	fmt.Fprintf(tw, "Expanded Job Type:\t%s\n", out.ExpandedJob.Type)
	fmt.Fprintf(tw, "Expanded Command:\t%s\n", out.ExpandedJob.Command)
	fmt.Fprintf(tw, "Creates Job:\t%t\n", out.CreatesJob)
	fmt.Fprintf(tw, "Contacts Coordinator:\t%t\n", out.ContactsCoordinator)
	fmt.Fprintf(tw, "Checks Agent Allowlist:\t%t\n", out.ChecksAgentAllowlist)
	fmt.Fprintln(tw)
	fmt.Fprintln(tw, "Args:")
	fmt.Fprintln(tw, "INDEX\tVALUE")
	for i, arg := range out.ExpandedJob.Args {
		fmt.Fprintf(tw, "%d\t%s\n", i+1, strconv.Quote(arg))
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

func formatCapabilities(capabilities []string) string {
	if len(capabilities) == 0 {
		return "-"
	}
	return strings.Join(capabilities, ",")
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

func formatReconciliation(reconciliation *protocol.ReconciliationStatus) string {
	if reconciliation == nil {
		return "-"
	}
	return fmt.Sprintf("grace=%s pending=%d", reconciliation.Grace, reconciliation.PendingRunningJobs)
}

func trimTrailingNewline(s string) string {
	return strings.TrimRight(s, "\r\n")
}

func formatTemplateParameters(parameters []workflowtemplate.Parameter) string {
	if len(parameters) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(parameters))
	for _, param := range parameters {
		if param.Required {
			parts = append(parts, param.Name+"(required)")
			continue
		}
		defaultValue := ""
		if param.Default != nil {
			defaultValue = *param.Default
		}
		parts = append(parts, fmt.Sprintf("%s(optional default=%q)", param.Name, defaultValue))
	}
	return strings.Join(parts, ",")
}

func formatTemplateDefault(defaultValue *string) string {
	if defaultValue == nil {
		return "-"
	}
	return strconv.Quote(*defaultValue)
}

func formatTemplateArgDescription(arg workflowtemplate.ArgDescription) string {
	if arg.Type == "literal" {
		return strconv.Quote(arg.Value)
	}
	return arg.Value
}
