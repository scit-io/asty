package api

import (
	"context"

	"asty/internal/platform/asty/core/config"
	"asty/internal/platform/asty/core/types"
	autometrics "asty/internal/platform/asty/features/autoscaling/metrics"
	"asty/internal/platform/asty/features/clustering/leader"
	"asty/internal/platform/asty/features/clustering/state"
	"asty/internal/platform/asty/features/deployment"
	"asty/internal/platform/asty/features/draining"
	"asty/internal/platform/asty/features/observability/logs"

	"github.com/nats-io/nats.go"
)

// ServerContext is the set of capabilities the API needs from the server.
type ServerContext interface {
	ClusterState() *state.ClusterState
	Services() []*types.ServiceDefinition
	Config() *config.Config
	LeaderElection() *leader.Election
	MetricsStore() *autometrics.Store
	LogBuffer() *logs.Buffer
	EventBuffer() EventBufferReader
	DrainManager() *draining.DrainManager
	Deployer() *deployment.Deployer
	NATSConn() *nats.Conn
	StreamHub() StreamHub
	DeployService(ctx context.Context, service, version string) (*deployment.DeploymentStatus, error)
}

// StreamHub is the subset of streamHub behavior the API handlers need.
type StreamHub interface {
	Subscribe() (<-chan *types.ClusterSnapshot, func())
	SubscribeDrain() (<-chan []byte, func())
	SubscribeEvents() (<-chan []byte, func())
	Snapshot() *types.ClusterSnapshot
}

// EventBufferReader reads cluster events from the buffer.
type EventBufferReader interface {
	GetLast(n int) []types.ClusterEvent
}
