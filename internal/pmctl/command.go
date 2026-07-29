package pmctl

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"planetary-mesh/internal/protocol"
	"planetary-mesh/internal/workflowtemplate"
)

type clientAPI interface {
	Status(context.Context) (protocol.CoordinatorStatusResponse, error)
	ListNodes(context.Context) ([]Node, error)
	ListJobs(context.Context) ([]Job, error)
	GetJob(context.Context, string) (Job, error)
	CreateCommandJob(context.Context, string, []string, []string) (Job, error)
}

const (
	submitCommandUsage   = "usage: pmctl submit command [--require-capability <label> ...] [--] <command> [args...]"
	submitTemplateUsage  = "usage: pmctl submit template <template-file> [--require-capability <label> ...] --set name=value [--set name=value...]"
	previewTemplateUsage = "usage: pmctl templates preview <template-file> [--require-capability <label> ...] --set name=value [--set name=value...]"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if err := RunE(ctx, args, stdout); err != nil {
		if exitErr, ok := doctorExit(err); ok {
			if !exitErr.reported {
				fmt.Fprintln(stderr, "pmctl:", safeDoctorError(err))
			}
			return exitErr.code
		}
		fmt.Fprintln(stderr, "pmctl:", err)
		return 1
	}
	return 0
}

func RunE(ctx context.Context, args []string, stdout io.Writer) error {
	localCfg := ConfigFromEnv()
	jsonOut, remaining, err := parseGlobalFlags(args, &localCfg)
	if err != nil {
		if doctorFollowsGlobalFlags(args) {
			return &doctorCommandError{
				code:    doctorExitUsage,
				message: "usage: pmctl [global flags] doctor [--strict] [--timeout <duration>]",
			}
		}
		return err
	}
	if len(remaining) == 0 {
		return usageError("missing command")
	}
	if isDoctorCommand(remaining) {
		return runDoctorFromSources(ctx, args, remaining[1:], stdout, jsonOut)
	}
	if isLocalTemplateCommand(remaining) {
		return runCommandWithClient(ctx, nil, remaining, stdout, jsonOut)
	}

	cfg, err := ConfigFromSources(args)
	if err != nil {
		return err
	}
	jsonOut, remaining, err = parseGlobalFlags(args, &cfg)
	if err != nil {
		return err
	}
	if len(remaining) == 0 {
		return usageError("missing command")
	}

	client, err := NewClient(cfg)
	if err != nil {
		return err
	}
	return runCommand(ctx, client, remaining, stdout, jsonOut)
}

func isLocalTemplateCommand(args []string) bool {
	return len(args) > 0 && args[0] == "templates"
}

func doctorFollowsGlobalFlags(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			return i+1 < len(args) && args[i+1] == "doctor"
		case arg == "doctor":
			return true
		case !strings.HasPrefix(arg, "-") || arg == "-":
			return false
		case globalFlagNeedsValue(arg):
			if !strings.Contains(arg, "=") {
				i++
			}
		}
	}
	return false
}

func globalFlagNeedsValue(arg string) bool {
	name := strings.SplitN(arg, "=", 2)[0]
	switch name {
	case "-config", "--config",
		"-coordinator-url", "--coordinator-url",
		"-ca-file", "--ca-file",
		"-cert-file", "--cert-file",
		"-key-file", "--key-file":
		return true
	default:
		return false
	}
}

