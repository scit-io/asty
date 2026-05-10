package autoscaling

import (
	"context"
	"fmt"
	"sort"
	"time"

	"asty/internal/platform/asty/core/config"
	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/autoscaling/metrics"
	"asty/internal/platform/asty/features/clustering/state"
	"asty/internal/platform/asty/features/scheduling"

	"github.com/rs/zerolog/log"
)

// ScalingDecision describes what the autoscaler intends to do next.
type ScalingDecision struct {
	ServiceName string
	Action      string // scale_up | scale_down | none
	Reason      string
	TargetNode  string
	RemoveNode  string
}

// Autoscaler grows services above MinCopies in response to traffic and
// resource pressure, and shrinks back to MinCopies when load subsides.
type Autoscaler struct {
	clusterState *state.ClusterState
	scheduler    *scheduling.Scheduler
	cfg          *config.Config
	metricsStore *metrics.Store
}

func NewAutoscaler(clusterState *state.ClusterState, scheduler *scheduling.Scheduler, cfg *config.Config, metricsStore *metrics.Store) *Autoscaler {
	return &Autoscaler{
		clusterState: clusterState,
		scheduler:    scheduler,
		cfg:          cfg,
		metricsStore: metricsStore,
	}
}

func (as *Autoscaler) lastActionAt(service string) (time.Time, bool) {
	c, err := as.clusterState.GetServiceCooldown(service)
	if err != nil {
		log.Warn().Err(err).Str("service", service).Msg("failed to read cooldown; treating as not in cooldown")
		return time.Time{}, false
	}
	switch {
	case !c.LastScaleUp.IsZero() && !c.LastScaleDown.IsZero():
		if c.LastScaleUp.After(c.LastScaleDown) {
			return c.LastScaleUp, true
		}
		return c.LastScaleDown, true
	case !c.LastScaleUp.IsZero():
		return c.LastScaleUp, true
	case !c.LastScaleDown.IsZero():
		return c.LastScaleDown, true
	}
	return time.Time{}, false
}

// EvaluateService decides whether svc should grow, shrink, or stay put.
func (as *Autoscaler) EvaluateService(ctx context.Context, svc *types.ServiceDefinition) (*ScalingDecision, error) {
	allocs, err := as.clusterState.ListAllocations(svc.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to list allocations: %w", err)
	}
	nodes, err := as.clusterState.ListNodes()
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	live := scheduling.LiveAllocations(allocs)

	if as.inCooldown(svc.Name) {
		return noop(svc.Name, "cooldown active"), nil
	}

	if d := as.evaluateScaleUp(svc, live, nodes); d != nil {
		return d, nil
	}
	if d := as.evaluateScaleDown(svc, live); d != nil {
		return d, nil
	}
	return noop(svc.Name, "within target thresholds"), nil
}

func noop(name, reason string) *ScalingDecision {
	return &ScalingDecision{ServiceName: name, Action: "none", Reason: reason}
}

func (as *Autoscaler) inCooldown(service string) bool {
	last, ok := as.lastActionAt(service)
	if !ok {
		return false
	}
	cd := as.cfg.CooldownDown
	if as.cfg.CooldownUp > cd {
		cd = as.cfg.CooldownUp
	}
	return time.Since(last) < cd
}

func (as *Autoscaler) evaluateScaleUp(svc *types.ServiceDefinition, live []*types.ServiceAllocation, nodes []*types.NodeInfo) *ScalingDecision {
	if node := as.findNodeWithTrafficWithoutService(nodes, live); node != nil {
		return &ScalingDecision{
			ServiceName: svc.Name,
			Action:      "scale_up",
			Reason:      fmt.Sprintf("gateway traffic on node %s without %s", node.ID, svc.Name),
			TargetNode:  node.ID,
		}
	}
	if hot := as.findOverloadedAlloc(live); hot != nil {
		target := as.pickFreeNode(svc, live, nodes)
		if target == nil {
			return nil
		}
		return &ScalingDecision{
			ServiceName: svc.Name,
			Action:      "scale_up",
			Reason:      fmt.Sprintf("copy on %s exceeded targets (cpu=%d%%, mem=%dMB) — adding copy on %s", hot.NodeID, hot.CPUUsage, hot.MemoryUsage, target.ID),
			TargetNode:  target.ID,
		}
	}
	return nil
}

