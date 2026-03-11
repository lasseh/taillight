package tui

// EventList is a bounded, append-only list that trims oldest entries on overflow.
type EventList[T any] struct {
	items []T
	max   int
}

// NewEventList creates an EventList with the given capacity.
func NewEventList[T any](capacity int) *EventList[T] {
	return &EventList[T]{
		items: make([]T, 0, capacity),
		max:   capacity,
	}
}

// Push appends an item, trimming the oldest if at capacity.
func (l *EventList[T]) Push(item T) {
	if len(l.items) >= l.max {
		// Trim oldest 10% to avoid trimming on every push.
		trim := max(l.max/10, 1)
		l.items = append(l.items[:0:0], l.items[trim:]...)
	}
	l.items = append(l.items, item)
}

// Len returns the number of items.
func (l *EventList[T]) Len() int {
	return len(l.items)
}

// Get returns the item at index i. Panics if out of range.
func (l *EventList[T]) Get(i int) T {
	return l.items[i]
}

// Slice returns all items as a slice (do not mutate).
func (l *EventList[T]) Slice() []T {
	return l.items
}

// Clear removes all items.
func (l *EventList[T]) Clear() {
	l.items = l.items[:0]
}
