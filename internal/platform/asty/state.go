package asty

import (
	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/clustering/state"
)

// Type aliases for backward compatibility
type NodeInfo = types.NodeInfo
type ServiceAllocation = types.ServiceAllocation
type ServiceCooldown = types.ServiceCooldown
type ClusterState = state.ClusterState

var NewClusterState = state.New
