package asty

import (
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// allocIndex is an in-memory mirror of the KV node and allocation state.
// Populated by KV Watch goroutines; reads are lock-free in the hot path via
// snapshot(). All mutations hold the write lock.
type allocIndex struct {
	mu     sync.RWMutex
	nodes  map[string]*NodeInfo          // nodeID → NodeInfo
	allocs map[string]*ServiceAllocation // "svc/nodeID" → ServiceAllocation
}

func newAllocIndex() *allocIndex {
	return &allocIndex{
		nodes:  make(map[string]*NodeInfo),
		allocs: make(map[string]*ServiceAllocation),
	}
}

func (idx *allocIndex) onNode(n *NodeInfo) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if n.Status == "deleted" {
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

func (idx *allocIndex) onAlloc(a *ServiceAllocation) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	k := a.ServiceName + "/" + a.NodeID
	if a.Status == "deleted" {
		delete(idx.allocs, k)
	} else {
		clone := *a
		idx.allocs[k] = &clone
	}
}

func (idx *allocIndex) snapshot() (nodes []*NodeInfo, allocs []*ServiceAllocation) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	nodes = make([]*NodeInfo, 0, len(idx.nodes))
	for _, n := range idx.nodes {
		nc := *n
		nodes = append(nodes, &nc)
	}
	allocs = make([]*ServiceAllocation, 0, len(idx.allocs))
	for _, a := range idx.allocs {
		ac := *a
		allocs = append(allocs, &ac)
	}
	return
}

// clusterSnapshot is the immutable per-tick state shared with all subscribers.
type clusterSnapshot struct {
	Timestamp int64

	Cluster ClusterStatusPayload
	Nodes   []*NodeInfo

	Services []ServiceWithUsage

	AllocsByNode    map[string][]*ServiceAllocation
	AllocsByService map[string][]*ServiceAllocation
	AllocByID       map[string]*ServiceAllocation
}

// ClusterStatusPayload is the cluster-level status block published in SSE.
type ClusterStatusPayload struct {
	Leader       string `json:"leader"`
	LeaderIP     string `json:"leader_ip"`
	IsLeader     bool   `json:"is_leader"`
	NodesTotal   int    `json:"nodes_total"`
	NodesHealthy int    `json:"nodes_healthy"`
}

// ServiceWithUsage extends ServiceDefinition with runtime metrics.
type ServiceWithUsage struct {
	*ServiceDefinition

	CurrentCopies      int     `json:"current_copies"`
	AvgCPUPercent      float64 `json:"avg_cpu_percent"`
	AvgMemoryPercent   float64 `json:"avg_memory_percent"`
	AvgCPUMHz          float64 `json:"avg_cpu_mhz"`
	AvgMemoryMB        float64 `json:"avg_memory_mb"`

	MinCopies          int    `json:"min_copies"`
	TargetCPU          int    `json:"target_cpu"`
	TargetMemory       int    `json:"target_memory"`
	TrafficThreshold   int    `json:"traffic_threshold"`
	CooldownUpActive   bool   `json:"cooldown_up_active"`
	CooldownDownActive bool   `json:"cooldown_down_active"`
	LastAction         string `json:"last_action,omitempty"`
	LastActionAt       int64  `json:"last_action_at,omitempty"`
}

// buildSnapshot assembles a complete cluster snapshot from the in-memory
// allocIndex with no KV reads.
func (h *streamHub) buildSnapshot() *clusterSnapshot {
	now := time.Now()

	nodes, rawAllocs := h.idx.snapshot()

	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].CreatedAt.Equal(nodes[j].CreatedAt) {
			return nodes[i].ID < nodes[j].ID
		}
		return nodes[i].CreatedAt.Before(nodes[j].CreatedAt)
	})

	allocsByNode := make(map[string][]*ServiceAllocation)
	allocsByService := make(map[string][]*ServiceAllocation)
	allocByID := make(map[string]*ServiceAllocation)

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
			if a.Status == "running" {
				running++
			}
		}
		node.AllocationsRunning = running
		node.AllocationsPlanned = planned

		if node.Status == "ready" && now.Sub(node.LastSeen) < 2*time.Minute {
			healthy++
		}
	}

	leader, _ := h.server.leaderElection.GetLeader()
	leaderNodeID := leader.ID
	for _, node := range nodes {
		if node.IP == leader.IP {
			leaderNodeID = node.ID
			break
		}
	}

	cluster := ClusterStatusPayload{
		Leader:       leaderNodeID,
		LeaderIP:     leader.IP,
		IsLeader:     h.server.leaderElection.IsLeader(),
		NodesTotal:   len(nodes),
		NodesHealthy: healthy,
	}

	cfg := h.server.cfg
	services := h.server.services
	servicesOut := make([]ServiceWithUsage, 0, len(services))
	for _, svc := range services {
		allocs := allocsByService[svc.Name]
		var sumCPU, sumMem float64
		var running int
		for _, a := range allocs {
			if a.Status == "running" {
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

		var cooldownUp, cooldownDown bool
		var lastAction string
		var lastActionAt int64
		if cd, err := h.server.clusterState.GetServiceCooldown(svc.Name); err == nil {
			if !cd.LastScaleUp.IsZero() {
				if now.Sub(cd.LastScaleUp) < cfg.CooldownUp {
					cooldownUp = true
				}
				lastAction = "scale_up"
				lastActionAt = cd.LastScaleUp.Unix()
			}
			if !cd.LastScaleDown.IsZero() {
				if now.Sub(cd.LastScaleDown) < cfg.CooldownDown {
					cooldownDown = true
				}
				if cd.LastScaleDown.Unix() > lastActionAt {
					lastAction = "scale_down"
					lastActionAt = cd.LastScaleDown.Unix()
				}
			}
		}

		servicesOut = append(servicesOut, ServiceWithUsage{
			ServiceDefinition:  svc,
			CurrentCopies:      running,
			AvgCPUPercent:      avgCPUPct,
			AvgMemoryPercent:   avgMemPct,
			AvgCPUMHz:          avgCPUMHz,
			AvgMemoryMB:        avgMemMB,
			MinCopies:          cfg.MinCopies,
			TargetCPU:          cfg.TargetCPU,
			TargetMemory:       cfg.TargetMemory,
			TrafficThreshold:   cfg.TrafficRPSThreshold,
			CooldownUpActive:   cooldownUp,
			CooldownDownActive: cooldownDown,
			LastAction:         lastAction,
			LastActionAt:       lastActionAt,
		})
	}

	return &clusterSnapshot{
		Timestamp:       now.Unix(),
		Cluster:         cluster,
		Nodes:           nodes,
		Services:        servicesOut,
		AllocsByNode:    allocsByNode,
		AllocsByService: allocsByService,
		AllocByID:       allocByID,
	}
}

func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		log.Error().Err(err).Msg("streamHub: marshal failed")
		return []byte("{}")
	}
	return b
}
