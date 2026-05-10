package testutil

import "testing"

func AssertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func AssertError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func AssertEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func AssertNotEqual[T comparable](t *testing.T, got, notWant T) {
	t.Helper()
	if got == notWant {
		t.Errorf("got %v, should not equal %v", got, notWant)
	}
}

func AssertContains(t *testing.T, slice []string, item string) {
	t.Helper()
	for _, s := range slice {
		if s == item {
			return
		}
	}
	t.Errorf("slice %v does not contain %q", slice, item)
}

func AssertLen[T any](t *testing.T, slice []T, want int) {
	t.Helper()
	if len(slice) != want {
		t.Errorf("len = %d, want %d", len(slice), want)
	}
}
