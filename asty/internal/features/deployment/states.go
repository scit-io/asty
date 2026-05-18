package deployment

// State is the terminal outcome of a deployment run. Used on both
// DeploymentRecord and DeploymentStatus so history and live status
// share one vocabulary (previously Record used "completed" while
// Status used "successful" for the same event).
type State string

const (
	// StateRunning — deployment is in progress.
	StateRunning State = "running"

	// StateCompleted — every batch reached MinHealthyTime; deploy succeeded.
	StateCompleted State = "completed"

	// StateFailed — a batch missed HealthyDeadline or another non-revert error.
	StateFailed State = "failed"

	// StateReverted — auto_revert kicked in and successfully rolled
	// allocations back to CurrentVersion.
	StateReverted State = "reverted"

	// StateRollbackFailed — auto_revert was attempted but the rollback
	// dispatch itself failed (allocations did not return to healthy on
	// CurrentVersion). The service is in mixed-version limbo and needs
	// operator action; autoscale should refuse to make changes for this
	// service until cleared.
	StateRollbackFailed State = "rollback_failed"
)

// Phase is the lifecycle stage of a deployment run, complementary to
// State (which holds the final result). The canary-then-rolling
// workflow walks Canary → Rolling → Complete; auto-revert detours to
// Revert.
type Phase string

const (
	PhaseCanary   Phase = "canary"
	PhaseRolling  Phase = "rolling"
	PhaseComplete Phase = "complete"
	PhaseRevert   Phase = "revert"
)
