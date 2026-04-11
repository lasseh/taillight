// Package buffer provides a generic ring buffer for bounded event storage.
package buffer

// Ring is a fixed-capacity ring buffer. When full, the oldest element is
// silently dropped on Push.
type Ring[T any] struct {
	data  []T
	head  int // next write position
	count int
	cap   int
}

// New creates a ring buffer with the given capacity.
func New[T any](capacity int) *Ring[T] {
	if capacity <= 0 {
		capacity = 10000
	}
	return &Ring[T]{
		data: make([]T, capacity),
		cap:  capacity,
	}
}

// Push appends an element. If the buffer is full, the oldest element is
// overwritten.
func (r *Ring[T]) Push(v T) {
	r.data[r.head] = v
	r.head = (r.head + 1) % r.cap
	if r.count < r.cap {
		r.count++
	}
}

// Len returns the number of elements in the buffer.
func (r *Ring[T]) Len() int {
	return r.count
}

// Slice returns all elements in insertion order (oldest first).
func (r *Ring[T]) Slice() []T {
	if r.count == 0 {
		return nil
	}
	out := make([]T, r.count)
	if r.count < r.cap {
		// Buffer not yet full — data starts at 0.
		copy(out, r.data[:r.count])
	} else {
		// Buffer full — head points to the oldest element.
		n := copy(out, r.data[r.head:r.cap])
		copy(out[n:], r.data[:r.head])
	}
	return out
}

// ReverseSlice returns all elements in reverse insertion order (newest first).
func (r *Ring[T]) ReverseSlice() []T {
	s := r.Slice()
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return s
}

// Clear resets the buffer to empty.
func (r *Ring[T]) Clear() {
	r.head = 0
	r.count = 0
	// Zero out to allow GC of referenced objects.
	var zero T
	for i := range r.data {
		r.data[i] = zero
	}
}

// Last returns the most recently pushed element and true, or the zero value
// and false if the buffer is empty.
func (r *Ring[T]) Last() (T, bool) {
	if r.count == 0 {
		var zero T
		return zero, false
	}
	idx := (r.head - 1 + r.cap) % r.cap
	return r.data[idx], true
}
