package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type textStats struct {
	Lines         int
	NonEmptyLines int
	Words         int
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "text-stats: expected exactly 1 file path argument")
		return 2
	}

	stats, err := collectTextStats(args[0])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "text-stats: %v\n", err)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "lines=%d\n", stats.Lines)
	_, _ = fmt.Fprintf(stdout, "non_empty_lines=%d\n", stats.NonEmptyLines)
	_, _ = fmt.Fprintf(stdout, "words=%d\n", stats.Words)
	return 0
}

func collectTextStats(path string) (textStats, error) {
	file, err := os.Open(path)
	if err != nil {
		return textStats{}, err
	}
	defer func() {
		_ = file.Close()
	}()

	return scanTextStats(file)
}

func scanTextStats(r io.Reader) (textStats, error) {
	reader := bufio.NewReader(r)
	var stats textStats

	for {
		line, err := reader.ReadString('\n')
		switch {
		case err == nil:
			stats.recordLine(line)
		case errors.Is(err, io.EOF):
			if len(line) > 0 {
				stats.recordLine(line)
			}
			return stats, nil
		default:
			return textStats{}, err
		}
	}
}

func (s *textStats) recordLine(line string) {
	s.Lines++
	if strings.TrimSpace(line) != "" {
		s.NonEmptyLines++
	}
	s.Words += len(strings.Fields(line))
}
