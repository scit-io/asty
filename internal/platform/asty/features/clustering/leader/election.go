package leader

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"asty/internal/platform/asty/core/netutil"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// Info holds leader identification data stored in KV
type Info struct {
	ID string `json:"id"`
	IP string `json:"ip"`
}

// Election handles leader election via NATS JetStream KV
type Election struct {
	nc       *nats.Conn
	js       nats.JetStreamContext
	bucket   nats.KeyValue
	nodeID   string
	nodeIP   string
	isLeader bool
}

// NewElection creates a new leader election instance
func NewElection(nc *nats.Conn, nodeID string, nodeIP string) (*Election, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	bucket, err := netutil.EnsureBucket(js, &nats.KeyValueConfig{
		Bucket:      "asty-leader",
		Description: "Asty leader election",
		TTL:         10 * time.Second,
		History:     5,
	})
	if err != nil {
		return nil, err
	}

	return &Election{
		nc:       nc,
		js:       js,
		bucket:   bucket,
		nodeID:   nodeID,
		nodeIP:   nodeIP,
		isLeader: false,
	}, nil
}

// CampaignForLeader attempts to become the leader.
func (e *Election) CampaignForLeader(ctx context.Context) error {
	if err := e.tryBecomeLeader(); err != nil {
		log.Debug().Err(err).Msg("initial leader claim failed")
	}

	ticker := time.NewTicker(5 * time.Second)
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

func (e *Election) tryBecomeLeader() error {
	leaderKey := "current-leader"

	entry, err := e.bucket.Get(leaderKey)
	if err != nil && err != nats.ErrKeyNotFound {
		return fmt.Errorf("failed to get leader key: %w", err)
	}

	if err == nats.ErrKeyNotFound || entry == nil {
		return e.claimLeadership()
	}

	currentLeaderID := parseLeaderID(entry.Value())

	if currentLeaderID == e.nodeID {
		return e.refreshLeadership()
	}

	if !e.isLeader {
		return nil
	}

	log.Warn().
		Str("old_leader", e.nodeID).
		Str("new_leader", currentLeaderID).
		Msg("lost leadership")

	e.isLeader = false
	return nil
}

func parseLeaderID(data []byte) string {
	var info Info
	if err := json.Unmarshal(data, &info); err == nil {
		return info.ID
	}
	return string(data)
}

func (e *Election) claimLeadership() error {
	leaderKey := "current-leader"

	data, _ := json.Marshal(Info{ID: e.nodeID, IP: e.nodeIP})
	_, err := e.bucket.Create(leaderKey, data)
	if err != nil {
		return fmt.Errorf("failed to claim leadership: %w", err)
	}

	e.isLeader = true

	log.Info().
		Str("node_id", e.nodeID).
		Msg("became cluster leader")

	return nil
}

func (e *Election) refreshLeadership() error {
	leaderKey := "current-leader"

	data, _ := json.Marshal(Info{ID: e.nodeID, IP: e.nodeIP})
	_, err := e.bucket.Put(leaderKey, data)
	if err != nil {
		e.isLeader = false
		return fmt.Errorf("failed to refresh leadership: %w", err)
	}

	log.Debug().
		Str("node_id", e.nodeID).
		Msg("refreshed leadership lease")

	return nil
}

func (e *Election) stepDown() error {
	if !e.isLeader {
		return nil
	}

	leaderKey := "current-leader"

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

	log.Info().
		Str("node_id", e.nodeID).
		Msg("stepped down from leadership")

	return nil
}

// IsLeader returns whether this node is currently the leader
func (e *Election) IsLeader() bool {
	return e.isLeader
}

// GetLeader returns the current leader info
func (e *Election) GetLeader() (Info, error) {
	leaderKey := "current-leader"

	entry, err := e.bucket.Get(leaderKey)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return Info{}, fmt.Errorf("no leader elected")
		}
		return Info{}, fmt.Errorf("failed to get leader: %w", err)
	}

	var info Info
	if err := json.Unmarshal(entry.Value(), &info); err != nil {
		return Info{ID: string(entry.Value())}, nil
	}
	return info, nil
}

// WaitForLeader waits until a leader is elected.
func (e *Election) WaitForLeader(ctx context.Context) (Info, error) {
	if leader, err := e.GetLeader(); err == nil {
		return leader, nil
	}

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return Info{}, ctx.Err()

		case <-ticker.C:
			leader, err := e.GetLeader()
			if err == nil {
				return leader, nil
			}

			log.Debug().Msg("waiting for leader election")
		}
	}
}

// WatchLeadership watches for leadership changes via NATS KV watcher.
func (e *Election) WatchLeadership(ctx context.Context, onBecomeLeader func(), onLoseLeadership func()) error {
	watcher, err := e.bucket.Watch("current-leader", nats.Context(ctx))
	if err != nil {
		return fmt.Errorf("failed to watch leader key: %w", err)
	}
	defer watcher.Stop()

	wasLeader := e.isLeader

	for {
		select {
		case <-ctx.Done():
			return nil
		case entry, ok := <-watcher.Updates():
			if !ok {
				return nil
			}
			if entry == nil {
				continue
			}

			var isLeader bool
			if entry.Operation() == nats.KeyValueDelete || entry.Operation() == nats.KeyValuePurge {
				isLeader = false
			} else {
				isLeader = parseLeaderID(entry.Value()) == e.nodeID
			}

			if isLeader && !wasLeader {
				onBecomeLeader()
			}
			if !isLeader && wasLeader {
				onLoseLeadership()
			}
			wasLeader = isLeader
		}
	}
}
