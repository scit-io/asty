package asty

import autometrics "asty/internal/platform/asty/features/autoscaling/metrics"

// Backward-compatible aliases
type MetricsStore = autometrics.Store
type MetricPoint = autometrics.MetricPoint
type ScalingEvent = autometrics.ScalingEvent

var NewMetricsStore = autometrics.NewStore
