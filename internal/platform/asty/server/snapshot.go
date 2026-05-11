package server

import (
	"sort"
	"sync"
	"time"

	"asty/internal/platform/asty/core/types"
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

// buildSnapshot assembles a complete cluster snapshot from the in-memory
// allocIndex with no KV reads.
func (h *streamHub) buildSnapshot() *types.ClusterSnapshot {
	now := time.Now()

	nodes, rawAllocs := h.idx.snapshot()

	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].CreatedAt.Equal(nodes[j].CreatedAt) {
			return nodes[i].ID < nodes[j].ID
		}
		return nodes[i].CreatedAt.Before(nodes[j].CreatedAt)
	})

	allocsByNode := make(map[string][]*types.ServiceAllocation)
	allocsByService := make(map[string][]*types.ServiceAllocation)
	allocByID := make(map[string]*types.ServiceAllocation)

	sort.Slice(rawAllocs, func(i, j int) bool {
		if rawAllocs[i].ServiceName != rawAllocs[j].ServiceName {
			return rawAllocs[i].ServiceName < rawAllocs[j].ServiceName
		}
		return rawAllocs[i].ID < rawAllocs[j].ID
	})

	for _, a := range rawAllocs {
		allocsByNode[a.NodeID] = append(allocsByNode[a.NodeID], a)
		allocsByService[a.ServiceName] = append(allocsByService[a.ServiceName], a)
		if a.ID != "" {
			allocByID[a.ID] = a
		}
	}

	healthy := 0
	for _, node := range nodes {
		running, planned := 0, 0
		for _, a := range allocsByNode[node.ID] {
			planned++
			if a.Status == types.AllocRunning {
				running++
			}
		}
		node.AllocationsRunning = running
		node.AllocationsPlanned = planned

		if node.IsHealthy(now) {
			healthy++
		}
	}

	leaderInfo, _ := h.server.leaderElection.GetLeader()
	leaderNodeID := leaderInfo.ID
	for _, node := range nodes {
		if node.IP == leaderInfo.IP {
			leaderNodeID = node.ID
			break
		}
	}

	cluster := types.ClusterStatusPayload{
		Leader:       leaderNodeID,
		LeaderIP:     leaderInfo.IP,
		IsLeader:     h.server.leaderElection.IsLeader(),
		NodesTotal:   len(nodes),
		NodesHealthy: healthy,
	}

	cfg := h.server.cfg
	services := h.server.services
	servicesOut := make([]types.ServiceWithUsage, 0, len(services))
	for _, svc := range services {
		allocs := allocsByService[svc.Name]
		var sumCPU, sumMem float64
		var running int
		for _, a := range allocs {
			if a.Status == types.AllocRunning {
				sumCPU += float64(a.CPUUsage)
				sumMem += float64(a.MemoryUsage)
				running++
			}
		}
		var avgCPUPct, avgMemMB, avgCPUMHz, avgMemPct float64
		if running > 0 {
			avgCPUPct = sumCPU / float64(running)
			avgMemMB = sumMem / float64(running)
			avgCPUMHz = (avgCPUPct / 100) * float64(svc.Resources.CPU)
			if svc.Resources.Memory > 0 {
				avgMemPct = (avgMemMB / float64(svc.Resources.Memory)) * 100
			}
		}

		var cooldown types.CooldownStatus
		if cd, err := h.server.clusterState.GetServiceCooldown(svc.Name); err == nil {
			cooldown = cd.Status(now, cfg.Autoscale.CooldownUp, cfg.Autoscale.CooldownDown)
		}

		servicesOut = append(servicesOut, types.ServiceWithUsage{
			ServiceDefinition:  svc,
			CurrentCopies:      running,
			AvgCPUPercent:      avgCPUPct,
			AvgMemoryPercent:   avgMemPct,
			AvgCPUMHz:          avgCPUMHz,
			AvgMemoryMB:        avgMemMB,
			MinCopies:          cfg.Autoscale.MinCopies,
			TargetCPU:          cfg.Autoscale.TargetCPU,
			TargetMemory:       cfg.Autoscale.TargetMemory,
			TrafficThreshold:   cfg.Autoscale.TrafficRPSThreshold,
			CooldownUpActive:   cooldown.UpActive,
			CooldownDownActive: cooldown.DownActive,
			LastAction:         cooldown.LastAction,
			LastActionAt:       cooldown.LastActionAt,
		})
	}

	return &types.ClusterSnapshot{
		Timestamp:       now.Unix(),
		Cluster:         cluster,
		Nodes:           nodes,
		Services:        servicesOut,
		AllocsByNode:    allocsByNode,
		AllocsByService: allocsByService,
		AllocByID:       allocByID,
	}
}

