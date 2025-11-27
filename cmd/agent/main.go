package main

import (
	"log"
	"net/http"
)

// main wires config, coordinator registration, heartbeat, and HTTP server.
func main() {
	// Address this agent will listen on (for now, just a port).
	addr := getEnv("AGENT_ADDR", ":8081")

	// Where the coordinator is (base URL).
	// Example: "http://localhost:8080"
	coordURL := getEnv("COORDINATOR_URL", "http://localhost:8080")

	// ID for this node. Default to hostname if not explicitly set.
	nodeID := getEnv("NODE_ID", defaultNodeID())

	// Initial registration attempt.
	if err := registerWithCoordinator(coordURL, nodeID, addr); err != nil {
		log.Printf("[agent] failed to register with coordinator: %v", err)
	} else {
		log.Printf("[agent] registered with coordinator as %q", nodeID)
	}

	// Start periodic heartbeat loop (best-effort).
	startHeartbeatLoop(coordURL, nodeID, addr)

	// HTTP server for health checks (and later: task endpoints).
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)

	log.Printf("[agent] starting on %s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[agent] server error: %v", err)
	}
}

// healthHandler handles /healthz as before.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
