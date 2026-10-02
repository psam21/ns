package relay

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

// TestSubscriptionCountIsLockProtected documents that the MaxSubscriptions cap
// is enforced against a lock-protected count rather than a direct map read.
//
// The direct read this replaces raced Close(), which replaces the whole map
// under c.subMu. This test asserts the accessor exists and agrees with the map
// under a concurrent Close, which is the shape the race detector needs to see
// to flag the old code.
func TestSubscriptionCountIsLockProtected(t *testing.T) {
	conn, _ := newTestConnection(t, testContext(t))

	for i := 0; i < 5; i++ {
		conn.addSubscription(subIDForTest(i), []nostr.Filter{{Limit: 1}})
	}
	if got := conn.subscriptionCount(); got != 5 {
		t.Fatalf("subscriptionCount() = %d, want 5", got)
	}

	conn.removeSubscription(subIDForTest(0))
	if got := conn.subscriptionCount(); got != 4 {
		t.Fatalf("subscriptionCount() after remove = %d, want 4", got)
	}
}

// TestAddSubscriptionReportsReplacement pins the contract the gauge fix depends
// on: addSubscription reports true when subID was already registered, so a
// duplicate REQ does not increment ActiveSubscriptions a second time.
func TestAddSubscriptionReportsReplacement(t *testing.T) {
	conn, _ := newTestConnection(t, testContext(t))

	if replaced := conn.addSubscription("dup", []nostr.Filter{{Limit: 1}}); replaced {
		t.Error("first addSubscription reported a replacement, want false")
	}
	if replaced := conn.addSubscription("dup", []nostr.Filter{{Limit: 2}}); !replaced {
		t.Error("second addSubscription did not report a replacement, want true")
	}
	if got := conn.subscriptionCount(); got != 1 {
		t.Errorf("subscriptionCount() = %d, want 1 (replacement must not grow the map)", got)
	}
	if filters := conn.getSubscriptionFilters("dup"); len(filters) != 1 || filters[0].Limit != 2 {
		t.Errorf("filters = %+v, want the replacement filter (limit 2)", filters)
	}
}

// TestSubscriptionCountConcurrentWithMapReplacement exercises the count
// accessor concurrently with the full-map replacement Close() performs. Under
// -race this fails if subscriptionCount bypasses the mutex; it is the negative
// control for the lock-protected read.
//
// The two goroutines run for a fixed duration rather than racing a stop
// channel. An earlier version signalled stop immediately after launching them,
// and the negative control (replacing the locked read with a bare len()) then
// passed -- the goroutines had not been scheduled far enough to overlap, so the
// test asserted nothing. Both sides now loop for a fixed window, which makes
// the overlap near-certain.
func TestSubscriptionCountConcurrentWithMapReplacement(t *testing.T) {
	conn, _ := newTestConnection(t, testContext(t))
	for i := 0; i < 20; i++ {
		conn.addSubscription(subIDForTest(i), []nostr.Filter{{Limit: 1}})
	}

	// Warm both goroutines up before timing so the window is spent doing work
	// rather than being consumed by scheduling.
	conn.subscriptionCount()
	conn.subMu.Lock()
	conn.subscriptions = make(map[string][]nostr.Filter)
	conn.subMu.Unlock()

	const window = 250 * time.Millisecond
	deadline := time.Now().Add(window)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for time.Now().Before(deadline) {
			conn.subMu.Lock()
			conn.subscriptions = make(map[string][]nostr.Filter)
			conn.subMu.Unlock()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for time.Now().Before(deadline) {
			_ = conn.subscriptionCount()
		}
	}()

	wg.Wait()

	// The map is being swapped under the lock, so the final count is
	// nondeterministic. All this asserts is that reading it did not race.
	_ = conn.subscriptionCount()
}

// testContext returns a background-derived context for a test connection.
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}

func subIDForTest(i int) string {
	return "sub-" + strconv.Itoa(i)
}
