package server

import (
	"asty/asty/internal/core/config"
	"asty/asty/internal/core/types"
	"asty/asty/internal/infra/kv"
	"asty/asty/internal/infra/logs"
	autometrics "asty/asty/internal/ops/autoscaler/metrics"
	"asty/asty/internal/ops/deployer"
	"asty/asty/internal/ops/drainer"
	"asty/asty/internal/ops/leader"
	"asty/asty/internal/ops/scheduler"

	apiPkg "asty/asty/internal/api/dashboard"

	"github.com/nats-io/nats.go"
)

// This file holds the simple getter methods that satisfy the API and
// drain interfaces (api.ServerContext, drainer.DrainDeps). Keeping them
// in one place prevents the rest of the server package from spreading
// "give me state" boilerplate across feature files.

func (s *Server) ClusterState() *kv.ClusterState        { return s.clusterState }
func (s *Server) Scheduler() *scheduler.Scheduler       { return s.scheduler }
func (s *Server) Services() []*types.ServiceDefinition  { return s.services }
func (s *Server) Config() *config.Config                { return s.cfg }
func (s *Server) LeaderElection() *leader.Election      { return s.leaderElection }
func (s *Server) MetricsStore() *autometrics.Store      { return s.metricsStore }
func (s *Server) LogBuffer() *logs.Buffer               { return s.logBuffer }
func (s *Server) EventBuffer() apiPkg.EventBufferReader { return s.eventBuffer }
func (s *Server) DrainManager() *drainer.DrainManager   { return s.drainManager }
func (s *Server) Deployer() *deployer.Deployer          { return s.deployer }
func (s *Server) NATSConn() *nats.Conn                  { return s.nc }
func (s *Server) StreamHub() apiPkg.StreamHub           { return s.streamHub }

// ClusterStabilized reports whether the cluster has fully healed from the
// last membership change — every Asty KV stream at its target replica count
// and fully current. SINGLE SOURCE OF TRUTH, derived ON DEMAND from NATS's
// own stream state (clusterHealed): any node computes the same answer by
// asking NATS, so there is no cached flag to drift and no writer to race on
// a leadership change. The kill gate reads this; after a kill the cluster is
// no longer healed (a replica is gone), so the gate closes on its own.
func (s *Server) ClusterStabilized() bool { return s.clusterHealed() }

// Compile-time interface checks. These guarantee that adding a new
// method to ServerContext or DrainDeps without implementing it here
// fails the build instead of crashing at runtime.
var (
	_ apiPkg.ServerContext = (*Server)(nil)
	_ drainer.DrainDeps    = (*Server)(nil)
)
