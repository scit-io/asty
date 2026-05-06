package asty

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/rs/zerolog/log"
)

// Autoscaler grows services above MinCopies in response to traffic and
// resource pressure, and shrinks back to MinCopies when load subsides. It
// never moves services around — only adds and removes copies.
//
// Cooldown lives in cluster state KV (ServiceCooldown), not in memory: a
// leader flip in the middle of a scale event used to drop the cooldown
// window and let the new leader scale again immediately.
type Autoscaler struct {
	clusterState *ClusterState
	scheduler    *Scheduler
	cfg          *Config
	metricsStore *MetricsStore
}

// ScalingDecision describes what the autoscaler intends to do next.
type ScalingDecision struct {
	ServiceName string
	Action      string // scale_up | scale_down | none
	Reason      string
	TargetNode  string // for scale_up
	RemoveNode  string // for scale_down
}

func NewAutoscaler(clusterState *ClusterState, scheduler *Scheduler, cfg *Config, metricsStore *MetricsStore) *Autoscaler {
	return &Autoscaler{
		clusterState: clusterState,
		scheduler:    scheduler,
		cfg:          cfg,
		metricsStore: metricsStore,
	}
}

// lastActionAt returns the more recent of LastScaleUp/LastScaleDown for the
// service, or (zero, false) when nothing has been recorded.
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
func (as *Autoscaler) EvaluateService(ctx context.Context, svc *ServiceDefinition) (*ScalingDecision, error) {
	allocs, err := as.clusterState.ListAllocations(svc.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to list allocations: %w", err)
	}
	nodes, err := as.clusterState.ListNodes()
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	live := liveAllocations(allocs)

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
	// Use the more conservative of the two cooldowns so a recent scale-down
	// also gates a scale-up (and vice versa).
	cd := as.cfg.CooldownDown
	if as.cfg.CooldownUp > cd {
		cd = as.cfg.CooldownUp
	}
	return time.Since(last) < cd
}

