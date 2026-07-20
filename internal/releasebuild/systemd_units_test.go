package releasebuild

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxSystemdUnits(t *testing.T) {
	tests := []struct {
		role     string
		required []string
	}{
		{
			role: "coordinator",
			required: []string{
				"Description=Planetary Mesh Coordinator",
				"User=planetary-mesh-coordinator",
				"Group=planetary-mesh-coordinator",
				"WorkingDirectory=/var/lib/planetary-mesh/coordinator",
				"ExecStart=/opt/planetary-mesh/bin/coordinator --config /etc/planetary-mesh/coordinator.env",
			},
		},
		{
			role: "agent",
			required: []string{
				"Description=Planetary Mesh Agent",
				"User=planetary-mesh-agent",
				"Group=planetary-mesh-agent",
				"WorkingDirectory=/var/lib/planetary-mesh/agent",
				"ExecStart=/opt/planetary-mesh/bin/agent --config /etc/planetary-mesh/agent.env",
			},
		},
	}

	sharedRequired := []string{
		"Wants=network-online.target",
		"After=network-online.target",
		"Type=exec",
		"Restart=on-failure",
		"RestartSec=5s",
		"TimeoutStartSec=30s",
		"TimeoutStopSec=15s",
		"KillSignal=SIGTERM",
		"KillMode=control-group",
		"StandardOutput=journal",
		"StandardError=journal",
		"NoNewPrivileges=true",
		"ProtectSystem=full",
		"WantedBy=multi-user.target",
	}
	forbidden := []string{
		"DynamicUser=",
		"EnvironmentFile=",
		"PrivateDevices=",
		"PrivateNetwork=",
		"PrivateTmp=",
		"ProtectHome=",
		"RestrictAddressFamilies=",
		"RestrictNamespaces=",
		"SystemCallFilter=",
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			path := filepath.Join("..", "..", "packaging", "linux", "systemd", "planetary-mesh-"+tt.role+".service")
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := string(contents)
			for _, line := range append(sharedRequired, tt.required...) {
				if !strings.Contains(text, line+"\n") {
					t.Errorf("%s is missing %q", path, line)
				}
			}
			for _, directive := range forbidden {
				if strings.Contains(text, directive) {
					t.Errorf("%s must not contain %q", path, directive)
				}
			}
		})
	}
}
