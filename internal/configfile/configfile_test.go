package configfile

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseEnvStyleConfig(t *testing.T) {
	got, err := Parse("test.env", strings.NewReader(`
# comment
COORDINATOR_ADDR=:8080
SPACED = value with spaces
DOUBLE_QUOTED="hello\nmesh"
SINGLE_QUOTED='literal value'
EMPTY=
DUPLICATE=first
DUPLICATE=second
HASH=value # not a comment
`))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	want := map[string]string{
		"COORDINATOR_ADDR": ":8080",
		"SPACED":           "value with spaces",
		"DOUBLE_QUOTED":    "hello\nmesh",
		"SINGLE_QUOTED":    "literal value",
		"EMPTY":            "",
		"DUPLICATE":        "second",
		"HASH":             "value # not a comment",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse mismatch\nwant: %#v\n got: %#v", want, got)
	}
}

func TestParseRejectsMalformedLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "missing equals", in: "COORDINATOR_ADDR\n", want: "bad.env:1: expected KEY=value"},
		{name: "empty key", in: "=value\n", want: `bad.env:1: invalid key ""`},
		{name: "invalid key", in: "BAD-KEY=value\n", want: `bad.env:1: invalid key "BAD-KEY"`},
		{name: "unterminated double quote", in: "KEY=\"value\n", want: "bad.env:1: unterminated double-quoted value"},
		{name: "unterminated single quote", in: "KEY='value\n", want: "bad.env:1: unterminated single-quoted value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("bad.env", strings.NewReader(tt.in))
			if err == nil {
				t.Fatalf("expected error")
			}
			if err.Error() != tt.want {
				t.Fatalf("unexpected error\nwant: %s\n got: %s", tt.want, err)
			}
		})
	}
}

func TestLoadReturnsMissingFileError(t *testing.T) {
	_, err := Load("does-not-exist.env")
	if err == nil {
		t.Fatalf("expected missing file error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}
