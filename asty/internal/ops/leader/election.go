package leader

import (
	"fmt"
	"time"

	"asty/asty/internal/core/codec"
	"asty/asty/internal/core/netutil"

	"github.com/nats-io/nats.go"
)

// leaderKey is the well-known KV entry that holds the current leader's
// identity. There is exactly one such entry per cluster.
const leaderKey = "current-leader"

// leaderTTL — how long an entry survives without a refresh. Coordinator
// nodes refresh on a tighter cadence (see refreshInterval); when the
// leader dies and stops refreshing, the entry expires after this TTL and
// the next campaigner can claim it.
const leaderTTL = 10 * time.Second

// Info holds leader identification data stored in KV.
type Info struct {
	ID string `json:"id"`
	IP string `json:"ip"`
}

// Election handles leader election via NATS JetStream KV.
type Election struct {
	nc       *nats.Conn
	js       nats.JetStreamContext
	bucket   nats.KeyValue
	nodeID   string
	nodeIP   string
	isLeader bool
}

// NewElection creates a new leader election instance.
func NewElection(nc *nats.Conn, nodeID string, nodeIP string) (*Election, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	bucket, err := netutil.EnsureBucket(js, &nats.KeyValueConfig{
		Bucket:      "asty-leader",
		Description: "Asty leader election",
		TTL:         leaderTTL,
		History:     5,
	})
	if err != nil {
		return nil, err
	}

	return &Election{
		nc:     nc,
		js:     js,
		bucket: bucket,
		nodeID: nodeID,
		nodeIP: nodeIP,
	}, nil
}

// IsLeader returns whether this node is currently the leader.
func (e *Election) IsLeader() bool {
	return e.isLeader
}

// GetLeader returns the current leader info, or an error if no leader is
// recorded yet.
func (e *Election) GetLeader() (Info, error) {
	entry, err := e.bucket.Get(leaderKey)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return Info{}, fmt.Errorf("no leader elected")
		}
		return Info{}, fmt.Errorf("failed to get leader: %w", err)
	}

	var info Info
	if err := codec.State.Unmarshal(entry.Value(), &info); err != nil {
		return Info{}, fmt.Errorf("parse leader info: %w", err)
	}
	return info, nil
}

// parseLeaderID returns the leader ID embedded in a KV entry, or an
// empty string when the entry cannot be parsed. Empty string ensures
// "this isn't me" callers (campaign, watch) take the not-leader
// branch rather than risking a false-positive identity match against
// raw bytes.
func parseLeaderID(data []byte) string {
	var info Info
	if err := codec.State.Unmarshal(data, &info); err != nil {
		return ""
	}
	return info.ID
}
