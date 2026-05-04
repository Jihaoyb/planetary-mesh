package configfile

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Load reads a KEY=value config file.
func Load(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return Parse(path, file)
}

// ResolvePath chooses the config file path to load.
func ResolvePath(flagPath, envPath, defaultPath string) (string, error) {
	if strings.TrimSpace(flagPath) != "" {
		return flagPath, nil
	}
	if strings.TrimSpace(envPath) != "" {
		return envPath, nil
	}
	if strings.TrimSpace(defaultPath) == "" {
		return "", nil
	}
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return "", nil
}

// Parse reads env-style KEY=value settings from r.
func Parse(name string, r io.Reader) (map[string]string, error) {
	out := make(map[string]string)
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSuffix(scanner.Text(), "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, lineError(name, lineNo, "expected KEY=value")
		}
		key = strings.TrimSpace(key)
		if !validKey(key) {
			return nil, lineError(name, lineNo, "invalid key %q", key)
		}

		parsedValue, err := parseValue(strings.TrimSpace(value))
		if err != nil {
			return nil, lineError(name, lineNo, "%v", err)
		}
		out[key] = parsedValue
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	switch raw[0] {
	case '"':
		if !strings.HasSuffix(raw, `"`) || len(raw) == 1 {
			return "", fmt.Errorf("unterminated double-quoted value")
		}
		value, err := strconv.Unquote(raw)
		if err != nil {
			return "", fmt.Errorf("invalid double-quoted value: %w", err)
		}
		return value, nil
	case '\'':
		if !strings.HasSuffix(raw, "'") || len(raw) == 1 {
			return "", fmt.Errorf("unterminated single-quoted value")
		}
		return raw[1 : len(raw)-1], nil
	default:
		return raw, nil
	}
}

func validKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func lineError(name string, lineNo int, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if name == "" {
		return fmt.Errorf("line %d: %s", lineNo, msg)
	}
	return fmt.Errorf("%s:%d: %s", name, lineNo, msg)
}
