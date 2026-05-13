package api

import (
	"context"

	"asty/asty/internal/core/config"
	"asty/asty/internal/core/types"
	autometrics "asty/asty/internal/features/autoscaling/metrics"
	"asty/asty/internal/features/clustering/leader"
	"asty/asty/internal/features/clustering/state"
	"asty/asty/internal/features/deployment"
	"asty/asty/internal/features/draining"
	"asty/asty/internal/features/observability/logs"

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
	StopServiceOnNode(nodeID, serviceName string) error
	ReconcileService(svcName string)
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
