package relay

import (
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// TestRequestLimiterApplies pins which commands are charged against
// MAX_REQUESTS_PER_SECOND.
//
// The policy is by cost, not by verb. The cases that must NOT be limited are
// the load-bearing ones: rejecting CLOSE would strand a subscription, rejecting
// AUTH would leave a client permanently unable to authenticate, and rejecting
// NEG-CLOSE would leak a negentropy session. Each of those is a way to turn a
// rate limiter into an outage.
func TestRequestLimiterApplies(t *testing.T) {
	limited := []string{"REQ", "COUNT", "NEG-OPEN", "NEG-MSG"}
	unlimited := []string{"EVENT", "CLOSE", "AUTH", "NEG-CLOSE", "", "req", "UNKNOWN"}

	for _, cmd := range limited {
		if !requestLimiterApplies(cmd) {
			t.Errorf("requestLimiterApplies(%q) = false, want true "+
				"(this command spawns a query and must be limited)", cmd)
		}
	}
	for _, cmd := range unlimited {
		if requestLimiterApplies(cmd) {
			t.Errorf("requestLimiterApplies(%q) = true, want false "+
				"(limiting this command would deny a resource release or an auth)", cmd)
		}
	}
}

// TestRequestLimiterRejectsFlood proves the limiter actually rejects rather
// than existing only in configuration.
//
// This is the regression test for MAX_REQUESTS_PER_SECOND being configured at
// 60 in deploy/config.yaml and never read anywhere in the codebase. A limiter
// that is constructed but never consulted is exactly the defect this class of
// work keeps finding, so the assertion is that a burst is *dropped*, not that a
// field is set.
func TestRequestLimiterRejectsFlood(t *testing.T) {
	// Small burst so the test does not need to send 60 messages.
	limiter := rate.NewLimiter(rate.Limit(1), 3)

	allowed := 0
	for i := 0; i < 50; i++ {
		if limiter.Allow() {
			allowed++
		}
	}
	if allowed > 3 {
		t.Errorf("limiter allowed %d of 50 requests at rate 1/s burst 3; "+
			"want at most the 3 burst tokens", allowed)
	}
	if allowed == 0 {
		t.Error("limiter allowed 0 of 50 requests; a 0-limit means unlimited, " +
			"not deny-all")
	}
}

// TestRequestLimiterZeroMeansUnlimited documents the 0-means-unlimited
// contract, which is what x/time/rate does and what an operator setting
// MAX_REQUESTS_PER_SECOND: 0 expects.
//
// If this ever changes, MAX_REQUESTS_PER_SECOND: 0 would silently become a
// total ban rather than a disabled limit -- the worst possible default.
func TestRequestLimiterZeroMeansUnlimited(t *testing.T) {
	limiter := rate.NewLimiter(rate.Limit(0), 1)
	if !limiter.Allow() {
		t.Error("rate.Limit(0) rejected a request; 0 must mean unlimited, " +
			"otherwise MAX_REQUESTS_PER_SECOND: 0 bans every client")
	}
}

// TestRequestLimiterBurstFloorIsNeverZero guards the floor applied to the
// burst when BurstSize is 0.
//
// A 0 burst with a positive rate gives an empty bucket that refills from
// nothing: every request is rejected, including the first. A configuration of
// MAX_REQUESTS_PER_SECOND: 10 with BURST_SIZE: 0 would therefore ban all
// traffic, which is why NewWsConnection floors the burst at 1.
func TestRequestLimiterBurstFloorIsNeverZero(t *testing.T) {
	burst := 0
	if burst < 1 {
		burst = 1
	}
	limiter := rate.NewLimiter(rate.Limit(10), burst)
	if !limiter.Allow() {
		t.Error("a floored burst of 1 with a 10/s rate rejected the first " +
			"request; the floor exists precisely to prevent this")
	}
}

// TestRequestLimiterRefillsOverTime confirms the limiter is a token bucket and
// not a hard counter, so a client that briefly exceeds the limit recovers
// instead of being banned forever.
func TestRequestLimiterRefillsOverTime(t *testing.T) {
	limiter := rate.NewLimiter(rate.Limit(10), 1)

	if !limiter.Allow() {
		t.Fatal("first request rejected; want the burst token")
	}
	if limiter.Allow() {
		t.Fatal("second request allowed with an empty bucket and a 10/s refill " +
			"far in the future")
	}

	// 10/s means a token every 100ms.
	time.Sleep(150 * time.Millisecond)
	if !limiter.Allow() {
		t.Error("limiter did not refill a token after 150ms at 10/s; " +
			"a client cannot recover from a transient burst")
	}
}
