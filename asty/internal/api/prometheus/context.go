package prometheus

import (
	"asty/asty/internal/core/types"
	"asty/asty/internal/infra/kv"
	"asty/asty/internal/ops/deployer"
)

// Context is the minimal capability set the prometheus exposition
// needs from the server. Keeping it small (and local to this package)
// lets the package compile without importing api/rest or server, and
// lets tests inject a stub.
type Context interface {
	ClusterState() *kv.ClusterState
	Services() []*types.ServiceDefinition
	StreamHub() SnapshotSource
	MetricsStore() RPSSource
	Deployer() DeployHistorySource
}

// SnapshotSource is the subset of StreamHub the collectors read. The
// real StreamHub (in server/) satisfies it; tests provide a stub that
// returns a precomputed *types.ClusterSnapshot.
type SnapshotSource interface {
	Snapshot() *types.ClusterSnapshot
}

// RPSSource is the subset of MetricsStore the cluster collector reads.
// MetricsStore in ops/autoscaler/metrics satisfies it; a nil RPSSource
// is tolerated by the collector (RPS just stays at zero).
type RPSSource interface {
	GetLatestRPS(nodeID string) float64
}

// DeployHistorySource is the subset of *deployer.Deployer the deploy
// collector reads. GetHistory returns recent records newest-first; the
// collector picks the latest per-service for the asty_deploy_* gauges.
type DeployHistorySource interface {
	GetHistory() []deployer.DeploymentRecord
}
