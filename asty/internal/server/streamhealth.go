package server

import "github.com/nats-io/nats.go/jetstream"

// clusterHealed reports whether every Asty-owned KV stream sits at the replica
// count the NATS cluster size warrants AND is fully current (leader present,
// all replicas online & current). DERIVED ON DEMAND from NATS's own state —
// the single source of truth: any node computes the same answer by asking
// NATS, so there is no cached flag to drift and no writer to race on a
// leadership change. Read-only; used by the kill gate (ClusterStabilized) and
// the dashboard snapshot/metric. Uses the same clusterSize (the JetStream meta
// group — see kv.go) the reconcile targets, so "healed" means "streams match
// what the reconcile drives toward". The shared s.js handle keeps this off the
// per-call jetstream.New path even though it runs on every snapshot build.
func (s *Server) clusterHealed() bool {
	size := s.clusterSize()
	infos := s.js.ListStreams(s.lifeCtx)
	for info := range infos.Info() {
		if info == nil {
			continue
		}
		target := s.targetReplicasFor(info.Config.Name, size)
		if target == 0 {
			continue // a stream Asty doesn't own
		}
		if info.Config.Replicas != target || !streamFullyCurrent(info, info.Config.Replicas) {
			return false
		}
	}
	return infos.Err() == nil
}

// streamFullyCurrent reports whether a stream at its target replica count
// is fully placed and caught up: a leader plus enough online, current
// followers. R=1 is trivially current (no quorum group).
func streamFullyCurrent(info *jetstream.StreamInfo, replicas int) bool {
	if replicas <= 1 {
		return true
	}
	if info.Cluster == nil || info.Cluster.Leader == "" {
		return false
	}
	online := 1 // the leader itself
	for _, r := range info.Cluster.Replicas {
		if r != nil && r.Current && !r.Offline {
			online++
		}
	}
	return online >= replicas
}
