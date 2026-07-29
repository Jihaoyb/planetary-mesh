package pmctl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"planetary-mesh/internal/protocol"
)

type fakeClient struct {
	status protocol.CoordinatorStatusResponse
	nodes  []Node
	jobs   []Job
	job    Job

	command              string
	args                 []string
	requiredCapabilities []string
	calls                int
	creates              int
	err                  error
}

func (f *fakeClient) Status(context.Context) (protocol.CoordinatorStatusResponse, error) {
	f.calls++
	return f.status, f.err
}

func (f *fakeClient) ListNodes(context.Context) ([]Node, error) {
	f.calls++
	return f.nodes, f.err
}

func (f *fakeClient) ListJobs(context.Context) ([]Job, error) {
	f.calls++
	return f.jobs, f.err
}

func (f *fakeClient) GetJob(_ context.Context, id string) (Job, error) {
	f.calls++
	if f.err != nil {
		return Job{}, f.err
	}
	if id != f.job.ID {
		return Job{}, errors.New("unexpected job id")
	}
	return f.job, nil
}

func (f *fakeClient) CreateCommandJob(_ context.Context, command string, args, requiredCapabilities []string) (Job, error) {
	f.calls++
	f.creates++
	f.command = command
	f.args = append([]string(nil), args...)
	f.requiredCapabilities = append([]string(nil), requiredCapabilities...)
	return f.job, f.err
}

func TestRunCommandStatus(t *testing.T) {
	client := &fakeClient{status: protocol.CoordinatorStatusResponse{
		Status:          "ok",
		ProtocolVersion: protocol.Version,
		StorageBackend:  "postgres",
		Dispatch:        protocol.DispatchStatus{MaxAttempts: 3, Timeout: "10s", BaseBackoff: "500ms"},
		Reconciliation:  &protocol.ReconciliationStatus{Grace: "30s", PendingRunningJobs: 2},
	}}
	var out bytes.Buffer

	if err := runCommandWithClient(context.Background(), client, []string{"status"}, &out, false); err != nil {
		t.Fatalf("status command: %v", err)
	}
	text := out.String()
	for _, want := range []string{"STATUS", "PROTOCOL", "postgres", "attempts=3", "RECONCILIATION", "grace=30s pending=2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, text)
		}
	}
}

func TestRunCommandSubmitCommand(t *testing.T) {
	client := &fakeClient{job: Job{ID: "job-1", Type: "command", Command: "echo", Args: []string{"hello"}, Status: "QUEUED"}}
	var out bytes.Buffer

	err := runCommandWithClient(context.Background(), client, []string{"submit", "command", "echo", "hello"}, &out, false)
	if err != nil {
		t.Fatalf("submit command: %v", err)
	}
	if client.command != "echo" || len(client.args) != 1 || client.args[0] != "hello" {
		t.Fatalf("unexpected command request: command=%q args=%q", client.command, client.args)
	}
	if !strings.Contains(out.String(), "job-1") || !strings.Contains(out.String(), "QUEUED") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestRunCommandSubmitCommandParsesPlacementBeforeWorkload(t *testing.T) {
	client := &fakeClient{job: Job{ID: "job-1", Type: "command", Command: "echo", Status: "QUEUED"}}
	var out bytes.Buffer

	err := runCommandWithClient(context.Background(), client, []string{
		"submit", "command",
		"--require-capability", " role:worker ",
		"--require-capability=profile:local",
		"--require-capability", "role:worker",
		"echo",
		"--require-capability", "workload-value",
		"--json",
	}, &out, false)
	if err != nil {
		t.Fatalf("submit constrained command: %v", err)
	}
	if client.command != "echo" {
		t.Fatalf("unexpected command: %q", client.command)
	}
	if !equalStringSlices(client.args, []string{"--require-capability", "workload-value", "--json"}) {
		t.Fatalf("unexpected workload args: %q", client.args)
	}
	if !equalStringSlices(client.requiredCapabilities, []string{"profile:local", "role:worker"}) {
		t.Fatalf("unexpected requirements: %q", client.requiredCapabilities)
	}
}

func TestRunCommandSubmitCommandExplicitFlagTerminator(t *testing.T) {
	client := &fakeClient{job: Job{ID: "job-1", Type: "command", Command: "--logical-command", Status: "QUEUED"}}

	err := runCommandWithClient(context.Background(), client, []string{
		"submit", "command", "--require-capability", "role:worker", "--", "--logical-command", "arg",
	}, ioDiscard{}, false)
	if err != nil {
		t.Fatalf("submit command with terminator: %v", err)
	}
	if client.command != "--logical-command" || !equalStringSlices(client.args, []string{"arg"}) {
		t.Fatalf("unexpected command request: command=%q args=%q", client.command, client.args)
	}
}

func TestRunCommandSubmitCommandPlacementErrorsAreLocal(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing value", args: []string{"--require-capability"}, want: "--require-capability requires a value"},
		{name: "unknown flag", args: []string{"--unknown", "echo"}, want: `unknown placement flag "--unknown"`},
		{name: "invalid label", args: []string{"--require-capability=-bad", "echo"}, want: `invalid required capability "-bad"`},
		{name: "missing command", args: []string{"--require-capability", "role:worker"}, want: submitCommandUsage},
		{name: "terminator without command", args: []string{"--"}, want: submitCommandUsage},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeClient{}
			args := append([]string{"submit", "command"}, tc.args...)
			err := runCommandWithClient(context.Background(), client, args, ioDiscard{}, false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
			if client.calls != 0 || client.creates != 0 {
				t.Fatalf("local validation contacted client: calls=%d creates=%d", client.calls, client.creates)
			}
		})
	}
}

