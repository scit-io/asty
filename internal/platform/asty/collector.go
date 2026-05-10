package asty

import "asty/internal/platform/asty/features/observability/metrics"

// Backward-compatible aliases
type MetricsCollector = metrics.Collector
type ProcessMetrics = metrics.ProcessMetrics

var NewMetricsCollector = metrics.NewCollector
