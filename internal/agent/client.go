package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"planetary-mesh/internal/protocol"
)

// registerPayload matches what the coordinator expects at /register.
type registerPayload struct {
	ID           string            `json:"id"`
	Address      string            `json:"address"`
	Capabilities []string          `json:"capabilities"`
	Load         protocol.NodeLoad `json:"load"`
}

type RegistrationMetadata struct {
	Capabilities []string
	Load         protocol.NodeLoad
}

// RegisterWithCoordinator sends a POST /register to the coordinator.
func RegisterWithCoordinator(coordBaseURL, nodeID, addr string) error {
	return RegisterWithCoordinatorClient(http.DefaultClient, coordBaseURL, nodeID, addr)
}

func RegisterWithCoordinatorClient(client *http.Client, coordBaseURL, nodeID, addr string) error {
	return RegisterWithCoordinatorClientWithMetadata(client, coordBaseURL, nodeID, addr, RegistrationMetadata{})
}

func RegisterWithCoordinatorClientWithMetadata(client *http.Client, coordBaseURL, nodeID, addr string, metadata RegistrationMetadata) error {
	if client == nil {
		client = http.DefaultClient
	}
	capabilities, err := protocol.NormalizeNodeCapabilities(metadata.Capabilities)
	if err != nil {
		return fmt.Errorf("invalid capabilities: %w", err)
	}
	if err := protocol.ValidateNodeLoad(metadata.Load); err != nil {
		return fmt.Errorf("invalid load: %w", err)
	}
	payload := registerPayload{
		ID:           nodeID,
		Address:      addr,
		Capabilities: capabilities,
		Load:         metadata.Load,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	url := coordBaseURL + "/register"
	slog.Info("agent registering", "coord_url", url, "node_id", nodeID, "addr", addr)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	protocol.SetVersionHeader(req.Header)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post to coordinator: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status from coordinator: %s", resp.Status)
	}

	return nil
}

// StartHeartbeatLoop periodically calls RegisterWithCoordinator to act as a heartbeat.
// It stops when stopCh is closed.
func StartHeartbeatLoop(coordBaseURL, nodeID, addr string, stopCh <-chan struct{}) {
	StartHeartbeatLoopWithClient(http.DefaultClient, coordBaseURL, nodeID, addr, stopCh)
}

func StartHeartbeatLoopWithClient(client *http.Client, coordBaseURL, nodeID, addr string, stopCh <-chan struct{}) {
	StartHeartbeatLoopWithClientAndMetadata(client, coordBaseURL, nodeID, addr, nil, stopCh)
}

func StartHeartbeatLoopWithClientAndMetadata(
	client *http.Client,
	coordBaseURL, nodeID, addr string,
	metadataProvider func() RegistrationMetadata,
	stopCh <-chan struct{},
) {
	if client == nil {
		client = http.DefaultClient
	}
	if metadataProvider == nil {
		metadataProvider = func() RegistrationMetadata { return RegistrationMetadata{} }
	}
	interval := 10 * time.Second

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := RegisterWithCoordinatorClientWithMetadata(client, coordBaseURL, nodeID, addr, metadataProvider()); err != nil {
					slog.Warn("heartbeat failed", "err", err)
				} else {
					slog.Debug("heartbeat ok")
				}
			case <-stopCh:
				return
			}
		}
	}()
}

// DefaultNodeID returns the hostname or a fallback.
func DefaultNodeID() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "agent-1"
}
