package server

import (
	"fmt"
	"sort"
	"time"

	"asty/asty/internal/core/types"
	"asty/asty/internal/ops/deployer"
	"asty/asty/internal/ops/scheduler"
)

// Deployment timing defaults — used when a service .asty file leaves
// the corresponding update.* field blank. Picked to be safe for typical
// services: 10 s of healthy-time before promotion, 3 min to reach
// healthy.
const (
	defaultMinHealthyTime  = 10 * time.Second
	defaultHealthyDeadline = 3 * time.Minute
)

// DeployService accepts a deploy request and starts the rollout
// asynchronously. Returns immediately with a `running` status; full
// progress is delivered via the asty.v1.deploy.progress.<service>
// SSE stream that the UI subscribes to. The browser does NOT block
// on the rollout — multi-minute rolling deploys outlive any
// reasonable fetch timeout, so the synchronous-wait model the
// handler used previously was unreliable in practice.
//
// Bootstrap: when no live allocations exist yet, the version pin is
// set directly and the scheduler picks it up on its next reconcile
// pass. No canary/rolling phase needed because there's nothing to
// update. Returns Completed immediately so the dashboard can show
// the pin took effect.
//
// CurrentVersion comes from the per-service pin (kv.GetServiceVersion);
// absent pin means "first deploy" and the field is left empty so an
// eventual revert refuses to fire (no version to roll back to).
//
// Refuses concurrent deploys for the same service via the deployer's
// in-flight set — caller maps deployer.ErrDeployInFlight to a 409.
func (s *Server) DeployService(serviceName, version string) (*deployer.DeploymentStatus, error) {
	if version == "" {
		return nil, fmt.Errorf("version required")
	}
	if s.deployer.IsInFlight(serviceName) {
		return nil, deployer.ErrDeployInFlight
	}

	svc, err := s.serviceLoader.GetService(serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to load service definition: %w", err)
	}

	pinned, _ := s.clusterState.GetServiceVersion(serviceName)

	allocs, err := s.clusterState.ListAllocations(serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to list allocations: %w", err)
	}
	live := scheduler.LiveAllocations(allocs)
	sort.Slice(live, func(i, j int) bool { return live[i].NodeID < live[j].NodeID })

	if len(live) == 0 {
		if err := s.clusterState.SetServiceVersion(serviceName, types.ServiceVersion{Current: version}); err != nil {
			return nil, fmt.Errorf("pin version: %w", err)
		}
		s.ReconcileService(serviceName)
		now := time.Now()
		return &deployer.DeploymentStatus{
			ServiceName: serviceName, Status: deployer.StateCompleted,
			Phase: deployer.PhaseComplete, StartTime: now, EndTime: now,
		}, nil
	}

	plan := &deployer.DeploymentPlan{
		ServiceName:    serviceName,
		CurrentVersion: pinned.Current,
		TargetVersion:  version,
		Allocations:    live,
		Service:        svc,
		UpdateStrategy: deployer.UpdateStrategy{
			MaxParallel:     svc.Update.GetMaxParallel(),
			MinHealthyTime:  types.ParseDurationOr(svc.Update.MinHealthyTime, defaultMinHealthyTime),
			HealthyDeadline: types.ParseDurationOr(svc.Update.HealthyDeadline, defaultHealthyDeadline),
			AutoRevert:      svc.Update.AutoRevert,
			Canary:          svc.Update.Canary,
			CanaryRetries:   svc.Update.CanaryRetries,
		},
	}

	go func() {
		if _, err := s.deployer.Deploy(s.lifeCtx, plan); err != nil {
			// Final state is already recorded in history + KV + SSE
			// via updateLastRecord/persistLast — nothing more to do
			// from this goroutine.
			_ = err
		}
	}()

	return &deployer.DeploymentStatus{
		ServiceName: plan.ServiceName,
		Status:      deployer.StateRunning,
		Phase:       deployer.PhaseCanary,
		Total:       len(live),
		StartTime:   time.Now(),
	}, nil
}
