package asty

import "asty/internal/platform/asty/features/autoscaling"

// Backward-compatible aliases
type Autoscaler = autoscaling.Autoscaler
type ScalingDecision = autoscaling.ScalingDecision

var NewAutoscaler = autoscaling.NewAutoscaler
