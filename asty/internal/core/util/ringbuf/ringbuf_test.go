package ringbuf

import (
	"reflect"
	"testing"
)

func TestRingBelowCapacity(t *testing.T) {
	r := New[int](5)
	r.Push(1)
	r.Push(2)
	r.Push(3)
	if r.Len() != 3 {
		t.Errorf("Len() = %d, want 3", r.Len())
	}
	got := r.Snapshot()
	if !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Errorf("Snapshot = %v, want [1 2 3]", got)
	}
}

func TestRingExactlyAtCapacity(t *testing.T) {
	r := New[int](3)
	r.Push(1)
	r.Push(2)
	r.Push(3)
	got := r.Snapshot()
	if !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Errorf("Snapshot = %v, want [1 2 3]", got)
	}
}

// TestRingOverflow verifies oldest-overwrite semantics: with capacity 3
// and 5 pushes (1..5), the ring should hold [3 4 5].
func TestRingOverflow(t *testing.T) {
	r := New[int](3)
	for i := 1; i <= 5; i++ {
		r.Push(i)
	}
	if r.Len() != 3 {
		t.Errorf("Len() = %d, want 3", r.Len())
	}
	got := r.Snapshot()
	if !reflect.DeepEqual(got, []int{3, 4, 5}) {
		t.Errorf("Snapshot = %v, want [3 4 5]", got)
	}
}

// TestRingLast covers the Last(n) helper with three cases: n smaller
// than count, n bigger than count, and n at zero.
func TestRingLast(t *testing.T) {
	r := New[int](5)
	for i := 1; i <= 4; i++ {
		r.Push(i)
	}

	tests := []struct {
		n    int
		want []int
	}{
		{2, []int{3, 4}},
		{10, []int{1, 2, 3, 4}},
		{0, []int{1, 2, 3, 4}},
	}
	for _, tt := range tests {
		got := r.Last(tt.n)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Last(%d) = %v, want %v", tt.n, got, tt.want)
		}
	}
}

func TestRingZeroCapacityPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic for capacity = 0")
		}
	}()
	_ = New[int](0)
}

// TestRingStringT is a tiny smoke test that the generic works for a
// type other than int.
func TestRingStringT(t *testing.T) {
	r := New[string](2)
	r.Push("a")
	r.Push("b")
	r.Push("c")
	got := r.Snapshot()
	if !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Errorf("Snapshot = %v, want [b c]", got)
	}
}
