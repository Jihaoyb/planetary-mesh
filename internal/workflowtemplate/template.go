package workflowtemplate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const Version = 1

var (
	namePattern       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
	commandKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

type Template struct {
	Version     int         `json:"version"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Command     string      `json:"command"`
	Parameters  []Parameter `json:"parameters,omitempty"`
	Args        []ArgToken  `json:"args"`
}

type Parameter struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Required    bool    `json:"required"`
	Default     *string `json:"default,omitempty"`
}

type ArgToken struct {
	Literal *string `json:"literal,omitempty"`
	Param   *string `json:"param,omitempty"`
}

type ExpandedCommand struct {
	Command string
	Args    []string
}

type ArgDescription struct {
	Index int    `json:"index"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

func Load(path string) (Template, error) {
	file, err := os.Open(path)
	if err != nil {
		return Template{}, err
	}
	defer file.Close()

	tmpl, err := Decode(file)
	if err != nil {
		return Template{}, err
	}
	return tmpl, nil
}

func Decode(r io.Reader) (Template, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()

	var tmpl Template
	if err := dec.Decode(&tmpl); err != nil {
		return Template{}, err
	}
	if err := ensureEOF(dec); err != nil {
		return Template{}, err
	}
	if err := Validate(tmpl); err != nil {
		return Template{}, err
	}
	return tmpl, nil
}

func Validate(tmpl Template) error {
	if tmpl.Version != Version {
		return fmt.Errorf("unsupported template version %d, expected %d", tmpl.Version, Version)
	}
	if !namePattern.MatchString(tmpl.Name) {
		return fmt.Errorf("template name %q must match %s", tmpl.Name, namePattern.String())
	}
	if err := validateCommandKey(tmpl.Command); err != nil {
		return err
	}
	if tmpl.Args == nil {
		return errors.New("args is required")
	}

	params := make(map[string]Parameter, len(tmpl.Parameters))
	for i, param := range tmpl.Parameters {
		if !namePattern.MatchString(param.Name) {
			return fmt.Errorf("parameter %d name %q must match %s", i, param.Name, namePattern.String())
		}
		if _, ok := params[param.Name]; ok {
			return fmt.Errorf("duplicate parameter %q", param.Name)
		}
		if param.Required && param.Default != nil {
			return fmt.Errorf("required parameter %q must not declare a default", param.Name)
		}
		if !param.Required && param.Default == nil {
			return fmt.Errorf("optional parameter %q must declare a default", param.Name)
		}
		params[param.Name] = param
	}

	for i, token := range tmpl.Args {
		literalSet := token.Literal != nil
		paramSet := token.Param != nil
		if literalSet == paramSet {
			return fmt.Errorf("arg token %d must contain exactly one of literal or param", i)
		}
		if paramSet {
			if _, ok := params[*token.Param]; !ok {
				return fmt.Errorf("arg token %d references unknown parameter %q", i, *token.Param)
			}
		}
	}

	return nil
}

func Expand(tmpl Template, values map[string]string) (ExpandedCommand, error) {
	if err := Validate(tmpl); err != nil {
		return ExpandedCommand{}, err
	}

	params := make(map[string]Parameter, len(tmpl.Parameters))
	resolved := make(map[string]string, len(tmpl.Parameters))
	for _, param := range tmpl.Parameters {
		params[param.Name] = param
		if param.Default != nil {
			resolved[param.Name] = *param.Default
		}
	}

	for name, value := range values {
		if _, ok := params[name]; !ok {
			return ExpandedCommand{}, fmt.Errorf("unknown parameter %q", name)
		}
		resolved[name] = value
	}
	for _, param := range tmpl.Parameters {
		if param.Required {
			if _, ok := resolved[param.Name]; !ok {
				return ExpandedCommand{}, fmt.Errorf("missing required parameter %q", param.Name)
			}
		}
	}

	args := make([]string, 0, len(tmpl.Args))
	for _, token := range tmpl.Args {
		if token.Literal != nil {
			args = append(args, *token.Literal)
			continue
		}
		args = append(args, resolved[*token.Param])
	}

	return ExpandedCommand{
		Command: tmpl.Command,
		Args:    args,
	}, nil
}

func ParameterNames(tmpl Template) []string {
	names := make([]string, 0, len(tmpl.Parameters))
	for _, param := range tmpl.Parameters {
		names = append(names, param.Name)
	}
	sort.Strings(names)
	return names
}

func DescribeArgs(tmpl Template) ([]ArgDescription, error) {
	if err := Validate(tmpl); err != nil {
		return nil, err
	}

	args := make([]ArgDescription, 0, len(tmpl.Args))
	for i, token := range tmpl.Args {
		desc := ArgDescription{Index: i + 1}
		if token.Literal != nil {
			desc.Type = "literal"
			desc.Value = *token.Literal
		} else {
			desc.Type = "param"
			desc.Value = *token.Param
		}
		args = append(args, desc)
	}
	return args, nil
}

func validateCommandKey(command string) error {
	switch {
	case command == "":
		return errors.New("command is required")
	case command == "." || command == "..":
		return fmt.Errorf("command %q is not a valid logical allowlist key", command)
	case strings.HasPrefix(command, "builtin:"):
		return fmt.Errorf("command %q must not use reserved builtin: target syntax", command)
	case strings.ContainsAny(command, `/\`):
		return fmt.Errorf("command %q must be a logical allowlist key, not a path", command)
	case hasWhitespaceOrControl(command):
		return fmt.Errorf("command %q must not contain whitespace or control characters", command)
	case !commandKeyPattern.MatchString(command):
		return fmt.Errorf("command %q must match %s", command, commandKeyPattern.String())
	default:
		return nil
	}
}

func hasWhitespaceOrControl(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func ensureEOF(dec *json.Decoder) error {
	var extra any
	err := dec.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("template file must contain exactly one JSON value")
}
