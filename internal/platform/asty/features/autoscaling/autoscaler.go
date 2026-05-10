package autoscaling

import (
	"context"
	"fmt"

	"asty/internal/platform/asty/core/config"
	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/autoscaling/metrics"
	"asty/internal/platform/asty/features/clustering/state"
	"asty/internal/platform/asty/features/scheduling"
)

// ScalingDecision describes what the autoscaler intends to do next.
// Action is one of "scale_up", "scale_down", "none". TargetNode is set
// for scale_up; RemoveNode is set for scale_down.
type ScalingDecision struct {
	ServiceName string
	Action      string
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

// NewAutoscaler constructs an autoscaler. The metricsStore is the only
// source for "is there gateway traffic on this node" decisions; without
// it, locality-aware scale-up degrades to resource-pressure-only.
func NewAutoscaler(clusterState *state.ClusterState, scheduler *scheduling.Scheduler, cfg *config.Config, metricsStore *metrics.Store) *Autoscaler {
	return &Autoscaler{
		clusterState: clusterState,
		scheduler:    scheduler,
		cfg:          cfg,
		metricsStore: metricsStore,
	}
}

// EvaluateService decides whether svc should grow, shrink, or stay put.
// Cooldowns are checked first so we don't oscillate on every tick.
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

// ExecuteScalingDecision applies a decision returned by EvaluateService.
// Splits to per-action methods that update KV and record an event.
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

func noop(name, reason string) *ScalingDecision {
	return &ScalingDecision{ServiceName: name, Action: "none", Reason: reason}
}
