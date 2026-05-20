package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"planetary-mesh/internal/agent"
	"planetary-mesh/internal/configfile"
	"planetary-mesh/internal/security"
)

const (
	defaultAgentConfigPath = "config/agent.env"
	agentConfigFileEnv     = "AGENT_CONFIG_FILE"
)

type agentConfig struct {
	ConfigFile     string
	Addr           string
	CoordinatorURL string
	AdvertiseAddr  string
	NodeID         string
	Capabilities   []string
	TLSFiles       security.TLSFiles
	Executor       agent.ExecutorConfig
	SecureMode     bool
}

func loadAgentConfig(args []string) (agentConfig, error) {
	var cfgPath string
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfgPath, "config", "", "path to env-style config file")
	if err := fs.Parse(args); err != nil {
		return agentConfig{}, err
	}
	if fs.NArg() != 0 {
		return agentConfig{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	path, err := configfile.ResolvePath(cfgPath, os.Getenv(agentConfigFileEnv), defaultAgentConfigPath)
	if err != nil {
		return agentConfig{}, fmt.Errorf("resolve config file: %w", err)
	}

	fileValues := map[string]string{}
	if path != "" {
		fileValues, err = configfile.Load(path)
		if err != nil {
			return agentConfig{}, fmt.Errorf("load config file %s: %w", path, err)
		}
	}
	source := configSource{fileValues: fileValues}

	tlsFiles := security.TLSFiles{
		CAFile:   source.get("AGENT_TLS_CA_FILE", ""),
		CertFile: source.get("AGENT_TLS_CERT_FILE", ""),
		KeyFile:  source.get("AGENT_TLS_KEY_FILE", ""),
	}
	if err := tlsFiles.ValidateComplete("AGENT"); err != nil {
		return agentConfig{}, err
	}
	secureMode := tlsFiles.Configured()

	addr := source.get("AGENT_ADDR", ":8081")
	defaultCoordURL := "http://localhost:8080"
	if secureMode {
		defaultCoordURL = "https://localhost:8080"
	}
	coordURL := source.get("COORDINATOR_URL", defaultCoordURL)
	if secureMode && !strings.HasPrefix(coordURL, "https://") {
		return agentConfig{}, fmt.Errorf("secure agent mode requires COORDINATOR_URL to use https")
	}

	advertiseAddr := source.get("AGENT_ADVERTISE_ADDR", addr)
	if secureMode && !source.has("AGENT_ADVERTISE_ADDR") {
		advertiseAddr = security.HostPortToURL("https", addr)
	}

	timeoutRaw := source.get("AGENT_EXEC_TIMEOUT", agent.DefaultExecutionTimeout.String())
	timeout, err := time.ParseDuration(timeoutRaw)
	if err != nil {
		return agentConfig{}, fmt.Errorf("invalid AGENT_EXEC_TIMEOUT %q: %w", timeoutRaw, err)
	}
	allowlistRaw := source.get("AGENT_COMMAND_ALLOWLIST", agent.DefaultAllowlist)
	allowlist, err := agent.ParseAllowlist(allowlistRaw)
	if err != nil {
		return agentConfig{}, fmt.Errorf("invalid AGENT_COMMAND_ALLOWLIST %q: %w", allowlistRaw, err)
	}
	capabilitiesRaw := source.get("AGENT_CAPABILITIES", agent.DefaultCapabilities)
	capabilities, err := agent.ParseCapabilities(capabilitiesRaw)
	if err != nil {
		return agentConfig{}, fmt.Errorf("invalid AGENT_CAPABILITIES %q: %w", capabilitiesRaw, err)
	}

	return agentConfig{
		ConfigFile:     path,
		Addr:           addr,
		CoordinatorURL: coordURL,
		AdvertiseAddr:  advertiseAddr,
		NodeID:         source.get("NODE_ID", agent.DefaultNodeID()),
		Capabilities:   capabilities,
		TLSFiles:       tlsFiles,
		Executor:       agent.ExecutorConfig{Allowlist: allowlist, Timeout: timeout},
		SecureMode:     secureMode,
	}, nil
}

type configSource struct {
	fileValues map[string]string
}

func (s configSource) get(key, def string) string {
	if v, ok := s.fileValues[key]; ok {
		def = v
	}
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func (s configSource) has(key string) bool {
	if _, ok := s.fileValues[key]; ok {
		return true
	}
	_, ok := os.LookupEnv(key)
	return ok
}
