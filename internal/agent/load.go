package agent

import (
	"sync/atomic"

	"planetary-mesh/internal/protocol"
)

type LoadTracker struct {
	activeExecutions atomic.Int64
}

func NewLoadTracker() *LoadTracker {
	return &LoadTracker{}
}

func (t *LoadTracker) BeginExecution() func() {
	if t == nil {
		return func() {}
	}
	t.activeExecutions.Add(1)
	return func() {
		t.activeExecutions.Add(-1)
	}
}

func (t *LoadTracker) Snapshot() protocol.NodeLoad {
	if t == nil {
		return protocol.NodeLoad{}
	}
	active := t.activeExecutions.Load()
	if active < 0 {
		active = 0
	}
	return protocol.NodeLoad{ActiveExecutions: int(active)}
}
