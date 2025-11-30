package main

import (
	"log"
	"net/http"
)

// main wires config, coordinator registration, heartbeat, and HTTP server.
func main() {
	addr := getEnv("AGENT_ADDR", ":8081")
	coordURL := getEnv("COORDINATOR_URL", "http://localhost:8080")
	nodeID := getEnv("NODE_ID", defaultNodeID())

	if err := registerWithCoordinator(coordURL, nodeID, addr); err != nil {
		log.Printf("[agent] failed to register with coordinator: %v", err)
	} else {
		log.Printf("[agent] registered with coordinator as %q", nodeID)
	}

	// Start periodic heartbeat
	startHeartbeatLoop(coordURL, nodeID, addr)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/execute", executeHandler)

	log.Printf("[agent] starting on %s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[agent] server error: %v", err)
	}
}

// healthHandler handles /healthz as before.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
