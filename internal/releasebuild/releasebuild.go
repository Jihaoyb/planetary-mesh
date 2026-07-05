package releasebuild

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const artifactPrefix = "planetary-mesh"

var defaultTargets = []Target{
	{GOOS: "darwin", GOARCH: "arm64"},
	{GOOS: "darwin", GOARCH: "amd64"},
	{GOOS: "linux", GOARCH: "amd64"},
	{GOOS: "linux", GOARCH: "arm64"},
	{GOOS: "windows", GOARCH: "amd64"},
}

var plannedBinaries = []plannedBinary{
	{Name: "coordinator", Package: "./cmd/coordinator", Directory: "bin"},
	{Name: "agent", Package: "./cmd/agent", Directory: "bin"},
	{Name: "pmctl", Package: "./cmd/pmctl", Directory: "bin"},
	{Name: "text-stats", Package: "./examples/workloads/text-stats", Directory: "workloads"},
}

var plannedCopies = []plannedCopy{
	{Source: "config/coordinator.env.example", Destination: "config/coordinator.env.example"},
	{Source: "config/agent-1.env.example", Destination: "config/agent-1.env.example"},
	{Source: "config/agent-2.env.example", Destination: "config/agent-2.env.example"},
	{Source: "config/pmctl.env.example", Destination: "config/pmctl.env.example"},
	{Source: "examples/templates/text-stats.pmtemplate.json", Destination: "templates/text-stats.pmtemplate.json"},
	{Source: "examples/templates/README.md", Destination: "templates/README.md"},
	{Source: "README.md", Destination: "README.md"},
	{Source: "docs/runbooks/local-release-install.md", Destination: "docs/runbooks/local-release-install.md", Optional: true},
	{Source: "docs/runbooks/practical-workload-recipe.md", Destination: "docs/runbooks/practical-workload-recipe.md"},
	{Source: "docs/runbooks/command-execution-safety.md", Destination: "docs/runbooks/command-execution-safety.md"},
}

type Target struct {
	GOOS   string
	GOARCH string
}

func (t Target) String() string {
	return t.GOOS + "/" + t.GOARCH
}

type Options struct {
	RootDir string
	OutDir  string
	Version string
	Targets []Target
}

type Artifact struct {
	Target      Target
	Directory   string
	Archive     string
	ArchiveName string
}

type PlanArtifact struct {
	Target        Target
	DirectoryName string
	Directory     string
	ArchiveName   string
	Archive       string
	Binaries      []PlannedBinary
	Copies        []PlannedCopy
}

type PlannedBinary struct {
	Name        string
	Package     string
	Destination string
}

type PlannedCopy struct {
	Source      string
	Destination string
	Optional    bool
}

type plannedBinary struct {
	Name      string
	Package   string
	Directory string
}

type plannedCopy struct {
	Source      string
	Destination string
	Optional    bool
}

func DefaultTargets() []Target {
	return append([]Target(nil), defaultTargets...)
}

func HostTarget() Target {
	return Target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
}

func ParseTargets(raw string) ([]Target, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "all" {
		return DefaultTargets(), nil
	}
	if raw == "host" {
		return []Target{HostTarget()}, nil
	}

	parts := strings.Split(raw, ",")
	targets := make([]Target, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		goos, goarch, ok := strings.Cut(part, "/")
		if !ok || strings.TrimSpace(goos) == "" || strings.TrimSpace(goarch) == "" {
			return nil, fmt.Errorf("invalid target %q, expected goos/goarch", part)
		}
		targets = append(targets, Target{GOOS: strings.TrimSpace(goos), GOARCH: strings.TrimSpace(goarch)})
	}
	return targets, nil
}