func TestRunCommandTemplatesValidateHumanOutput(t *testing.T) {
	path := writeTemplateFile(t, `{
  "version": 1,
  "name": "text-stats",
  "description": "Count text statistics for one agent-local file.",
  "command": "text-stats",
  "parameters": [
    {"name": "input_path", "description": "Agent-local input path.", "required": true}
  ],
  "args": [
    {"param": "input_path"}
  ]
}`)
	client := &fakeClient{}
	var out bytes.Buffer

	if err := runCommandWithClient(context.Background(), client, []string{"templates", "validate", path}, &out, false); err != nil {
		t.Fatalf("templates validate: %v", err)
	}
	text := out.String()
	for _, want := range []string{"Template:", path, "Status:", "valid", "Name:", "text-stats", "Version:", "1", "Command:", "text-stats", "Parameters:", "input_path(required)", "Args:", "1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, text)
		}
	}
	if client.creates != 0 {
		t.Fatalf("templates validate should not create jobs, got %d calls", client.creates)
	}
}

func TestRunCommandTemplatesValidateJSONOutput(t *testing.T) {
	path := writeTemplateFile(t, `{
  "version": 1,
  "name": "text-stats",
  "command": "text-stats",
  "parameters": [
    {"name": "input_path", "required": true}
  ],
  "args": [
    {"param": "input_path"}
  ]
}`)
	var out bytes.Buffer

	if err := runCommandWithClient(context.Background(), &fakeClient{}, []string{"templates", "validate", path}, &out, true); err != nil {
		t.Fatalf("templates validate JSON: %v", err)
	}

	var got struct {
		Valid    bool   `json:"valid"`
		Path     string `json:"path"`
		Template struct {
			Version int    `json:"version"`
			Name    string `json:"name"`
			Command string `json:"command"`
			Args    []struct {
				Param string `json:"param"`
			} `json:"args"`
		} `json:"template"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, out.String())
	}
	if !got.Valid || got.Path != path || got.Template.Version != 1 || got.Template.Name != "text-stats" || got.Template.Command != "text-stats" || len(got.Template.Args) != 1 || got.Template.Args[0].Param != "input_path" {
		t.Fatalf("unexpected validation JSON: %+v", got)
	}
}

func TestRunCommandTemplatesInspectHumanOutput(t *testing.T) {
	path := writeTemplateFile(t, `{
  "version": 1,
  "name": "text-stats",
  "description": "Count text statistics for one agent-local file.",
  "command": "text-stats",
  "parameters": [
    {"name": "input_path", "description": "Agent-local text file path.", "required": true}
  ],
  "args": [
    {"param": "input_path"}
  ]
}`)
	client := &fakeClient{}
	var out bytes.Buffer

	if err := runCommandWithClient(context.Background(), client, []string{"templates", "inspect", path}, &out, false); err != nil {
		t.Fatalf("templates inspect: %v", err)
	}
	text := out.String()
	for _, want := range []string{"Template:", path, "Status:", "valid", "Name:", "text-stats", "Description:", "Count text statistics for one agent-local file.", "Parameters:", "NAME", "REQUIRED", "DEFAULT", "DESCRIPTION", "input_path", "true", "Agent-local text file path.", "Args:", "INDEX", "TYPE", "VALUE", "1", "param"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, text)
		}
	}
	if client.calls != 0 {
		t.Fatalf("templates inspect should not contact coordinator, got %d calls", client.calls)
	}
}

func TestRunCommandTemplatesInspectJSONOutput(t *testing.T) {
	path := writeTemplateFile(t, `{
  "version": 1,
  "name": "text-stats",
  "description": "Count text statistics for one agent-local file.",
  "command": "text-stats",
  "parameters": [
    {"name": "input_path", "description": "Agent-local text file path.", "required": true}
  ],
  "args": [
    {"param": "input_path"}
  ]
}`)
	client := &fakeClient{}
	var out bytes.Buffer

	if err := runCommandWithClient(context.Background(), client, []string{"templates", "inspect", path}, &out, true); err != nil {
		t.Fatalf("templates inspect JSON: %v", err)
	}

	var got struct {
		Valid       bool   `json:"valid"`
		Path        string `json:"path"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     int    `json:"version"`
		Command     string `json:"command"`
		Parameters  []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Required    bool   `json:"required"`
		} `json:"parameters"`
		Args []struct {
			Index int    `json:"index"`
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"args"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, out.String())
	}
	if !got.Valid || got.Path != path || got.Name != "text-stats" || got.Description == "" || got.Version != 1 || got.Command != "text-stats" {
		t.Fatalf("unexpected inspect JSON: %+v", got)
	}
	if len(got.Parameters) != 1 || got.Parameters[0].Name != "input_path" || !got.Parameters[0].Required {
		t.Fatalf("unexpected inspect parameters JSON: %+v", got.Parameters)
	}
	if len(got.Args) != 1 || got.Args[0].Index != 1 || got.Args[0].Type != "param" || got.Args[0].Value != "input_path" {
		t.Fatalf("unexpected inspect args JSON: %+v", got.Args)
	}
	if client.calls != 0 {
		t.Fatalf("templates inspect JSON should not contact coordinator, got %d calls", client.calls)
	}
}

