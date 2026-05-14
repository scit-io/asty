// Package ringbuf provides a small fixed-capacity ring buffer with
// O(1) Push and an oldest-to-newest snapshot view. It backs the
// metrics ScalingEvent ring, the log buffer, and the cluster event
// buffer — three places that previously did `s = s[1:]; append(...)`
// and paid for periodic reallocations.
package ringbuf

// Ring is a generic ring buffer with fixed capacity. Not safe for
// concurrent use; callers serialise via their own mutex.
type Ring[T any] struct {
	buf  []T
	head int // index of the oldest element
	n    int // current count, n <= cap(buf)
}

// New creates a ring with the given capacity. Capacity <= 0 panics —
// silently allocating a zero-capacity buffer would hide bugs at call
// sites that forgot to size the buffer.
func New[T any](capacity int) *Ring[T] {
	if capacity <= 0 {
		panic("ringbuf: capacity must be > 0")
	}
	return &Ring[T]{buf: make([]T, capacity)}
}

// Push appends item; when the ring is full the oldest entry is
// overwritten and head advances.
func (r *Ring[T]) Push(item T) {
	if r.n < len(r.buf) {
		r.buf[(r.head+r.n)%len(r.buf)] = item
		r.n++
		return
	}
	r.buf[r.head] = item
	r.head = (r.head + 1) % len(r.buf)
}

// Len returns the current number of stored elements.
func (r *Ring[T]) Len() int { return r.n }

// Cap returns the configured capacity.
func (r *Ring[T]) Cap() int { return len(r.buf) }

// Snapshot returns a fresh slice with every element in
// oldest-to-newest order.
func (r *Ring[T]) Snapshot() []T {
	out := make([]T, r.n)
	for i := 0; i < r.n; i++ {
		out[i] = r.buf[(r.head+i)%len(r.buf)]
	}
	return out
}

// Last returns the last n elements in oldest-to-newest order. n <= 0
// or n >= Len() returns the full snapshot.
func (r *Ring[T]) Last(n int) []T {
	if n <= 0 || n >= r.n {
		return r.Snapshot()
	}
	out := make([]T, n)
	start := r.n - n
	for i := 0; i < n; i++ {
		out[i] = r.buf[(r.head+start+i)%len(r.buf)]
	}
	return out
}
