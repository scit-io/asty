package deployer

import (
	"time"

	"asty/asty/internal/core/types"
)

// DeploymentRecord stores deployment history. RollbackSteps is the
// audit trail of every rollback dispatch performed by revertDeployment
// — what got rolled back, to which version, and what happened. Empty
// on deployments that succeeded or failed without auto_revert.
type DeploymentRecord struct {
	ID            string         `json:"id"`
	Service       string         `json:"service"`
	Version       string         `json:"version"`
	Strategy      string         `json:"strategy"`
	Status        State          `json:"status"`
	StartedAt     time.Time      `json:"started_at"`
	CompletedAt   time.Time      `json:"completed_at,omitempty"`
	Progress      int            `json:"progress"` // 0-100
	RollbackSteps []RollbackStep `json:"rollback_steps,omitempty"`
}

// RollbackStep records one allocation walked back to the previous
// version during auto_revert. The deployer appends one entry per
// dispatch + one for the final batch-health verdict; operators
// reading the record can reconstruct exactly what happened during
// the rollback.
type RollbackStep struct {
	Timestamp time.Time `json:"timestamp"`
	NodeID    string    `json:"node_id,omitempty"` // empty for the batch-wait verdict
	FromVer   string    `json:"from_version"`
	ToVer     string    `json:"to_version"`
	Action    string    `json:"action"`  // "mark_pending", "send_update", "wait_health"
	Outcome   string    `json:"outcome"` // "ok" | "error"
	Error     string    `json:"error,omitempty"`
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
	// Service is the freshly-loaded definition the deployer ships to
	// agents in restart commands. The server populates it from the
	// service loader so per-deploy changes (env, resources, etc.) are
	// honoured.
	Service *types.ServiceDefinition
}

// UpdateStrategy defines how to update.
//
// CanaryRetries is the number of times the canary phase will retry
// dispatch before declaring the canary unhealthy. 0 means "no retry";
// 1 (default in svc.Update.GetCanaryRetries) covers the common slow-
// artifact-pull case without infinite re-tries.
type UpdateStrategy struct {
	MaxParallel     int
	MinHealthyTime  time.Duration
	HealthyDeadline time.Duration
	AutoRevert      bool
	Canary          int
	CanaryRetries   int
}

// DeploymentStatus tracks deployment progress.
type DeploymentStatus struct {
	ServiceName   string
	Status        State
	Phase         Phase
	Updated       int
	Total         int
	CanaryHealthy bool
	StartTime     time.Time
	EndTime       time.Time
	Error         string
}
