package workflowtemplate

import (
	"strings"
	"testing"
)

func TestDecodeAndExpandTemplate(t *testing.T) {
	tmpl := decodeTemplate(t, `{
  "version": 1,
  "name": "text-stats",
  "description": "Count text statistics for one agent-local file.",
  "command": "text-stats",
  "parameters": [
    {"name": "input_path", "description": "Agent-local input path.", "required": true},
    {"name": "format", "required": false, "default": "plain"}
  ],
  "args": [
    {"literal": "--format"},
    {"param": "format"},
    {"param": "input_path"}
  ]
}`)

	expanded, err := Expand(tmpl, map[string]string{"input_path": "/tmp/input.txt"})
	if err != nil {
		t.Fatalf("Expand returned error: %v", err)
	}
	if expanded.Command != "text-stats" {
		t.Fatalf("expected text-stats command, got %q", expanded.Command)
	}
	wantArgs := []string{"--format", "plain", "/tmp/input.txt"}
	if !equalStrings(expanded.Args, wantArgs) {
		t.Fatalf("expected args %q, got %q", wantArgs, expanded.Args)
	}
}

func TestExpandAllowsEmptyValuesDefaultsAndLiterals(t *testing.T) {
	empty := ""
	tmpl := Template{
		Version: Version,
		Name:    "empty-values",
		Command: "helper",
		Parameters: []Parameter{
			{Name: "required", Required: true},
			{Name: "optional", Default: &empty},
		},
		Args: []ArgToken{
			{Literal: &empty},
			{Param: stringPtr("required")},
			{Param: stringPtr("optional")},
		},
	}

	expanded, err := Expand(tmpl, map[string]string{"required": ""})
	if err != nil {
		t.Fatalf("Expand returned error: %v", err)
	}
	wantArgs := []string{"", "", ""}
	if !equalStrings(expanded.Args, wantArgs) {
		t.Fatalf("expected args %q, got %q", wantArgs, expanded.Args)
	}
}

func TestDecodeRejectsInvalidTemplates(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "unknown top-level field",
			body: `{"version":1,"name":"bad","command":"helper","args":[],"extra":true}`,
			want: `unknown field "extra"`,
		},
		{
			name: "unknown parameter field",
			body: `{"version":1,"name":"bad","command":"helper","parameters":[{"name":"input","required":true,"extra":true}],"args":[{"param":"input"}]}`,
			want: `unknown field "extra"`,
		},
		{
			name: "unknown arg field",
			body: `{"version":1,"name":"bad","command":"helper","args":[{"literal":"x","extra":true}]}`,
			want: `unknown field "extra"`,
		},
		{
			name: "unsupported version",
			body: `{"version":2,"name":"bad","command":"helper","args":[]}`,
			want: "unsupported template version 2",
		},
		{
			name: "invalid template name",
			body: `{"version":1,"name":"bad name","command":"helper","args":[]}`,
			want: "template name",
		},
		{
			name: "missing args",
			body: `{"version":1,"name":"bad","command":"helper"}`,
			want: "args is required",
		},
		{
			name: "duplicate parameter",
			body: `{"version":1,"name":"bad","command":"helper","parameters":[{"name":"input","required":true},{"name":"input","required":true}],"args":[{"param":"input"}]}`,
			want: `duplicate parameter "input"`,
		},
		{
			name: "invalid parameter name",
			body: `{"version":1,"name":"bad","command":"helper","parameters":[{"name":"1input","required":true}],"args":[{"param":"1input"}]}`,
			want: "parameter 0 name",
		},
		{
			name: "optional without default",
			body: `{"version":1,"name":"bad","command":"helper","parameters":[{"name":"mode","required":false}],"args":[{"param":"mode"}]}`,
			want: `optional parameter "mode" must declare a default`,
		},
		{
			name: "required with default",
			body: `{"version":1,"name":"bad","command":"helper","parameters":[{"name":"input","required":true,"default":"x"}],"args":[{"param":"input"}]}`,
			want: `required parameter "input" must not declare a default`,
		},
		{
			name: "unknown parameter reference",
			body: `{"version":1,"name":"bad","command":"helper","args":[{"param":"input"}]}`,
			want: `arg token 0 references unknown parameter "input"`,
		},
		{
			name: "arg token has literal and param",
			body: `{"version":1,"name":"bad","command":"helper","parameters":[{"name":"input","required":true}],"args":[{"literal":"x","param":"input"}]}`,
			want: "arg token 0 must contain exactly one of literal or param",
		},
		{
			name: "arg token has neither",
			body: `{"version":1,"name":"bad","command":"helper","args":[{}]}`,
			want: "arg token 0 must contain exactly one of literal or param",
		},
		{
			name: "multiple JSON values",
			body: `{"version":1,"name":"bad","command":"helper","args":[]} {}`,
			want: "template file must contain exactly one JSON value",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestValidateRejectsUnsafeCommandKeys(t *testing.T) {
	for _, command := range []string{"", ".", "..", "builtin:echo", "/bin/echo", `tools\echo`, "echo hello", "echo\nhello", "echo;rm"} {
		t.Run(command, func(t *testing.T) {
			tmpl := Template{Version: Version, Name: "bad", Command: command, Args: []ArgToken{}}
			if err := Validate(tmpl); err == nil {
				t.Fatalf("expected command %q to be rejected", command)
			}
		})
	}
}

func TestExpandRejectsMissingAndUnknownParameters(t *testing.T) {
	tmpl := Template{
		Version: Version,
		Name:    "missing",
		Command: "helper",
		Parameters: []Parameter{
			{Name: "input", Required: true},
		},
		Args: []ArgToken{{Param: stringPtr("input")}},
	}

	if _, err := Expand(tmpl, nil); err == nil || !strings.Contains(err.Error(), `missing required parameter "input"`) {
		t.Fatalf("expected missing parameter error, got %v", err)
	}
	if _, err := Expand(tmpl, map[string]string{"other": "x"}); err == nil || !strings.Contains(err.Error(), `unknown parameter "other"`) {
		t.Fatalf("expected unknown parameter error, got %v", err)
	}
}

func TestParameterNamesReturnsSortedNames(t *testing.T) {
	got := ParameterNames(Template{Parameters: []Parameter{{Name: "z"}, {Name: "a"}, {Name: "m"}}})
	want := []string{"a", "m", "z"}
	if !equalStrings(got, want) {
		t.Fatalf("expected names %q, got %q", want, got)
	}
}

func decodeTemplate(t *testing.T, body string) Template {
	t.Helper()
	tmpl, err := Decode(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	return tmpl
}

func stringPtr(s string) *string {
	return &s
}

func equalStrings(a, b []string) bool {
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
