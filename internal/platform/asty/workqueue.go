package asty

import (
	"container/heap"
	"sync"
	"time"
)

// Workqueue is a deduplicating, rate-limited FIFO of string keys, modeled on
// k8s.io/client-go/util/workqueue. Three properties matter:
//
//   - Dedup: Add(k) while k is already in the queue is a no-op.
//   - Processing tracking: Add(k) while a worker holds k via Get does not
//     re-enqueue immediately; instead k is marked dirty and re-added when
//     Done(k) lands. Workers therefore process at most one in-flight copy of
//     a key, while still observing changes that occur during processing.
//   - Rate-limited retry: AddRateLimited(k) doubles the per-key backoff up
//     to maxDelay. Forget(k) resets the counter on success.
//
// Two long-lived goroutines: the workqueue itself (cond-var driven) and a
// delayedLoop that drains the heap of (ready_at, key) into the main queue.
type Workqueue struct {
	mu   sync.Mutex
	cond *sync.Cond

	queue      []string            // FIFO of keys ready for a worker
	dirty      map[string]struct{} // keys waiting (in queue) or marked-while-processing
	processing map[string]struct{} // keys currently held by a worker

	delayed     delayedHeap
	delayedSig  chan struct{}
	failures    map[string]int

	baseDelay time.Duration
	maxDelay  time.Duration

	closed bool
}

// NewWorkqueue returns a queue with sensible defaults: 500ms base backoff,
// capped at 60s (≈ 8 doublings). Tune via the public fields if needed.
func NewWorkqueue() *Workqueue {
	q := &Workqueue{
		dirty:      make(map[string]struct{}),
		processing: make(map[string]struct{}),
		delayedSig: make(chan struct{}, 1),
		failures:   make(map[string]int),
		baseDelay:  500 * time.Millisecond,
		maxDelay:   60 * time.Second,
	}
	q.cond = sync.NewCond(&q.mu)
	heap.Init(&q.delayed)
	go q.delayedLoop()
	return q
}

// Add enqueues key for immediate processing. If key is in flight, the dirty
// bit is set; Done will re-add it after the worker releases it.
func (q *Workqueue) Add(key string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.addLocked(key)
}

func (q *Workqueue) addLocked(key string) {
	if q.closed {
		return
	}
	if _, ok := q.dirty[key]; ok {
		return // already enqueued or marked-while-processing
	}
	q.dirty[key] = struct{}{}
	if _, ok := q.processing[key]; ok {
		return // will be re-queued by Done
	}
	q.queue = append(q.queue, key)
	q.cond.Signal()
}

// Get blocks until a key is available or the queue is shut down. Caller must
// pair every successful Get with Done(key).
func (q *Workqueue) Get() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.queue) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.queue) == 0 {
		return "", false // closed and drained
	}
	key := q.queue[0]
	q.queue = q.queue[1:]
	delete(q.dirty, key)
	q.processing[key] = struct{}{}
	return key, true
}

// Done releases a key. If Add was called for it during processing, the key
// is re-queued for the next worker.
func (q *Workqueue) Done(key string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.processing, key)
	if _, dirty := q.dirty[key]; dirty {
		delete(q.dirty, key)
		q.queue = append(q.queue, key)
		q.cond.Signal()
	}
}

// AddAfter schedules key after delay. Multiple AddAfter calls before the
// delay elapses are coalesced because the heap is keyed on string and Add
// itself dedups — the earliest matching ready time wins in practice.
func (q *Workqueue) AddAfter(key string, delay time.Duration) {
	if delay <= 0 {
		q.Add(key)
		return
	}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	heap.Push(&q.delayed, &delayedItem{key: key, ready: time.Now().Add(delay)})
	q.mu.Unlock()
	q.kickDelayed()
}

// AddRateLimited applies exponential backoff per key (failures map). Pair
// with Forget on success to reset.
func (q *Workqueue) AddRateLimited(key string) {
	q.mu.Lock()
	n := q.failures[key]
	q.failures[key] = n + 1
	base := q.baseDelay
	max := q.maxDelay
	q.mu.Unlock()

	delay := base
	for i := 0; i < n && delay < max; i++ {
		delay *= 2
	}
	if delay > max {
		delay = max
	}
	q.AddAfter(key, delay)
}

// Forget clears the per-key failure counter. Call after a successful pass
// so the next failure starts at baseDelay again.
func (q *Workqueue) Forget(key string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.failures, key)
}

// ShutDown stops accepting new items and wakes any blocked Get callers.
// In-flight processing is allowed to complete; subsequent Adds are dropped.
func (q *Workqueue) ShutDown() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	q.cond.Broadcast()
	q.kickDelayedLocked()
}

// Len returns approximate queue depth (FIFO + dirty-while-processing).
// Used for telemetry; not a strict invariant.
func (q *Workqueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.dirty) + len(q.processing)
}

func (q *Workqueue) kickDelayed() {
	select {
	case q.delayedSig <- struct{}{}:
	default:
	}
}

func (q *Workqueue) kickDelayedLocked() {
	select {
	case q.delayedSig <- struct{}{}:
	default:
	}
}

// delayedLoop services the delayed-add heap. Sleeps until the next item is
// due, then promotes everything that's ripe to the main queue. Wakes early
// on AddAfter signals.
func (q *Workqueue) delayedLoop() {
	for {
		q.mu.Lock()
		if q.closed && q.delayed.Len() == 0 {
			q.mu.Unlock()
			return
		}
		// Drain any items already due.
		now := time.Now()
		for q.delayed.Len() > 0 && !q.delayed[0].ready.After(now) {
			it := heap.Pop(&q.delayed).(*delayedItem)
			q.addLocked(it.key)
		}
		var wait time.Duration
		if q.delayed.Len() == 0 {
			wait = -1
		} else {
			wait = time.Until(q.delayed[0].ready)
		}
		q.mu.Unlock()

		if wait < 0 {
			<-q.delayedSig
		} else {
			t := time.NewTimer(wait)
			select {
			case <-t.C:
			case <-q.delayedSig:
				if !t.Stop() {
					<-t.C
				}
			}
		}
	}
}

// delayedItem is a (key, ready_time) waiting to be promoted to the main FIFO.
type delayedItem struct {
	key   string
	ready time.Time
}

type delayedHeap []*delayedItem

func (h delayedHeap) Len() int            { return len(h) }
func (h delayedHeap) Less(i, j int) bool  { return h[i].ready.Before(h[j].ready) }
func (h delayedHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *delayedHeap) Push(x interface{}) { *h = append(*h, x.(*delayedItem)) }
func (h *delayedHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}
