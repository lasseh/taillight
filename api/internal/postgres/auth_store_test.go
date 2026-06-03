package postgres

import (
	"sync"
	"testing"
)

// TestAuthStore_EnqueueTouchAfterStopDoesNotPanic verifies the touch-send path
// is panic-safe across shutdown: touchCh is never closed, so a request that
// touches a session concurrently with Stop cannot send on a closed channel
// (audit M5). Uses a struct literal so no worker goroutine or pool is needed.
func TestAuthStore_EnqueueTouchAfterStopDoesNotPanic(t *testing.T) {
	s := &AuthStore{
		touchCh: make(chan touchOp, 4),
		stopCh:  make(chan struct{}),
	}
	s.Stop()

	// Far more sends than the buffer can hold; none may panic.
	for range 100 {
		s.enqueueTouch(touchOp{query: "UPDATE x", arg: 1})
	}
}

// TestAuthStore_StopIsIdempotent verifies Stop can be called repeatedly and
// concurrently without panicking on a double close.
func TestAuthStore_StopIsIdempotent(t *testing.T) {
	s := &AuthStore{
		touchCh: make(chan touchOp, 4),
		stopCh:  make(chan struct{}),
	}

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Stop()
		}()
	}
	wg.Wait()
}

// TestAuthStore_EnqueueTouchConcurrentWithStop exercises the race between an
// in-flight touch send and Stop under the race detector.
func TestAuthStore_EnqueueTouchConcurrentWithStop(t *testing.T) {
	s := &AuthStore{
		touchCh: make(chan touchOp, 4),
		stopCh:  make(chan struct{}),
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 1000 {
			s.enqueueTouch(touchOp{query: "UPDATE x", arg: 1})
		}
	}()
	go func() {
		defer wg.Done()
		s.Stop()
	}()
	wg.Wait()
}
