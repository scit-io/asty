package server

import (
	"sort"
	"time"

	"asty/asty/internal/core/types"
	"asty/asty/internal/ops/deployer"
)

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
	leaderHost := leaderInfo.Host
	var leaderDC string
	for _, node := range nodes {
		if node.IP == leaderInfo.IP {
			leaderNodeID = node.ID
			leaderDC = node.Datacenter
			// Snapshot Host wins over the KV-stored leader.Host: the
			// agent on the leader rewrites NodeInfo.Host on every
			// heartbeat, so it's the freshest source if the operator
			// rotated the DNS name without restarting.
			if node.Host != "" {
				leaderHost = node.Host
			}
			break
		}
	}

	cluster := types.ClusterStatusPayload{
		Leader:       leaderNodeID,
		LeaderIP:     leaderInfo.IP,
		LeaderDC:     leaderDC,
		LeaderHost:   leaderHost,
		IsLeader:     h.server.leaderElection.IsLeader(),
		NodesTotal:   len(nodes),
		NodesHealthy: healthy,
		ServedBy:     h.server.nodeID,
	}

	cfg := h.server.cfg
	services := h.server.services
	servicesOut := make([]types.ServiceWithUsage, 0, len(services))

	// Index the deploy history once: first record per service wins
	// because GetHistory returns newest-first. Without this the per-
	// service loop below would call GetHistory() N times, each call
	// copying the full ring under the deployer's mutex.
	lastDeployByService := make(map[string]deployer.DeploymentRecord, len(services))
	for _, rec := range h.server.deployer.GetHistory() {
		if _, seen := lastDeployByService[rec.Service]; seen {
			continue
		}
		lastDeployByService[rec.Service] = rec
		if len(lastDeployByService) == len(services) {
			break
		}
	}

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

		minCopies := cfg.Autoscale.MinCopies
		if override, ok := h.server.clusterState.GetServiceScale(svc.Name); ok {
			minCopies = override
		}

		// Last action is sourced from the scaling-events ring rather
		// than ServiceCooldown.LastScaleUp/Down — the ring also
		// captures manual scale operations (the autoscaler-only
		// cooldown fields never see those). Falls back to the
		// cooldown timestamps when the ring is empty.
		lastAction := cooldown.LastAction
		lastActionAt := cooldown.LastActionAt
		var lastReason string
		if events := h.server.metricsStore.GetEvents(svc.Name, 1); len(events) > 0 {
			lastAction = events[0].Action
			lastActionAt = events[0].Timestamp
			lastReason = events[0].Reason
		}

		// Latest deploy for the same service — surfaced separately so
		// the UI's Last action column can pick the more recent of
		// {scaling event, deploy} per-row without a second fetch.
		var lastDeployVersion, lastDeployStatus string
		var lastDeployAt int64
		if rec, ok := lastDeployByService[svc.Name]; ok {
			lastDeployVersion = rec.Version
			lastDeployStatus = string(rec.Status)
			lastDeployAt = rec.StartedAt.Unix()
		}

		servicesOut = append(servicesOut, types.ServiceWithUsage{
			ServiceDefinition:  svc,
			CurrentCopies:      running,
			AvgCPUPercent:      avgCPUPct,
			AvgMemoryPercent:   avgMemPct,
			AvgCPUMHz:          avgCPUMHz,
			AvgMemoryMB:        avgMemMB,
			MinCopies:          minCopies,
			TargetCPU:          cfg.Autoscale.TargetCPU,
			TargetMemory:       cfg.Autoscale.TargetMemory,
			TrafficThreshold:   cfg.Autoscale.TrafficRPSThreshold,
			CooldownUpActive:   cooldown.UpActive,
			CooldownDownActive: cooldown.DownActive,
			LastAction:         lastAction,
			LastActionAt:       lastActionAt,
			LastReason:         lastReason,
			LastDeployVersion:  lastDeployVersion,
			LastDeployStatus:   lastDeployStatus,
			LastDeployAt:       lastDeployAt,
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
