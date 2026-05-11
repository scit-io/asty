package server

import (
	"context"
	"time"

	"asty/internal/platform/asty/features/clustering/controller"

	"github.com/rs/zerolog/log"
)

// watchLeadership re-arms the leader loop on flips. It blocks until ctx
// is cancelled, so callers run it as its own goroutine.
func (s *Server) watchLeadership(ctx context.Context) {
	err := s.leaderElection.WatchLeadership(ctx,
		func() {
			log.Info().Msg("became leader")
			s.startLeaderWork(ctx)
		},
		func() {
			log.Info().Msg("lost leadership")
			s.stopLeaderWork()
		},
	)
	if err != nil {
		log.Error().Err(err).Msg("leadership watcher failed")
	}
}

// watchClusterNodes is a thin adapter over node discovery: it logs the
// current node list whenever DNS reports a change.
func (s *Server) watchClusterNodes(ctx context.Context) {
	s.nodeDiscovery.WatchNodes(ctx, func(nodes []string) {
		log.Info().
			Strs("nodes", nodes).
			Int("count", len(nodes)).
			Msg("cluster nodes updated")
	})
}

// startLeaderWork spawns the controller under a sub-context derived
// from the server context. Cancellation of that sub-context (on loss of
// leadership) stops the controller — workers drain, watchers exit, the
// workqueue shuts down. Idempotent: a second call while already running
// is a no-op.
func (s *Server) startLeaderWork(parent context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leaderCancel != nil {
		return
	}
	leaderCtx, cancel := context.WithCancel(parent)
	s.leaderCancel = cancel

	resync := computeResync(s.cfg.Autoscale.EvalInterval)
	ctrl := controller.NewServiceController(
		s.clusterState,
		s.scheduler,
		s.autoscaler,
		s.services,
		s.sendStartCommand,
		s.cfg.Autoscale.ControllerWorkers,
		resync,
	)
	ctrl.OnEvent = s.addClusterEvent
	go ctrl.Run(leaderCtx)
}

func (s *Server) stopLeaderWork() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leaderCancel != nil {
		s.leaderCancel()
		s.leaderCancel = nil
	}
}

// computeResync derives the controller's safety-net resync interval
// from EvalInterval. Watchers handle the reactive path; this only
// catches drift and drives the metric-driven autoscaler. We deliberately
// run resync slower than EvalInterval to avoid bursty re-reconciliation
// on every metric tick.
func computeResync(eval time.Duration) time.Duration {
	if eval <= 0 {
		return defaultResyncFallback
	}
	resync := eval * resyncMultiplier
	if resync > resyncCap {
		resync = resyncCap
	}
	return resync
}
