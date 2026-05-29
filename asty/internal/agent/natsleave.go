package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog/log"
)

// natsLeaveTimeout bounds every JS admin round-trip during shutdown.
// With healthy meta-RAFT (SERVER.REMOVE keeps it in sync) real commits
// land in tens of ms; this ceiling only matters as a deadlock fallback.
const natsLeaveTimeout = 5 * time.Second

// survivingClusterPeers counts live `node.<id>` entries minus self.
// DNS discovery only updates on a start.sh remove or a DNS edit — a
// dashboard kill doesn't, so it lies about membership the moment kills
// go through the UI. KV is updated by every agent's graceful path
// (RemoveNode before exit), so it tracks reality. Fallback to the DNS
// count on KV failure beats returning 0, which would skip both shrink
// AND decommission.
func (a *Agent) survivingClusterPeers() int {
	if a.clusterState != nil {
		nodes, err := a.clusterState.ListNodes()
		if err == nil {
			n := 0
			for _, node := range nodes {
				if node.ID != a.nodeID {
					n++
				}
			}
			return n
		}
		log.Warn().Err(err).Msg("pre-departure: ListNodes failed, falling back to DNS peer count")
	}
	return len(a.resolveNATSPeers(a.resolveNodeIP()))
}

// shrinkStreamsToSingle runs only when the surviving cluster size will
// be 1: it transfers leadership away from us (so NATS keeps R=1 data
// on the survivor) and lowers Replicas to 1 on every R>1 stream.
// Without this, the survivor's nats-server cannot serve previously-
// replicated streams after the cluster→standalone transition.
func (a *Agent) shrinkStreamsToSingle() {
	if a.nc == nil {
		return
	}
	js, err := jetstream.New(a.nc)
	if err != nil {
		log.Warn().Err(err).Msg("pre-departure shrink: jetstream init failed")
		return
	}
	listCtx, cancel := context.WithTimeout(context.Background(), natsLeaveTimeout)
	defer cancel()
	for info := range js.ListStreams(listCtx).Info() {
		if info == nil || info.Config.Replicas <= 1 {
			continue
		}
		if info.Cluster != nil && info.Cluster.Leader == a.nodeID {
			a.transferStreamLeader(info.Config.Name)
		}
		cfg := info.Config
		from := cfg.Replicas
		cfg.Replicas = 1
		updCtx, updCancel := context.WithTimeout(context.Background(), natsLeaveTimeout)
		if _, err := js.UpdateStream(updCtx, cfg); err != nil {
			ev := log.Warn()
			var jsErr jetstream.JetStreamError
			if errors.As(err, &jsErr) && jsErr.APIError() != nil {
				ev = ev.Uint16("code", uint16(jsErr.APIError().ErrorCode))
			}
			ev.Err(err).Str("stream", cfg.Name).Int("from", from).Msg("pre-departure: replicas-to-1 failed")
		} else {
			log.Info().Str("stream", cfg.Name).Int("from", from).Msg("pre-departure: replicas lowered to 1")
		}
		updCancel()
	}
}

// transferStreamLeader hands stream leadership to another peer and
// blocks on the LEADER_ELECTED advisory — purely event-driven, no
// time-based wait.
func (a *Agent) transferStreamLeader(stream string) {
	sub, err := a.nc.SubscribeSync(fmt.Sprintf("$JS.EVENT.ADVISORY.STREAM.LEADER_ELECTED.%s", stream))
	if err != nil {
		return
	}
	defer func() { _ = sub.Unsubscribe() }()
	if err := a.nc.Flush(); err != nil {
		return
	}
	reply, err := a.nc.Request(fmt.Sprintf("$JS.API.STREAM.LEADER.STEPDOWN.%s", stream), []byte("{}"), natsLeaveTimeout)
	if err != nil {
		return
	}
	var resp struct {
		Success bool `json:"success"`
	}
	if json.Unmarshal(reply.Data, &resp) != nil || !resp.Success {
		return
	}
	if _, err := sub.NextMsg(natsLeaveTimeout); err == nil {
		log.Info().Str("stream", stream).Msg("pre-departure: stream leader transferred")
	}
}

// decommissionSelf is the official graceful-decommission path:
// $JS.API.SERVER.REMOVE proposes one EntryRemovePeer in meta-RAFT
// that both shrinks the meta-cluster config and remaps every stream
// we are a member of. Skipping it would leave dead peers in the meta
// config and the quorum target unreachable for any later proposal.
//
// Pure event-driven: subscribe, publish, wait for SERVER.REMOVED
// advisory. Requires SYS-account access (see deploy/*/config.asty).
//
// Callers MUST run shrinkStreamsToSingle BEFORE this when
// surviving == 1 — SERVER.REMOVE disables our JS, so UpdateStream
// after would have nothing to talk to.
func (a *Agent) decommissionSelf() {
	if a.ncSys == nil {
		return
	}
	sub, err := a.ncSys.SubscribeSync("$JS.EVENT.ADVISORY.SERVER.REMOVED")
	if err != nil {
		return
	}
	defer func() { _ = sub.Unsubscribe() }()
	if err := a.ncSys.Flush(); err != nil {
		return
	}
	body, err := json.Marshal(map[string]string{"peer": a.nodeID})
	if err != nil {
		return
	}
	if err := a.ncSys.Publish("$JS.API.SERVER.REMOVE", body); err != nil {
		return
	}
	// Flush so the publish reaches NATS before we block on the
	// advisory — Publish is buffered.
	if err := a.ncSys.Flush(); err != nil {
		return
	}
	if _, err := sub.NextMsg(natsLeaveTimeout); err == nil {
		log.Info().Msg("pre-departure: decommissioned from meta and stream groups")
	}
}
