package asty

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// Autoscaler handles automatic scaling decisions
type Autoscaler struct {
	clusterState *ClusterState
	scheduler    *Scheduler
	cfg          *Config
	metricsStore *MetricsStore

	// Cooldown tracking
	lastScaleUp   map[string]time.Time // key: service name
	lastScaleDown map[string]time.Time
}

// NewAutoscaler creates a new autoscaler
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

// ScalingDecision represents an autoscaling decision
type ScalingDecision struct {
	ServiceName string
	Action      string // scale_up, scale_down, none
	Reason      string
	TargetNode  string // for scale_up
}

// EvaluateService evaluates if a service needs scaling
func (as *Autoscaler) EvaluateService(ctx context.Context, svc *ServiceDefinition) (*ScalingDecision, error) {
	// Get current allocations
	allocs, err := as.clusterState.ListAllocations(svc.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to list allocations: %w", err)
	}

	runningAllocs := as.filterRunningAllocations(allocs)
	currentCount := len(runningAllocs)

	// Get all nodes
	nodes, err := as.clusterState.ListNodes()
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	// Check for scale up conditions
	decision := as.evaluateScaleUp(svc, runningAllocs, nodes)
	if decision != nil {
		return decision, nil
	}

	// Check for scale down conditions
	decision = as.evaluateScaleDown(svc, runningAllocs, currentCount)
	if decision != nil {
		return decision, nil
	}

	return &ScalingDecision{
		ServiceName: svc.Name,
		Action:      "none",
		Reason:      "within target thresholds",
	}, nil
}

// evaluateScaleUp checks if service needs to scale up
func (as *Autoscaler) evaluateScaleUp(svc *ServiceDefinition, allocs []*ServiceAllocation, nodes []*NodeInfo) *ScalingDecision {
	// Check cooldown
	if lastUp, exists := as.lastScaleUp[svc.Name]; exists {
		if time.Since(lastUp) < as.cfg.CooldownUp {
			return nil
		}
	}

	// 1. Check for Gateway traffic on nodes without service
	nodeWithTraffic := as.findNodeWithTrafficWithoutService(nodes, allocs)
	if nodeWithTraffic != nil {
		return &ScalingDecision{
			ServiceName: svc.Name,
			Action:      "scale_up",
			Reason:      fmt.Sprintf("gateway traffic on node %s without service", nodeWithTraffic.ID),
			TargetNode:  nodeWithTraffic.ID,
		}
	}

	// 2. Check for process overload (CPU/Memory >75%)
	overloadedNode := as.findOverloadedNode(allocs, nodes)
	if overloadedNode != nil {
		return &ScalingDecision{
			ServiceName: svc.Name,
			Action:      "scale_up",
			Reason:      fmt.Sprintf("process overloaded on node %s", overloadedNode.ID),
			TargetNode:  overloadedNode.ID,
		}
	}

	return nil
}

// evaluateScaleDown checks if service needs to scale down
func (as *Autoscaler) evaluateScaleDown(svc *ServiceDefinition, allocs []*ServiceAllocation, currentCount int) *ScalingDecision {
	minCopies := as.cfg.MinCopies
	if minCopies < 3 {
		minCopies = 3
	}

	// Don't scale below minimum
	if currentCount <= minCopies {
		return nil
	}

	// Check cooldown
	if lastDown, exists := as.lastScaleDown[svc.Name]; exists {
		if time.Since(lastDown) < as.cfg.CooldownDown {
			return nil
		}
	}

	// Check if all processes are below target
	allBelowTarget := true
	for _, alloc := range allocs {
		// TODO: get actual metrics from collector
		// For now, assume below target
		_ = alloc
	}

	if allBelowTarget {
		return &ScalingDecision{
			ServiceName: svc.Name,
			Action:      "scale_down",
			Reason:      "all processes below target thresholds",
		}
	}

	return nil
}

// findNodeWithTrafficWithoutService finds a node with Gateway traffic but no service instance
func (as *Autoscaler) findNodeWithTrafficWithoutService(nodes []*NodeInfo, allocs []*ServiceAllocation) *NodeInfo {
	// Build map of nodes with service
	nodesWithService := make(map[string]bool)
	for _, alloc := range allocs {
		nodesWithService[alloc.NodeID] = true
	}

	// Check each node for traffic
	for _, node := range nodes {
		if node.Status != "ready" {
			continue
		}

		// Already has service
		if nodesWithService[node.ID] {
			continue
		}

		// Check if node has Gateway traffic
		// TODO: implement actual traffic metrics check
		// For now, use placeholder logic
		hasTraffic := as.hasGatewayTraffic(node)
		if hasTraffic {
			return node
		}
	}

	return nil
}

// hasGatewayTraffic checks if a node has Gateway traffic above threshold
func (as *Autoscaler) hasGatewayTraffic(node *NodeInfo) bool {
	// TODO: implement actual traffic metrics
	// Should check:
	// 1. Gateway valid_rps > A_TRAFFIC_RPS_THRESHOLD
	// 2. Over sliding window A_TRAFFIC_WINDOW
	// 3. Filtered by authenticated traffic only

	// Placeholder: check if Gateway process exists
	for _, process := range node.Processes {
		if process == "gateway" {
			// TODO: query actual traffic metrics
			return false
		}
	}

	return false
}

