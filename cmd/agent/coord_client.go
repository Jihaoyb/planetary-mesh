package main

import (
	"bytes"
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
func registerWithCoordinator(coordBaseURL, nodeID, addr string) error {
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

	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
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
func startHeartbeatLoop(coordBaseURL, nodeID, addr string) {
	interval := 10 * time.Second // how often to send heartbeats

	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if err := registerWithCoordinator(coordBaseURL, nodeID, addr); err != nil {
				log.Printf("[agent] heartbeat failed: %v", err)
			} else {
				log.Printf("[agent] heartbeat OK")
			}
		}
	}()
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
