package reconciler

import (
	"container/heap"
	"sync"
	"time"
)

// Workqueue is a deduplicating, rate-limited FIFO of string keys, modeled on
// k8s.io/client-go/util/workqueue.
type Workqueue struct {
	mu   sync.Mutex
	cond *sync.Cond

	queue      []string
	dirty      map[string]struct{}
	processing map[string]struct{}

	delayed    delayedHeap
	delayedSig chan struct{}
	failures   map[string]int

	BaseDelay time.Duration
	MaxDelay  time.Duration

	closed bool
}

func NewWorkqueue() *Workqueue {
	q := &Workqueue{
		dirty:      make(map[string]struct{}),
		processing: make(map[string]struct{}),
		delayedSig: make(chan struct{}, 1),
		failures:   make(map[string]int),
		BaseDelay:  500 * time.Millisecond,
		MaxDelay:   60 * time.Second,
	}
	q.cond = sync.NewCond(&q.mu)
	heap.Init(&q.delayed)
	go q.delayedLoop()
	return q
}

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
		return
	}
	q.dirty[key] = struct{}{}
	if _, ok := q.processing[key]; ok {
		return
	}
	q.queue = append(q.queue, key)
	q.cond.Signal()
}

func (q *Workqueue) Get() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.queue) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.queue) == 0 {
		return "", false
	}
	key := q.queue[0]
	q.queue = q.queue[1:]
	delete(q.dirty, key)
	q.processing[key] = struct{}{}
	return key, true
}

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

func (q *Workqueue) AddRateLimited(key string) {
	q.mu.Lock()
	n := q.failures[key]
	q.failures[key] = n + 1
	base := q.BaseDelay
	max := q.MaxDelay
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

func (q *Workqueue) Forget(key string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.failures, key)
}

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

func (q *Workqueue) delayedLoop() {
	for {
		q.mu.Lock()
		if q.closed && q.delayed.Len() == 0 {
			q.mu.Unlock()
			return
		}
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

type delayedItem struct {
	key   string
	ready time.Time
}

type delayedHeap []*delayedItem

func (h delayedHeap) Len() int           { return len(h) }
func (h delayedHeap) Less(i, j int) bool { return h[i].ready.Before(h[j].ready) }
func (h delayedHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *delayedHeap) Push(x any)        { *h = append(*h, x.(*delayedItem)) }
func (h *delayedHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}
