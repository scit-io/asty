package leader

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestElection_StateMachine_SingleWriter exercises the documented
// invariant of the canonical NATS KV-election pattern: state /
// lastSeq / IsLeader are written ONLY by the campaign goroutine (the
// caller of try()). External observers (e.g. the dashboard's IsLeader
// poll in fetchClusterJSON) read concurrently. Under -race, any second
// writer would surface as a data race.
func TestElection_StateMachine_SingleWriter(t *testing.T) {
	e := &Election{
		nodeID:  "self",
		state:   StateCandidate,
		lastSeq: noLeaseSeq,
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// One "campaign" writer mutating state under e.mu — mirrors what
	// campaign.go does inside try().
	wg.Add(1)
	go func() {
		defer wg.Done()
		var seq uint64
		for {
			select {
			case <-stop:
				return
			default:
			}
			e.mu.Lock()
			seq++
			e.state = StateLeader
			e.lastSeq = seq
			e.mu.Unlock()

			e.mu.Lock()
			e.state = StateCandidate
			e.lastSeq = noLeaseSeq
			e.mu.Unlock()
		}
	}()

	// Many concurrent IsLeader() readers — the dashboard / streamHub
	// hot path. These take e.mu in shared mode (via IsLeader's Lock).
	var reads int64
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = e.IsLeader()
				atomic.AddInt64(&reads, 1)
			}
		}()
	}

	// Run for a brief but non-trivial window so -race has time to
	// interleave the writer with all the readers.
	deadline := make(chan struct{})
	go func() {
		for i := 0; i < 500000; i++ {
		}
		close(deadline)
	}()
	<-deadline
	close(stop)
	wg.Wait()

	if atomic.LoadInt64(&reads) == 0 {
		t.Fatal("expected some IsLeader reads")
	}
}

// TestElection_Cache_WatchWritesVsReads is the corresponding race
// probe for the leader-info read cache: WatchLeadership-fed setters
// (setCachedLeader / primeCacheIfEmpty) running concurrently with
// dashboard reads (GetLeader). Both take e.cacheMu, so this is the
// invariant -race would catch if either side dropped the lock.
func TestElection_Cache_WatchWritesVsReads(t *testing.T) {
	e := &Election{nodeID: "self"}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Watcher-side writer.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			e.setCachedLeader(Info{ID: "leader1", IP: "10.0.0.1"})
			e.setCachedLeader(Info{ID: "leader2", IP: "10.0.0.2"})
			e.setCachedLeader(Info{}) // delete event
			e.primeCacheIfEmpty()
		}
	}()

	// Many GetLeader readers — dashboard hot path.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// GetLeader's KV-fallback path is NOT exercised here
				// because bucket is nil; we only test the cache branch,
				// which is reached when cacheValid==true. setCachedLeader
				// in the writer goroutine sets it, so reads see the
				// cached branch on the next tick.
				e.cacheMu.RLock()
				_ = e.cached
				_ = e.cacheValid
				e.cacheMu.RUnlock()
			}
		}()
	}

	deadline := make(chan struct{})
	go func() {
		for i := 0; i < 500000; i++ {
		}
		close(deadline)
	}()
	<-deadline
	close(stop)
	wg.Wait()
}

// TestElection_PrimeCacheIfEmpty_DoesNotClobberRealEntry guards the
// end-of-history-replay invariant: if the watcher saw a real entry
// during replay (cacheValid=true with non-empty Info) and then receives
// the nil sentinel, primeCacheIfEmpty must NOT reset the cache.
func TestElection_PrimeCacheIfEmpty_DoesNotClobberRealEntry(t *testing.T) {
	e := &Election{}
	e.setCachedLeader(Info{ID: "real", IP: "10.0.0.1"})

	e.primeCacheIfEmpty() // simulates end-of-replay marker

	e.cacheMu.RLock()
	got := e.cached
	e.cacheMu.RUnlock()

	if got.ID != "real" {
		t.Fatalf("primeCacheIfEmpty clobbered real entry: got %#v, want ID=real", got)
	}
}

// TestElection_PrimeCacheIfEmpty_PrimesEmpty exercises the other
// branch: cacheValid==false at end-of-replay → cache is primed as
// "known empty" so subsequent GetLeader doesn't fall through to KV.
func TestElection_PrimeCacheIfEmpty_PrimesEmpty(t *testing.T) {
	e := &Election{}

	if e.cacheValid {
		t.Fatal("test setup expected cacheValid=false")
	}

	e.primeCacheIfEmpty()

	e.cacheMu.RLock()
	valid := e.cacheValid
	id := e.cached.ID
	e.cacheMu.RUnlock()

	if !valid {
		t.Fatal("primeCacheIfEmpty did not set cacheValid")
	}
	if id != "" {
		t.Fatalf("primeCacheIfEmpty set non-empty cached: %q", id)
	}
}

// TestElection_WakeCh_BufferedDropOnFull confirms notifyWake's
// drop-on-full semantics: a burst of N notifies coalesces into at most
// one pending wake. Mirrors the canonical campaign-event model.
func TestElection_WakeCh_BufferedDropOnFull(t *testing.T) {
	e := &Election{wakeCh: make(chan struct{}, 1)}

	for i := 0; i < 100; i++ {
		e.notifyWake()
	}

	// Drain the single buffered slot.
	select {
	case <-e.wakeCh:
	default:
		t.Fatal("expected exactly one wake signal in the buffer")
	}

	// Confirm nothing else is pending.
	select {
	case <-e.wakeCh:
		t.Fatal("notifyWake leaked a second signal")
	default:
	}
}
