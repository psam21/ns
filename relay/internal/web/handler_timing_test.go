package web

import (
	"testing"
	"time"
)

// TestDashboardRefreshIntervalsAreCoherent pins the relationship between the
// event_kind_stats refresh period, the query timeout, and the interval.
//
// Measured on the production host (nostr.ltd, 2026-10-02) against a 1,198,749
// row events table with shared_buffers=128MB on a 1.8GB t4g.small:
//
//	REFRESH MATERIALIZED VIEW CONCURRENTLY event_kind_stats  ->  40.9 s
//	SELECT count(*) FROM events                                ->  25.3 s
//
// The refresher ran on a 30s ticker with a 20s query timeout. Because the
// query needs ~41s and the context is cancelled at 20s, *every* refresh ended
// in "context deadline exceeded". The materialized view was therefore never
// successfully updated, the in-memory cache never filled, and
// HandleEventsAPI kept falling back to the legacy path. That fallback logs a
// warning per request and is what the dashboard's empty panels came from.
//
// Worse, pg_stat_activity showed 3 concurrent DataFileRead queries: the
// 20s-timeout refresh was being killed, the ticker fired again, and the
// discarded scans overlapped against a pool capped at 25 connections.
//
// The invariant these tests enforce is the one that makes the loop
// terminating: the timeout must exceed the query, and the tick must not be
// scheduled so often that a previous attempt is still running.
func TestDashboardRefreshIntervalsAreCoherent(t *testing.T) {
	// Worst-case measured duration of the underlying MV refresh on the
	// production dataset. Treat this as the floor for the timeout.
	const measuredRefreshDuration = 41 * time.Second

	if eventQueryTimeout <= measuredRefreshDuration {
		t.Errorf("eventQueryTimeout = %v, must exceed the measured refresh duration (%v); "+
			"otherwise every refresh is cancelled and the cache never fills",
			eventQueryTimeout, measuredRefreshDuration)
	}

	// The cache TTL must be no shorter than the timeout, or a refresh can be
	// cancelled mid-flight after the value was already considered stale.
	if eventBreakdownCacheTTL < eventQueryTimeout {
		t.Errorf("eventBreakdownCacheTTL = %v, must be >= eventQueryTimeout (%v)",
			eventBreakdownCacheTTL, eventQueryTimeout)
	}
}

// TestEventKindStatsRefreshIntervalExceedsTimeout asserts the ticker does not
// fire while the previous refresh can still be running.
//
// With a 30s tick and a 60s timeout, a refresh that takes the full timeout
// would still be in flight when the next tick arrives. The refresher guards
// against overlap with eventKindStatsRefreshing, so this is not a correctness
// bug — but it means the tick is wasted work, and it is the property that
// produced three stacked scans in production.
func TestEventKindStatsRefreshIntervalExceedsTimeout(t *testing.T) {
	if eventKindStatsRefreshEvery < eventQueryTimeout {
		t.Errorf("eventKindStatsRefreshEvery = %v, must be >= eventQueryTimeout (%v) "+
			"so the interval is shorter than a full refresh",
			eventKindStatsRefreshEvery, eventQueryTimeout)
	}
}