func (as *Autoscaler) evaluateScaleDown(svc *types.ServiceDefinition, live []*types.ServiceAllocation) *ScalingDecision {
	floor := as.cfg.MinCopies
	if floor < 1 {
		floor = 1
	}
	if len(live) <= floor {
		return nil
	}

	running := make([]*types.ServiceAllocation, 0, len(live))
	for _, a := range live {
		if a.Status == "running" && a.PID > 0 {
			running = append(running, a)
		}
	}
	if len(running) <= floor {
		return nil
	}

	var cpuSum, memSum int
	for _, a := range running {
		cpuSum += a.CPUUsage
		memSum += a.MemoryUsage
	}
	avgCPU := cpuSum / len(running)
	avgMem := memSum / len(running)

	cpuFloor := as.cfg.TargetCPU / 2
	memFloor := as.cfg.TargetMemory / 2
	if avgCPU > cpuFloor || avgMem > memFloor {
		return nil
	}

	nodes, err := as.clusterState.ListNodes()
	if err != nil {
		return nil
	}
	victim := as.pickAllocationToRemove(running, nodes)
	if victim == nil {
		return nil
	}
	return &ScalingDecision{
		ServiceName: svc.Name,
		Action:      "scale_down",
		Reason:      fmt.Sprintf("avg cpu=%d%% mem=%dMB across %d copies, floor cpu=%d mem=%d", avgCPU, avgMem, len(running), cpuFloor, memFloor),
		RemoveNode:  victim.NodeID,
	}
}

func (as *Autoscaler) findNodeWithTrafficWithoutService(nodes []*types.NodeInfo, live []*types.ServiceAllocation) *types.NodeInfo {
	hasService := scheduling.NodeIDsOf(live)
	for _, node := range nodes {
		if node.Status != "ready" {
			continue
		}
		if hasService[node.ID] {
			continue
		}
		if !as.hasGatewayTraffic(node) {
			continue
		}
		return node
	}
	return nil
}

func (as *Autoscaler) hasGatewayTraffic(node *types.NodeInfo) bool {
	if as.metricsStore == nil {
		return false
	}
	points := as.metricsStore.GetRPS(node.ID, time.Now().Add(-60*time.Second))
	if len(points) == 0 {
		return false
	}
	var sum float64
	for _, p := range points {
		sum += p.Value
	}
	return sum/float64(len(points)) >= float64(as.cfg.TrafficRPSThreshold)
}

func (as *Autoscaler) findOverloadedAlloc(live []*types.ServiceAllocation) *types.ServiceAllocation {
	for _, alloc := range live {
		if alloc.Status != "running" || alloc.PID == 0 {
			continue
		}
		if alloc.CPUUsage > as.cfg.TargetCPU || alloc.MemoryUsage > as.cfg.TargetMemory {
			return alloc
		}
	}
	return nil
}

func (as *Autoscaler) pickFreeNode(svc *types.ServiceDefinition, live []*types.ServiceAllocation, nodes []*types.NodeInfo) *types.NodeInfo {
	healthy := as.scheduler.FilterHealthyNodes(nodes)
	occupied := scheduling.NodeIDsOf(live)
	dcCounts := scheduling.DatacenterCountsByOccupied(healthy, occupied)
	nodeAllocCounts := as.scheduler.ComputeNodeAllocCounts()
	picks := as.scheduler.PickCandidates(svc, healthy, occupied, dcCounts, nodeAllocCounts, 1)
	if len(picks) == 0 {
		return nil
	}
	return picks[0]
}

