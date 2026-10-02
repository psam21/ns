package relay

import (
	"math"
	"sync"
	"testing"
	"time"
)

// TestBanDurationForEscalates is the core assertion for PROGRESSIVE_BAN, which
// was configured in deploy/config.yaml and read nowhere in the codebase.
func TestBanDurationForEscalates(t *testing.T) {
	const base = 5 * time.Minute
	const max = 24 * time.Hour

	tests := []struct {
		violations int
		want       time.Duration
	}{
		{1, base},
		{2, 10 * time.Minute},
		{3, 20 * time.Minute},
		{4, 40 * time.Minute},
		{5, 80 * time.Minute},
		{6, 160 * time.Minute},
		{7, 320 * time.Minute},
		{8, 640 * time.Minute},
		{9, 1280 * time.Minute},
		{10, 24 * time.Hour}, // 2560m would exceed max, so capped
		{50, 24 * time.Hour},
		{1000, 24 * time.Hour},
	}

	for _, tt := range tests {
		got := banDurationFor(tt.violations, base, max, true)
		if got != tt.want {
			t.Errorf("banDurationFor(%d, %v, %v, true) = %v, want %v",
				tt.violations, base, max, got, tt.want)
		}
	}
}

// TestBanDurationForIsMonotonic asserts a longer ban never gets shorter as
// violations increase. A non-monotonic ladder is worse than a flat one because
// it punishes escalation with a reprieve.
func TestBanDurationForIsMonotonic(t *testing.T) {
	base := 5 * time.Minute
	max := 24 * time.Hour
	prev := time.Duration(0)

	for v := 1; v <= 200; v++ {
		got := banDurationFor(v, base, max, true)
		if got < prev {
			t.Fatalf("banDurationFor(%d) = %v, which is shorter than the %v "+
				"given to fewer violations; the ladder must be monotonic", v, got, prev)
		}
		prev = got
	}
}

// TestBanDurationForDoesNotOverflow is the reason MAX_BAN_DURATION is checked
// before each doubling rather than after.
//
// time.Duration is int64 nanoseconds; ~292 years is the ceiling. A client with
// a large violation count and no cap would wrap to a negative duration, and
// time.Now().Add(negative) is an already-expired ban -- a repeat offender
// punished with a shorter ban than a first offender.
func TestBanDurationForDoesNotOverflow(t *testing.T) {
	base := time.Hour
	// An absurd cap that no sane operator would set, and a violation count
	// large enough that unguarded doubling would wrap int64.
	const hugeMax = time.Duration(math.MaxInt64)
	negative := 0

	for v := 1; v <= 5000; v++ {
		got := banDurationFor(v, base, hugeMax, true)
		if got < 0 {
			negative++
		}
		if got > hugeMax {
			t.Fatalf("banDurationFor(%d) = %v, exceeds the cap %v", v, got, hugeMax)
		}
	}
	if negative > 0 {
		t.Errorf("%d of 5000 escalating bans produced a negative duration; "+
			"a negative ban is already expired, so the cap is not being enforced "+
			"before the multiplication", negative)
	}
}

// TestBanDurationForWithoutProgressivePreservesFlatBehaviour confirms that
// PROGRESSIVE_BAN: false reproduces exactly the old fixed-duration behaviour,
// so operators who never set it get no surprise.
func TestBanDurationForWithoutProgressivePreservesFlatBehaviour(t *testing.T) {
	base := 5 * time.Minute
	for v := 1; v <= 100; v++ {
		if got := banDurationFor(v, base, 24*time.Hour, false); got != base {
			t.Fatalf("banDurationFor(%d, progressive=false) = %v, want the flat %v",
				v, got, base)
		}
	}
}

// TestBanDurationForClampBelowBase covers the misconfiguration where
// MAX_BAN_DURATION is shorter than BAN_DURATION. Clamping to base is right;
// taking the min would make later violations get a *shorter* ban.
func TestBanDurationForClampBelowBase(t *testing.T) {
	base := time.Hour
	max := time.Minute // operator set a max below the base

	for v := 1; v <= 20; v++ {
		if got := banDurationFor(v, base, max, true); got != base {
			t.Errorf("banDurationFor(%d, base=%v, max=%v, true) = %v, want %v "+
				"(a max below base must clamp to base, not shorten the ban)",
				v, base, max, got, base)
		}
	}
}

