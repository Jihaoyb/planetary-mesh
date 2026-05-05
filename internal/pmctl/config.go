package pmctl

import (
	"fmt"
	"os"

	"planetary-mesh/internal/configfile"
	"planetary-mesh/internal/security"
)

const (
	defaultPMCTLConfigPath = "config/pmctl.env"
	pmctlConfigFileEnv     = "PMCTL_CONFIG_FILE"
)

func ConfigFromEnv() Config {
	return Config{
		CoordinatorURL: os.Getenv("PMCTL_COORDINATOR_URL"),
		TLSFiles: security.TLSFiles{
			CAFile:   os.Getenv("PMCTL_TLS_CA_FILE"),
			CertFile: os.Getenv("PMCTL_TLS_CERT_FILE"),
			KeyFile:  os.Getenv("PMCTL_TLS_KEY_FILE"),
		},
	}
}

func ConfigFromSources(args []string) (Config, error) {
	pathCfg := Config{}
	if _, _, err := parseGlobalFlags(args, &pathCfg); err != nil {
		return Config{}, err
	}

	path, err := configfile.ResolvePath(pathCfg.ConfigFile, os.Getenv(pmctlConfigFileEnv), defaultPMCTLConfigPath)
	if err != nil {
		return Config{}, fmt.Errorf("resolve config file: %w", err)
	}

	fileValues := map[string]string{}
	if path != "" {
		fileValues, err = configfile.Load(path)
		if err != nil {
			return Config{}, fmt.Errorf("load config file %s: %w", path, err)
		}
	}
	source := configSource{fileValues: fileValues}

	return Config{
		ConfigFile:     path,
		CoordinatorURL: source.get("PMCTL_COORDINATOR_URL", ""),
		TLSFiles: security.TLSFiles{
			CAFile:   source.get("PMCTL_TLS_CA_FILE", ""),
			CertFile: source.get("PMCTL_TLS_CERT_FILE", ""),
			KeyFile:  source.get("PMCTL_TLS_KEY_FILE", ""),
		},
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
