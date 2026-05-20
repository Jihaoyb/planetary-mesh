package agent

import (
	"fmt"
	"strings"
	"time"

	"planetary-mesh/internal/protocol"
)

const (
	DefaultExecutionTimeout = 30 * time.Second
	DefaultAllowlist        = "echo=echo,false=false,sleep=sleep"
	DefaultCapabilities     = ""
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

func ParseCapabilities(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return []string{}, nil
	}

	parts := strings.Split(raw, ",")
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		label := strings.TrimSpace(part)
		if label == "" {
			continue
		}
		labels = append(labels, label)
	}
	return protocol.NormalizeNodeCapabilities(labels)
}
