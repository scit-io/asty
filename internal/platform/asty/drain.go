package asty

import "asty/internal/platform/asty/features/draining"

// Backward-compatible aliases
type DrainStatus = draining.DrainStatus
type DrainManager = draining.DrainManager
type DrainDeps = draining.DrainDeps

var NewDrainManager = draining.NewDrainManager
