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
type Autoscaler struct {
	clusterState *ClusterState
	scheduler    *Scheduler
	cfg          *Config
	metricsStore *MetricsStore

	// Per-service cooldown tracking. Both maps are kept so the UI can show
	// "last scale-up" and "last scale-down" independently; inCooldown reads
	// the max of the two so an up event still gates a down (and vice versa).
	lastScaleUp   map[string]time.Time
	lastScaleDown map[string]time.Time
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
		clusterState:  clusterState,
		scheduler:     scheduler,
		cfg:           cfg,
		metricsStore:  metricsStore,
		lastScaleUp:   make(map[string]time.Time),
		lastScaleDown: make(map[string]time.Time),
	}
}

func (as *Autoscaler) lastActionAt(service string) (time.Time, bool) {
	up, hasUp := as.lastScaleUp[service]
	down, hasDown := as.lastScaleDown[service]
	switch {
	case hasUp && hasDown:
		if up.After(down) {
			return up, true
		}
		return down, true
	case hasUp:
		return up, true
	case hasDown:
		return down, true
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

// evaluateScaleUp checks for traffic on a node without a service copy, or
// resource overload on existing copies. Both signals depend on metrics that
// are wired through MetricsStore — until both arms return concrete data the
// autoscaler stays a no-op for growth.
func (as *Autoscaler) evaluateScaleUp(svc *ServiceDefinition, live []*ServiceAllocation, nodes []*NodeInfo) *ScalingDecision {
	if node := as.findNodeWithTrafficWithoutService(nodes, live); node != nil {
		return &ScalingDecision{
			ServiceName: svc.Name,
			Action:      "scale_up",
			Reason:      fmt.Sprintf("gateway traffic on node %s without %s", node.ID, svc.Name),
			TargetNode:  node.ID,
		}
	}
	if node := as.findOverloadedNode(svc, live, nodes); node != nil {
		return &ScalingDecision{
			ServiceName: svc.Name,
			Action:      "scale_up",
			Reason:      fmt.Sprintf("process on %s exceeded resource targets", node.ID),
			TargetNode:  node.ID,
		}
	}
	return nil
}

// evaluateScaleDown shrinks toward MinCopies when copies are clearly
// underused. Until process metrics are aggregated reliably this returns nil —
// previously a placeholder loop set allBelowTarget=true unconditionally and
// removed copies as soon as currentCount > MinCopies, which caused random
// churn. Hard-disable until the metrics path is real.
func (as *Autoscaler) evaluateScaleDown(svc *ServiceDefinition, live []*ServiceAllocation) *ScalingDecision {
	target := as.scheduler.targetCopies(len(live))
	if len(live) <= target {
		return nil
	}
	// TODO: aggregate per-allocation CPU/Memory from MetricsStore and only
	// shrink when the average across `live` is below TargetCPU/TargetMemory.
	// Until that's wired, refuse to shrink rather than guessing.
	return nil
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

// findOverloadedNode returns the node hosting the hottest live copy when
// CPU or memory is above the configured target. Falls back to nil when no
// metrics are available yet — caller treats nil as "no decision".
func (as *Autoscaler) findOverloadedNode(svc *ServiceDefinition, live []*ServiceAllocation, nodes []*NodeInfo) *NodeInfo {
	if as.metricsStore == nil {
		return nil
	}
	byID := make(map[string]*NodeInfo, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}
	for _, alloc := range live {
		if alloc.CPUUsage > as.cfg.TargetCPU || alloc.MemoryUsage > as.cfg.TargetMemory {
			if n, ok := byID[alloc.NodeID]; ok {
				return n
			}
		}
	}
	return nil
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
	as.lastScaleUp[svc.Name] = time.Now()

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
	as.lastScaleDown[svc.Name] = time.Now()

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