func TestRunCommandTemplatesPreviewHumanOutput(t *testing.T) {
	path := writeTemplateFile(t, `{
  "version": 1,
  "name": "text-stats",
  "command": "text-stats",
  "parameters": [
    {"name": "input_path", "required": true},
    {"name": "suffix", "required": false, "default": ""}
  ],
  "args": [
    {"literal": "--suffix"},
    {"param": "suffix"},
    {"param": "input_path"}
  ]
}`)
	client := &fakeClient{}
	var out bytes.Buffer

	err := runCommandWithClient(context.Background(), client, []string{"templates", "preview", path, "--set", "input_path=/tmp/input.txt"}, &out, false)
	if err != nil {
		t.Fatalf("templates preview: %v", err)
	}
	text := out.String()
	for _, want := range []string{"Template:", path, "Status:", "preview", "Name:", "text-stats", "Expanded Job Type:", "command", "Expanded Command:", "text-stats", "Creates Job:", "false", "Contacts Coordinator:", "false", "Checks Agent Allowlist:", "false", "Args:", `"--suffix"`, `""`, `"/tmp/input.txt"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, text)
		}
	}
	if client.calls != 0 || client.creates != 0 {
		t.Fatalf("templates preview should not contact coordinator or create jobs, calls=%d creates=%d", client.calls, client.creates)
	}
}

func TestRunCommandTemplatesPreviewIncludesCanonicalRequirements(t *testing.T) {
	path := writeTemplateFile(t, `{
  "version": 1,
  "name": "text-stats",
  "command": "text-stats",
  "args": []
}`)
	client := &fakeClient{}
	var out bytes.Buffer

	err := runCommandWithClient(context.Background(), client, []string{
		"templates", "preview", path,
		"--require-capability", " role:text-worker ",
		"--require-capability=profile:local",
	}, &out, true)
	if err != nil {
		t.Fatalf("templates preview requirements: %v", err)
	}
	var got struct {
		ExpandedJob struct {
			RequiredCapabilities []string `json:"required_capabilities"`
		} `json:"expanded_job"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if !equalStringSlices(got.ExpandedJob.RequiredCapabilities, []string{"profile:local", "role:text-worker"}) {
		t.Fatalf("unexpected preview requirements: %+v", got.ExpandedJob.RequiredCapabilities)
	}
	if client.calls != 0 {
		t.Fatalf("preview contacted client %d times", client.calls)
	}
}

func TestRunCommandTemplatesPreviewJSONOutput(t *testing.T) {
	path := writeTemplateFile(t, `{
  "version": 1,
  "name": "text-stats",
  "command": "text-stats",
  "parameters": [
    {"name": "input_path", "required": true},
    {"name": "format", "required": false, "default": "plain"}
  ],
  "args": [
    {"literal": "--format"},
    {"param": "format"},
    {"param": "input_path"}
  ]
}`)
	client := &fakeClient{}
	var out bytes.Buffer

	err := runCommandWithClient(context.Background(), client, []string{"templates", "preview", path, "--set", "input_path=/tmp/input.txt", "--set", "format=json"}, &out, true)
	if err != nil {
		t.Fatalf("templates preview JSON: %v", err)
	}

	var got struct {
		Valid       bool   `json:"valid"`
		Path        string `json:"path"`
		Name        string `json:"name"`
		ExpandedJob struct {
			Type    string   `json:"type"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"expanded_job"`
		CreatesJob           bool `json:"creates_job"`
		ContactsCoordinator  bool `json:"contacts_coordinator"`
		ChecksAgentAllowlist bool `json:"checks_agent_allowlist"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, out.String())
	}
	if !got.Valid || got.Path != path || got.Name != "text-stats" {
		t.Fatalf("unexpected preview JSON: %+v", got)
	}
	if got.ExpandedJob.Type != "command" || got.ExpandedJob.Command != "text-stats" || !equalStringSlices(got.ExpandedJob.Args, []string{"--format", "json", "/tmp/input.txt"}) {
		t.Fatalf("unexpected expanded job JSON: %+v", got.ExpandedJob)
	}
	if got.CreatesJob || got.ContactsCoordinator || got.ChecksAgentAllowlist {
		t.Fatalf("unexpected preview booleans: %+v", got)
	}
	if client.calls != 0 || client.creates != 0 {
		t.Fatalf("templates preview JSON should not contact coordinator or create jobs, calls=%d creates=%d", client.calls, client.creates)
	}
}

func TestRunCommandTemplatesPreviewJSONOutputUsesEmptyArgsArray(t *testing.T) {
	path := writeTemplateFile(t, `{
  "version": 1,
  "name": "no-args",
  "command": "health-check",
  "args": []
}`)
	client := &fakeClient{}
	var out bytes.Buffer

	if err := runCommandWithClient(context.Background(), client, []string{"templates", "preview", path}, &out, true); err != nil {
		t.Fatalf("templates preview JSON: %v", err)
	}

	var got struct {
		ExpandedJob struct {
			Args                 []string `json:"args"`
			RequiredCapabilities []string `json:"required_capabilities"`
		} `json:"expanded_job"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, out.String())
	}
	if got.ExpandedJob.Args == nil || len(got.ExpandedJob.Args) != 0 {
		t.Fatalf("expected empty args array, got %s", out.String())
	}
	if got.ExpandedJob.RequiredCapabilities == nil || len(got.ExpandedJob.RequiredCapabilities) != 0 {
		t.Fatalf("expected empty required_capabilities array, got %s", out.String())
	}
	if client.calls != 0 {
		t.Fatalf("templates preview JSON should not contact coordinator, got %d calls", client.calls)
	}
}

func TestRunCommandSubmitTemplate(t *testing.T) {
	path := writeTemplateFile(t, `{
  "version": 1,
  "name": "text-stats",
  "command": "text-stats",
  "parameters": [
    {"name": "input_path", "required": true},
    {"name": "format", "required": false, "default": "plain"}
  ],
  "args": [
    {"literal": "--format"},
    {"param": "format"},
    {"param": "input_path"}
  ]
}`)
	client := &fakeClient{job: Job{ID: "job-1", Type: "command", Command: "text-stats", Args: []string{"--format", "json", "/agent/input.txt"}, Status: "QUEUED"}}
	var out bytes.Buffer

	err := runCommandWithClient(context.Background(), client, []string{
		"submit", "template", path,
		"--set", "input_path=/agent/input.txt",
		"--require-capability", "role:text-worker",
		"--set=format=json",
		"--require-capability=profile:local",
	}, &out, false)
	if err != nil {
		t.Fatalf("submit template: %v", err)
	}
	if client.creates != 1 || client.command != "text-stats" {
		t.Fatalf("unexpected create call: creates=%d command=%q args=%q", client.creates, client.command, client.args)
	}
	wantArgs := []string{"--format", "json", "/agent/input.txt"}
	if !equalStringSlices(client.args, wantArgs) {
		t.Fatalf("expected args %q, got %q", wantArgs, client.args)
	}
	if !equalStringSlices(client.requiredCapabilities, []string{"profile:local", "role:text-worker"}) {
		t.Fatalf("unexpected requirements: %q", client.requiredCapabilities)
	}
	if !strings.Contains(out.String(), "job-1") || !strings.Contains(out.String(), "QUEUED") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
}

func TestRunCommandSubmitTemplateValidationErrorsDoNotCreateJobs(t *testing.T) {
	tests := []struct {
		name string
		body string
		args []string
		want string
	}{
		{
			name: "invalid template",
			body: `{"version":1,"name":"bad","command":"text-stats","parameters":[{"name":"input_path","required":true}],"args":[{"param":"input_path","extra":true}]}`,
			args: []string{"--set", "input_path=/agent/input.txt"},
			want: "invalid template",
		},
		{
			name: "missing required parameter",
			body: `{"version":1,"name":"text-stats","command":"text-stats","parameters":[{"name":"input_path","required":true}],"args":[{"param":"input_path"}]}`,
			args: nil,
			want: `missing required parameter "input_path"`,
		},
		{
			name: "unknown set parameter",
			body: `{"version":1,"name":"text-stats","command":"text-stats","parameters":[{"name":"input_path","required":true}],"args":[{"param":"input_path"}]}`,
			args: []string{"--set", "input_path=/agent/input.txt", "--set", "other=x"},
			want: `unknown parameter "other"`,
		},
		{
			name: "duplicate set parameter",
			body: `{"version":1,"name":"text-stats","command":"text-stats","parameters":[{"name":"input_path","required":true}],"args":[{"param":"input_path"}]}`,
			args: []string{"--set", "input_path=/agent/input.txt", "--set", "input_path=/other.txt"},
			want: `duplicate --set value for parameter "input_path"`,
		},
		{
			name: "invalid set",
			body: `{"version":1,"name":"text-stats","command":"text-stats","parameters":[{"name":"input_path","required":true}],"args":[{"param":"input_path"}]}`,
			args: []string{"--set", "input_path"},
			want: `invalid --set "input_path": expected name=value`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTemplateFile(t, tc.body)
			client := &fakeClient{}
			args := append([]string{"submit", "template", path}, tc.args...)
			err := runCommandWithClient(context.Background(), client, args, ioDiscard{}, false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
			if client.creates != 0 {
				t.Fatalf("validation error should not create jobs, got %d calls", client.creates)
			}
		})
	}
}

func TestRunCommandTemplatesPreviewValidationErrorsDoNotContactCoordinator(t *testing.T) {
	tests := []struct {
		name string
		body string
		args []string
		want string
	}{
		{
			name: "invalid template",
			body: `{"version":1,"name":"bad","command":"text-stats","parameters":[{"name":"input_path","required":true}],"args":[{"param":"input_path","extra":true}]}`,
			args: []string{"--set", "input_path=/agent/input.txt"},
			want: "invalid template",
		},
		{
			name: "missing required parameter",
			body: `{"version":1,"name":"text-stats","command":"text-stats","parameters":[{"name":"input_path","required":true}],"args":[{"param":"input_path"}]}`,
			args: nil,
			want: `missing required parameter "input_path"`,
		},
		{
			name: "unknown set parameter",
			body: `{"version":1,"name":"text-stats","command":"text-stats","parameters":[{"name":"input_path","required":true}],"args":[{"param":"input_path"}]}`,
			args: []string{"--set", "input_path=/agent/input.txt", "--set", "other=x"},
			want: `unknown parameter "other"`,
		},
		{
			name: "duplicate set parameter",
			body: `{"version":1,"name":"text-stats","command":"text-stats","parameters":[{"name":"input_path","required":true}],"args":[{"param":"input_path"}]}`,
			args: []string{"--set", "input_path=/agent/input.txt", "--set", "input_path=/other.txt"},
			want: `duplicate --set value for parameter "input_path"`,
		},
		{
			name: "invalid set",
			body: `{"version":1,"name":"text-stats","command":"text-stats","parameters":[{"name":"input_path","required":true}],"args":[{"param":"input_path"}]}`,
			args: []string{"--set", "input_path"},
			want: `invalid --set "input_path": expected name=value`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTemplateFile(t, tc.body)
			client := &fakeClient{}
			args := append([]string{"templates", "preview", path}, tc.args...)
			err := runCommandWithClient(context.Background(), client, args, ioDiscard{}, false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
			if client.calls != 0 || client.creates != 0 {
				t.Fatalf("preview validation error should not contact coordinator or create jobs, calls=%d creates=%d", client.calls, client.creates)
			}
		})
	}
}

func TestRunETemplatesValidateDoesNotRequireClientConfig(t *testing.T) {
	clearPMCTLEnv(t)
	t.Setenv("PMCTL_TLS_CA_FILE", filepath.Join(t.TempDir(), "missing-ca.pem"))
	path := writeTemplateFile(t, `{
  "version": 1,
  "name": "text-stats",
  "command": "text-stats",
  "parameters": [
    {"name": "input_path", "required": true}
  ],
  "args": [
    {"param": "input_path"}
  ]
}`)
	var out bytes.Buffer

	if err := RunE(context.Background(), []string{"--json", "templates", "validate", path}, &out); err != nil {
		t.Fatalf("RunE templates validate returned error: %v", err)
	}
	if !strings.Contains(out.String(), `"valid": true`) {
		t.Fatalf("expected validation JSON, got:\n%s", out.String())
	}
}

func TestRunETemplatesInspectAndPreviewDoNotRequireClientConfig(t *testing.T) {
	clearPMCTLEnv(t)
	t.Setenv("PMCTL_TLS_CA_FILE", filepath.Join(t.TempDir(), "missing-ca.pem"))
	path := writeTemplateFile(t, `{
  "version": 1,
  "name": "text-stats",
  "command": "text-stats",
  "parameters": [
    {"name": "input_path", "required": true}
  ],
  "args": [
    {"param": "input_path"}
  ]
}`)

	var out bytes.Buffer
	if err := RunE(context.Background(), []string{"--json", "templates", "inspect", path}, &out); err != nil {
		t.Fatalf("RunE templates inspect returned error: %v", err)
	}
	if !strings.Contains(out.String(), `"name": "text-stats"`) {
		t.Fatalf("expected inspect JSON, got:\n%s", out.String())
	}

	out.Reset()
	if err := RunE(context.Background(), []string{"--json", "templates", "preview", path, "--set", "input_path=/agent/input.txt"}, &out); err != nil {
		t.Fatalf("RunE templates preview returned error: %v", err)
	}
	if !strings.Contains(out.String(), `"creates_job": false`) || !strings.Contains(out.String(), `"/agent/input.txt"`) {
		t.Fatalf("expected preview JSON, got:\n%s", out.String())
	}
}

func TestRunCommandJSONOutput(t *testing.T) {
	client := &fakeClient{jobs: []Job{{ID: "job-1", Status: "COMPLETED"}}}
	var out bytes.Buffer

	err := runCommandWithClient(context.Background(), client, []string{"jobs", "list"}, &out, true)
	if err != nil {
		t.Fatalf("jobs list: %v", err)
	}
	if !strings.Contains(out.String(), `"id": "job-1"`) {
		t.Fatalf("expected JSON output, got:\n%s", out.String())
	}
}

func TestRunCommandNodesJSONOutputIncludesMetadataDefaults(t *testing.T) {
	client := &fakeClient{nodes: []Node{{ID: "node-1", State: "HEALTHY"}}}
	var out bytes.Buffer

	err := runCommandWithClient(context.Background(), client, []string{"nodes", "list"}, &out, true)
	if err != nil {
		t.Fatalf("nodes list: %v", err)
	}
	text := out.String()
	for _, want := range []string{`"id": "node-1"`, `"capabilities": []`, `"active_executions": 0`} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected JSON output to contain %q, got:\n%s", want, text)
		}
	}
}

