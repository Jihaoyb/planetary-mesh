package configfile

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestShellScriptSyntax(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not available")
	}

	patterns := []string{
		filepath.Join("..", "..", "examples", "*.sh"),
		filepath.Join("..", "..", "packaging", "linux", "*.sh"),
	}
	var paths []string
	for _, pattern := range patterns {
		matched, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		paths = append(paths, matched...)
	}
	if len(paths) == 0 {
		t.Fatalf("expected at least one example shell script")
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			cmd := exec.Command(bash, "-n", path)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("bash -n %s failed: %v\n%s", path, err, out)
			}
		})
	}
}
