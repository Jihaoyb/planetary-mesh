package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	// Coordinator listen address, default :8080.
	addr := getEnv("COORDINATOR_ADDR", ":8080")

	// In-memory node registry.
	registry := NewNodeRegistry()
	srv := &server{registry: registry}

	// Start background health checker for nodes.
	startHealthChecker(registry)

	// HTTP routing.
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/register", srv.handleRegister)
	mux.HandleFunc("/nodes", srv.handleListNodes)

	log.Printf("[coordinator] starting on %s\n", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[coordinator] server error: %v", err)
	}
}

// getEnv reads an environment variable, or returns a default if not set.
func getEnv(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}
