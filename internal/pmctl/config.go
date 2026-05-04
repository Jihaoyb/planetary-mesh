package pmctl

import (
	"os"

	"planetary-mesh/internal/security"
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
