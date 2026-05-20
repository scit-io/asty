package server

import (
	"context"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// watchSelfRemoval ends Start when our own `node.<id>` entry is
// deleted from cluster KV. The local agent's graceful path publishes
// that delete (RemoveNode in agent/start.go) before stopping
// nats-server, so the event reaches us over a still-live connection.
//
// Without this, a dashboard kill — which signals only the agent —
// leaves the server process alive, refreshing the leader lease over
// auto-discovered NATS peers; no other node can claim leadership and
// subsequent writes proxy into a self-loop.
func (s *Server) watchSelfRemoval(ctx context.Context, cancel context.CancelFunc) {
	err := s.clusterState.WatchNodes(ctx, func(n *types.NodeInfo) {
		if n == nil || n.ID != s.nodeID || n.Status != types.NodeDeleted {
			return
		}
		log.Info().Str("node_id", s.nodeID).Msg("self node removed from KV; shutting down server")
		cancel()
	})
	if err != nil && ctx.Err() == nil {
		log.Warn().Err(err).Msg("self-removal watcher errored")
	}
}
