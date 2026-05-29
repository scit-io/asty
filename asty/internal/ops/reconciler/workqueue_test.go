package reconciler

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

// Backoff: each consecutive failure doubles the computed delay, capped at
// MaxDelay. Asserts the schedule directly instead of timing Get, so it is
// deterministic — the wall-clock version flaked under -race when scheduler
// jitter inflated one measurement past the doubling ratio.
func TestWorkqueueRateLimitedBackoff(t *testing.T) {
	const base = 20 * time.Millisecond
	const max = 200 * time.Millisecond
	want := []time.Duration{base, 2 * base, 4 * base, 8 * base, max, max}
	for n, w := range want {
		if got := rateLimitedDelay(n, base, max); got != w {
			t.Errorf("rateLimitedDelay(n=%d) = %v, want %v", n, got, w)
		}
	}
}

// Forget resets the failure counter.
func TestWorkqueueForget(t *testing.T) {
	q := NewWorkqueue()
	q.BaseDelay = 10 * time.Millisecond
	q.MaxDelay = 200 * time.Millisecond
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
