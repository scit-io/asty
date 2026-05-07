package asty

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// streamHub is the single source of truth for SSE handlers. One goroutine
// refreshes a complete cluster snapshot on a fixed interval; SSE handlers
// subscribe and read from the latest snapshot instead of independently
// querying the KV store on every tick.
type streamHub struct {
	server   *Server
	interval time.Duration

	mu       sync.RWMutex
	snapshot *clusterSnapshot

	// Per-allocation usage history → allocation metric time-series.
	// Kept in the hub (not metricsStore) because hub already iterates
	// allocations on every tick.

	subsMu sync.Mutex
	subs   map[int]chan *clusterSnapshot
	nextID int

	// drain_progress is event-driven (not snapshot)
	drainSubsMu sync.Mutex
	drainSubs   map[int]chan []byte
	drainNextID int
}

// clusterSnapshot is the immutable per-tick state shared with all subscribers.
// All fields are read-only after publication; subscribers must not mutate.
type clusterSnapshot struct {
	Timestamp int64

	// Cluster-wide
	Cluster ClusterStatusPayload
	Nodes   []*NodeInfo

	// Services with runtime usage merged in
	Services []ServiceWithUsage

	// Indexed allocation views
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

// ServiceWithUsage extends ServiceDefinition with runtime metrics, so a single
// 'services' SSE event carries everything the UI list needs (no per-service
// follow-up requests).
type ServiceWithUsage struct {
	*ServiceDefinition

	CurrentCopies      int     `json:"current_copies"`
	AvgCPUPercent      float64 `json:"avg_cpu_percent"`
	AvgMemoryPercent   float64 `json:"avg_memory_percent"`
	AvgCPUMHz          float64 `json:"avg_cpu_mhz"`
	AvgMemoryMB        float64 `json:"avg_memory_mb"`

	// Autoscaler state
	MinCopies          int    `json:"min_copies"`
	TargetCPU          int    `json:"target_cpu"`
	TargetMemory       int    `json:"target_memory"`
	TrafficThreshold   int    `json:"traffic_threshold"`
	CooldownUpActive   bool   `json:"cooldown_up_active"`
	CooldownDownActive bool   `json:"cooldown_down_active"`
	LastAction         string `json:"last_action,omitempty"`
	LastActionAt       int64  `json:"last_action_at,omitempty"`
}

func newStreamHub(server *Server, interval time.Duration) *streamHub {
	return &streamHub{
		server:    server,
		interval:  interval,
		subs:      make(map[int]chan *clusterSnapshot),
		drainSubs: make(map[int]chan []byte),
	}
}

// Run is the hub's main loop: refresh snapshot, fan out to subscribers, sleep.
// Also subscribes to drain events on NATS and forwards to drain subscribers.
func (h *streamHub) Run(ctx context.Context) {
	// Subscribe once to drain events (instead of per-SSE-connection)
	drainSub, err := h.server.nc.Subscribe("asty.v1.drain.progress", func(msg *nats.Msg) {
		h.fanoutDrain(msg.Data)
	})
	if err != nil {
		log.Error().Err(err).Msg("streamHub: failed to subscribe drain events")
	} else {
		defer drainSub.Unsubscribe()
	}

	// Compute initial snapshot before any subscriber connects.
	h.refresh()

	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.refresh()
		}
	}
}

func (h *streamHub) refresh() {
	snap := h.buildSnapshot()
	// Feed snapshot into metricsStore — no separate KV-polling loop needed.
	h.server.metricsStore.IngestSnapshot(snap)
	h.mu.Lock()
	h.snapshot = snap
	h.mu.Unlock()
	h.fanout(snap)
}

// Snapshot returns the most recently published snapshot. May be nil if Run
// hasn't completed its first tick yet (very narrow window at boot).
func (h *streamHub) Snapshot() *clusterSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.snapshot
}

// Subscribe registers a channel for snapshot updates. Returns the channel and
// an unsubscribe function. The latest snapshot (if any) is delivered
// immediately so the caller doesn't block waiting for the next tick.
func (h *streamHub) Subscribe() (<-chan *clusterSnapshot, func()) {
	ch := make(chan *clusterSnapshot, 4)

	h.subsMu.Lock()
	id := h.nextID
	h.nextID++
	h.subs[id] = ch
	h.subsMu.Unlock()

	if snap := h.Snapshot(); snap != nil {
		// Non-blocking: capacity is 4, so this can never fail on a fresh chan.
		ch <- snap
	}

	return ch, func() {
		h.subsMu.Lock()
		if existing, ok := h.subs[id]; ok {
			delete(h.subs, id)
			close(existing)
		}
		h.subsMu.Unlock()
	}
}

// SubscribeDrain registers for raw drain_progress event payloads (forwarded
// from NATS). Snapshot path doesn't carry per-event drain progress.
func (h *streamHub) SubscribeDrain() (<-chan []byte, func()) {
	ch := make(chan []byte, 16)

	h.drainSubsMu.Lock()
	id := h.drainNextID
	h.drainNextID++
	h.drainSubs[id] = ch
	h.drainSubsMu.Unlock()

	return ch, func() {
		h.drainSubsMu.Lock()
		if existing, ok := h.drainSubs[id]; ok {
			delete(h.drainSubs, id)
			close(existing)
		}
		h.drainSubsMu.Unlock()
	}
}

func (h *streamHub) fanout(snap *clusterSnapshot) {
	h.subsMu.Lock()
	defer h.subsMu.Unlock()
	for _, ch := range h.subs {
		// Non-blocking send: if a slow consumer's buffer is full, drop the
		// update — they'll get the next one. SSE is best-effort; we never
		// stall the hub for a stuck client.
		select {
		case ch <- snap:
		default:
		}
	}
}

func (h *streamHub) fanoutDrain(data []byte) {
	h.drainSubsMu.Lock()
	defer h.drainSubsMu.Unlock()
	for _, ch := range h.drainSubs {
		select {
		case ch <- data:
		default:
		}
	}
}

// buildSnapshot does a single sweep over KV: list nodes once, list allocations
// once per service. Result is cached for the next interval. Cost: O(nodes + services).
func (h *streamHub) buildSnapshot() *clusterSnapshot {
	now := time.Now()

	nodes, _ := h.server.clusterState.ListNodes()
	if nodes == nil {
		nodes = []*NodeInfo{}
	}

	allocsByNode := make(map[string][]*ServiceAllocation)
	allocsByService := make(map[string][]*ServiceAllocation, len(h.server.services))
	allocByID := make(map[string]*ServiceAllocation)

	services := h.server.services
	for _, svc := range services {
		allocs, err := h.server.clusterState.ListAllocations(svc.Name)
		if err != nil {
			continue
		}
		allocsByService[svc.Name] = allocs
		for _, a := range allocs {
			allocsByNode[a.NodeID] = append(allocsByNode[a.NodeID], a)
			allocByID[a.ID] = a
		}
	}

	// Enrich nodes with allocation counts (mutates the NodeInfo copies returned
	// by ListNodes, which are decoded fresh on each call — safe).
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

	// Per-service usage + autoscaler info
	cfg := h.server.cfg
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

// Helper: marshal value or log error and return empty bytes.
func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		log.Error().Err(err).Msg("streamHub: marshal failed")
		return []byte("{}")
	}
	return b
}
