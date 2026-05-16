package leader

import (
	"context"
	"fmt"
	"time"

	"asty/asty/internal/core/codec"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// refreshInterval — how often the leader rewrites its KV entry to push
// out the TTL. Picked at 5 s = leaderTTL/2 so a single missed refresh
// (e.g. a GC pause) doesn't already trigger failover.
const refreshInterval = 5 * time.Second

// CampaignForLeader runs the leader-election loop until ctx is cancelled.
// It claims leadership when the slot is free and refreshes the lease
// while held.
func (e *Election) CampaignForLeader(ctx context.Context) error {
	if err := e.tryBecomeLeader(); err != nil {
		log.Debug().Err(err).Msg("initial leader claim failed")
	}

	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if e.isLeader {
				e.stepDown()
			}
			return nil

		case <-ticker.C:
			if err := e.tryBecomeLeader(); err != nil {
				log.Debug().Err(err).Msg("failed to become leader")
			}
		}
	}
}

// tryBecomeLeader is the per-tick state machine: claim the slot if free,
// refresh if we hold it, mark ourselves as a follower if someone else
// holds it.
func (e *Election) tryBecomeLeader() error {
	entry, err := e.bucket.Get(leaderKey)
	if err != nil && err != nats.ErrKeyNotFound {
		return fmt.Errorf("failed to get leader key: %w", err)
	}

	if err == nats.ErrKeyNotFound || entry == nil {
		return e.claimLeadership()
	}

	current := parseLeaderID(entry.Value())
	if current == e.nodeID {
		return e.refreshLeadership()
	}

	if !e.isLeader {
		return nil
	}

	log.Warn().
		Str("old_leader", e.nodeID).
		Str("new_leader", current).
		Msg("lost leadership")
	e.isLeader = false
	return nil
}

func (e *Election) claimLeadership() error {
	data, _ := codec.State.Marshal(Info{ID: e.nodeID, IP: e.nodeIP})
	if _, err := e.bucket.Create(leaderKey, data); err != nil {
		return fmt.Errorf("failed to claim leadership: %w", err)
	}
	e.isLeader = true
	log.Info().Str("node_id", e.nodeID).Msg("became cluster leader")
	return nil
}

func (e *Election) refreshLeadership() error {
	data, _ := codec.State.Marshal(Info{ID: e.nodeID, IP: e.nodeIP})
	if _, err := e.bucket.Put(leaderKey, data); err != nil {
		e.isLeader = false
		return fmt.Errorf("failed to refresh leadership: %w", err)
	}
	log.Debug().Str("node_id", e.nodeID).Msg("refreshed leadership lease")
	return nil
}

func (e *Election) stepDown() error {
	if !e.isLeader {
		return nil
	}

	entry, err := e.bucket.Get(leaderKey)
	if err != nil {
		return fmt.Errorf("failed to get leader key: %w", err)
	}
	if parseLeaderID(entry.Value()) != e.nodeID {
		e.isLeader = false
		return nil
	}
	if err := e.bucket.Delete(leaderKey); err != nil {
		return fmt.Errorf("failed to delete leader key: %w", err)
	}
	e.isLeader = false
	log.Info().Str("node_id", e.nodeID).Msg("stepped down from leadership")
	return nil
}
