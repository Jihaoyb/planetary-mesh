package coordinator

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

// NodeStateCounts is an aggregate snapshot of known nodes by health state.
type NodeStateCounts struct {
	Healthy int
	Suspect int
	Offline int
}

// NodeStore is the coordinator's narrow node persistence contract.
type NodeStore interface {
	Register(id, addr string) (Node, error)
	List() ([]Node, error)
	UpdateHealthStates(now time.Time, suspectAfter, offlineAfter time.Duration) error
	CountByState() (NodeStateCounts, error)
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

// Register inserts or updates a node in the registry. We treat registration as
// a heartbeat: each call updates LastSeen and sets state to HEALTHY.
func (r *NodeRegistry) Register(id, addr string) (Node, error) {
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
	return *n, nil
}

// List returns a snapshot of all nodes as a slice of copies.
func (r *NodeRegistry) List() ([]Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]Node, 0, len(r.nodes))
	for _, n := range r.nodes {
		out = append(out, *n)
	}
	return out, nil
}

// UpdateHealthStates updates each node's State based on LastSeen and thresholds.
func (r *NodeRegistry) UpdateHealthStates(now time.Time, suspectAfter, offlineAfter time.Duration) error {
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
	return nil
}

// CountByState returns node-state gauges without exposing individual rows.
func (r *NodeRegistry) CountByState() (NodeStateCounts, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var counts NodeStateCounts
	for _, n := range r.nodes {
		switch n.State {
		case NodeStateHealthy:
			counts.Healthy++
		case NodeStateSuspect:
			counts.Suspect++
		case NodeStateOffline:
			counts.Offline++
		}
	}
	return counts, nil
}

// StartHealthChecker launches a background goroutine that periodically updates node states.
// It stops when stopCh is closed. Pass nil to run forever (current behavior).
func StartHealthChecker(registry NodeStore, stopCh <-chan struct{}) {
	suspectAfter := 15 * time.Second
	offlineAfter := 30 * time.Second

	ticker := time.NewTicker(5 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				_ = registry.UpdateHealthStates(now, suspectAfter, offlineAfter)
			case <-stopCh:
				return
			}
		}
	}()
}
