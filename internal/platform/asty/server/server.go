package server

import (
	"context"
	"sync"

	"asty/internal/platform/asty/core/config"
	"asty/internal/platform/asty/core/netutil"
	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/autoscaling"
	autometrics "asty/internal/platform/asty/features/autoscaling/metrics"
	"asty/internal/platform/asty/features/clustering/discovery"
	"asty/internal/platform/asty/features/clustering/leader"
	"asty/internal/platform/asty/features/clustering/state"
	"asty/internal/platform/asty/features/deployment"
	"asty/internal/platform/asty/features/draining"
	"asty/internal/platform/asty/features/observability/events"
	"asty/internal/platform/asty/features/observability/logs"
	"asty/internal/platform/asty/features/scheduling"
	"asty/internal/platform/asty/features/scheduling/proximity"

	apiPkg "asty/internal/platform/asty/features/api"

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

	clusterState    *state.ClusterState
	leaderElection  *leader.Election
	nodeDiscovery   *discovery.NodeDiscovery
	scheduler       *scheduling.Scheduler
	autoscaler      *autoscaling.Autoscaler
	proximityMatrix *proximity.Matrix
	deployer        *deployment.Deployer
	serviceLoader   *deployment.ServiceLoader
	services        []*types.ServiceDefinition
	httpAPI         *apiPkg.API
	metricsStore    *autometrics.Store
	logBuffer       *logs.Buffer
	eventBuffer     *events.Buffer
	drainManager    *draining.DrainManager
	streamHub       *streamHub

	// Leadership-scoped goroutines (scheduler/autoscaler) run under
	// leaderCtx, which is cancelled on loss of leadership. mu guards the
	// cancel handle when leadership flips.
	mu           sync.Mutex
	leaderCancel context.CancelFunc
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