func parseGlobalFlags(args []string, cfg *Config) (bool, []string, error) {
	fs := flag.NewFlagSet("pmctl", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var jsonOut bool
	fs.StringVar(&cfg.ConfigFile, "config", cfg.ConfigFile, "path to env-style config file")
	fs.StringVar(&cfg.CoordinatorURL, "coordinator-url", cfg.CoordinatorURL, "coordinator base URL")
	fs.StringVar(&cfg.TLSFiles.CAFile, "ca-file", cfg.TLSFiles.CAFile, "TLS CA file")
	fs.StringVar(&cfg.TLSFiles.CertFile, "cert-file", cfg.TLSFiles.CertFile, "TLS client certificate file")
	fs.StringVar(&cfg.TLSFiles.KeyFile, "key-file", cfg.TLSFiles.KeyFile, "TLS client key file")
	fs.BoolVar(&jsonOut, "json", false, "write JSON output")
	if err := fs.Parse(args); err != nil {
		return false, nil, err
	}
	return jsonOut, fs.Args(), nil
}

func runCommand(ctx context.Context, client *Client, args []string, stdout io.Writer, jsonOut bool) error {
	return runCommandWithClient(ctx, clientAdapter{client}, args, stdout, jsonOut)
}

func runCommandWithClient(ctx context.Context, client clientAPI, args []string, stdout io.Writer, jsonOut bool) error {
	switch args[0] {
	case "status":
		if len(args) != 1 {
			return usageError("usage: pmctl status")
		}
		status, err := client.Status(ctx)
		if err != nil {
			return err
		}
		return writeValue(stdout, status, jsonOut, writeStatus)
	case "nodes":
		if len(args) != 2 || args[1] != "list" {
			return usageError("usage: pmctl nodes list")
		}
		nodes, err := client.ListNodes(ctx)
		if err != nil {
			return err
		}
		if err := normalizeNodes(nodes); err != nil {
			return err
		}
		return writeValue(stdout, nodes, jsonOut, writeNodes)
	case "jobs":
		if len(args) == 2 && args[1] == "list" {
			jobs, err := client.ListJobs(ctx)
			if err != nil {
				return err
			}
			if err := normalizeJobs(jobs); err != nil {
				return err
			}
			return writeValue(stdout, jobs, jsonOut, writeJobs)
		}
		if len(args) == 3 && args[1] == "inspect" {
			job, err := client.GetJob(ctx, args[2])
			if err != nil {
				return err
			}
			if err := normalizeJob(&job); err != nil {
				return err
			}
			return writeValue(stdout, job, jsonOut, writeJobDetail)
		}
		return usageError("usage: pmctl jobs list | pmctl jobs inspect <job-id>")
	case "templates":
		return runTemplateCommand(args[1:], stdout, jsonOut)
	case "submit":
		switch {
		case len(args) >= 2 && args[1] == "command":
			submission, err := parseCommandSubmission(args[2:])
			if err != nil {
				return err
			}
			job, err := client.CreateCommandJob(
				ctx,
				submission.command,
				submission.args,
				submission.requiredCapabilities,
			)
			if err != nil {
				return err
			}
			if err := normalizeJob(&job); err != nil {
				return err
			}
			return writeValue(stdout, job, jsonOut, writeJobDetail)
		case len(args) >= 2 && args[1] == "template":
			submission, err := parseTemplateSubmission(args[2:])
			if err != nil {
				return err
			}
			tmpl, err := loadTemplate(submission.path)
			if err != nil {
				return err
			}
			expanded, err := workflowtemplate.Expand(tmpl, submission.values)
			if err != nil {
				return fmt.Errorf("invalid template parameters for %s: %w", submission.path, err)
			}
			job, err := client.CreateCommandJob(
				ctx,
				expanded.Command,
				expanded.Args,
				submission.requiredCapabilities,
			)
			if err != nil {
				return err
			}
			if err := normalizeJob(&job); err != nil {
				return err
			}
			return writeValue(stdout, job, jsonOut, writeJobDetail)
		default:
			return usageError(submitCommandUsage + " | " + strings.TrimPrefix(submitTemplateUsage, "usage: "))
		}
	default:
		return usageError("unknown command " + args[0])
	}
}

func runTemplateCommand(args []string, stdout io.Writer, jsonOut bool) error {
	if len(args) == 0 {
		return usageError("usage: pmctl templates validate <template-file> | pmctl templates inspect <template-file> | pmctl templates preview <template-file> [--require-capability <label> ...] --set name=value [--set name=value...]")
	}

	switch args[0] {
	case "validate":
		if len(args) != 2 {
			return usageError("usage: pmctl templates validate <template-file>")
		}
		tmpl, err := loadTemplate(args[1])
		if err != nil {
			return err
		}
		return writeValue(stdout, templateValidationOutput{
			Valid:    true,
			Path:     args[1],
			Template: tmpl,
		}, jsonOut, writeTemplateValidation)
	case "inspect":
		if len(args) != 2 {
			return usageError("usage: pmctl templates inspect <template-file>")
		}
		tmpl, err := loadTemplate(args[1])
		if err != nil {
			return err
		}
		describedArgs, err := workflowtemplate.DescribeArgs(tmpl)
		if err != nil {
			return fmt.Errorf("invalid template %s: %w", args[1], err)
		}
		return writeValue(stdout, newTemplateInspectionOutput(args[1], tmpl, describedArgs), jsonOut, writeTemplateInspection)
	case "preview":
		preview, err := parseTemplatePreview(args[1:])
		if err != nil {
			return err
		}
		tmpl, err := loadTemplate(preview.path)
		if err != nil {
			return err
		}
		expanded, err := workflowtemplate.Expand(tmpl, preview.values)
		if err != nil {
			return fmt.Errorf("invalid template parameters for %s: %w", preview.path, err)
		}
		return writeValue(
			stdout,
			newTemplatePreviewOutput(preview.path, tmpl, expanded, preview.requiredCapabilities),
			jsonOut,
			writeTemplatePreview,
		)
	default:
		return usageError("usage: pmctl templates validate <template-file> | pmctl templates inspect <template-file> | pmctl templates preview <template-file> [--require-capability <label> ...] --set name=value [--set name=value...]")
	}
}

func writeValue[T any](w io.Writer, value T, jsonOut bool, writeHuman func(io.Writer, T) error) error {
	if jsonOut {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	}
	return writeHuman(w, value)
}

type templateValidationOutput struct {
	Valid    bool                      `json:"valid"`
	Path     string                    `json:"path"`
	Template workflowtemplate.Template `json:"template"`
}

type templateInspectionOutput struct {
	Valid       bool                              `json:"valid"`
	Path        string                            `json:"path"`
	Name        string                            `json:"name"`
	Description string                            `json:"description"`
	Version     int                               `json:"version"`
	Command     string                            `json:"command"`
	Parameters  []workflowtemplate.Parameter      `json:"parameters"`
	Args        []workflowtemplate.ArgDescription `json:"args"`
}

type templatePreviewOutput struct {
	Valid                bool               `json:"valid"`
	Path                 string             `json:"path"`
	Name                 string             `json:"name"`
	ExpandedJob          expandedCommandJob `json:"expanded_job"`
	CreatesJob           bool               `json:"creates_job"`
	ContactsCoordinator  bool               `json:"contacts_coordinator"`
	ChecksAgentAllowlist bool               `json:"checks_agent_allowlist"`
}

type expandedCommandJob struct {
	Type                 string   `json:"type"`
	Command              string   `json:"command"`
	Args                 []string `json:"args"`
	RequiredCapabilities []string `json:"required_capabilities"`
}

type templateParameters struct {
	path                 string
	values               map[string]string
	requiredCapabilities []string
}

type commandSubmission struct {
	command              string
	args                 []string
	requiredCapabilities []string
}

func parseTemplateSubmission(args []string) (templateParameters, error) {
	return parseTemplateParameters(args, submitTemplateUsage)
}

func parseTemplatePreview(args []string) (templateParameters, error) {
	return parseTemplateParameters(args, previewTemplateUsage)
}

func parseCommandSubmission(args []string) (commandSubmission, error) {
	var rawRequirements []string
	for i := 0; i < len(args); {
		arg := args[i]
		switch {
		case arg == "--":
			i++
			if i >= len(args) {
				return commandSubmission{}, usageError(submitCommandUsage)
			}
			return newCommandSubmission(args[i], args[i+1:], rawRequirements)
		case arg == "--require-capability":
			if i+1 >= len(args) {
				return commandSubmission{}, errors.New("--require-capability requires a value")
			}
			rawRequirements = append(rawRequirements, args[i+1])
			i += 2
		case strings.HasPrefix(arg, "--require-capability="):
			rawRequirements = append(rawRequirements, strings.TrimPrefix(arg, "--require-capability="))
			i++
		case strings.HasPrefix(arg, "-"):
			return commandSubmission{}, fmt.Errorf("unknown placement flag %q", arg)
		default:
			return newCommandSubmission(arg, args[i+1:], rawRequirements)
		}
	}
	return commandSubmission{}, usageError(submitCommandUsage)
}

func newCommandSubmission(command string, args, rawRequirements []string) (commandSubmission, error) {
	requirements, err := protocol.NormalizeRequiredCapabilities(rawRequirements)
	if err != nil {
		return commandSubmission{}, fmt.Errorf("invalid required capabilities: %w", err)
	}
	return commandSubmission{
		command:              command,
		args:                 append([]string(nil), args...),
		requiredCapabilities: requirements,
	}, nil
}

func parseTemplateParameters(args []string, usage string) (templateParameters, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return templateParameters{}, usageError(usage)
	}
	if strings.HasPrefix(args[0], "-") {
		return templateParameters{}, usageError(usage)
	}

	var rawSets []string
	var rawRequirements []string
	for i := 1; i < len(args); {
		arg := args[i]
		switch {
		case arg == "--set":
			if i+1 >= len(args) {
				return templateParameters{}, errors.New("--set requires a value")
			}
			rawSets = append(rawSets, args[i+1])
			i += 2
		case strings.HasPrefix(arg, "--set="):
			rawSets = append(rawSets, strings.TrimPrefix(arg, "--set="))
			i++
		case arg == "--require-capability":
			if i+1 >= len(args) {
				return templateParameters{}, errors.New("--require-capability requires a value")
			}
			rawRequirements = append(rawRequirements, args[i+1])
			i += 2
		case strings.HasPrefix(arg, "--require-capability="):
			rawRequirements = append(rawRequirements, strings.TrimPrefix(arg, "--require-capability="))
			i++
		case strings.HasPrefix(arg, "-"):
			return templateParameters{}, fmt.Errorf("unknown template flag %q", arg)
		default:
			return templateParameters{}, usageError(usage)
		}
	}

	values := make(map[string]string, len(rawSets))
	for _, entry := range rawSets {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			return templateParameters{}, fmt.Errorf("invalid --set %q: expected name=value", entry)
		}
		if _, exists := values[name]; exists {
			return templateParameters{}, fmt.Errorf("duplicate --set value for parameter %q", name)
		}
		values[name] = value
	}
	requirements, err := protocol.NormalizeRequiredCapabilities(rawRequirements)
	if err != nil {
		return templateParameters{}, fmt.Errorf("invalid required capabilities: %w", err)
	}

	return templateParameters{
		path:                 args[0],
		values:               values,
		requiredCapabilities: requirements,
	}, nil
}

