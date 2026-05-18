package rest

import (
	"context"

	"asty/asty/internal/core/config"
	"asty/asty/internal/core/types"
	autometrics "asty/asty/internal/ops/autoscaler/metrics"
	"asty/asty/internal/ops/leader"
	"asty/asty/internal/infra/kv"
	"asty/asty/internal/ops/deployer"
	"asty/asty/internal/ops/drainer"
	"asty/asty/internal/infra/logs"

	"github.com/nats-io/nats.go"
)

// ServerContext is the set of capabilities the API needs from the server.
type ServerContext interface {
	ClusterState() *kv.ClusterState
	Services() []*types.ServiceDefinition
	Config() *config.Config
	LeaderElection() *leader.Election
	MetricsStore() *autometrics.Store
	LogBuffer() *logs.Buffer
	EventBuffer() EventBufferReader
	DrainManager() *drainer.DrainManager
	Deployer() *deployer.Deployer
	NATSConn() *nats.Conn
	StreamHub() StreamHub
	DeployService(ctx context.Context, service, version string) (*deployer.DeploymentStatus, error)
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
