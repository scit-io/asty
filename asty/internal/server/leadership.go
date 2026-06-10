package server

import (
	"context"
	"time"

	"asty/asty/internal/ops/reconciler"

	"github.com/rs/zerolog/log"
)

// wireLeadershipCallbacks installs the become/lose-leadership hooks on
// the election state machine BEFORE CampaignForLeader starts. The
// canonical NATS KV-election pattern fires these callbacks directly from
// its single campaign goroutine on every Create / Update return-value
// transition — there is NO separate watcher that drives leadership state.
//
// startLeaderWork / stopLeaderWork take s.mu; election.try() drops e.mu
// around the callback so the lock order is e.mu → s.mu (and never the
// reverse), making this race-free without a global ordering doc.
func (s *Server) wireLeadershipCallbacks(ctx context.Context) {
	s.leaderElection.SetCallbacks(
		func() {
			log.Info().Msg("became leader")
			s.startLeaderWork(ctx)
		},
		func() {
			log.Info().Msg("lost leadership")
			s.stopLeaderWork()
		},
	)
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
	ctrl := reconciler.NewServiceController(
		s.clusterState,
		s.scheduler,
		s.autoscaler,
		s.services,
		s.sendStartCommand,
		s.cfg.Autoscale.ControllerWorkers,
		resync,
	)
	ctrl.OnEvent = s.addClusterEvent
	s.controller = ctrl
	go ctrl.Run(leaderCtx)
	go s.watchStreamReplicas(leaderCtx)
}

func (s *Server) stopLeaderWork() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leaderCancel != nil {
		s.leaderCancel()
		s.leaderCancel = nil
	}
	s.controller = nil
	// Nothing to reset: cluster-stabilized is derived on demand from NATS
	// (clusterHealed), not a flag this process owns.
}

// ReconcileService enqueues svcName for re-reconciliation on the leader's
// controller. No-op when this node is not currently the leader. Used by API
// handlers after operator-driven state changes (scale, restart, stop) to
// avoid waiting for the periodic resync.
func (s *Server) ReconcileService(svcName string) {
	s.mu.Lock()
	ctrl := s.controller
	s.mu.Unlock()
	if ctrl != nil {
		ctrl.Enqueue(svcName)
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
