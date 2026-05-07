package configfile

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDemoScriptSyntax(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not available")
	}

	path := filepath.Join("..", "..", "examples", "demo.sh")
	cmd := exec.Command(bash, "-n", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash -n %s failed: %v\n%s", path, err, out)
	}
}
