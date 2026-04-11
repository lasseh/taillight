package buffer

import (
	"testing"
)

func TestRing_PushAndSlice(t *testing.T) {
	r := New[int](5)

	// Empty buffer.
	if got := r.Len(); got != 0 {
		t.Fatalf("empty Len() = %d, want 0", got)
	}
	if got := r.Slice(); got != nil {
		t.Fatalf("empty Slice() = %v, want nil", got)
	}

	// Push within capacity.
	for i := 1; i <= 3; i++ {
		r.Push(i)
	}
	if got := r.Len(); got != 3 {
		t.Fatalf("Len() = %d, want 3", got)
	}
	assertSlice(t, r.Slice(), []int{1, 2, 3})
}

func TestRing_Overflow(t *testing.T) {
	r := New[int](3)
	for i := 1; i <= 5; i++ {
		r.Push(i)
	}
	if got := r.Len(); got != 3 {
		t.Fatalf("Len() = %d, want 3", got)
	}
	// Oldest two (1, 2) were dropped.
	assertSlice(t, r.Slice(), []int{3, 4, 5})
}

func TestRing_ReverseSlice(t *testing.T) {
	r := New[int](5)
	for i := 1; i <= 4; i++ {
		r.Push(i)
	}
	assertSlice(t, r.ReverseSlice(), []int{4, 3, 2, 1})
}

func TestRing_Last(t *testing.T) {
	r := New[int](3)

	if _, ok := r.Last(); ok {
		t.Fatal("Last() on empty buffer should return false")
	}

	r.Push(10)
	r.Push(20)
	got, ok := r.Last()
	if !ok || got != 20 {
		t.Fatalf("Last() = (%d, %v), want (20, true)", got, ok)
	}

	// After overflow, last should still be the most recent.
	r.Push(30)
	r.Push(40) // capacity 3, so 10 is dropped
	got, ok = r.Last()
	if !ok || got != 40 {
		t.Fatalf("Last() after overflow = (%d, %v), want (40, true)", got, ok)
	}
}

func TestRing_Clear(t *testing.T) {
	r := New[int](3)
	r.Push(1)
	r.Push(2)
	r.Clear()

	if got := r.Len(); got != 0 {
		t.Fatalf("after Clear(), Len() = %d, want 0", got)
	}
	if got := r.Slice(); got != nil {
		t.Fatalf("after Clear(), Slice() = %v, want nil", got)
	}
}

func TestRing_DefaultCapacity(t *testing.T) {
	r := New[int](0)
	if r.cap != 10000 {
		t.Fatalf("New(0) capacity = %d, want 10000", r.cap)
	}
}

func assertSlice(t *testing.T, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d; got %v", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("index %d: got %d, want %d; full: %v", i, got[i], want[i], got)
		}
	}
}
