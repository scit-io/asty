package deployer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"asty/asty/internal/core/types"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// ErrDeployInFlight is returned by Deploy when another rollout for
// the same service is already running on this Deployer. The dashboard
// surfaces it as a 409 so the operator sees "deploy already in
// progress" instead of silently launching a clobbering run.
var ErrDeployInFlight = errors.New("deploy already in progress for this service")

// StateAccessor provides access to cluster state for deployments. The
// real ClusterState satisfies it; tests inject a stub. SetRollbackFailed
// lets the deployer flag a service that failed auto_revert so the
// autoscaler stops touching it; the operator clears the flag via the API
// once they reconcile the mixed-version state.
//
// WatchAllocations is the sole batch-health mechanism: waitForBatchHealth
// (wait.go) reacts to KV alloc-status events, never polls. It also
// satisfies the narrow allocWatcher there, so no type assertion is
// needed.
//
// Get/SetServiceVersion is the version pin scheduler.createAllocation
// reads when placing new copies — Deploy is the sole writer so that
// autoscaler-spawned allocs created mid-rollout pick up the new
// version instead of drifting to a stale "latest".
type StateAccessor interface {
	GetAllocation(serviceName, nodeID string) (*types.ServiceAllocation, error)
	WatchAllocations(ctx context.Context, onChange func(*types.ServiceAllocation)) error
	MutateAllocation(serviceName, nodeID string, fn func(*types.ServiceAllocation) bool) error
	SetRollbackFailed(serviceName string, failed bool) error
	SetDeployInProgress(serviceName string, active bool) error
	GetServiceVersion(serviceName string) (types.ServiceVersion, error)
	SetServiceVersion(serviceName string, v types.ServiceVersion) error
}

// SendRestartCommand is the dispatcher the deployer uses to tell an
// agent to apply a new version. Mirrors reconciler.SendStartCommand
// in spirit: the server provides the implementation, the deployer
// stays decoupled from NATS and URL-resolution details.
type SendRestartCommand func(nodeID string, svc *types.ServiceDefinition, version string) error

// Deployer handles service deployments with rolling updates.
type Deployer struct {
	clusterState StateAccessor
	nc           *nats.Conn
	restart      SendRestartCommand

	mu       sync.Mutex
	history  []DeploymentRecord
	inFlight map[string]bool // serviceName → true while a deploy is running

	// touchedMu protects the touched slice for the active deployment.
	// Deploy() resets it at the start of each run; recordTouched
	// appends from canary and rolling dispatch loops. revertDeployment
	// reads it to know which allocs to walk back.
	touchedMu sync.Mutex
	touched   []*types.ServiceAllocation
}

// NewDeployer creates a new deployer. restart is the per-dispatch
// callback used by the rolling-update path to push a new version to a
// node's agent (server.sendRestartCommand in production).
func NewDeployer(clusterState StateAccessor, nc *nats.Conn, restart SendRestartCommand) *Deployer {
	return &Deployer{
		clusterState: clusterState,
		nc:           nc,
		restart:      restart,
		history:      make([]DeploymentRecord, 0),
		inFlight:     make(map[string]bool),
	}
}

// Concurrency guards (claim/release/IsInFlight) and the small KV
// helpers used by Deploy (pinVersion, setDeployGate) live in
// lifecycle.go alongside their shared concept.

// Deploy runs a (canary →) rolling deployment, updating the deployment
// record as it progresses. Returns the final status; errors are also
// surfaced via the status struct so callers can inspect partial
// progress on failure.
//
// Before any dispatch the service-level version pin is updated to
// {Current: target, Previous: plan.CurrentVersion} so that allocations
// created by the scheduler mid-rollout pick up the new version. On
// success Previous is cleared; on revert Current is restored from
// Previous (see history.go).
//
// Refuses to start a second concurrent deploy on the same service —
// returns ErrDeployInFlight so the dashboard can surface a 409 rather
// than silently launching racing goroutines that would clobber each
// other's `touched` slice.
func (d *Deployer) Deploy(ctx context.Context, plan *DeploymentPlan) (*DeploymentStatus, error) {
	if !d.claim(plan.ServiceName) {
		return nil, ErrDeployInFlight
	}
	defer d.release(plan.ServiceName)

	status := &DeploymentStatus{
		ServiceName: plan.ServiceName,
		Status:      StateRunning,
		Phase:       PhaseCanary,
		Total:       len(plan.Allocations),
		StartTime:   time.Now(),
	}

	d.resetTouched()
	d.beginRecord(plan)
	d.pinVersion(plan.ServiceName, types.ServiceVersion{
		Current:  plan.TargetVersion,
		Previous: plan.CurrentVersion,
	})
	d.setDeployGate(plan.ServiceName, true)
	defer d.setDeployGate(plan.ServiceName, false)
	log.Info().
		Str("service", plan.ServiceName).
		Str("from", plan.CurrentVersion).
		Str("to", plan.TargetVersion).
		Int("total", status.Total).
		Msg("starting deployment")

	if plan.UpdateStrategy.Canary > 0 {
		ok, err := d.deployCanaryWithRetries(ctx, plan, status)
		if err != nil {
			return d.handleFailure(ctx, plan, status, fmt.Errorf("canary failed: %w", err))
		}
		if !ok {
			return d.handleFailure(ctx, plan, status, fmt.Errorf("canary unhealthy"))
		}
		status.CanaryHealthy = true
		log.Info().Str("service", plan.ServiceName).Msg("canary healthy, proceeding to rolling update")
	}

	status.Phase = PhaseRolling
	if err := d.rollingUpdate(ctx, plan, status); err != nil {
		return d.handleFailure(ctx, plan, status, err)
	}

	status.Phase = PhaseComplete
	status.Status = StateCompleted
	status.EndTime = time.Now()
	d.updateLastRecord(StateCompleted, 100)
	d.pinVersion(plan.ServiceName, types.ServiceVersion{Current: plan.TargetVersion})

	log.Info().
		Str("service", plan.ServiceName).
		Dur("duration", status.EndTime.Sub(status.StartTime)).
		Msg("deployment successful")
	return status, nil
}

// handleFailure decides between rollback and outright failure based on
// AutoRevert, then delegates. Centralised so both canary and rolling
// failure paths take the same branch.
func (d *Deployer) handleFailure(ctx context.Context, plan *DeploymentPlan, status *DeploymentStatus, cause error) (*DeploymentStatus, error) {
	if plan.UpdateStrategy.AutoRevert {
		log.Warn().Str("service", plan.ServiceName).Err(cause).Msg("deploy failure, reverting")
		return d.revertDeployment(ctx, plan, status, cause.Error())
	}
	return d.failDeployment(status, cause)
}

// Touched-set helpers (recordTouched, resetTouched, touchedSnapshot)
// live in touched.go alongside their shared concept.
