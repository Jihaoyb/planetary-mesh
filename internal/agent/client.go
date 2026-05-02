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
	ID      string `json:"id"`
	Address string `json:"address"`
}

// RegisterWithCoordinator sends a POST /register to the coordinator.
func RegisterWithCoordinator(coordBaseURL, nodeID, addr string) error {
	return RegisterWithCoordinatorClient(http.DefaultClient, coordBaseURL, nodeID, addr)
}

func RegisterWithCoordinatorClient(client *http.Client, coordBaseURL, nodeID, addr string) error {
	if client == nil {
		client = http.DefaultClient
	}
	payload := registerPayload{
		ID:      nodeID,
		Address: addr,
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
	if client == nil {
		client = http.DefaultClient
	}
	interval := 10 * time.Second

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := RegisterWithCoordinatorClient(client, coordBaseURL, nodeID, addr); err != nil {
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
