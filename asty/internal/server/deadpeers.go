package server

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// deadPeerQueryTimeout caps one JSZ broadcast collect and one
// $JS.API.SERVER.REMOVE round-trip. A no-response usually means the JS
// meta group is mid-election (the peer being removed may have BEEN the
// meta leader); the caller re-arms a retry rather than blocking.
const deadPeerQueryTimeout = 5 * time.Second

// reapDeadPeers evicts from the JetStream meta group every peer the meta
// leader reports as offline, via $JS.API.SERVER.REMOVE. Runs only on the
// leader, called from watchStreamReplicas on the same membership signal
// (and the NATS gossip signal) that drives replica upgrades. Returns
// true ("incomplete") when a removal could not be applied, so the caller
// schedules a retry.
//
// This is the "survivors clean up after the unplugged node" half of the
// resilience model. A departing node no longer decommissions itself
// (agent/start.go) — a dying meta leader proposing its own removal
// stalled the cluster KV for ~30s. Instead a healthy meta group, driven
// by the current leader, removes the dead peer; SERVER.REMOVE makes
// JetStream migrate the stream replicas that peer held onto live nodes,
// restoring full replication.
//
// The dead-peer set comes from the JetStream meta group via $SYS JSZ,
// NOT from a KV read. A dead replica makes NEW consumer creation on a KV
// stream stall, and listing nodes from KV creates exactly such a
// consumer — so a KV-based reaper deadlocks precisely when a peer needs
// reaping (observed: of two nodes killed at once, one was never reaped
// because the node-list read kept timing out on the dead replica). JSZ
// is served from a nats-server's in-memory RAFT state and stays
// answerable through the disruption.
//
// Self is never reaped. No-op when the SYS connection is absent (logged
// once at boot by connectNATSSys).
func (s *Server) reapDeadPeers() bool {
	if s.ncSys == nil {
		return false
	}
	// Standalone (clusterSize <= 1) is gated out by the caller
	// (reconcileCluster) — a lone node has no meta cluster to reap, and the JSZ
	// meta-leader query below would otherwise spin against a leaderless meta.
	// By here we are clustered.
	offline, err := s.offlineMetaPeers()
	if err != nil {
		log.Debug().Err(err).Msg("dead-peer reaper: JSZ meta query failed, will retry")
		return true
	}
	for _, peer := range offline {
		if peer == s.nodeID {
			continue
		}
		s.removeNATSPeer(peer)
	}
	// Re-arm a retry while ANY peer is offline. Nodes killed at the same
	// instant get marked offline by the meta leader a few seconds apart,
	// so one pass often sees only the first; without a retry the rest
	// wait for the next unrelated membership event (observed: a co-killed
	// peer lingered ~3 min). Retrying until the offline set is empty
	// drains stragglers within one retry interval. A removed peer drops
	// out of the next JSZ reply, so this terminates.
	return len(offline) > 0
}

// jszResponse is the slice of the $SYS JSZ reply the reaper needs: the
// reporting server's name plus its view of the meta group. Only the
// meta leader's view is authoritative — in RAFT only the leader pings
// every peer, so a follower cannot reliably tell which peers are
// offline.
type jszResponse struct {
	Server struct {
		Name string `json:"name"`
	} `json:"server"`
	Data struct {
		Meta struct {
			Leader   string `json:"leader"`
			Size     int    `json:"cluster_size"`
			Replicas []struct {
				Name    string `json:"name"`
				Offline bool   `json:"offline"`
			} `json:"replicas"`
		} `json:"meta_cluster"`
	} `json:"data"`
}

// offlineMetaPeers returns the node IDs (== NATS server_names) the
// JetStream meta leader currently considers offline. It broadcasts
// $SYS.REQ.SERVER.PING.JSZ, then keeps the one reply that came FROM the
// meta leader (its meta_cluster.leader names itself). No meta leader yet
// (election in flight) surfaces as an error so the caller retries —
// exactly the window where SERVER.REMOVE would fail anyway.
func (s *Server) offlineMetaPeers() ([]string, error) {
	inbox := nats.NewInbox()
	sub, err := s.ncSys.SubscribeSync(inbox)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sub.Unsubscribe() }()
	if err := s.ncSys.Flush(); err != nil {
		return nil, err
	}
	if err := s.ncSys.PublishRequest("$SYS.REQ.SERVER.PING.JSZ", inbox, nil); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(deadPeerQueryTimeout)
	for time.Now().Before(deadline) {
		msg, err := sub.NextMsg(time.Until(deadline))
		if err != nil {
			break
		}
		var resp jszResponse
		if json.Unmarshal(msg.Data, &resp) != nil {
			continue
		}
		// Trust only the meta leader's view of who is offline.
		if resp.Data.Meta.Leader == "" || resp.Data.Meta.Leader != resp.Server.Name {
			continue
		}
		out := make([]string, 0)
		for _, r := range resp.Data.Meta.Replicas {
			if r.Offline {
				out = append(out, r.Name)
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("no meta-leader JSZ response within %s", deadPeerQueryTimeout)
}

// removeNATSPeer issues one $JS.API.SERVER.REMOVE for peer and reports
// whether the peer is now gone (true) or the call needs retrying
// (false). A transport timeout means the meta group has no leader yet
// (electing, often because the removed peer was the meta leader) — retry.
// A definitive error response (peer already gone) is treated as done so
// a missing peer doesn't loop forever.
func (s *Server) removeNATSPeer(peer string) bool {
	body, err := json.Marshal(map[string]string{"peer": peer})
	if err != nil {
		return false
	}
	msg, err := s.ncSys.Request("$JS.API.SERVER.REMOVE", body, deadPeerQueryTimeout)
	if err != nil {
		log.Debug().Err(err).Str("peer", peer).Msg("dead-peer reaper: SERVER.REMOVE no response (meta electing?), will retry")
		return false
	}
	var resp struct {
		Success bool `json:"success"`
		Error   *struct {
			Description string `json:"description"`
		} `json:"error"`
	}
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		log.Debug().Err(err).Str("peer", peer).Msg("dead-peer reaper: bad SERVER.REMOVE response, will retry")
		return false
	}
	if resp.Error != nil {
		// A definitive answer from a reachable meta leader — usually
		// "peer not found" because a prior pass already removed it.
		// Treat as done; retrying can't change a definitive answer.
		log.Debug().Str("peer", peer).Str("err", resp.Error.Description).Msg("dead-peer reaper: SERVER.REMOVE rejected, treating as done")
		return true
	}
	if !resp.Success {
		return false
	}
	log.Info().Str("peer", peer).Msg("dead-peer reaper: removed offline peer from NATS meta; its stream replicas will migrate onto live nodes")
	return true
}
