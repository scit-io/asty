package leader

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

	// Leader-info cache, kept in sync by WatchLeadership on every KV
	// update or delete event. GetLeader reads from here instead of issuing
	// a per-call KV.Get — the asty-leader stream's RAFT group can briefly
	// stall during a leader-kill cascade, and a snapshot read or write-API
	// auth check that hits KV.Get during that window can time out (or worse,
	// return ErrKeyNotFound on an entry that logically exists), giving the
	// dashboard a transient "no leader" snapshot even though the cluster
	// holds one a second later. The watcher is event-driven, so the cache
	// stays as fresh as KV does, with one exception: on KV delete it goes
	// genuinely empty until the next claim. That's the right answer too —
	// there really is no leader in that window.
	cacheMu    sync.RWMutex
	cached     Info
	cacheValid bool
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

// GetLeader returns the current leader info from the WatchLeadership-fed
// cache, falling back to a one-shot KV read only while the watcher has
// not yet replayed the initial state (boot window). The cache is the
// single source of truth for who holds the leader slot — it tracks every
// KV update + delete event on the leader key and never touches the bucket
// on the hot path.
func (e *Election) GetLeader() (Info, error) {
	e.cacheMu.RLock()
	valid, info := e.cacheValid, e.cached
	e.cacheMu.RUnlock()
	if valid {
		if info.ID == "" {
			return Info{}, fmt.Errorf("no leader elected")
		}
		return info, nil
	}
	return e.getLeaderFromKV()
}

func (e *Election) getLeaderFromKV() (Info, error) {
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

// setCachedLeader records the latest KV-observed leader state in memory.
// Called by WatchLeadership on every update/delete it sees. Empty Info
// records "no leader" (KV delete or no entry); cacheValid being true
// after this call signals GetLeader that the watcher has caught up.
func (e *Election) setCachedLeader(info Info) {
	e.cacheMu.Lock()
	e.cached = info
	e.cacheValid = true
	e.cacheMu.Unlock()
}

// primeCacheIfEmpty is called by WatchLeadership when it sees the
// end-of-history-replay marker. If the cache is already populated (we
// saw a real entry during replay), this is a no-op so we don't clobber
// it. Otherwise it marks the cache as "known empty" so GetLeader stops
// falling through to a KV.Get for an entry the watcher just confirmed
// doesn't exist.
func (e *Election) primeCacheIfEmpty() {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	if e.cacheValid {
		return
	}
	e.cached = Info{}
	e.cacheValid = true
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
