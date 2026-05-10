package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"asty/internal/platform/asty/core/types"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// DeploymentRecord stores deployment history
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

// DeploymentPlan describes a deployment
type DeploymentPlan struct {
	ServiceName      string
	CurrentVersion   string
	TargetVersion    string
	UpdateStrategy   UpdateStrategy
	Allocations      []*types.ServiceAllocation
	HealthyDeadline  time.Duration
	MinHealthyTime   time.Duration
	MaxParallel      int
	AutoRevert       bool
	Canary           int
}

// UpdateStrategy defines how to update
type UpdateStrategy struct {
	MaxParallel      int
	MinHealthyTime   time.Duration
	HealthyDeadline  time.Duration
	ProgressDeadline time.Duration
	AutoRevert       bool
	Canary           int
}

// DeploymentStatus tracks deployment progress
type DeploymentStatus struct {
	ServiceName    string
	Status         string // running, successful, failed, reverted
	Phase          string // canary, rolling, complete
	Updated        int
	Total          int
	CanaryHealthy  bool
	StartTime      time.Time
	EndTime        time.Time
	Error          string
}

// StateAccessor provides access to cluster state for deployments
type StateAccessor interface {
	GetAllocation(serviceName, nodeID string) (*types.ServiceAllocation, error)
	MutateAllocation(serviceName, nodeID string, fn func(*types.ServiceAllocation) bool) error
}

// Deployer handles service deployments with rolling updates
type Deployer struct {
	clusterState StateAccessor
	nc           *nats.Conn
	cfg          DeployerConfig

	mu      sync.Mutex
	history []DeploymentRecord
}

// DeployerConfig holds deployer-specific configuration
type DeployerConfig struct{}

// NewDeployer creates a new deployer
func NewDeployer(clusterState StateAccessor, nc *nats.Conn, cfg DeployerConfig) *Deployer {
	return &Deployer{
		clusterState: clusterState,
		nc:           nc,
		cfg:          cfg,
		history:      make([]DeploymentRecord, 0),
	}
}

// Deploy executes a rolling deployment
func (d *Deployer) Deploy(ctx context.Context, plan *DeploymentPlan) (*DeploymentStatus, error) {
	status := &DeploymentStatus{
		ServiceName: plan.ServiceName,
		Status:      "running",
		Phase:       "canary",
		Total:       len(plan.Allocations),
		StartTime:   time.Now(),
	}

	record := DeploymentRecord{
		ID:        fmt.Sprintf("deploy-%s-%d", plan.ServiceName, time.Now().UnixNano()),
		Service:   plan.ServiceName,
		Version:   plan.TargetVersion,
		Strategy:  "rolling",
		Status:    "running",
		StartedAt: time.Now(),
		Progress:  0,
	}
	if plan.Canary > 0 {
		record.Strategy = "canary"
	}
	d.addRecord(record)

	log.Info().
		Str("service", plan.ServiceName).
		Str("from", plan.CurrentVersion).
		Str("to", plan.TargetVersion).
		Int("total", status.Total).
		Msg("starting deployment")

	// Phase 1: Canary deployment
	if plan.Canary > 0 {
		canarySuccess, err := d.deployCanary(ctx, plan, status)
		if err != nil {
			return d.failDeployment(status, fmt.Errorf("canary failed: %w", err))
		}

		if !canarySuccess {
			if plan.AutoRevert {
				log.Warn().Str("service", plan.ServiceName).Msg("canary unhealthy, reverting")
				return d.revertDeployment(status, "canary unhealthy")
			}
			return d.failDeployment(status, fmt.Errorf("canary unhealthy"))
		}

		status.CanaryHealthy = true
		log.Info().Str("service", plan.ServiceName).Msg("canary healthy, proceeding to rolling update")
	}

	// Phase 2: Rolling update
	status.Phase = "rolling"
	if err := d.rollingUpdate(ctx, plan, status); err != nil {
		if plan.AutoRevert {
			log.Warn().Str("service", plan.ServiceName).Err(err).Msg("rolling update failed, reverting")
			return d.revertDeployment(status, err.Error())
		}
		return d.failDeployment(status, err)
	}

	// Complete
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

func (d *Deployer) deployCanary(ctx context.Context, plan *DeploymentPlan, status *DeploymentStatus) (bool, error) {
	if plan.Canary <= 0 {
		return true, nil
	}

	log.Info().
		Str("service", plan.ServiceName).
		Int("count", plan.Canary).
		Msg("deploying canary")

	canaryAllocs := plan.Allocations[:min(plan.Canary, len(plan.Allocations))]

	for _, alloc := range canaryAllocs {
		version := plan.TargetVersion
		if err := d.clusterState.MutateAllocation(plan.ServiceName, alloc.NodeID, func(a *types.ServiceAllocation) bool {
			a.Version = version
			a.Status = "pending"
			return true
		}); err != nil {
			return false, fmt.Errorf("failed to update canary allocation: %w", err)
		}

		if err := d.sendUpdateCommand(alloc.NodeID, plan.ServiceName, plan.TargetVersion); err != nil {
			return false, fmt.Errorf("failed to send update command: %w", err)
		}
	}

	deadline := time.Now().Add(plan.HealthyDeadline)
	healthyFor := time.Duration(0)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(5 * time.Second):
			healthy := d.checkAllocationsHealth(canaryAllocs)

			if healthy {
				healthyFor += 5 * time.Second
				if healthyFor >= plan.MinHealthyTime {
					log.Info().
						Str("service", plan.ServiceName).
						Dur("healthy_for", healthyFor).
						Msg("canary healthy")
					return true, nil
				}
			} else {
				healthyFor = 0
			}
		}
	}

	log.Warn().
		Str("service", plan.ServiceName).
		Dur("deadline", plan.HealthyDeadline).
		Msg("canary health check timeout")

	return false, nil
}

