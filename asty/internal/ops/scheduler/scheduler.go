package scheduler

import (
	"context"
	"fmt"
	"time"

	"asty/asty/internal/core/config"
	"asty/asty/internal/core/types"
	"asty/asty/internal/domain/proximity"
	"asty/asty/internal/infra/kv"

	"github.com/rs/zerolog/log"
)

// nodeStaleAfter — heartbeat-age threshold beyond which a node is
// excluded from scheduling. Distinct from NodeInfo.IsHealthy (2 min) by
// design: scheduling tolerates a longer lag because moving allocations
// is more expensive than skipping a stats refresh.
const nodeStaleAfter = 10 * time.Minute

// Placement is a scheduling decision: place ServiceName on NodeID.
type Placement struct {
	ServiceName string
	NodeID      string
	Resources   types.Resources
}

// Scheduler maintains the baseline placement for services.
type Scheduler struct {
	clusterState    *kv.ClusterState
	cfg             *config.Config
	proximityMatrix *proximity.Matrix
}

// NewScheduler builds a scheduler with the proximity matrix loaded
// from cfg.Autoscale.DCLatency. A bad matrix string is logged but not fatal:
// scheduling falls back to round-robin order across DCs in that case.
func NewScheduler(clusterState *kv.ClusterState, cfg *config.Config) *Scheduler {
	pm := proximity.NewMatrix()
	if err := pm.LoadFromConfig(cfg.Autoscale.DCLatency); err != nil {
		log.Error().Err(err).Msg("failed to load proximity matrix")
	}
	return &Scheduler{
		clusterState:    clusterState,
		cfg:             cfg,
		proximityMatrix: pm,
	}
}

// ReconcileService brings the service's running set in line with the
// configured target. System services get one copy per healthy node;
// regular services get MinCopies copies spread across DCs and packed
// onto the busiest nodes (good for cache locality, bad for blast
// radius — that's a deliberate trade-off).
func (s *Scheduler) ReconcileService(ctx context.Context, svc *types.ServiceDefinition) error {
	allocs, err := s.clusterState.ListAllocations(svc.Name)
	if err != nil {
		return fmt.Errorf("failed to list allocations: %w", err)
	}
	nodes, err := s.clusterState.ListNodes()
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	// Reap ghost allocations: a copy whose node has left cluster KV (NATS's
	// authoritative membership) can never run again. Drop it before counting
	// so a stale "running" copy on a departed node neither blocks rescheduling
	// onto a live node nor lingers as a phantom in the dashboard.
	allocs = s.reapGhostAllocations(allocs, nodes)

	healthy := s.FilterHealthyNodes(nodes)
	if len(healthy) == 0 {
		return fmt.Errorf("no healthy nodes available")
	}

	live := LiveAllocations(allocs)
	occupied := OccupiedNodes(allocs)

	switch svc.Type {
	case types.ServiceTypeSystem:
		return s.reconcileSystem(svc, healthy, occupied)
	case types.ServiceTypeService:
		nodeAllocCounts := s.ComputeNodeAllocCounts()
		return s.reconcileRegular(svc, healthy, live, occupied, nodeAllocCounts)
	default:
		return fmt.Errorf("unknown service type: %s", svc.Type)
	}
}

// reapGhostAllocations deletes allocations whose node has left cluster KV
// and returns the survivors (those on nodes that still exist). It runs on
// the leader only, since ReconcileService is leader-only. A node is "gone"
// per NATS's authoritative membership — the KV node list — not per the
// allocation's own (possibly stale) status. Survivors are returned even if
// a delete fails, so a transient delete error never makes a ghost count as
// live and block rescheduling; the next pass retries the delete.
func (s *Scheduler) reapGhostAllocations(allocs []*types.ServiceAllocation, nodes []*types.NodeInfo) []*types.ServiceAllocation {
	known := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		known[n.ID] = true
	}
	onKnown, ghosts := partitionAllocsByNode(allocs, known)
	for _, g := range ghosts {
		if err := s.clusterState.DeleteAllocation(g.ServiceName, g.NodeID); err != nil {
			log.Warn().Err(err).
				Str("service", g.ServiceName).
				Str("node_id", g.NodeID).
				Msg("failed to reap ghost allocation")
			continue
		}
		log.Info().
			Str("service", g.ServiceName).
			Str("node_id", g.NodeID).
			Str("status", string(g.Status)).
			Msg("reaped ghost allocation: node gone from cluster KV")
	}
	return onKnown
}

