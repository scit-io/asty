package asty

import "asty/internal/platform/asty/features/clustering/leader"

// Backward-compatible aliases
type LeaderElection = leader.Election
type LeaderInfo = leader.Info

var NewLeaderElection = leader.NewElection