func (d *Deployer) rollingUpdate(ctx context.Context, plan *DeploymentPlan, status *DeploymentStatus) error {
	startIdx := 0
	if plan.Canary > 0 {
		startIdx = min(plan.Canary, len(plan.Allocations))
		status.Updated = startIdx
	}

	remaining := plan.Allocations[startIdx:]

	log.Info().
		Str("service", plan.ServiceName).
		Int("remaining", len(remaining)).
		Int("max_parallel", plan.MaxParallel).
		Msg("starting rolling update")

	for i := 0; i < len(remaining); i += plan.MaxParallel {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		end := min(i+plan.MaxParallel, len(remaining))
		batch := remaining[i:end]

		log.Info().
			Str("service", plan.ServiceName).
			Int("batch", len(batch)).
			Int("progress", status.Updated).
			Int("total", status.Total).
			Msg("updating batch")

		for _, alloc := range batch {
			version := plan.TargetVersion
			if err := d.clusterState.MutateAllocation(plan.ServiceName, alloc.NodeID, func(a *types.ServiceAllocation) bool {
				a.Version = version
				a.Status = "pending"
				return true
			}); err != nil {
				return fmt.Errorf("failed to update allocation: %w", err)
			}

			if err := d.sendUpdateCommand(alloc.NodeID, plan.ServiceName, plan.TargetVersion); err != nil {
				return fmt.Errorf("failed to send update command: %w", err)
			}
		}

		if !d.waitForBatchHealth(ctx, batch, plan) {
			return fmt.Errorf("batch update failed health check")
		}

		status.Updated += len(batch)

		time.Sleep(plan.MinHealthyTime)
	}

	return nil
}

func (d *Deployer) waitForBatchHealth(ctx context.Context, batch []*types.ServiceAllocation, plan *DeploymentPlan) bool {
	deadline := time.Now().Add(plan.HealthyDeadline)
	healthyFor := time.Duration(0)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(5 * time.Second):
			healthy := d.checkAllocationsHealth(batch)

			if healthy {
				healthyFor += 5 * time.Second
				if healthyFor >= plan.MinHealthyTime {
					return true
				}
			} else {
				healthyFor = 0
			}
		}
	}

	return false
}

func (d *Deployer) checkAllocationsHealth(allocs []*types.ServiceAllocation) bool {
	for _, alloc := range allocs {
		current, err := d.clusterState.GetAllocation(alloc.ServiceName, alloc.NodeID)
		if err != nil {
			return false
		}

		if current.Status != "running" {
			return false
		}
	}

	return true
}

func (d *Deployer) sendUpdateCommand(nodeID, serviceName, version string) error {
	subject := fmt.Sprintf("asty.v1.agent.%s.cmd", nodeID)

	cmd := types.Command{
		Type: "restart",
		Data: []byte(fmt.Sprintf(`{"service_name":"%s","version":"%s"}`, serviceName, version)),
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	_, err = d.nc.Request(subject, data, 30*time.Second)
	return err
}

func (d *Deployer) revertDeployment(status *DeploymentStatus, reason string) (*DeploymentStatus, error) {
	log.Warn().
		Str("service", status.ServiceName).
		Str("reason", reason).
		Msg("reverting deployment")

	status.Status = "reverted"
	status.Phase = "revert"
	status.Error = reason
	status.EndTime = time.Now()

	d.updateLastRecord("reverted", 0)

	return status, fmt.Errorf("deployment reverted: %s", reason)
}

func (d *Deployer) failDeployment(status *DeploymentStatus, err error) (*DeploymentStatus, error) {
	status.Status = "failed"
	status.Error = err.Error()
	status.EndTime = time.Now()

	d.updateLastRecord("failed", status.Updated*100/max(status.Total, 1))

	log.Error().
		Err(err).
		Str("service", status.ServiceName).
		Msg("deployment failed")

	return status, err
}

// GetDeploymentStatus retrieves current deployment status
func (d *Deployer) GetDeploymentStatus(serviceName string) (*DeploymentStatus, error) {
	return nil, fmt.Errorf("not implemented")
}

// GetHistory returns deployment history
func (d *Deployer) GetHistory() []DeploymentRecord {
	d.mu.Lock()
	defer d.mu.Unlock()

	result := make([]DeploymentRecord, len(d.history))
	copy(result, d.history)

	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result
}

func (d *Deployer) addRecord(record DeploymentRecord) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.history) >= 100 {
		d.history = d.history[1:]
	}
	d.history = append(d.history, record)
}

func (d *Deployer) updateLastRecord(status string, progress int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.history) == 0 {
		return
	}
	last := &d.history[len(d.history)-1]
	last.Status = status
	last.Progress = progress
	if status != "running" {
		last.CompletedAt = time.Now()
	}
}
