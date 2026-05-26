package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const builtinTargetPrefix = "builtin:"

type commandExitError struct {
	code int
}

func (e commandExitError) Error() string {
	return fmt.Sprintf("command exited with code %d", e.code)
}

func validateAllowlistTarget(target string) error {
	if !strings.HasPrefix(target, builtinTargetPrefix) {
		return nil
	}
	name := strings.TrimPrefix(target, builtinTargetPrefix)
	if !isKnownBuiltin(name) {
		return fmt.Errorf("unknown built-in target %q", target)
	}
	return nil
}

func isKnownBuiltin(name string) bool {
	switch name {
	case "echo", "false", "sleep", "line-count":
		return true
	default:
		return false
	}
}

func runAllowlistedTarget(ctx context.Context, target string, args []string, stdout, stderr io.Writer) error {
	if strings.HasPrefix(target, builtinTargetPrefix) {
		name := strings.TrimPrefix(target, builtinTargetPrefix)
		if !isKnownBuiltin(name) {
			return fmt.Errorf("unknown built-in target %q", target)
		}
		return runBuiltin(ctx, name, args, stdout, stderr)
	}

	cmd := exec.CommandContext(ctx, target, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func commandExitCode(err error) (int, bool) {
	var builtinErr commandExitError
	if errors.As(err, &builtinErr) {
		return builtinErr.code, true
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}

	return 0, false
}

func runBuiltin(ctx context.Context, name string, args []string, stdout, stderr io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	switch name {
	case "echo":
		_, err := fmt.Fprintln(stdout, strings.Join(args, " "))
		return err
	case "false":
		return commandExitError{code: 1}
	case "sleep":
		return runBuiltinSleep(ctx, args, stderr)
	case "line-count":
		return runBuiltinLineCount(ctx, args, stdout, stderr)
	default:
		return fmt.Errorf("unknown built-in target %q", builtinTargetPrefix+name)
	}
}

func runBuiltinSleep(ctx context.Context, args []string, stderr io.Writer) error {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "sleep: expected exactly 1 duration argument")
		return commandExitError{code: 2}
	}

	duration, err := parseBuiltinSleepDuration(args[0])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "sleep: invalid duration %q\n", args[0])
		return commandExitError{code: 2}
	}
	if duration < 0 {
		_, _ = fmt.Fprintf(stderr, "sleep: invalid negative duration %q\n", args[0])
		return commandExitError{code: 2}
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func parseBuiltinSleepDuration(raw string) (time.Duration, error) {
	if duration, err := time.ParseDuration(raw); err == nil {
		return duration, nil
	}

	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(seconds) * time.Second, nil
}

func runBuiltinLineCount(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "line-count: expected exactly 1 file path argument")
		return commandExitError{code: 2}
	}

	file, err := os.Open(args[0])
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "line-count: %v\n", err)
		return commandExitError{code: 1}
	}
	defer func() {
		_ = file.Close()
	}()

	reader := bufio.NewReader(file)
	lines := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		line, err := reader.ReadString('\n')
		switch {
		case err == nil:
			lines++
		case errors.Is(err, io.EOF):
			if len(line) > 0 {
				lines++
			}
			_, _ = fmt.Fprintf(stdout, "%d\n", lines)
			return nil
		default:
			_, _ = fmt.Fprintf(stderr, "line-count: %v\n", err)
			return commandExitError{code: 1}
		}
	}
}
