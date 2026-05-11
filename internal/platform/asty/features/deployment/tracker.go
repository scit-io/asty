package deployment

import (
	"sync"
	"time"

	"asty/internal/platform/asty/core/types"
)

// healthTracker tracks per-allocation status and the timestamp when
// every allocation last became healthy. satisfied() returns true once
// the batch has stayed healthy continuously for minHealthyTime.
type healthTracker struct {
	mu             sync.Mutex
	statuses       map[string]types.AllocationStatus
	healthyAt      time.Time
	minHealthyTime time.Duration
}

func newHealthTracker(keys map[string]bool, minHealthyTime time.Duration) *healthTracker {
	statuses := make(map[string]types.AllocationStatus, len(keys))
	for k := range keys {
		statuses[k] = ""
	}
	return &healthTracker{statuses: statuses, minHealthyTime: minHealthyTime}
}

// update records the latest status for one allocation and resets or
// starts the healthy-since timestamp accordingly.
func (t *healthTracker) update(key string, status types.AllocationStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.statuses[key] = status
	if !t.allRunningLocked() {
		t.healthyAt = time.Time{}
		return
	}
	if t.healthyAt.IsZero() {
		t.healthyAt = time.Now()
	}
}

// markHealthyNow seeds healthyAt when the initial KV scan already
// showed the batch as healthy.
func (t *healthTracker) markHealthyNow() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.allRunningLocked() && t.healthyAt.IsZero() {
		t.healthyAt = time.Now()
	}
}

// healthy reports the current state without touching the timer.
func (t *healthTracker) healthy() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.allRunningLocked()
}

func (t *healthTracker) allRunningLocked() bool {
	for _, s := range t.statuses {
		if s != types.AllocRunning {
			return false
		}
	}
	return true
}

// satisfied returns true once the batch has been continuously healthy
// for minHealthyTime.
func (t *healthTracker) satisfied() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.healthyAt.IsZero() {
		return false
	}
	return time.Since(t.healthyAt) >= t.minHealthyTime
}

// until returns how long until satisfied() can become true given the
// current state. Zero means "wait indefinitely until next event"; a
// 1 ms value means "already past the threshold, fire ASAP".
func (t *healthTracker) until() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.healthyAt.IsZero() {
		return 0
	}
	d := t.minHealthyTime - time.Since(t.healthyAt)
	if d <= 0 {
		return 1 * time.Millisecond
	}
	return d
}

func allocKey(a *types.ServiceAllocation) string {
	return a.ServiceName + "/" + a.NodeID
}

func batchKeys(batch []*types.ServiceAllocation) map[string]bool {
	out := make(map[string]bool, len(batch))
	for _, a := range batch {
		out[allocKey(a)] = true
	}
	return out
}