// FilterHealthyNodes keeps only ready nodes with recent heartbeats.
// Joining/Stale/Draining/etc. are excluded explicitly via EffectiveStatus
// so a brief network blip or a still-initialising node doesn't get a
// new copy placed on it.
func (s *Scheduler) FilterHealthyNodes(nodes []*types.NodeInfo) []*types.NodeInfo {
	healthy := make([]*types.NodeInfo, 0, len(nodes))
	now := time.Now()
	for _, node := range nodes {
		if node.EffectiveStatus(now) != types.NodeReady {
			continue
		}
		if time.Since(node.LastSeen) > nodeStaleAfter {
			log.Warn().
				Str("node_id", node.ID).
				Time("last_seen", node.LastSeen).
				Msg("node stale, excluding from scheduling")
			continue
		}
		healthy = append(healthy, node)
	}
	return healthy
}

// hasResources reports whether a node has enough free CPU/Memory after
// reserving the per-node overhead (ReservedCPU/ReservedMemory) for the
// agent itself.
func (s *Scheduler) hasResources(node *types.NodeInfo, required types.Resources) bool {
	cpuFree := node.CPUAvailable - s.cfg.Resources.ReservedCPU
	memFree := node.MemoryAvailable - int64(s.cfg.Resources.ReservedMemory)
	return cpuFree >= required.CPU && memFree >= int64(required.Memory)
}

// ComputeNodeAllocCounts returns per-node count of live allocations
// across every service. Used by PickCandidates to prefer "warm" nodes
// (more cache hits) when other tie-breakers are equal.
func (s *Scheduler) ComputeNodeAllocCounts() map[string]int {
	out := make(map[string]int)
	all, err := s.clusterState.ListAllAllocations()
	if err != nil {
		return out
	}
	for _, a := range all {
		if a.Status.IsLive() {
			out[a.NodeID]++
		}
	}
	return out
}

// createAllocation writes a fresh "pending" allocation to KV. The
// controller picks it up via WatchAllocations and dispatches a start
// command to the target agent.
//
// Version is read from the per-service pin maintained by the deployer
// (see kv.GetServiceVersion). Falls back to "latest" only when no
// deploy has ever happened — dev artifacts (`url: local`, `file://`)
// then still resolve, while prod URLs with `${VERSION}` fail loudly
// at the downloader instead of silently drifting to a stale version.
func (s *Scheduler) createAllocation(svc *types.ServiceDefinition, nodeID string) error {
	return s.clusterState.CreateAllocation(&types.ServiceAllocation{
		ServiceName: svc.Name,
		NodeID:      nodeID,
		Status:      types.AllocPending,
		Version:     s.VersionFor(svc.Name),
	})
}

// VersionFor returns the version new allocations of svc should run.
// Reads the deployer-maintained pin; falls back to "latest" when the
// service has never been deployed. Exported so the autoscaler shares
// the same source of truth — without this the autoscaler's scale-up
// would silently create a stale-version copy after a deploy.
func (s *Scheduler) VersionFor(serviceName string) string {
	if s.clusterState == nil {
		return "latest"
	}
	v, err := s.clusterState.GetServiceVersion(serviceName)
	if err != nil || v.Current == "" {
		return "latest"
	}
	return v.Current
}

// TargetCopies returns the desired live copy count for svc, respecting an
// operator-set scale override (kv.SetServiceScale) when present, falling
// back to AutoscaleConfig.MinCopies otherwise. The result is capped at the
// number of healthy nodes — we can't schedule more copies than we have nodes.
func (s *Scheduler) TargetCopies(svc *types.ServiceDefinition, healthyNodes int) int {
	target := s.cfg.Autoscale.MinCopies
	if s.clusterState != nil {
		if override, ok := s.clusterState.GetServiceScale(svc.Name); ok {
			target = override
		}
	}
	if target < 0 {
		target = 0
	}
	if target > healthyNodes {
		target = healthyNodes
	}
	return target
}
