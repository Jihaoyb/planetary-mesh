package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// server holds dependencies for HTTP handlers.
type server struct {
	registry *NodeRegistry
}

// registerRequest is the JSON payload agents send to /register.
type registerRequest struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

// healthHandler is a basic health check.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handleRegister handles POST /register from agents.
// We treat each call as both registration and heartbeat.
func (s *server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[coordinator] failed to decode register request: %v", err)
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.ID == "" || req.Address == "" {
		http.Error(w, "id and address are required", http.StatusBadRequest)
		return
	}

	node := s.registry.Register(req.ID, req.Address)
	log.Printf("[coordinator] node registered/heartbeat: id=%s addr=%s", node.ID, node.Address)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(node); err != nil {
		log.Printf("[coordinator] failed to encode register response: %v", err)
	}
}

// handleListNodes handles GET /nodes and returns all registered nodes.
func (s *server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	nodes := s.registry.List()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(nodes); err != nil {
		log.Printf("[coordinator] failed to encode nodes: %v", err)
	}
}
