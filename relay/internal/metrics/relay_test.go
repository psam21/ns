package metrics

import "testing"

// TestActiveConnectionsFlooredAtZero verifies the contract that
// GetActiveConnectionsCount is never negative even if Decrement is
// called more times than Increment (issue #99).
func TestActiveConnectionsFlooredAtZero(t *testing.T) {
	// Reset the local counter to a known state. The package-level variable
	// is shared across tests, so we coerce to 0 before asserting.
	for {
		cur := atomicLoadActive()
		if cur == 0 {
			break
		}
		atomicAddActive(-cur)
	}

	// Decrementing an already-zero counter must not produce a negative
	// value.
	for i := 0; i < 100; i++ {
		DecrementActiveConnections()
		if got := GetActiveConnectionsCount(); got < 0 {
			t.Fatalf("counter went negative after %d decrements: %d", i+1, got)
		}
	}

	// After N increments and N+M decrements, the counter is exactly 0.
	for i := 0; i < 5; i++ {
		IncrementActiveConnections()
	}
	for i := 0; i < 8; i++ {
		DecrementActiveConnections()
	}
	if got := GetActiveConnectionsCount(); got != 0 {
		t.Fatalf("counter not floored at 0: got %d", got)
	}
}

// atomicLoadActive / atomicAddActive are thin test helpers that go
// through the same atomic operations as the real functions, so the
// test does not need to import sync/atomic directly here.
func atomicLoadActive() int64 {
	return GetActiveConnectionsCount()
}

func atomicAddActive(delta int64) {
	if delta > 0 {
		for i := int64(0); i < delta; i++ {
			IncrementActiveConnections()
		}
		return
	}
	for i := int64(0); i < -delta; i++ {
		DecrementActiveConnections()
	}
}
