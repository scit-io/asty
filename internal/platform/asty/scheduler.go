package asty

import "asty/internal/platform/asty/features/scheduling"

// Backward-compatible aliases
type Scheduler = scheduling.Scheduler
type Placement = scheduling.Placement

var NewScheduler = scheduling.NewScheduler
