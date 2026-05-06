package asty

import (
	"sync"
	"testing"
	"time"
)

// Add+Get returns the same key, preserves FIFO across distinct keys.
func TestWorkqueueFIFO(t *testing.T) {
	q := NewWorkqueue()
	defer q.ShutDown()

	q.Add("a")
	q.Add("b")
	q.Add("c")

	for _, want := range []string{"a", "b", "c"} {
		got, ok := q.Get()
		if !ok || got != want {
			t.Fatalf("got %q,%v want %q", got, ok, want)
		}
		q.Done(got)
	}
}

// Adding the same key twice while it's pending dedups to one item.
func TestWorkqueueDedup(t *testing.T) {
	q := NewWorkqueue()
	defer q.ShutDown()

	q.Add("a")
	q.Add("a")
	q.Add("a")

	got, _ := q.Get()
	if got != "a" {
		t.Fatalf("first Get got %q want a", got)
	}
	q.Done("a")

	if q.Len() != 0 {
		t.Errorf("queue should be empty after Done, Len=%d", q.Len())
	}
}

// Add(k) while a worker holds k must re-queue k after Done — the second
// observation should not be lost.
func TestWorkqueueAddDuringProcessing(t *testing.T) {
	q := NewWorkqueue()
	defer q.ShutDown()

	q.Add("a")
	got, _ := q.Get() // a is now processing
	if got != "a" {
		t.Fatalf("got %q want a", got)
	}

	q.Add("a") // marks dirty while processing — must re-queue on Done

	q.Done("a")

	got2, ok := q.Get()
	if !ok || got2 != "a" {
		t.Fatalf("after Done, got %q,%v want a,true", got2, ok)
	}
	q.Done("a")
}

// AddAfter respects delay: Get must block past the delay, then return.
func TestWorkqueueAddAfter(t *testing.T) {
	q := NewWorkqueue()
	defer q.ShutDown()

	start := time.Now()
	q.AddAfter("a", 80*time.Millisecond)

	got, ok := q.Get()
	if !ok || got != "a" {
		t.Fatalf("got %q,%v want a", got, ok)
	}
	q.Done(got)
	if time.Since(start) < 60*time.Millisecond {
		t.Errorf("Get returned too soon: %v", time.Since(start))
	}
}

// Backoff: each AddRateLimited without Forget doubles the delay.
func TestWorkqueueRateLimitedBackoff(t *testing.T) {
	q := NewWorkqueue()
	q.baseDelay = 20 * time.Millisecond
	q.maxDelay = 200 * time.Millisecond
	defer q.ShutDown()

	q.AddRateLimited("k")
	first := time.Now()
	got, _ := q.Get()
	d1 := time.Since(first)
	q.Done(got)

	q.AddRateLimited("k")
	second := time.Now()
	q.Get()
	d2 := time.Since(second)
	q.Done("k")

	// d2 should be roughly 2× d1 (within scheduler jitter).
	if d2 < d1*3/2 {
		t.Errorf("backoff not doubling: d1=%v d2=%v", d1, d2)
	}
}

// Forget resets the failure counter.
func TestWorkqueueForget(t *testing.T) {
	q := NewWorkqueue()
	q.baseDelay = 10 * time.Millisecond
	q.maxDelay = 200 * time.Millisecond
	defer q.ShutDown()

	q.AddRateLimited("k")
	q.AddRateLimited("k")
	q.AddRateLimited("k") // backoff is now ~80ms
	q.Get()
	q.Done("k")
	q.Forget("k")

	q.AddRateLimited("k")
	start := time.Now()
	q.Get()
	d := time.Since(start)
	q.Done("k")
	if d > 50*time.Millisecond {
		t.Errorf("after Forget, expected ~baseDelay, got %v", d)
	}
}

// ShutDown wakes a blocked Get.
func TestWorkqueueShutdownUnblocksGet(t *testing.T) {
	q := NewWorkqueue()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, ok := q.Get()
		if ok {
			t.Errorf("Get on closed empty queue should return ok=false")
		}
	}()

	time.Sleep(20 * time.Millisecond)
	q.ShutDown()
	wg.Wait()
}
