package leader

import (
	"context"
	"time"

	"asty/asty/internal/core/codec"

	"github.com/rs/zerolog/log"
)

// CampaignForLeader runs the canonical NATS KV-election state machine:
// a single goroutine tickling the bucket on a fixed interval, deriving
// its leader/candidate state from the return values of Create and Update.
// There is no separate watcher that writes state — the watch surface
// elsewhere in this package only feeds the read cache and the
// event-driven wake channel.
//
// Three triggers advance the loop:
//   - ticker.C  — canonical ≥75% TTL refresh / claim cadence.
//   - wakeCh    — leader-info watcher saw a delete/purge event
//                 (previous leader's lease expired or was released).
//                 Candidates Create immediately instead of waiting
//                 for the next tick, cutting SIGKILL failover from
//                 ~(TTL + campaignInterval) to ~one Watch-event RTT.
//   - ctx.Done  — graceful shutdown.
//
// State writes still happen ONLY inside try() under e.mu. wakeCh is
// purely a scheduler hint.
func (e *Election) CampaignForLeader(ctx context.Context) error {
	e.try(ctx)

	ticker := time.NewTicker(campaignInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.stepDown()
			return nil
		case <-ticker.C:
			e.try(ctx)
		case <-e.wakeCh:
			e.try(ctx)
		}
	}
}

// try is the state-machine step. Holds e.mu for the whole step so the
// IsLeader / lastSeq view is always self-consistent. Mirrors the
// canonical `try()` in ripienaar/nats-kv-leader-elect.
func (e *Election) try(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch e.state {
	case StateLeader:
		e.maintainLeadership(ctx)
	default:
		e.campaignForLeadership(ctx)
	}
}

// campaignForLeadership tries to claim the slot via Create. A successful
// Create returns the sequence number we then have to pass to Update on
// every refresh. Failure (key exists) leaves us as a candidate — another
// node holds the slot.
//
// Mutex held by caller (try).
func (e *Election) campaignForLeadership(ctx context.Context) {
	data := encodeInfo(Info{ID: e.nodeID, IP: e.nodeIP, Host: e.nodeHost})
	opCtx, cancel := context.WithTimeout(ctx, kvOpTimeout)
	defer cancel()
	seq, err := e.bucket.Create(opCtx, leaderKey, data)
	if err != nil {
		log.Debug().Err(err).Msg("failed to claim leadership")
		return
	}
	e.state = StateLeader
	e.lastSeq = seq
	cb := e.onBecomeLeader
	log.Info().Str("node_id", e.nodeID).Uint64("seq", seq).Msg("became cluster leader")
	if cb != nil {
		// Drop the lock around the user callback; startLeaderWork takes
		// other locks (server.mu) and could otherwise deadlock with code
		// that calls IsLeader() while holding server.mu.
		e.mu.Unlock()
		cb()
		e.mu.Lock()
	}
}

// maintainLeadership refreshes the lease via Update with CAS on lastSeq.
// A successful Update bumps lastSeq and resets the bucket-wide TTL. ANY
// failure means we no longer own the slot — be it a transient stream
// hiccup, a sequence mismatch from a concurrent write (impossible if the
// CAS contract holds, but treat defensively), or the entry expiring under
// us — so we demote IMMEDIATELY and let the next campaign tick try Create.
//
// Mutex held by caller (try).
func (e *Election) maintainLeadership(ctx context.Context) {
	data := encodeInfo(Info{ID: e.nodeID, IP: e.nodeIP, Host: e.nodeHost})
	opCtx, cancel := context.WithTimeout(ctx, kvOpTimeout)
	defer cancel()
	seq, err := e.bucket.Update(opCtx, leaderKey, data, e.lastSeq)
	if err != nil {
		log.Warn().Err(err).Uint64("seq", e.lastSeq).Msg("refresh failed, stepping down")
		e.state = StateCandidate
		e.lastSeq = noLeaseSeq
		cb := e.onLoseLeader
		if cb != nil {
			e.mu.Unlock()
			cb()
			e.mu.Lock()
		}
		return
	}
	e.lastSeq = seq
	log.Debug().Str("node_id", e.nodeID).Uint64("seq", seq).Msg("refreshed leadership lease")
}

// stepDown is the graceful-shutdown path called when CampaignForLeader's
// ctx is cancelled. Best-effort Delete of our entry; if it fails the
// bucket TTL will sweep it within leaderTTL.
func (e *Election) stepDown() {
	e.mu.Lock()
	wasLeader := e.state == StateLeader
	e.state = StateCandidate
	e.lastSeq = noLeaseSeq
	cb := e.onLoseLeader
	e.mu.Unlock()

	if !wasLeader {
		return
	}
	opCtx, cancel := context.WithTimeout(context.Background(), kvOpTimeout)
	defer cancel()
	if err := e.bucket.Delete(opCtx, leaderKey); err != nil {
		log.Debug().Err(err).Msg("step down delete failed, lease will expire via TTL")
	}
	log.Info().Str("node_id", e.nodeID).Msg("stepped down from leadership")
	if cb != nil {
		cb()
	}
}

func encodeInfo(info Info) []byte {
	data, _ := codec.State.Marshal(info)
	return data
}