func TestRunCommandUsageErrors(t *testing.T) {
	err := runCommandWithClient(context.Background(), &fakeClient{}, []string{"jobs"}, ioDiscard{}, false)
	if err == nil || !isUsageError(err) {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestRunCommandOutputFailuresPreserveCreationBoundary(t *testing.T) {
	client := &fakeClient{job: Job{ID: "job-1", Type: "command", Command: "echo", Status: "QUEUED"}}
	err := runCommandWithClient(
		context.Background(),
		client,
		[]string{"submit", "command", "--require-capability", "role:worker", "echo"},
		failingWriter{},
		false,
	)
	if err == nil || !strings.Contains(err.Error(), "writer-secret") {
		t.Fatalf("expected writer error after submission, got %v", err)
	}
	if client.creates != 1 {
		t.Fatalf("expected job creation before output failure, got %d", client.creates)
	}

	path := writeTemplateFile(t, `{
  "version": 1,
  "name": "preview",
  "command": "echo",
  "args": []
}`)
	previewClient := &fakeClient{}
	err = runCommandWithClient(
		context.Background(),
		previewClient,
		[]string{"templates", "preview", path, "--require-capability", "role:worker"},
		failingWriter{},
		false,
	)
	if err == nil || !strings.Contains(err.Error(), "writer-secret") {
		t.Fatalf("expected preview writer error, got %v", err)
	}
	if previewClient.calls != 0 {
		t.Fatalf("preview output failure contacted client %d times", previewClient.calls)
	}
}

func TestRunCommandTemplateUsageErrors(t *testing.T) {
	tests := [][]string{
		{"templates"},
		{"templates", "validate"},
		{"templates", "inspect"},
		{"templates", "preview"},
		{"templates", "preview", "template.json", "unexpected"},
		{"submit", "template"},
		{"submit", "template", "template.json", "unexpected"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			err := runCommandWithClient(context.Background(), &fakeClient{}, args, ioDiscard{}, false)
			if err == nil || !isUsageError(err) {
				t.Fatalf("expected usage error, got %v", err)
			}
		})
	}
}

func TestParseGlobalFlagsUsesEnvDefaults(t *testing.T) {
	cfg := Config{CoordinatorURL: "http://from-env:8080"}
	jsonOut, args, err := parseGlobalFlags([]string{"--json", "status"}, &cfg)
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if !jsonOut || len(args) != 1 || args[0] != "status" {
		t.Fatalf("unexpected parse result: json=%t args=%q", jsonOut, args)
	}
	if cfg.CoordinatorURL != "http://from-env:8080" {
		t.Fatalf("unexpected coordinator URL: %s", cfg.CoordinatorURL)
	}
}

func TestConfigFromSourcesLoadsFileAndEnvOverride(t *testing.T) {
	clearPMCTLEnv(t)
	path := writePMCTLTempConfig(t, `
PMCTL_COORDINATOR_URL=http://from-file:8080
PMCTL_TLS_CA_FILE=file-ca.pem
PMCTL_TLS_CERT_FILE=file-cert.pem
PMCTL_TLS_KEY_FILE=file-key.pem
`)
	t.Setenv("PMCTL_COORDINATOR_URL", "http://from-env:8080")

	cfg, err := ConfigFromSources([]string{"--config", path, "status"})
	if err != nil {
		t.Fatalf("ConfigFromSources returned error: %v", err)
	}
	if cfg.ConfigFile != path {
		t.Fatalf("expected config file %q, got %q", path, cfg.ConfigFile)
	}
	if cfg.CoordinatorURL != "http://from-env:8080" {
		t.Fatalf("expected env coordinator URL, got %q", cfg.CoordinatorURL)
	}
	if cfg.TLSFiles.CAFile != "file-ca.pem" || cfg.TLSFiles.CertFile != "file-cert.pem" || cfg.TLSFiles.KeyFile != "file-key.pem" {
		t.Fatalf("unexpected TLS config: %+v", cfg.TLSFiles)
	}
}

func TestConfigFromSourcesUsesConfigPathEnv(t *testing.T) {
	clearPMCTLEnv(t)
	path := writePMCTLTempConfig(t, `PMCTL_COORDINATOR_URL=http://from-path-env:8080`)
	t.Setenv("PMCTL_CONFIG_FILE", path)

	cfg, err := ConfigFromSources([]string{"status"})
	if err != nil {
		t.Fatalf("ConfigFromSources returned error: %v", err)
	}
	if cfg.ConfigFile != path || cfg.CoordinatorURL != "http://from-path-env:8080" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestConfigFromSourcesLoadsExampleConfig(t *testing.T) {
	clearPMCTLEnv(t)
	path := filepath.Join("..", "..", "config", "pmctl.env.example")

	cfg, err := ConfigFromSources([]string{"--config", path, "status"})
	if err != nil {
		t.Fatalf("ConfigFromSources returned error: %v", err)
	}
	if cfg.ConfigFile != path {
		t.Fatalf("expected config file %q, got %q", path, cfg.ConfigFile)
	}
	if cfg.CoordinatorURL != "http://localhost:8080" {
		t.Fatalf("expected local coordinator URL, got %q", cfg.CoordinatorURL)
	}
	if cfg.TLSFiles.Configured() {
		t.Fatalf("expected example pmctl config to default to plain mode")
	}
}

func TestConfigFromSourcesFlagOverridesEnvAfterParsing(t *testing.T) {
	clearPMCTLEnv(t)
	path := writePMCTLTempConfig(t, `PMCTL_COORDINATOR_URL=http://from-file:8080`)
	t.Setenv("PMCTL_COORDINATOR_URL", "http://from-env:8080")

	cfg, err := ConfigFromSources([]string{"--config", path, "--coordinator-url", "http://from-flag:8080", "status"})
	if err != nil {
		t.Fatalf("ConfigFromSources returned error: %v", err)
	}
	_, args, err := parseGlobalFlags([]string{"--config", path, "--coordinator-url", "http://from-flag:8080", "status"}, &cfg)
	if err != nil {
		t.Fatalf("parseGlobalFlags returned error: %v", err)
	}
	if len(args) != 1 || args[0] != "status" {
		t.Fatalf("unexpected args: %q", args)
	}
	if cfg.CoordinatorURL != "http://from-flag:8080" {
		t.Fatalf("expected flag coordinator URL, got %q", cfg.CoordinatorURL)
	}
}

func TestConfigFromSourcesRejectsMissingExplicitFile(t *testing.T) {
	clearPMCTLEnv(t)
	_, err := ConfigFromSources([]string{"--config", filepath.Join(t.TempDir(), "missing.env"), "status"})
	if err == nil || !strings.Contains(err.Error(), "load config file") || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing config file error, got %v", err)
	}
}

func TestWriteNodesAndJobs(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	if err := writeNodes(&out, []Node{{
		ID:           "node-1",
		State:        "HEALTHY",
		Address:      "localhost:8081",
		LastSeen:     now,
		Capabilities: []string{"profile:local", "role:worker"},
		Load:         protocol.NodeLoad{ActiveExecutions: 2},
	}}); err != nil {
		t.Fatalf("write nodes: %v", err)
	}
	if !strings.Contains(out.String(), "ACTIVE") || !strings.Contains(out.String(), "node-1") || !strings.Contains(out.String(), "HEALTHY") || !strings.Contains(out.String(), "profile:local,role:worker") {
		t.Fatalf("unexpected nodes output:\n%s", out.String())
	}

	out.Reset()
	if err := writeJobs(&out, []Job{{ID: "job-1", Status: "COMPLETED", Type: "command", Command: "echo", Args: []string{"hi"}, UpdatedAt: now}}); err != nil {
		t.Fatalf("write jobs: %v", err)
	}
	if !strings.Contains(out.String(), "job-1") || !strings.Contains(out.String(), "echo hi") || !strings.Contains(out.String(), "REQUIRES") {
		t.Fatalf("unexpected jobs output:\n%s", out.String())
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

func clearPMCTLEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"PMCTL_CONFIG_FILE",
		"PMCTL_COORDINATOR_URL",
		"PMCTL_TLS_CA_FILE",
		"PMCTL_TLS_CERT_FILE",
		"PMCTL_TLS_KEY_FILE",
	}
	saved := make(map[string]string)
	present := make(map[string]bool)
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			saved[key] = value
			present[key] = true
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
	t.Cleanup(func() {
		for _, key := range keys {
			if present[key] {
				_ = os.Setenv(key, saved[key])
			} else {
				_ = os.Unsetenv(key)
			}
		}
	})
}

func writePMCTLTempConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pmctl.env")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func writeTemplateFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "template.pmtemplate.json")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o600); err != nil {
		t.Fatalf("write template: %v", err)
	}
	return path
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
