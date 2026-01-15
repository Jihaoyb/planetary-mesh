package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// registerPayload matches what the coordinator expects at /register.
type registerPayload struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

// registerWithCoordinator sends a POST /register to the coordinator.
func registerWithCoordinator(coordBaseURL, nodeID, addr string, client *http.Client) error {
	payload := registerPayload{
		ID:      nodeID,
		Address: addr, // For now we just send the listen address (e.g., ":8081").
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	url := coordBaseURL + "/register"
	log.Printf("[agent] registering with coordinator at %s", url)

	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	timeout := client.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

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

// startHeartbeatLoop periodically calls registerWithCoordinator to act as a heartbeat.
// Returns a stop function to halt the ticker.
func startHeartbeatLoop(coordBaseURL, nodeID, addr string, interval time.Duration, client *http.Client) func() {
	ticker := time.NewTicker(interval)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				if err := registerWithCoordinator(coordBaseURL, nodeID, addr, client); err != nil {
					log.Printf("[agent] heartbeat failed: %v", err)
				} else {
					log.Printf("[agent] heartbeat OK")
				}
			case <-stop:
				ticker.Stop()
				return
			}
		}
	}()

	return func() {
		close(stop)
	}
}

// defaultNodeID tries to use the hostname as a default ID.
func defaultNodeID() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "agent-1"
}

// getEnv reads an environment variable or returns the default.
func getEnv(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}
