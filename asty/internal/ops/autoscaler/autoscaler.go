package autoscaler

import (
	"context"
	"fmt"

	"asty/asty/internal/core/config"
	"asty/asty/internal/core/types"
	"asty/asty/internal/ops/autoscaler/metrics"
	"asty/asty/internal/infra/kv"
	"asty/asty/internal/ops/scheduler"
)

// ScalingDecision describes what the autoscaler intends to do next.
// Action is one of types.ScaleUp / ScaleDown / ScaleNone. TargetNode
// is set for ScaleUp; RemoveNode is set for ScaleDown.
type ScalingDecision struct {
	ServiceName string
	Action      types.ScalingAction
	Reason      string
	TargetNode  string
	RemoveNode  string
}

// Autoscaler grows services above MinCopies in response to traffic and
// resource pressure, and shrinks back to MinCopies when load subsides.
type Autoscaler struct {
	clusterState *kv.ClusterState
	scheduler    *scheduler.Scheduler
	cfg          *config.Config
	metricsStore *metrics.Store
}

// NewAutoscaler constructs an autoscaler. The metricsStore is the only
// source for "is there gateway traffic on this node" decisions; without
// it, locality-aware scale-up degrades to resource-pressure-only.
func NewAutoscaler(clusterState *kv.ClusterState, scheduler *scheduler.Scheduler, cfg *config.Config, metricsStore *metrics.Store) *Autoscaler {
	return &Autoscaler{
		clusterState: clusterState,
		scheduler:    scheduler,
		cfg:          cfg,
		metricsStore: metricsStore,
	}
}

// EvaluateService decides whether svc should grow, shrink, or stay put.
// Cooldowns are checked first so we don't oscillate on every tick.
// A service whose previous deployment ended in RollbackFailed is left
// alone entirely — the cluster is in mixed-version limbo and any
// "fixup" the autoscaler attempts could amplify the inconsistency.
// The operator clears the flag via the API after reconciling state
// manually.
func (as *Autoscaler) EvaluateService(ctx context.Context, svc *types.ServiceDefinition) (*ScalingDecision, error) {
	allocs, err := as.clusterState.ListAllocations(svc.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to list allocations: %w", err)
	}
	nodes, err := as.clusterState.ListNodes()
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	if cd, err := as.clusterState.GetServiceCooldown(svc.Name); err == nil {
		if cd.RollbackFailed {
			return noop(svc.Name, "rollback_failed: operator intervention required"), nil
		}
		if cd.DeployInProgress {
			return noop(svc.Name, "deploy in progress"), nil
		}
	}

	live := scheduler.LiveAllocations(allocs)

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
	case types.ScaleUp:
		return as.executeScaleUp(d, svc)
	case types.ScaleDown:
		return as.executeScaleDown(d, svc)
	case types.ScaleNone:
		return nil
	}
	return fmt.Errorf("unknown action: %s", d.Action)
}

func noop(name, reason string) *ScalingDecision {
	return &ScalingDecision{ServiceName: name, Action: types.ScaleNone, Reason: reason}
}
