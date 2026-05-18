package server

import (
	"context"
	"sync"

	"asty/asty/internal/core/config"
	"asty/asty/internal/core/netutil"
	"asty/asty/internal/core/types"
	"asty/asty/internal/ops/autoscaler"
	autometrics "asty/asty/internal/ops/autoscaler/metrics"
	"asty/asty/internal/ops/reconciler"
	"asty/asty/internal/ops/discovery"
	"asty/asty/internal/ops/leader"
	"asty/asty/internal/infra/kv"
	"asty/asty/internal/ops/deployer"
	"asty/asty/internal/ops/drainer"
	"asty/asty/internal/infra/events"
	"asty/asty/internal/infra/logs"
	"asty/asty/internal/ops/scheduler"
	"asty/asty/internal/domain/proximity"

	apiPkg "asty/asty/internal/api/dashboard"

	"github.com/nats-io/nats.go"
)

// Server handles scheduling, autoscaling, and orchestration. The struct
// is intentionally a bag of dependencies; lifecycle is managed by Start
// (boot.go), and feature-specific behaviour lives in the per-feature
// files (commands.go, deployment.go, leadership.go, …).
type Server struct {
	cfg    *config.Config
	nc     *nats.Conn
	nodeID string

	clusterState    *kv.ClusterState
	leaderElection  *leader.Election
	nodeDiscovery   *discovery.NodeDiscovery
	scheduler       *scheduler.Scheduler
	autoscaler      *autoscaler.Autoscaler
	proximityMatrix *proximity.Matrix
	deployer        *deployer.Deployer
	serviceLoader   *deployer.ServiceLoader
	services        []*types.ServiceDefinition
	httpAPI         *apiPkg.API
	metricsStore    *autometrics.Store
	logBuffer       *logs.Buffer
	eventBuffer     *events.Buffer
	drainManager    *drainer.DrainManager
	streamHub       *streamHub

	// Leadership-scoped goroutines (scheduler/autoscaler) run under
	// leaderCtx, which is cancelled on loss of leadership. mu guards the
	// cancel handle and the controller reference when leadership flips.
	mu           sync.Mutex
	leaderCancel context.CancelFunc
	controller   *reconciler.ServiceController // non-nil only while this node is the leader
}

// New creates a new Server. NodeID falls back to the OS hostname if the
// caller doesn't supply one — useful in dev mode where every binary
// runs on the same host.
func New(cfg *config.Config) (*Server, error) {
	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = netutil.Hostname()
	}
	return &Server{
		cfg:    cfg,
		nodeID: nodeID,
	}, nil
}

// addClusterEvent stores e in the event buffer and fans it out to all
// active SSE subscribers via the stream hub. Called by the controller
// when reconcile produces a notable event (alloc_failed, scale_up, …).
func (s *Server) addClusterEvent(e types.ClusterEvent) {
	s.eventBuffer.Add(e)
	if s.streamHub != nil {
		s.streamHub.FanoutEvent(e)
	}
}