// TestBanRegistryViolationsSurviveReconnect is the regression test for the
// defect that made the ladder useless.
//
// The old code deleted clientExceededCount[clientIP] on every accepted
// connection, so a client could trip the limiter, disconnect, reconnect, and
// be treated as a first offender forever. Nothing in the old code path could
// produce a second-rung ban.
func TestBanRegistryViolationsSurviveReconnect(t *testing.T) {
	r := newBanRegistry()

	for i := 1; i <= 5; i++ {
		if got := r.recordViolation("1.2.3.4"); got != i {
			t.Fatalf("recordViolation returned %d, want %d", got, i)
		}
	}

	// A reconnect must not clear the history. There is deliberately no
	// method that does so other than resetViolations, which is only called
	// for a well-behaved client.
	if got := r.violationCount("1.2.3.4"); got != 5 {
		t.Errorf("violationCount after reconnect = %d, want 5; a reconnect must "+
			"not reset the escalation ladder", got)
	}

	r.resetViolations("1.2.3.4")
	if got := r.violationCount("1.2.3.4"); got != 0 {
		t.Errorf("violationCount after resetViolations = %d, want 0", got)
	}
}

// TestBanRegistryIsBannedExpires covers the expiry boundary.
func TestBanRegistryIsBanned(t *testing.T) {
	r := newBanRegistry()

	if _, banned := r.isBanned("9.9.9.9"); banned {
		t.Error("an unknown client reported as banned")
	}

	r.ban("9.9.9.9", 50*time.Millisecond)
	if _, banned := r.isBanned("9.9.9.9"); !banned {
		t.Error("a freshly banned client reported as not banned")
	}

	time.Sleep(80 * time.Millisecond)
	if _, banned := r.isBanned("9.9.9.9"); banned {
		t.Error("an expired ban still reports as banned")
	}
	// The expired entry should have been dropped on read.
	if active, _ := r.stats(); active != 0 {
		t.Errorf("active ban count = %d after expiry, want 0; the entry should be "+
			"reaped on read", active)
	}
}

func TestBanRegistrySweepExpired(t *testing.T) {
	r := newBanRegistry()

	r.ban("1.1.1.1", 10*time.Millisecond)
	r.ban("2.2.2.2", time.Hour)
	r.ban("3.3.3.3", 10*time.Millisecond)

	time.Sleep(30 * time.Millisecond)

	if n := r.sweepExpired(); n != 2 {
		t.Errorf("sweepExpired removed %d, want 2", n)
	}
	if _, banned := r.isBanned("2.2.2.2"); !banned {
		t.Error("sweepExpired removed a ban that had not expired")
	}
}

func TestBanRegistryUnbanClearsHistory(t *testing.T) {
	r := newBanRegistry()

	r.recordViolation("4.4.4.4")
	r.recordViolation("4.4.4.4")
	r.ban("4.4.4.4", time.Hour)

	r.unban("4.4.4.4")

	if _, banned := r.isBanned("4.4.4.4"); banned {
		t.Error("unban left the client banned")
	}
	if n := r.violationCount("4.4.4.4"); n != 0 {
		t.Errorf("unban left %d violations; an operator unblock should not leave "+
			"the client on a stale escalation rung", n)
	}
}

// TestBanRegistryConcurrentAccess exercises the registry under -race, replacing
// the package-level maps and their single mutex.
func TestBanRegistryConcurrentAccess(t *testing.T) {
	r := newBanRegistry()
	ips := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"}

	const window = 200 * time.Millisecond
	deadline := time.Now().Add(window)

	var wg sync.WaitGroup
	for _, ip := range ips {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			for time.Now().Before(deadline) {
				r.recordViolation(ip)
				_, _ = r.isBanned(ip)
				_ = r.violationCount(ip)
			}
		}(ip)
	}

	// A ban/unban sweep from a different goroutine, standing in for the
	// 10-minute cleanExpiredBans loop.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for time.Now().Before(deadline) {
			for _, ip := range ips {
				r.ban(ip, time.Millisecond)
			}
			r.sweepExpired()
		}
	}()

	wg.Wait()
}
