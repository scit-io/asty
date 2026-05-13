package server

import (
	"asty/asty/internal/core/config"
	"asty/asty/internal/core/types"
	autometrics "asty/asty/internal/features/autoscaling/metrics"
	"asty/asty/internal/features/clustering/leader"
	"asty/asty/internal/features/clustering/state"
	"asty/asty/internal/features/deployment"
	"asty/asty/internal/features/draining"
	"asty/asty/internal/features/observability/logs"
	"asty/asty/internal/features/scheduling"

	apiPkg "asty/asty/internal/features/api"

	"github.com/nats-io/nats.go"
)

// This file holds the simple getter methods that satisfy the API and
// drain interfaces (api.ServerContext, draining.DrainDeps). Keeping them
// in one place prevents the rest of the server package from spreading
// "give me state" boilerplate across feature files.

// --- api.ServerContext ---

func (s *Server) ClusterState() *state.ClusterState     { return s.clusterState }
func (s *Server) Services() []*types.ServiceDefinition  { return s.services }
func (s *Server) Config() *config.Config                { return s.cfg }
func (s *Server) LeaderElection() *leader.Election      { return s.leaderElection }
func (s *Server) MetricsStore() *autometrics.Store      { return s.metricsStore }
func (s *Server) LogBuffer() *logs.Buffer               { return s.logBuffer }
func (s *Server) EventBuffer() apiPkg.EventBufferReader { return s.eventBuffer }
func (s *Server) DrainManager() *draining.DrainManager  { return s.drainManager }
func (s *Server) Deployer() *deployment.Deployer        { return s.deployer }
func (s *Server) NATSConn() *nats.Conn                  { return s.nc }
func (s *Server) StreamHub() apiPkg.StreamHub           { return s.streamHub }

// --- draining.DrainDeps ---

func (s *Server) GetClusterState() *state.ClusterState    { return s.clusterState }
func (s *Server) GetScheduler() *scheduling.Scheduler     { return s.scheduler }
func (s *Server) GetServices() []*types.ServiceDefinition { return s.services }
func (s *Server) GetNATSConn() *nats.Conn                 { return s.nc }

// Compile-time interface checks. These guarantee that adding a new
// method to ServerContext or DrainDeps without implementing it here
// fails the build instead of crashing at runtime.
var (
	_ apiPkg.ServerContext = (*Server)(nil)
	_ draining.DrainDeps   = (*Server)(nil)
)
