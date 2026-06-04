package server

import (
	"encoding/json"
	"fmt"
)

// clusterSize is the number of servers in the JetStream RAFT meta group —
// NATS's OWN authoritative view of cluster membership, read from this node's
// JSZ ($SYS.REQ.SERVER.<id>.JSZ, targeted at the local server). It is the
// SINGLE replica-size source: the reconcile, bucket creation (autoReplicas /
// systemReplicas), and clusterHealed.
//
// gossip (DiscoveredServers) is deliberately NOT the size. It is a cumulative
// client-discovery set that never shrinks and stays stale after a solo
// collapse, whereas the RAFT meta IS the cluster — per NATS docs a stopped
// server remains a meta peer until explicitly removed, and the dead-peer
// reaper does exactly that, so this count stays honest. An empty meta_cluster
// means standalone JetStream (the natssolo survivor, or a fresh single node)
// → 1, so a lone node targets R=1 and never re-inflates its own streams.
// gossip stays ONLY as an early reconcile trigger (gossipChanged), never the
// size. Falls back to the gossip count only when the SYS connection is absent
// (the reaper is disabled then too) or the JSZ query fails.
func (s *Server) clusterSize() int {
	if s.ncSys == nil {
		return len(s.nc.DiscoveredServers()) + 1
	}
	subj := fmt.Sprintf("$SYS.REQ.SERVER.%s.JSZ", s.ncSys.ConnectedServerId())
	msg, err := s.ncSys.Request(subj, nil, deadPeerQueryTimeout)
	if err != nil {
		return len(s.nc.DiscoveredServers()) + 1
	}
	var resp jszResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		return len(s.nc.DiscoveredServers()) + 1
	}
	if resp.Data.Meta.Size > 0 {
		return resp.Data.Meta.Size
	}
	// Distinct meta members: self + leader + replicas, de-duped — correct
	// whether or not meta_cluster.replicas includes the reporting server.
	// Empty meta_cluster (standalone) leaves just self ⇒ 1.
	peers := make(map[string]struct{}, len(resp.Data.Meta.Replicas)+2)
	if resp.Server.Name != "" {
		peers[resp.Server.Name] = struct{}{}
	}
	if resp.Data.Meta.Leader != "" {
		peers[resp.Data.Meta.Leader] = struct{}{}
	}
	for _, r := range resp.Data.Meta.Replicas {
		if r.Name != "" {
			peers[r.Name] = struct{}{}
		}
	}
	if len(peers) == 0 {
		return 1
	}
	return len(peers)
}
