package deployment

import (
	"context"
	"fmt"
	"sync"
	"time"

	"asty/internal/platform/asty/core/types"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// DeploymentRecord stores deployment history.
type DeploymentRecord struct {
	ID          string    `json:"id"`
	Service     string    `json:"service"`
	Version     string    `json:"version"`
	Strategy    string    `json:"strategy"`
	Status      string    `json:"status"` // running, completed, failed, reverted
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Progress    int       `json:"progress"` // 0-100
}

// DeploymentPlan describes a deployment.
//
// All knobs (MaxParallel, MinHealthyTime, HealthyDeadline, AutoRevert,
// Canary) live on UpdateStrategy. The deployer reads them from there;
// the previous duplicated fields on the plan itself were a confusing
// holdover and have been removed.
type DeploymentPlan struct {
	ServiceName    string
	CurrentVersion string
	TargetVersion  string
	UpdateStrategy UpdateStrategy
	Allocations    []*types.ServiceAllocation
}

// UpdateStrategy defines how to update.
type UpdateStrategy struct {
	MaxParallel      int
	MinHealthyTime   time.Duration
	HealthyDeadline  time.Duration
	ProgressDeadline time.Duration
	AutoRevert       bool
	Canary           int
}

// DeploymentStatus tracks deployment progress.
type DeploymentStatus struct {
	ServiceName   string
	Status        string // running, successful, failed, reverted
	Phase         string // canary, rolling, complete
	Updated       int
	Total         int
	CanaryHealthy bool
	StartTime     time.Time
	EndTime       time.Time
	Error         string
}

// StateAccessor provides access to cluster state for deployments. Kept
// minimal so tests can inject a stub.
type StateAccessor interface {
	GetAllocation(serviceName, nodeID string) (*types.ServiceAllocation, error)
	MutateAllocation(serviceName, nodeID string, fn func(*types.ServiceAllocation) bool) error
}

// Deployer handles service deployments with rolling updates.
type Deployer struct {
	clusterState StateAccessor
	nc           *nats.Conn
	cfg          DeployerConfig

	mu      sync.Mutex
	history []DeploymentRecord
}

// DeployerConfig holds deployer-specific configuration. Empty for now;
// kept as a struct so adding knobs in the future doesn't break the API.
type DeployerConfig struct{}

// NewDeployer creates a new deployer.
func NewDeployer(clusterState StateAccessor, nc *nats.Conn, cfg DeployerConfig) *Deployer {
	return &Deployer{
		clusterState: clusterState,
		nc:           nc,
		cfg:          cfg,
		history:      make([]DeploymentRecord, 0),
	}
}

// Deploy runs a (canary →) rolling deployment, updating the deployment
// record as it progresses. Returns the final status; errors are also
// surfaced via the status struct so callers can inspect partial
// progress on failure.
func (d *Deployer) Deploy(ctx context.Context, plan *DeploymentPlan) (*DeploymentStatus, error) {
	status := &DeploymentStatus{
		ServiceName: plan.ServiceName,
		Status:      "running",
		Phase:       "canary",
		Total:       len(plan.Allocations),
		StartTime:   time.Now(),
	}

	d.beginRecord(plan)
	log.Info().
		Str("service", plan.ServiceName).
		Str("from", plan.CurrentVersion).
		Str("to", plan.TargetVersion).
		Int("total", status.Total).
		Msg("starting deployment")

	if plan.UpdateStrategy.Canary > 0 {
		ok, err := d.deployCanary(ctx, plan, status)
		if err != nil {
			return d.failDeployment(status, fmt.Errorf("canary failed: %w", err))
		}
		if !ok {
			if plan.UpdateStrategy.AutoRevert {
				log.Warn().Str("service", plan.ServiceName).Msg("canary unhealthy, reverting")
				return d.revertDeployment(status, "canary unhealthy")
			}
			return d.failDeployment(status, fmt.Errorf("canary unhealthy"))
		}
		status.CanaryHealthy = true
		log.Info().Str("service", plan.ServiceName).Msg("canary healthy, proceeding to rolling update")
	}

	status.Phase = "rolling"
	if err := d.rollingUpdate(ctx, plan, status); err != nil {
		if plan.UpdateStrategy.AutoRevert {
			log.Warn().Str("service", plan.ServiceName).Err(err).Msg("rolling update failed, reverting")
			return d.revertDeployment(status, err.Error())
		}
		return d.failDeployment(status, err)
	}

	status.Phase = "complete"
	status.Status = "successful"
	status.EndTime = time.Now()
	d.updateLastRecord("completed", 100)

	log.Info().
		Str("service", plan.ServiceName).
		Dur("duration", status.EndTime.Sub(status.StartTime)).
		Msg("deployment successful")
	return status, nil
}
