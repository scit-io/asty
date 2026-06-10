package leader

import (
	"context"
	"errors"
	"fmt"
	"time"

	"asty/asty/internal/core/codec"
	"asty/asty/internal/core/netutil"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// leaderKey is the well-known KV entry that holds the current leader's
// identity. There is exactly one such entry per cluster.
const leaderKey = "current-leader"

// leaderTTL — how long an entry survives without a refresh. Coordinator
// nodes refresh on a tighter cadence (see refreshInterval); when the
// leader dies and stops refreshing, the entry expires after this TTL and
// the next campaigner can claim it.
const leaderTTL = 10 * time.Second

// kvOpTimeout caps one KV Get/Create/Put/Delete round-trip on the
// context-based jetstream API.
const kvOpTimeout = 10 * time.Second

// Info holds leader identification data stored in KV.
type Info struct {
	ID   string `json:"id"`
	IP   string `json:"ip"`
	Host string `json:"host,omitempty"` // operator-provided public DNS name, when set
}

// Election handles leader election via NATS JetStream KV. It uses the
// nats.go `jetstream` package (NOT the deprecated JetStreamContext): the
// WatchLeadership ordered consumer auto-recreates after a nats-server
// restart (the natssolo 2→1 collapse), so leadership-flip detection
// survives the broker bounce. See nats.go #1094/#1097.
type Election struct {
	bucket   jetstream.KeyValue
	nodeID   string
	nodeIP   string
	nodeHost string
	isLeader bool
}

// NewElection creates a new leader election instance.
func NewElection(nc *nats.Conn, nodeID, nodeIP, nodeHost string) (*Election, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream init: %w", err)
	}

	// History=1 (was 5). nats-server 2.14.2 commit 92cf2e3 ("Filestore only
	// stores last block when MaxMsgsPerSubject 1") fixes a filestore
	// block-tracking bug where the head pointer could reference an
	// obsolete block on read AFTER an entry expired or was overwritten —
	// observable here as a transient ErrKeyNotFound on GetLeader during
	// the 5-second window between a KV-stream RAFT leader stepdown and
	// catch-up on the freshly-claimed leader entry. The fix is GATED on
	// MaxMsgsPerSubject=1; a KV bucket created with History=N maps to
	// MaxMsgsPerSubject=N, so History=5 here disqualifies the bucket from
	// the fix and reproduces the race in Phase C 16-node degrade cycles.
	// Asty never reads past revisions of this key — History>1 was pure
	// overhead.
	bucket, err := netutil.EnsureBucket(js, jetstream.KeyValueConfig{
		Bucket:      "asty-leader",
		Description: "Asty leader election",
		TTL:         leaderTTL,
		History:     1,
	})
	if err != nil {
		return nil, err
	}

	return &Election{
		bucket:   bucket,
		nodeID:   nodeID,
		nodeIP:   nodeIP,
		nodeHost: nodeHost,
	}, nil
}

// kvCtx returns a bounded context for a single KV operation.
func kvCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), kvOpTimeout)
}

// IsLeader returns whether this node is currently the leader.
func (e *Election) IsLeader() bool {
	return e.isLeader
}

// GetLeader returns the current leader info, or an error if no leader is
// recorded yet.
func (e *Election) GetLeader() (Info, error) {
	ctx, cancel := kvCtx()
	defer cancel()
	entry, err := e.bucket.Get(ctx, leaderKey)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
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
