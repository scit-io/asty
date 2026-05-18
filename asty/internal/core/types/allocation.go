package types

import "time"

// AllocationStatus is the lifecycle state of a single service copy on a
// node. Defined as a typed string so the compiler catches stray
// literals; the underlying string preserves the existing wire format.
type AllocationStatus string

const (
	// AllocPending — record exists in KV but no start command has
	// been dispatched yet. The controller picks these up.
	AllocPending AllocationStatus = "pending"

	// AllocStarting — start command dispatched, awaiting agent
	// confirmation. Stuck-in-starting allocs get reverted to pending
	// by the controller (see startingStuckAfter).
	AllocStarting AllocationStatus = "starting"

	// AllocRunning — agent has reported the process as alive AND the
	// first health probe passed.
	AllocRunning AllocationStatus = "running"

	// AllocRestarting — agent has observed an unexpected exit and is
	// inside the restart budget. The slot is still occupied by this
	// allocation; the next agent report will move it back to Running
	// (restart succeeded) or to Failed (attempts exhausted).
	AllocRestarting AllocationStatus = "restarting"

	// AllocStopping — Stop command dispatched; agent is asking the
	// process to exit gracefully. Followed by AllocStopped on clean
	// exit within kill_timeout or by AllocFailed on SIGKILL.
	AllocStopping AllocationStatus = "stopping"

	// AllocStopped — agent reported a clean exit (from a Stop call
	// or a drain). Not eligible for restart.
	AllocStopped AllocationStatus = "stopped"

	// AllocFailed — agent reported an unexpected exit with no restart
	// budget left, OR a kill_timeout-bound stop was escalated to
	// SIGKILL. The reconciler eventually prunes Failed records.
	AllocFailed AllocationStatus = "failed"

	// AllocDeleted — synthetic marker the state-watcher emits for KV
	// delete/purge events. Never persisted in KV.
	AllocDeleted AllocationStatus = "deleted"
)

// ServiceAllocation represents a service instance placement.
type ServiceAllocation struct {
	ID                  string           `json:"id"`
	ServiceName         string           `json:"service_name"`
	NodeID              string           `json:"node_id"`
	Status              AllocationStatus `json:"status"`
	Version             string           `json:"version"`
	PID                 int              `json:"pid"`
	StartedAt           time.Time        `json:"started_at"`
	HealthStatus        HealthState      `json:"health_status"`
	CPUUsage            int              `json:"cpu_usage"`    // percentage
	MemoryUsage         int              `json:"memory_usage"` // MB
	DiskUsage           int64            `json:"disk_usage"`   // MB, bytes-on-disk under <work_dir>/<service>
	Restarts            int              `json:"restarts"`
	ConsecutiveFailures int              `json:"consecutive_failures"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
}

// IsLive reports whether the allocation counts toward the desired set
// for placement decisions. Restarting counts as live because the slot
// is held while the agent re-runs the process; Stopping is also live
// because the process is still draining and another copy on the same
// node would race with it.
func (s AllocationStatus) IsLive() bool {
	switch s {
	case AllocPending, AllocStarting, AllocRunning, AllocRestarting, AllocStopping:
		return true
	}
	return false
}

// Occupies reports whether the allocation should prevent another copy
// of the same service from being scheduled onto the node. Adds Failed
// to IsLive because the slot is still owned until pruning catches up.
func (s AllocationStatus) Occupies() bool {
	switch s {
	case AllocPending, AllocStarting, AllocRunning, AllocRestarting, AllocStopping, AllocFailed:
		return true
	}
	return false
}

// ServiceCooldown captures the timestamps of the most recent autoscaler
// actions for a service plus the moment usage first dropped into the
// scale-down floor (drives the idle_hold hysteresis introduced in
// TZ §5.2). IdleSince is zero whenever the service is not currently
// observed as idle; the autoscaler resets it on every evaluation that
// exits the floor and seeds it on every evaluation that enters.
//
// RollbackFailed reflects the deploy FSM's terminal RollbackFailed
// state at the service level: the last deploy attempted auto_revert
// and the rollback dispatch itself did not return to healthy. The
// autoscaler reads this flag and refuses to act (scale up or scale
// down) until the operator clears it via the API. Without this gate
// the autoscaler would "fix" a mixed-version cluster by adding more
// copies, which is more dangerous than leaving it alone.
type ServiceCooldown struct {
	LastScaleUp    time.Time `json:"last_scale_up,omitempty"`
	LastScaleDown  time.Time `json:"last_scale_down,omitempty"`
	IdleSince      time.Time `json:"idle_since,omitempty"`
	RollbackFailed bool      `json:"rollback_failed,omitempty"`
}

// CooldownStatus describes which cooldowns are currently active and what
// the most-recent autoscaler action was. It mirrors the fields the API
// and the snapshot builder both surface to the UI.
type CooldownStatus struct {
	UpActive     bool          `json:"cooldown_up_active"`
	DownActive   bool          `json:"cooldown_down_active"`
	LastAction   ScalingAction `json:"last_action"`    // ScaleUp, ScaleDown, or "" if never scaled
	LastActionAt int64         `json:"last_action_at"` // unix seconds
}

// Status reports cooldown state at time `at` given the configured
// scale-up/down windows. The result tells the UI which controls to disable
// (cooldown is active) and labels the most recent action so operators see
// "why nothing happened".
func (c ServiceCooldown) Status(at time.Time, up, down time.Duration) CooldownStatus {
	var s CooldownStatus
	if !c.LastScaleUp.IsZero() {
		s.UpActive = at.Sub(c.LastScaleUp) < up
		s.LastAction = ScaleUp
		s.LastActionAt = c.LastScaleUp.Unix()
	}
	if !c.LastScaleDown.IsZero() {
		s.DownActive = at.Sub(c.LastScaleDown) < down
		if c.LastScaleDown.Unix() > s.LastActionAt {
			s.LastAction = ScaleDown
			s.LastActionAt = c.LastScaleDown.Unix()
		}
	}
	return s
}
