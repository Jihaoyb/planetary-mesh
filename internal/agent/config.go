package agent

import (
	"fmt"
	"strings"
	"time"
)

const (
	DefaultExecutionTimeout = 30 * time.Second
	DefaultAllowlist        = "echo=echo,false=false,sleep=sleep"
)

type ExecutorConfig struct {
	Allowlist map[string]string
	Timeout   time.Duration
}

func DefaultExecutorConfig() ExecutorConfig {
	allowlist, _ := ParseAllowlist(DefaultAllowlist)
	return ExecutorConfig{
		Allowlist: allowlist,
		Timeout:   DefaultExecutionTimeout,
	}
}

func ParseAllowlist(raw string) (map[string]string, error) {
	out := make(map[string]string)
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}

	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid allowlist entry %q", entry)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" || value == "" {
			return nil, fmt.Errorf("invalid allowlist entry %q", entry)
		}

		out[key] = value
	}

	return out, nil
}
