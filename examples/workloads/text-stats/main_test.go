package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunTextStats(t *testing.T) {
	path := writeInput(t, "input.txt", "alpha\nbeta\ngamma\n")
	var stdout, stderr bytes.Buffer

	code := run([]string{path}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", code, stderr.String())
	}
	const want = "lines=3\nnon_empty_lines=3\nwords=3\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout:\n%s", stdout.String())
	}
	if stderr.String() != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestRunTextStatsHandlesCRLFAndBlankLines(t *testing.T) {
	path := writeInput(t, "input.txt", "alpha\r\n\r\nbeta gamma\r\n")
	var stdout, stderr bytes.Buffer

	code := run([]string{path}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", code, stderr.String())
	}
	const want = "lines=3\nnon_empty_lines=2\nwords=3\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout:\n%s", stdout.String())
	}
}

func TestRunTextStatsHandlesUnterminatedFinalLine(t *testing.T) {
	path := writeInput(t, "input.txt", "alpha\nbeta")
	var stdout, stderr bytes.Buffer

	code := run([]string{path}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", code, stderr.String())
	}
	const want = "lines=2\nnon_empty_lines=2\nwords=2\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout:\n%s", stdout.String())
	}
}

func TestRunTextStatsHandlesEmptyFile(t *testing.T) {
	path := writeInput(t, "empty.txt", "")
	var stdout, stderr bytes.Buffer

	code := run([]string{path}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", code, stderr.String())
	}
	const want = "lines=0\nnon_empty_lines=0\nwords=0\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout:\n%s", stdout.String())
	}
}

func TestRunTextStatsRejectsWrongArgumentCount(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "missing", args: nil},
		{name: "too many", args: []string{"one.txt", "two.txt"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run(tc.args, &stdout, &stderr)

			if code != 2 {
				t.Fatalf("expected exit code 2, got %d", code)
			}
			if stdout.String() != "" {
				t.Fatalf("expected empty stdout, got %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), "expected exactly 1 file path argument") {
				t.Fatalf("unexpected stderr: %q", stderr.String())
			}
		})
	}
}

func TestRunTextStatsReportsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.txt")
	var stdout, stderr bytes.Buffer

	code := run([]string{path}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if stdout.String() != "" {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "text-stats:") || !strings.Contains(stderr.String(), "missing.txt") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func writeInput(t *testing.T, name string, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	return path
}