func newTemplateInspectionOutput(path string, tmpl workflowtemplate.Template, args []workflowtemplate.ArgDescription) templateInspectionOutput {
	parameters := append([]workflowtemplate.Parameter(nil), tmpl.Parameters...)
	if parameters == nil {
		parameters = []workflowtemplate.Parameter{}
	}
	describedArgs := append([]workflowtemplate.ArgDescription(nil), args...)
	if describedArgs == nil {
		describedArgs = []workflowtemplate.ArgDescription{}
	}
	return templateInspectionOutput{
		Valid:       true,
		Path:        path,
		Name:        tmpl.Name,
		Description: tmpl.Description,
		Version:     tmpl.Version,
		Command:     tmpl.Command,
		Parameters:  parameters,
		Args:        describedArgs,
	}
}

func newTemplatePreviewOutput(
	path string,
	tmpl workflowtemplate.Template,
	expanded workflowtemplate.ExpandedCommand,
	requiredCapabilities []string,
) templatePreviewOutput {
	return templatePreviewOutput{
		Valid: true,
		Path:  path,
		Name:  tmpl.Name,
		ExpandedJob: expandedCommandJob{
			Type:                 "command",
			Command:              expanded.Command,
			Args:                 append([]string{}, expanded.Args...),
			RequiredCapabilities: append([]string{}, requiredCapabilities...),
		},
		CreatesJob:           false,
		ContactsCoordinator:  false,
		ChecksAgentAllowlist: false,
	}
}

