package asty

import (
	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/scheduling"
)

// Backward-compatible aliases
type Scheduler = scheduling.Scheduler
type Placement = scheduling.Placement

var NewScheduler = scheduling.NewScheduler

// Exported helper functions
var (
	LiveAllocations   = scheduling.LiveAllocations
	OccupiedNodes     = scheduling.OccupiedNodes
	NodeIDsOf         = scheduling.NodeIDsOf
	DatacenterOf      = scheduling.DatacenterOf
	GroupByDatacenter = scheduling.GroupByDatacenter
)

// Package-internal aliases for backward compat (used by autoscaler, drain)
var liveAllocations = scheduling.LiveAllocations
var occupiedNodes = scheduling.OccupiedNodes
var nodeIDsOf = scheduling.NodeIDsOf

func datacenterOf(n *types.NodeInfo) string { return scheduling.DatacenterOf(n) }
func groupByDatacenter(nodes []*types.NodeInfo) map[string][]*types.NodeInfo {
	return scheduling.GroupByDatacenter(nodes)
}
func datacenterCountsByOccupied(healthy []*types.NodeInfo, occupied map[string]bool) map[string]int {
	counts := make(map[string]int)
	for _, n := range healthy {
		counts[datacenterOf(n)] += 0
		if occupied[n.ID] {
			counts[datacenterOf(n)]++
		}
	}
	return counts
}
