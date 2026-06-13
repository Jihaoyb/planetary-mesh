package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"planetary-mesh/internal/releasebuild"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("releasebuild", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	outDir := fs.String("out", "dist", "output directory for generated release artifacts")
	version := fs.String("version", "dev", "artifact version label")
	targetsRaw := fs.String("targets", "all", "targets to build: all, host, or comma-separated goos/goarch entries")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	root, err := os.Getwd()
	if err != nil {
		return err
	}
	targets, err := releasebuild.ParseTargets(*targetsRaw)
	if err != nil {
		return err
	}
	artifacts, err := releasebuild.Build(context.Background(), releasebuild.Options{
		RootDir: root,
		OutDir:  filepath.Clean(*outDir),
		Version: *version,
		Targets: targets,
	})
	if err != nil {
		return err
	}

	for _, artifact := range artifacts {
		fmt.Printf("built %s\n", artifact.Target)
		fmt.Printf("  layout: %s\n", artifact.Directory)
		fmt.Printf("  archive: %s\n", artifact.Archive)
	}
	return nil
}
