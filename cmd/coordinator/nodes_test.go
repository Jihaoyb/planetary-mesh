package main

import (
	"testing"
	"time"
)

// Test that Register creates nodes, updates exisiting ones, and List return them
func TestNodeRegistryRegisterAndList(t *testing.T) {
	reg := NewNodeRegistry()

	// first registration
	n1 := reg.Register("node-1", ":8081")
	if n1.ID != "node-1" {
		t.Fatalf("expected id node-1, got %s", n1.ID)
	}
	if n1.Address != ":8081" {
		t.Fatalf("expected address :8081, got %s", n1.Address)
	}
	if n1.State != NodeStateHealthy {
		t.Fatalf("expected state %s, got %s", NodeStateHealthy, n1.State)
	}

	// re-register same ID with a different address; should update
	n2 := reg.Register("node-1", ":9090")
	if n2.Address != ":9090" {
		t.Fatalf("expected address :9090, got %s", n2.Address)
	}

	// register a second node
	reg.Register("node-2", ":8082")

	nodes := reg.List()
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	byID := make(map[string]Node)
	for _, n := range nodes {
		byID[n.ID] = n
	}

	if byID["node-1"].Address != ":9090" {
		t.Errorf("node-1 address not updated; got %s", byID["node-1"].Address)
	}
	if byID["node-2"].Address != ":8082" {
		t.Errorf("node-2 address not updated; got %s", byID["node-2"].Address)
	}
}

// Test that UpdateHealthStates flips nodes into HEALTHY / SUSPECT / OFFLINE based on LastSeen and the provided thresholds
func TestNodeRegistryUpdateHealthStates(t *testing.T) {
	reg := NewNodeRegistry()
	now := time.Now().UTC()

	// manually insert nodes with different LastSeen values
	reg.mu.Lock()
	reg.nodes["healthy"] = &Node{
		ID:       "healthy",
		Address:  ":1",
		LastSeen: now.Add(-5 * time.Second),
		State:    NodeStateHealthy,
	}
	reg.nodes["suspect"] = &Node{
		ID:       "suspect",
		Address:  ":2",
		LastSeen: now.Add(-20 * time.Second),
		State:    NodeStateHealthy,
	}
	reg.nodes["offline"] = &Node{
		ID:       "offline",
		Address:  ":3",
		LastSeen: now.Add(-40 * time.Second),
		State:    NodeStateHealthy,
	}
	reg.mu.Unlock()

	suspectAfter := 15 * time.Second
	offlineAfter := 30 * time.Second

	reg.UpdateHealthStates(now, suspectAfter, offlineAfter)

	nodes := reg.List()
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}

	byID := make(map[string]Node)
	for _, n := range nodes {
		byID[n.ID] = n
	}

	if byID["healthy"].State != NodeStateHealthy {
		t.Errorf("expected 'healthy' to be HEALTHY, got %s, byID['healthy'].State", byID["healthy"].State)
	}
	if byID["suspect"].State != NodeStateSuspect {
		t.Errorf("expected 'suspect' to be SUSPECT, got %s", byID["suspect"].State)
	}
	if byID["offline"].State != NodeStateOffline {
		t.Errorf("expected 'offline' to be OFFLINE, got %s", byID["offline"].State)
	}
}
