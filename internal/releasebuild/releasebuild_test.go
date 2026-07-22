package releasebuild

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseTargets(t *testing.T) {
	targets, err := ParseTargets("linux/amd64, windows/amd64 ")
	if err != nil {
		t.Fatalf("ParseTargets returned error: %v", err)
	}
	if len(targets) != 2 || targets[0] != (Target{GOOS: "linux", GOARCH: "amd64"}) || targets[1] != (Target{GOOS: "windows", GOARCH: "amd64"}) {
		t.Fatalf("unexpected targets: %+v", targets)
	}

	host, err := ParseTargets("host")
	if err != nil {
		t.Fatalf("ParseTargets host returned error: %v", err)
	}
	if len(host) != 1 || host[0] != (Target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}) {
		t.Fatalf("unexpected host target: %+v", host)
	}

	all, err := ParseTargets("all")
	if err != nil {
		t.Fatalf("ParseTargets all returned error: %v", err)
	}
	if len(all) != len(defaultTargets) {
		t.Fatalf("expected default target count %d, got %d", len(defaultTargets), len(all))
	}
}

func TestParseTargetsRejectsMalformedTarget(t *testing.T) {
	if _, err := ParseTargets("linux"); err == nil || !strings.Contains(err.Error(), "expected goos/goarch") {
		t.Fatalf("expected malformed target error, got %v", err)
	}
}

func TestPlanArtifactNamesAndBinaryLayout(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "repo")
	out := filepath.Join(string(filepath.Separator), "tmp", "dist")
	artifacts, err := Plan(Options{
		RootDir: root,
		OutDir:  out,
		Version: "dev",
		Targets: []Target{
			{GOOS: "linux", GOARCH: "arm64"},
			{GOOS: "darwin", GOARCH: "arm64"},
			{GOOS: "windows", GOARCH: "amd64"},
		},
	})
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(artifacts) != 3 {
		t.Fatalf("expected 3 artifacts, got %d", len(artifacts))
	}

	linux := artifacts[0]
	if linux.DirectoryName != "planetary-mesh-dev-linux-arm64" || linux.ArchiveName != "planetary-mesh-dev-linux-arm64.tar.gz" {
		t.Fatalf("unexpected linux artifact: %+v", linux)
	}
	requirePlannedBinary(t, linux, filepath.Join(out, "planetary-mesh-dev-linux-arm64", "bin", "coordinator"))
	requirePlannedBinary(t, linux, filepath.Join(out, "planetary-mesh-dev-linux-arm64", "workloads", "text-stats"))
	requirePlannedCopyDestination(t, linux, filepath.Join(out, "planetary-mesh-dev-linux-arm64", "templates", "text-stats.pmtemplate.json"))
	requirePlannedCopyDestination(t, linux, filepath.Join(out, "planetary-mesh-dev-linux-arm64", "docs", "runbooks", "linux-service-install.md"))
	requirePlannedCopyDestination(t, linux, filepath.Join(out, "planetary-mesh-dev-linux-arm64", "install", "install-linux.sh"))
	requirePlannedCopyDestination(t, linux, filepath.Join(out, "planetary-mesh-dev-linux-arm64", "install", "uninstall-linux.sh"))
	requirePlannedCopyDestination(t, linux, filepath.Join(out, "planetary-mesh-dev-linux-arm64", "install", "systemd", "planetary-mesh-coordinator.service"))
	requirePlannedCopyDestination(t, linux, filepath.Join(out, "planetary-mesh-dev-linux-arm64", "install", "systemd", "planetary-mesh-agent.service"))

	darwin := artifacts[1]
	requirePlannedBinary(t, darwin, filepath.Join(out, "planetary-mesh-dev-darwin-arm64", "bin", "coordinator"))
	requireNoPlannedCopyDestination(t, darwin, filepath.Join(out, "planetary-mesh-dev-darwin-arm64", "install", "install-linux.sh"))
	requireNoPlannedCopyDestination(t, darwin, filepath.Join(out, "planetary-mesh-dev-darwin-arm64", "docs", "runbooks", "linux-service-install.md"))

	windows := artifacts[2]
	if windows.DirectoryName != "planetary-mesh-dev-windows-amd64" || windows.ArchiveName != "planetary-mesh-dev-windows-amd64.zip" {
		t.Fatalf("unexpected windows artifact: %+v", windows)
	}
	requirePlannedBinary(t, windows, filepath.Join(out, "planetary-mesh-dev-windows-amd64", "bin", "coordinator.exe"))
	requirePlannedBinary(t, windows, filepath.Join(out, "planetary-mesh-dev-windows-amd64", "workloads", "text-stats.exe"))
	requirePlannedCopy(t, windows, filepath.Join(root, "config", "agent-1.env.example"))
	requirePlannedCopyDestination(t, windows, filepath.Join(out, "planetary-mesh-dev-windows-amd64", "templates", "README.md"))
	requireNoPlannedCopyDestination(t, windows, filepath.Join(out, "planetary-mesh-dev-windows-amd64", "install", "install-linux.sh"))
	requireNoPlannedCopyDestination(t, windows, filepath.Join(out, "planetary-mesh-dev-windows-amd64", "docs", "runbooks", "linux-service-install.md"))
}

func TestPlanRejectsUnsafeVersion(t *testing.T) {
	_, err := Plan(Options{
		RootDir: "/repo",
		OutDir:  "/tmp/dist",
		Version: "../dev",
		Targets: []Target{{GOOS: "linux", GOARCH: "amd64"}},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("expected invalid version error, got %v", err)
	}
}

func TestBinaryName(t *testing.T) {
	if got := BinaryName("pmctl", "windows"); got != "pmctl.exe" {
		t.Fatalf("expected pmctl.exe, got %q", got)
	}
	if got := BinaryName("pmctl", "linux"); got != "pmctl" {
		t.Fatalf("expected pmctl, got %q", got)
	}
}

func requirePlannedBinary(t *testing.T, artifact PlanArtifact, destination string) {
	t.Helper()
	for _, binary := range artifact.Binaries {
		if binary.Destination == destination {
			return
		}
	}
	t.Fatalf("missing planned binary destination %q in %+v", destination, artifact.Binaries)
}

func requirePlannedCopy(t *testing.T, artifact PlanArtifact, source string) {
	t.Helper()
	for _, copySpec := range artifact.Copies {
		if copySpec.Source == source {
			return
		}
	}
	t.Fatalf("missing planned copy source %q in %+v", source, artifact.Copies)
}

func requirePlannedCopyDestination(t *testing.T, artifact PlanArtifact, destination string) {
	t.Helper()
	for _, copySpec := range artifact.Copies {
		if copySpec.Destination == destination {
			return
		}
	}
	t.Fatalf("missing planned copy destination %q in %+v", destination, artifact.Copies)
}

func requireNoPlannedCopyDestination(t *testing.T, artifact PlanArtifact, destination string) {
	t.Helper()
	for _, copySpec := range artifact.Copies {
		if copySpec.Destination == destination {
			t.Fatalf("unexpected planned copy destination %q in %+v", destination, artifact.Copies)
		}
	}
}
