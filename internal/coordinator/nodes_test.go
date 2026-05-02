package coordinator

import (
	"testing"
	"time"
)

// Test that Register creates nodes, updates existing ones, and List returns them.
func TestNodeRegistryRegisterAndList(t *testing.T) {
	reg := NewNodeRegistry()

	n1, err := reg.Register(NodeRegistration{ID: "node-1", Address: ":8081"})
	if err != nil {
		t.Fatalf("register node-1: %v", err)
	}
	if n1.ID != "node-1" {
		t.Fatalf("expected id node-1, got %s", n1.ID)
	}
	if n1.Address != ":8081" {
		t.Fatalf("expected address :8081, got %s", n1.Address)
	}
	if n1.State != NodeStateHealthy {
		t.Fatalf("expected state %s, got %s", NodeStateHealthy, n1.State)
	}

	n2, err := reg.Register(NodeRegistration{ID: "node-1", Address: ":9090"})
	if err != nil {
		t.Fatalf("update node-1: %v", err)
	}
	if n2.Address != ":9090" {
		t.Fatalf("expected address :9090, got %s", n2.Address)
	}

	if _, err := reg.Register(NodeRegistration{ID: "node-2", Address: ":8082"}); err != nil {
		t.Fatalf("register node-2: %v", err)
	}

	nodes, err := reg.List()
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
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

// Test that UpdateHealthStates flips nodes into HEALTHY / SUSPECT / OFFLINE based on LastSeen.
func TestNodeRegistryUpdateHealthStates(t *testing.T) {
	reg := NewNodeRegistry()
	now := time.Now().UTC()

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

	if err := reg.UpdateHealthStates(now, suspectAfter, offlineAfter); err != nil {
		t.Fatalf("update health states: %v", err)
	}

	nodes, err := reg.List()
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}

	byID := make(map[string]Node)
	for _, n := range nodes {
		byID[n.ID] = n
	}

	if byID["healthy"].State != NodeStateHealthy {
		t.Errorf("expected 'healthy' to be HEALTHY, got %s", byID["healthy"].State)
	}
	if byID["suspect"].State != NodeStateSuspect {
		t.Errorf("expected 'suspect' to be SUSPECT, got %s", byID["suspect"].State)
	}
	if byID["offline"].State != NodeStateOffline {
		t.Errorf("expected 'offline' to be OFFLINE, got %s", byID["offline"].State)
	}
}