// evaluateScaleUp checks for two scale-up signals: gateway traffic on a node
// that doesn't yet host the service (locality-aware placement), or any live
// copy of the service exceeding CPU/Memory targets. The traffic case is
// self-targeted — we want a copy ON the busy node. The overload case is the
// opposite: we add a copy on a *free* node so the existing hot copy can
// shed load. Targeting the overloaded node itself would just overwrite its
// allocation (KV key is alloc.<svc>.<node>).
func (as *Autoscaler) evaluateScaleUp(svc *ServiceDefinition, live []*ServiceAllocation, nodes []*NodeInfo) *ScalingDecision {
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

// evaluateScaleDown shrinks toward MinCopies when copies are clearly
// underused. Uses CAS-safe alloc.CPUUsage / MemoryUsage that the agent
// publishes from MetricsCollector. Threshold is half the scale-up target
// (e.g. TargetCPU=75 → shrink at <=37) to give hysteresis — without the
// gap, a service oscillates between scale-up and scale-down on each tick.
//
// Only running allocations count: a starting/pending copy has no metrics
// yet and shouldn't sway the average.
func (as *Autoscaler) evaluateScaleDown(svc *ServiceDefinition, live []*ServiceAllocation) *ScalingDecision {
	floor := as.cfg.MinCopies
	if floor < 1 {
		floor = 1
	}
	if len(live) <= floor {
		return nil
	}

	running := make([]*ServiceAllocation, 0, len(live))
	for _, a := range live {
		if a.Status == "running" && a.PID > 0 {
			running = append(running, a)
		}
	}
	// Wait until enough copies are stable-running to give meaningful averages
	// — a starting copy reports no metrics and would skew the floor check low.
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

// findNodeWithTrafficWithoutService returns a node that handles authenticated
// gateway traffic above the threshold but has no live copy of svc.
func (as *Autoscaler) findNodeWithTrafficWithoutService(nodes []*NodeInfo, live []*ServiceAllocation) *NodeInfo {
	hasService := nodeIDsOf(live)
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

// hasGatewayTraffic returns true when validated RPS sustained on `node`
// exceeds the configured threshold. Averages the last 60s of samples
// reported by gateway via asty.v1.metrics.gateway.*. With no samples (cold
// start), returns false — safer than speculative scale-up.
func (as *Autoscaler) hasGatewayTraffic(node *NodeInfo) bool {
	if as.metricsStore == nil {
		return false
	}
	points := as.metricsStore.Get("node."+node.ID+".rps", time.Now().Add(-60*time.Second))
	if len(points) == 0 {
		return false
	}
	var sum float64
	for _, p := range points {
		sum += p.Value
	}
	return sum/float64(len(points)) >= float64(as.cfg.TrafficRPSThreshold)
}

// findOverloadedAlloc returns the first live allocation whose CPU or memory
// usage exceeds the configured targets. Returns nil when no metrics are
// available yet — caller treats nil as "no decision". Only running copies
// with a real PID count: a starting copy reports nothing.
func (as *Autoscaler) findOverloadedAlloc(live []*ServiceAllocation) *ServiceAllocation {
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

// pickFreeNode returns a healthy node that doesn't yet host this service and
// has free resources. Delegates to the scheduler so geo-spread and the same
// stable tiebreak rules used during baseline reconcile are honored.
func (as *Autoscaler) pickFreeNode(svc *ServiceDefinition, live []*ServiceAllocation, nodes []*NodeInfo) *NodeInfo {
	healthy := as.scheduler.filterHealthyNodes(nodes)
	occupied := nodeIDsOf(live)
	dcCounts := datacenterCountsByOccupied(healthy, occupied)
	picks := as.scheduler.pickCandidates(svc, healthy, occupied, dcCounts, 1)
	if len(picks) == 0 {
		return nil
	}
	return picks[0]
}

// ExecuteScalingDecision applies a decision returned by EvaluateService.
func (as *Autoscaler) ExecuteScalingDecision(ctx context.Context, d *ScalingDecision, svc *ServiceDefinition) error {
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

func (as *Autoscaler) executeScaleUp(d *ScalingDecision, svc *ServiceDefinition) error {
	log.Info().
		Str("service", svc.Name).
		Str("target_node", d.TargetNode).
		Str("reason", d.Reason).
		Msg("scaling up")

	alloc := &ServiceAllocation{
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
	count := len(liveAllocations(allocs))
	as.metricsStore.AddEvent(ScalingEvent{
		Service:   svc.Name,
		Action:    "scale_up",
		Reason:    d.Reason,
		FromCount: count - 1,
		ToCount:   count,
		NodeID:    d.TargetNode,
	})
	return nil
}

func (as *Autoscaler) executeScaleDown(d *ScalingDecision, svc *ServiceDefinition) error {
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
	count := len(liveAllocations(allocs))
	as.metricsStore.AddEvent(ScalingEvent{
		Service:   svc.Name,
		Action:    "scale_down",
		Reason:    d.Reason,
		FromCount: count + 1,
		ToCount:   count,
		NodeID:    d.RemoveNode,
	})
	return nil
}

// pickAllocationToRemove chooses which copy to drop on scale-down. Preserves
// geo-diversity by removing from the most-represented DC first; ties broken
// by node ID for determinism.
func (as *Autoscaler) pickAllocationToRemove(live []*ServiceAllocation, nodes []*NodeInfo) *ServiceAllocation {
	if len(live) == 0 {
		return nil
	}
	nodeDC := make(map[string]string, len(nodes))
	for _, n := range nodes {
		nodeDC[n.ID] = datacenterOf(n)
	}
	dcCount := make(map[string]int)
	for _, a := range live {
		dcCount[nodeDC[a.NodeID]]++
	}
	sorted := append([]*ServiceAllocation(nil), live...)
	sort.Slice(sorted, func(i, j int) bool {
		di, dj := nodeDC[sorted[i].NodeID], nodeDC[sorted[j].NodeID]
		if dcCount[di] != dcCount[dj] {
			return dcCount[di] > dcCount[dj]
		}
		return sorted[i].NodeID < sorted[j].NodeID
	})
	return sorted[0]
}
