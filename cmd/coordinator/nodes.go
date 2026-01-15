package main

import (
	"sync"
	"time"
)

// NodeState represents the health state of a node.
type NodeState string

const (
	NodeStateHealthy NodeState = "HEALTHY"
	NodeStateSuspect NodeState = "SUSPECT"
	NodeStateOffline NodeState = "OFFLINE"
)

// Node represents an agent node known to the coordinator.
type Node struct {
	ID       string    `json:"id"`
	Address  string    `json:"address"`
	LastSeen time.Time `json:"last_seen"`
	State    NodeState `json:"state"`
}

// NodeRegistry safely stores nodes in memory.
type NodeRegistry struct {
	mu    sync.Mutex
	nodes map[string]*Node
}

// NewNodeRegistry creates an empty registry.
func NewNodeRegistry() *NodeRegistry {
	return &NodeRegistry{
		nodes: make(map[string]*Node),
	}
}

// Register inserts or updates a node in the registry.
// We treat registration as a heartbeat: each call updates LastSeen and sets state to HEALTHY.
func (r *NodeRegistry) Register(id, addr string) Node {
	r.mu.Lock()
	defer r.mu.Unlock()

	n, exists := r.nodes[id]
	if !exists {
		n = &Node{ID: id}
		r.nodes[id] = n
	}
	n.Address = addr
	n.LastSeen = time.Now().UTC()
	n.State = NodeStateHealthy

	// Return a copy so callers can't mutate internal state.
	return *n
}

// List returns a snapshot of all nodes as a slice of copies.
func (r *NodeRegistry) List() []Node {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]Node, 0, len(r.nodes))
	for _, n := range r.nodes {
		out = append(out, *n)
	}
	return out
}

// UpdateHealthStates updates each node's State based on LastSeen and thresholds.
func (r *NodeRegistry) UpdateHealthStates(now time.Time, suspectAfter, offlineAfter time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, n := range r.nodes {
		age := now.Sub(n.LastSeen)
		switch {
		case age > offlineAfter:
			n.State = NodeStateOffline
		case age > suspectAfter:
			n.State = NodeStateSuspect
		default:
			n.State = NodeStateHealthy
		}
	}
}

// startHealthChecker launches a background goroutine that periodically updates node states.
// Returns a stop function to halt the ticker.
func startHealthChecker(registry *NodeRegistry) func() {
	// How long before a node is considered SUSPECT / OFFLINE.
	suspectAfter := 15 * time.Second
	offlineAfter := 30 * time.Second

	ticker := time.NewTicker(5 * time.Second) // how often we recalc health
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case now := <-ticker.C:
				registry.UpdateHealthStates(now, suspectAfter, offlineAfter)
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
