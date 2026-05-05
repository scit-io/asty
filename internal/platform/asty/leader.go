package asty

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// LeaderElection handles leader election via NATS JetStream KV
type LeaderElection struct {
	nc       *nats.Conn
	js       nats.JetStreamContext
	bucket   nats.KeyValue
	nodeID   string
	isLeader bool
}

// NewLeaderElection creates a new leader election instance
func NewLeaderElection(nc *nats.Conn, nodeID string) (*LeaderElection, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	// Retry KV bucket creation — JetStream meta-group may still be electing leader
	var bucket nats.KeyValue
	for attempt := 0; attempt < 30; attempt++ {
		bucket, err = js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket:      "asty-leader",
			Description: "Asty leader election",
			TTL:         10 * time.Second,
			History:     5,
		})
		if err == nil {
			break
		}
		bucket, err = js.KeyValue("asty-leader")
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create/get leader KV bucket after retries: %w", err)
	}

	// Wait until bucket is operational
	for attempt := 0; attempt < 30; attempt++ {
		if _, err := bucket.Keys(); err == nats.ErrNoKeysFound || err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}

	return &LeaderElection{
		nc:       nc,
		js:       js,
		bucket:   bucket,
		nodeID:   nodeID,
		isLeader: false,
	}, nil
}

// CampaignForLeader attempts to become the leader
func (le *LeaderElection) CampaignForLeader(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Step down if we're the leader
			if le.isLeader {
				le.stepDown()
			}
			return nil

		case <-ticker.C:
			if err := le.tryBecomeLeader(); err != nil {
				log.Debug().Err(err).Msg("failed to become leader")
			}
		}
	}
}

// tryBecomeLeader attempts to acquire leadership
func (le *LeaderElection) tryBecomeLeader() error {
	leaderKey := "current-leader"

	// Try to get current leader
	entry, err := le.bucket.Get(leaderKey)
	if err != nil && err != nats.ErrKeyNotFound {
		return fmt.Errorf("failed to get leader key: %w", err)
	}

	// No current leader or leader expired - try to claim leadership
	if err == nats.ErrKeyNotFound || entry == nil {
		return le.claimLeadership()
	}

	currentLeader := string(entry.Value())

	// We are already the leader - refresh lease
	if currentLeader == le.nodeID {
		return le.refreshLeadership()
	}

	// Another node is leader
	if !le.isLeader {
		// We weren't leader before, this is normal
		return nil
	}

	// We lost leadership
	log.Warn().
		Str("old_leader", le.nodeID).
		Str("new_leader", currentLeader).
		Msg("lost leadership")

	le.isLeader = false
	return nil
}

// claimLeadership attempts to claim leadership
func (le *LeaderElection) claimLeadership() error {
	leaderKey := "current-leader"

	// Try to create the key (will fail if it already exists)
	_, err := le.bucket.Create(leaderKey, []byte(le.nodeID))
	if err != nil {
		return fmt.Errorf("failed to claim leadership: %w", err)
	}

	le.isLeader = true

	log.Info().
		Str("node_id", le.nodeID).
		Msg("became cluster leader")

	return nil
}

// refreshLeadership refreshes the leadership lease
func (le *LeaderElection) refreshLeadership() error {
	leaderKey := "current-leader"

	// Update the key to refresh TTL
	_, err := le.bucket.Put(leaderKey, []byte(le.nodeID))
	if err != nil {
		le.isLeader = false
		return fmt.Errorf("failed to refresh leadership: %w", err)
	}

	log.Debug().
		Str("node_id", le.nodeID).
		Msg("refreshed leadership lease")

	return nil
}

// stepDown voluntarily steps down from leadership
func (le *LeaderElection) stepDown() error {
	if !le.isLeader {
		return nil
	}

	leaderKey := "current-leader"

	// Verify we are still the leader before deleting
	entry, err := le.bucket.Get(leaderKey)
	if err != nil {
		return fmt.Errorf("failed to get leader key: %w", err)
	}

	if string(entry.Value()) != le.nodeID {
		// Someone else is leader now
		le.isLeader = false
		return nil
	}

	// Delete the key
	if err := le.bucket.Delete(leaderKey); err != nil {
		return fmt.Errorf("failed to delete leader key: %w", err)
	}

	le.isLeader = false

	log.Info().
		Str("node_id", le.nodeID).
		Msg("stepped down from leadership")

	return nil
}

// IsLeader returns whether this node is currently the leader
func (le *LeaderElection) IsLeader() bool {
	return le.isLeader
}

// GetLeader returns the current leader node ID
func (le *LeaderElection) GetLeader() (string, error) {
	leaderKey := "current-leader"

	entry, err := le.bucket.Get(leaderKey)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return "", fmt.Errorf("no leader elected")
		}
		return "", fmt.Errorf("failed to get leader: %w", err)
	}

	return string(entry.Value()), nil
}

// WaitForLeader waits until a leader is elected
func (le *LeaderElection) WaitForLeader(ctx context.Context) (string, error) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()

		case <-ticker.C:
			leader, err := le.GetLeader()
			if err == nil {
				return leader, nil
			}

			log.Debug().Msg("waiting for leader election")
		}
	}
}