func loadTemplate(path string) (workflowtemplate.Template, error) {
	tmpl, err := workflowtemplate.Load(path)
	if err != nil {
		return workflowtemplate.Template{}, fmt.Errorf("invalid template %s: %w", path, err)
	}
	return tmpl, nil
}

type usageError string

func (e usageError) Error() string {
	return string(e)
}

type clientAdapter struct {
	client *Client
}

func (a clientAdapter) Status(ctx context.Context) (protocol.CoordinatorStatusResponse, error) {
	return a.client.Status(ctx)
}

func (a clientAdapter) ListNodes(ctx context.Context) ([]Node, error) {
	return a.client.ListNodes(ctx)
}

func (a clientAdapter) ListJobs(ctx context.Context) ([]Job, error) {
	return a.client.ListJobs(ctx)
}

func (a clientAdapter) GetJob(ctx context.Context, id string) (Job, error) {
	return a.client.GetJob(ctx, id)
}

func (a clientAdapter) CreateCommandJob(ctx context.Context, command string, args, requiredCapabilities []string) (Job, error) {
	return a.client.CreateCommandJob(ctx, command, args, requiredCapabilities)
}

func isUsageError(err error) bool {
	var usage usageError
	return errors.As(err, &usage)
}

func joinCommand(command string, args []string) string {
	parts := append([]string{command}, args...)
	return strings.Join(parts, " ")
}
