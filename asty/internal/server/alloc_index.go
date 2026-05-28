package server

import (
	"sync"

	"asty/asty/internal/core/types"
)

// allocIndex is an in-memory mirror of the KV node and allocation state.
// Populated by KV Watch goroutines; reads are lock-free in the hot path via
// snapshot(). All mutations hold the write lock.
type allocIndex struct {
	mu     sync.RWMutex
	nodes  map[string]*types.NodeInfo
	allocs map[string]*types.ServiceAllocation
}

func newAllocIndex() *allocIndex {
	return &allocIndex{
		nodes:  make(map[string]*types.NodeInfo),
		allocs: make(map[string]*types.ServiceAllocation),
	}
}

func (idx *allocIndex) onNode(n *types.NodeInfo) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if n.Status == types.NodeDeleted {
		delete(idx.nodes, n.ID)
	} else {
		clone := *n
		idx.nodes[n.ID] = &clone
	}
}

func (idx *allocIndex) hasNode(id string) bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	_, ok := idx.nodes[id]
	return ok
}

func (idx *allocIndex) onAlloc(a *types.ServiceAllocation) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	k := a.ServiceName + "/" + a.NodeID
	if a.Status == types.AllocDeleted {
		delete(idx.allocs, k)
	} else {
		clone := *a
		idx.allocs[k] = &clone
	}
}

func (idx *allocIndex) snapshot() (nodes []*types.NodeInfo, allocs []*types.ServiceAllocation) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	nodes = make([]*types.NodeInfo, 0, len(idx.nodes))
	for _, n := range idx.nodes {
		nc := *n
		nodes = append(nodes, &nc)
	}
	allocs = make([]*types.ServiceAllocation, 0, len(idx.allocs))
	for _, a := range idx.allocs {
		ac := *a
		allocs = append(allocs, &ac)
	}
	return
}
