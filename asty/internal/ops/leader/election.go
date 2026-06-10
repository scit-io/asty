package leader

import (
	"context"
	"errors"
	"fmt"
	"math"
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

// leaderTTL — bucket-wide TTL on the leader entry. The canonical
// NATS-recommended KV-election pattern (ripienaar/nats-kv-leader-elect,
// which the NATS docs point to) REQUIRES this to be ≥30 s — its
// NewElection returns an error for shorter TTLs because the underlying
// RAFT can't guarantee tight refresh under load. A leader that misses
// one refresh has the rest of the TTL window to recover before another
// candidate claims the slot, which makes transient nats-stream hiccups
// during a degrade cascade survivable instead of constantly triggering
// leadership flap.
const leaderTTL = 30 * time.Second

// campaignInterval — how often the campaign loop ticks. Canonical
// pattern uses 75 % of TTL: a leader that's still alive will refresh
// well before the entry expires, and a candidate has a meaningful gap
// between probes instead of hammering Create on every node simultaneously.
const campaignInterval = (leaderTTL * 3) / 4

// kvOpTimeout caps one KV Get/Create/Update/Delete round-trip on the
// context-based jetstream API.
const kvOpTimeout = 10 * time.Second

// noLeaseSeq marks "no current lease held by us" (sentinel value the
// canonical impl uses to force the next maintainLeadership path through
// CAS-Create rather than CAS-Update).
const noLeaseSeq = math.MaxUint64

// State enumerates leader-election states. Matches the canonical
// implementation's State type for readability.
type State uint

const (
	StateUnknown State = iota
	StateCandidate
	StateLeader
)

// Info holds leader identification data stored in KV.
type Info struct {
	ID   string `json:"id"`
	IP   string `json:"ip"`
	Host string `json:"host,omitempty"` // operator-provided public DNS name, when set
}

// Election handles leader election via NATS JetStream KV.
//
// Follows the canonical pattern documented by
// ripienaar/nats-kv-leader-elect:
//   - Single campaign goroutine, single mutex, no separate watcher
//     driving state. The state machine reads its truth from Create /
//     Update return values directly — those are the only authoritative
//     answers.
//   - `bucket.Create` for the initial claim (CAS: succeeds only when
//     the slot is empty).
//   - `bucket.Update(key, val, lastSeq)` for the refresh — CAS-checked
//     against the sequence of our last write, so a stale leader can't
//     overwrite a fresh claim by another node.
//   - On ANY Update failure we IMMEDIATELY demote to Candidate and fire
//     the lost-leadership callback. Watch-driven flag-keeping (the path
//     this codebase used to have) lets `isLeader` lag the KV by minutes
//     if the watcher silently dies, and races the campaign loop on the
//     same field.
//   - Bucket TTL is ≥30 s — anything tighter doesn't survive the RAFT
//     churn we trigger during a degrade run.
type Election struct {
	bucket   jetstream.KeyValue
	nodeID   string
	nodeIP   string
	nodeHost string

	mu      sync.Mutex
	state   State
	lastSeq uint64

	onBecomeLeader func()
	onLoseLeader   func()

	// wakeCh kicks the campaign goroutine into running its next try()
	// immediately, instead of waiting for the next campaignInterval tick.
	// The leader-info watcher sends to it on KeyValueDelete / Purge events
	// — i.e. the moment a previous leader's lease either expired (TTL
	// sweep) or was explicitly released (stepDown). Buffered 1 with
	// drop-on-full: a burst of delete events between ticks coalesces into
	// a single wake, and a wake that arrives while try() is already
	// running is ignored (the in-progress try() will observe the new KV
	// state on its Create call). State writes still happen ONLY in the
	// campaign goroutine — wakeCh is just an event-driven scheduler hint.
	wakeCh chan struct{}

	// Leader-info read cache, fed by a separate KV watch on the leader
	// key. Independent of the election state machine: this exists so the
	// dashboard's hot read path (`GetLeader`) doesn't have to do a KV.Get
	// on every snapshot rebuild. The watch never writes `state` /
	// `lastSeq` — those are the campaign loop's alone.
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

	// History=1 (was 5): nats-server 2.14.2 commit 92cf2e3 ("Filestore
	// only stores last block when MaxMsgsPerSubject 1") fixes a filestore
	// block-tracking bug that causes transient ErrKeyNotFound on read.
	// The fix is gated on MaxMsgsPerSubject=1, which maps to KV
	// History=1. Asty never reads past leader revisions.
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
		state:    StateCandidate,
		lastSeq:  noLeaseSeq,
		wakeCh:   make(chan struct{}, 1),
	}, nil
}

// notifyWake is the watch-side event nudge: send if the buffer is empty,
// drop otherwise. Safe to call concurrently with the campaign loop —
// no state write here, just a scheduler signal.
func (e *Election) notifyWake() {
	select {
	case e.wakeCh <- struct{}{}:
	default:
	}
}

// SetCallbacks installs the become/lose-leadership hooks BEFORE
// CampaignForLeader starts. Replaces the old watch-driven callback
// pattern: the campaign state machine fires these directly on every
// transition observed via Create / Update return values.
func (e *Election) SetCallbacks(onBecomeLeader, onLoseLeader func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onBecomeLeader = onBecomeLeader
	e.onLoseLeader = onLoseLeader
}

// kvCtx returns a bounded context for a single KV operation.
func kvCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), kvOpTimeout)
}

// IsLeader returns whether this node is currently the leader.
func (e *Election) IsLeader() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state == StateLeader
}

// GetLeader returns the current leader info from the watch-fed cache,
// falling back to a one-shot KV read only while the watcher has not yet
// replayed the initial state. The cache is event-driven and stays as
// fresh as KV — no per-call KV.Get on the dashboard hot path.
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

// setCachedLeader is called by the leader-info watcher on every KV event.
// The election state machine doesn't touch the cache, and the cache never
// drives state — they are independent surfaces.
func (e *Election) setCachedLeader(info Info) {
	e.cacheMu.Lock()
	e.cached = info
	e.cacheValid = true
	e.cacheMu.Unlock()
}

// primeCacheIfEmpty is invoked at end-of-history-replay so a watcher that
// confirmed the slot is genuinely empty stops GetLeader falling through
// to a KV.Get for the rest of its lifetime. Does NOT clobber a real
// entry that was already cached during the replay.
func (e *Election) primeCacheIfEmpty() {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	if e.cacheValid {
		return
	}
	e.cached = Info{}
	e.cacheValid = true
}

// parseLeaderID returns the leader ID embedded in a KV entry, or an empty
// string when the entry cannot be parsed. Kept for the cache watcher's
// use.
func parseLeaderID(data []byte) string {
	var info Info
	if err := codec.State.Unmarshal(data, &info); err != nil {
		return ""
	}
	return info.ID
}
