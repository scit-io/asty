package asty

import "asty/internal/platform/asty/features/execution/health"

// Backward-compatible aliases
type HealthChecker = health.Checker
type HealthCheck = health.Check

var NewHealthChecker = health.NewChecker