func Plan(opts Options) ([]PlanArtifact, error) {
	if err := validateVersion(opts.Version); err != nil {
		return nil, err
	}
	if opts.RootDir == "" {
		return nil, errors.New("root dir is required")
	}
	if opts.OutDir == "" {
		return nil, errors.New("output dir is required")
	}
	targets := opts.Targets
	if len(targets) == 0 {
		targets = DefaultTargets()
	}

	artifacts := make([]PlanArtifact, 0, len(targets))
	for _, target := range targets {
		if strings.TrimSpace(target.GOOS) == "" || strings.TrimSpace(target.GOARCH) == "" {
			return nil, fmt.Errorf("invalid target %q", target.String())
		}

		dirName := DirectoryName(opts.Version, target)
		artifact := PlanArtifact{
			Target:        target,
			DirectoryName: dirName,
			Directory:     filepath.Join(opts.OutDir, dirName),
			ArchiveName:   ArchiveName(opts.Version, target),
		}
		artifact.Archive = filepath.Join(opts.OutDir, artifact.ArchiveName)

		for _, binary := range plannedBinaries {
			artifact.Binaries = append(artifact.Binaries, PlannedBinary{
				Name:        binary.Name,
				Package:     binary.Package,
				Destination: filepath.Join(artifact.Directory, binary.Directory, BinaryName(binary.Name, target.GOOS)),
			})
		}
		for _, copySpec := range plannedCopies {
			artifact.Copies = append(artifact.Copies, PlannedCopy{
				Source:      filepath.Join(opts.RootDir, filepath.FromSlash(copySpec.Source)),
				Destination: filepath.Join(artifact.Directory, filepath.FromSlash(copySpec.Destination)),
				Optional:    copySpec.Optional,
			})
		}

		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

func Build(ctx context.Context, opts Options) ([]Artifact, error) {
	plan, err := Plan(opts)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	artifacts := make([]Artifact, 0, len(plan))
	for _, artifact := range plan {
		if err := os.RemoveAll(artifact.Directory); err != nil {
			return nil, fmt.Errorf("clean layout %s: %w", artifact.Directory, err)
		}
		if err := os.Remove(artifact.Archive); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("clean archive %s: %w", artifact.Archive, err)
		}
		if err := os.MkdirAll(artifact.Directory, 0o755); err != nil {
			return nil, fmt.Errorf("create layout %s: %w", artifact.Directory, err)
		}

		for _, binary := range artifact.Binaries {
			if err := os.MkdirAll(filepath.Dir(binary.Destination), 0o755); err != nil {
				return nil, fmt.Errorf("create binary dir: %w", err)
			}
			if err := buildBinary(ctx, opts.RootDir, artifact.Target, binary.Package, binary.Destination); err != nil {
				return nil, err
			}
		}
		for _, copySpec := range artifact.Copies {
			if err := copyFile(copySpec.Source, copySpec.Destination, copySpec.Optional); err != nil {
				return nil, err
			}
		}
		if err := createArchive(artifact.DirectoryName, artifact.Directory, artifact.Archive, artifact.Target.GOOS); err != nil {
			return nil, err
		}

		artifacts = append(artifacts, Artifact{
			Target:      artifact.Target,
			Directory:   artifact.Directory,
			Archive:     artifact.Archive,
			ArchiveName: artifact.ArchiveName,
		})
	}
	return artifacts, nil
}

func DirectoryName(version string, target Target) string {
	return fmt.Sprintf("%s-%s-%s-%s", artifactPrefix, version, target.GOOS, target.GOARCH)
}

func ArchiveName(version string, target Target) string {
	name := DirectoryName(version, target)
	if target.GOOS == "windows" {
		return name + ".zip"
	}
	return name + ".tar.gz"
}

func BinaryName(name, goos string) string {
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}

func validateVersion(version string) error {
	if strings.TrimSpace(version) == "" {
		return errors.New("version is required")
	}
	for _, r := range version {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return fmt.Errorf("invalid version %q: use only letters, numbers, dots, underscores, and dashes", version)
		}
	}
	return nil
}

func buildBinary(ctx context.Context, root string, target Target, pkg, output string) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", output, pkg)
	cmd.Dir = root
	cmd.Env = withBuildEnv(os.Environ(), target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build %s for %s: %w\n%s", pkg, target.String(), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func withBuildEnv(env []string, target Target) []string {
	out := withoutEnv(env, "GOOS", "GOARCH", "CGO_ENABLED")
	out = append(out, "GOOS="+target.GOOS, "GOARCH="+target.GOARCH, "CGO_ENABLED=0")
	return out
}

func withoutEnv(env []string, keys ...string) []string {
	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keySet[key] = struct{}{}
	}
	out := env[:0]
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, remove := keySet[key]; remove {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func copyFile(source, destination string, optional bool) error {
	in, err := os.Open(source)
	if err != nil {
		if optional && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", source, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create copy dir: %w", err)
	}

	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create %s: %w", destination, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s to %s: %w", source, destination, err)
	}
	return nil
}

func createArchive(baseName, sourceDir, archivePath, goos string) error {
	if goos == "windows" {
		return createZip(baseName, sourceDir, archivePath)
	}
	return createTarGz(baseName, sourceDir, archivePath)
}

func createTarGz(baseName, sourceDir, archivePath string) error {
	file, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("create tar.gz %s: %w", archivePath, err)
	}
	defer file.Close()

	gz := gzip.NewWriter(file)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	return walkArchiveFiles(sourceDir, func(path, name string, info os.FileInfo) error {
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(filepath.Join(baseName, name))
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		return writeFileToArchive(path, tw)
	})
}

func createZip(baseName, sourceDir, archivePath string) error {
	file, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("create zip %s: %w", archivePath, err)
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	defer zw.Close()

	return walkArchiveFiles(sourceDir, func(path, name string, info os.FileInfo) error {
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(filepath.Join(baseName, name))
		header.Method = zip.Deflate
		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		return writeFileToArchive(path, writer)
	})
}

func walkArchiveFiles(root string, visit func(path, name string, info os.FileInfo) error) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return visit(path, name, info)
	})
}

func writeFileToArchive(path string, writer io.Writer) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(writer, file)
	return err
}
