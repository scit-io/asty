package types

// ScalingAction names the three possible outcomes of an autoscaler
// evaluation. Defined as a typed string so the compiler catches stray
// literals; the underlying string preserves the JSON wire format used
// by ScalingDecision and Allocation.LastAction.
type ScalingAction string

const (
	// ScaleNone — the autoscaler evaluated the service and decided no
	// action is needed (within thresholds, or in cooldown).
	ScaleNone ScalingAction = "none"

	// ScaleUp — add a copy on TargetNode.
	ScaleUp ScalingAction = "scale_up"

	// ScaleDown — remove the copy on RemoveNode.
	ScaleDown ScalingAction = "scale_down"
)