// ExecuteScalingDecision applies a decision returned by EvaluateService.
func (as *Autoscaler) ExecuteScalingDecision(ctx context.Context, d *ScalingDecision, svc *types.ServiceDefinition) error {
	switch d.Action {
	case "scale_up":
		return as.executeScaleUp(d, svc)
	case "scale_down":
		return as.executeScaleDown(d, svc)
	case "none":
		return nil
	}
	return fmt.Errorf("unknown action: %s", d.Action)
}

func (as *Autoscaler) executeScaleUp(d *ScalingDecision, svc *types.ServiceDefinition) error {
	log.Info().
		Str("service", svc.Name).
		Str("target_node", d.TargetNode).
		Str("reason", d.Reason).
		Msg("scaling up")

	alloc := &types.ServiceAllocation{
		ServiceName: svc.Name,
		NodeID:      d.TargetNode,
		Status:      "pending",
		Version:     "latest",
	}
	if err := as.clusterState.CreateAllocation(alloc); err != nil {
		return fmt.Errorf("failed to create allocation: %w", err)
	}
	if err := as.clusterState.MarkScaleUp(svc.Name, time.Now()); err != nil {
		log.Warn().Err(err).Str("service", svc.Name).Msg("failed to persist scale-up cooldown")
	}

	allocs, _ := as.clusterState.ListAllocations(svc.Name)
	count := len(scheduling.LiveAllocations(allocs))
	as.metricsStore.AddEvent(metrics.ScalingEvent{
		Service:   svc.Name,
		Action:    "scale_up",
		Reason:    d.Reason,
		FromCount: count - 1,
		ToCount:   count,
		NodeID:    d.TargetNode,
	})
	return nil
}

func (as *Autoscaler) executeScaleDown(d *ScalingDecision, svc *types.ServiceDefinition) error {
	if d.RemoveNode == "" {
		return fmt.Errorf("scale_down decision missing RemoveNode")
	}
	log.Info().
		Str("service", svc.Name).
		Str("remove_node", d.RemoveNode).
		Str("reason", d.Reason).
		Msg("scaling down")

	if err := as.clusterState.DeleteAllocation(svc.Name, d.RemoveNode); err != nil {
		return fmt.Errorf("failed to delete allocation: %w", err)
	}
	if err := as.clusterState.MarkScaleDown(svc.Name, time.Now()); err != nil {
		log.Warn().Err(err).Str("service", svc.Name).Msg("failed to persist scale-down cooldown")
	}

	allocs, _ := as.clusterState.ListAllocations(svc.Name)
	count := len(scheduling.LiveAllocations(allocs))
	as.metricsStore.AddEvent(metrics.ScalingEvent{
		Service:   svc.Name,
		Action:    "scale_down",
		Reason:    d.Reason,
		FromCount: count + 1,
		ToCount:   count,
		NodeID:    d.RemoveNode,
	})
	return nil
}

func (as *Autoscaler) pickAllocationToRemove(live []*types.ServiceAllocation, nodes []*types.NodeInfo) *types.ServiceAllocation {
	if len(live) == 0 {
		return nil
	}
	nodeDC := make(map[string]string, len(nodes))
	for _, n := range nodes {
		nodeDC[n.ID] = scheduling.DatacenterOf(n)
	}
	dcCount := make(map[string]int)
	for _, a := range live {
		dcCount[nodeDC[a.NodeID]]++
	}
	sorted := append([]*types.ServiceAllocation(nil), live...)
	sort.Slice(sorted, func(i, j int) bool {
		di, dj := nodeDC[sorted[i].NodeID], nodeDC[sorted[j].NodeID]
		if dcCount[di] != dcCount[dj] {
			return dcCount[di] > dcCount[dj]
		}
		return sorted[i].NodeID < sorted[j].NodeID
	})
	return sorted[0]
}

