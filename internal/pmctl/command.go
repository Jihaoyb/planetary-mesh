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
	CreateCommandJob(context.Context, string, []string) (Job, error)
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if err := RunE(ctx, args, stdout); err != nil {
		fmt.Fprintln(stderr, "pmctl:", err)
		return 1
	}
	return 0
}

func RunE(ctx context.Context, args []string, stdout io.Writer) error {
	localCfg := ConfigFromEnv()
	jsonOut, remaining, err := parseGlobalFlags(args, &localCfg)
	if err != nil {
		return err
	}
	if len(remaining) == 0 {
		return usageError("missing command")
	}
	if isLocalTemplateValidation(remaining) {
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

func isLocalTemplateValidation(args []string) bool {
	return len(args) > 0 && args[0] == "templates"
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
			return writeValue(stdout, jobs, jsonOut, writeJobs)
		}
		if len(args) == 3 && args[1] == "inspect" {
			job, err := client.GetJob(ctx, args[2])
			if err != nil {
				return err
			}
			return writeValue(stdout, job, jsonOut, writeJobDetail)
		}
		return usageError("usage: pmctl jobs list | pmctl jobs inspect <job-id>")
	case "templates":
		if len(args) != 3 || args[1] != "validate" {
			return usageError("usage: pmctl templates validate <template-file>")
		}
		tmpl, err := loadTemplate(args[2])
		if err != nil {
			return err
		}
		return writeValue(stdout, templateValidationOutput{
			Valid:    true,
			Path:     args[2],
			Template: tmpl,
		}, jsonOut, writeTemplateValidation)
	case "submit":
		switch {
		case len(args) >= 3 && args[1] == "command":
			job, err := client.CreateCommandJob(ctx, args[2], args[3:])
			if err != nil {
				return err
			}
			return writeValue(stdout, job, jsonOut, writeJobDetail)
		case len(args) >= 3 && args[1] == "template":
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
			job, err := client.CreateCommandJob(ctx, expanded.Command, expanded.Args)
			if err != nil {
				return err
			}
			return writeValue(stdout, job, jsonOut, writeJobDetail)
		default:
			return usageError("usage: pmctl submit command <command> [args...] | pmctl submit template <template-file> --set name=value [--set name=value...]")
		}
	default:
		return usageError("unknown command " + args[0])
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

type templateSubmission struct {
	path   string
	values map[string]string
}

type setFlags []string

func (f *setFlags) String() string {
	return strings.Join(*f, ",")
}

func (f *setFlags) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func parseTemplateSubmission(args []string) (templateSubmission, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return templateSubmission{}, usageError("usage: pmctl submit template <template-file> --set name=value [--set name=value...]")
	}

	fs := flag.NewFlagSet("pmctl submit template", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var raw setFlags
	fs.Var(&raw, "set", "template parameter value name=value")
	if err := fs.Parse(args[1:]); err != nil {
		return templateSubmission{}, err
	}
	if fs.NArg() != 0 {
		return templateSubmission{}, usageError("usage: pmctl submit template <template-file> --set name=value [--set name=value...]")
	}

	values := make(map[string]string, len(raw))
	for _, entry := range raw {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			return templateSubmission{}, fmt.Errorf("invalid --set %q: expected name=value", entry)
		}
		if _, exists := values[name]; exists {
			return templateSubmission{}, fmt.Errorf("duplicate --set value for parameter %q", name)
		}
		values[name] = value
	}

	return templateSubmission{
		path:   args[0],
		values: values,
	}, nil
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

func (a clientAdapter) CreateCommandJob(ctx context.Context, command string, args []string) (Job, error) {
	return a.client.CreateCommandJob(ctx, command, args)
}

func isUsageError(err error) bool {
	var usage usageError
	return errors.As(err, &usage)
}

func joinCommand(command string, args []string) string {
	parts := append([]string{command}, args...)
	return strings.Join(parts, " ")
}