// findOverloadedNode finds a node where process is overloaded
func (as *Autoscaler) findOverloadedNode(allocs []*ServiceAllocation, nodes []*NodeInfo) *NodeInfo {
	// TODO: integrate with metrics collector
	// Check if any process has:
	// - CPU > A_TARGET_CPU (75%)
	// - Memory > A_TARGET_MEMORY (75%)

	// Placeholder
	return nil
}

// filterRunningAllocations returns only running allocations
func (as *Autoscaler) filterRunningAllocations(allocs []*ServiceAllocation) []*ServiceAllocation {
	running := make([]*ServiceAllocation, 0)
	for _, alloc := range allocs {
		if alloc.Status == "running" {
			running = append(running, alloc)
		}
	}
	return running
}

// ExecuteScalingDecision executes a scaling decision
func (as *Autoscaler) ExecuteScalingDecision(ctx context.Context, decision *ScalingDecision, svc *ServiceDefinition) error {
	switch decision.Action {
	case "scale_up":
		return as.executeScaleUp(ctx, decision, svc)
	case "scale_down":
		return as.executeScaleDown(ctx, decision, svc)
	case "none":
		return nil
	default:
		return fmt.Errorf("unknown action: %s", decision.Action)
	}
}

// executeScaleUp adds a service instance
func (as *Autoscaler) executeScaleUp(ctx context.Context, decision *ScalingDecision, svc *ServiceDefinition) error {
	log.Info().
		Str("service", svc.Name).
		Str("reason", decision.Reason).
		Str("target_node", decision.TargetNode).
		Msg("scaling up")

	// Create allocation
	alloc := &ServiceAllocation{
		ServiceName: svc.Name,
		NodeID:      decision.TargetNode,
		Status:      "pending",
		Version:     "latest", // TODO: version management
	}

	if err := as.clusterState.CreateAllocation(alloc); err != nil {
		return fmt.Errorf("failed to create allocation: %w", err)
	}

	// Update cooldown
	as.lastScaleUp[svc.Name] = time.Now()

	// Record event
	allocs, _ := as.clusterState.ListAllocations(svc.Name)
	as.metricsStore.AddEvent(ScalingEvent{
		Service:   svc.Name,
		Action:    "scale_up",
		Reason:    decision.Reason,
		FromCount: len(allocs) - 1,
		ToCount:   len(allocs),
		NodeID:    decision.TargetNode,
	})

	return nil
}

// executeScaleDown removes a service instance
func (as *Autoscaler) executeScaleDown(ctx context.Context, decision *ScalingDecision, svc *ServiceDefinition) error {
	log.Info().
		Str("service", svc.Name).
		Str("reason", decision.Reason).
		Msg("scaling down")

	// Get current allocations
	allocs, err := as.clusterState.ListAllocations(svc.Name)
	if err != nil {
		return fmt.Errorf("failed to list allocations: %w", err)
	}

	runningAllocs := as.filterRunningAllocations(allocs)

	// Find least loaded node to remove
	targetAlloc := as.selectAllocationToRemove(runningAllocs)
	if targetAlloc == nil {
		return fmt.Errorf("no allocation to remove")
	}

	fromCount := len(runningAllocs)

	// Delete allocation
	if err := as.clusterState.DeleteAllocation(svc.Name, targetAlloc.NodeID); err != nil {
		return fmt.Errorf("failed to delete allocation: %w", err)
	}

	// Update cooldown
	as.lastScaleDown[svc.Name] = time.Now()

	// Record event
	as.metricsStore.AddEvent(ScalingEvent{
		Service:   svc.Name,
		Action:    "scale_down",
		Reason:    decision.Reason,
		FromCount: fromCount,
		ToCount:   fromCount - 1,
		NodeID:    targetAlloc.NodeID,
	})

	log.Info().
		Str("service", svc.Name).
		Str("node_id", targetAlloc.NodeID).
		Msg("scaled down")

	return nil
}

// selectAllocationToRemove selects the allocation to remove (least loaded)
func (as *Autoscaler) selectAllocationToRemove(allocs []*ServiceAllocation) *ServiceAllocation {
	if len(allocs) == 0 {
		return nil
	}

	// TODO: select based on actual load metrics
	// For now, just pick the first one
	// Should preserve geo-diversity

	return allocs[0]
}

// Run starts the autoscaler evaluation loop
func (as *Autoscaler) Run(ctx context.Context, services []*ServiceDefinition) {
	ticker := time.NewTicker(as.cfg.EvalInterval)
	defer ticker.Stop()

	log.Info().
		Dur("interval", as.cfg.EvalInterval).
		Msg("autoscaler started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			as.evaluateAllServices(ctx, services)
		}
	}
}

// evaluateAllServices evaluates all services for scaling
func (as *Autoscaler) evaluateAllServices(ctx context.Context, services []*ServiceDefinition) {
	for _, svc := range services {
		// Skip system services (not autoscaled)
		if svc.Type == ServiceTypeSystem {
			continue
		}

		decision, err := as.EvaluateService(ctx, svc)
		if err != nil {
			log.Error().Err(err).Str("service", svc.Name).Msg("failed to evaluate service")
			continue
		}

		if decision.Action != "none" {
			log.Info().
				Str("service", svc.Name).
				Str("action", decision.Action).
				Str("reason", decision.Reason).
				Msg("autoscaling decision")

			if err := as.ExecuteScalingDecision(ctx, decision, svc); err != nil {
				log.Error().Err(err).Str("service", svc.Name).Msg("failed to execute scaling decision")
			}
		}
	}
}
