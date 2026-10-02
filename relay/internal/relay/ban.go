package relay

import (
	"sync"
	"time"
)

// banRegistry tracks banned clients and their violation history.
//
// Replaces three package-level maps (clientBanList, clientExceededCount) that
// were read and written under a single mutex, with one type owning its own
// lock. The behaviour difference is the point: violations are now recorded
// per IP and *retained across connections*, which is what makes
// PROGRESSIVE_BAN meaningful.
//
// The previous code deleted a client's violation count on every new connection
// ("Reset exceeded count on new allowed connection"). Combined with bans that
// used a single fixed duration, that meant a client could never be punished
// more harshly than a first offender: disconnect, reconnect, count is zero.
// PROGRESSIVE_BAN and MAX_BAN_DURATION were configured in deploy/config.yaml
// and read nowhere, so there was no way to express "ban this one longer".
type banRegistry struct {
	mu sync.Mutex

	// banned maps client IP to when the ban expires.
	banned map[string]time.Time

	// violations counts rate-limit violations per client IP.
	//
	// Deliberately NOT cleared on connect. A client that trips the limiter
	// repeatedly must stay on the escalation ladder even across separate
	// connections, otherwise the ladder is trivially defeated by reconnecting.
	violations map[string]int
}

func newBanRegistry() *banRegistry {
	return &banRegistry{
		banned:     make(map[string]time.Time),
		violations: make(map[string]int),
	}
}

// isBanned reports whether the client is currently banned, and when the ban
// expires. An expired entry is removed on read so callers do not have to
// distinguish "expired" from "absent".
func (r *banRegistry) isBanned(ip string) (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	expiry, ok := r.banned[ip]
	if !ok {
		return time.Time{}, false
	}
	if time.Now().After(expiry) {
		delete(r.banned, ip)
		return time.Time{}, false
	}
	return expiry, true
}

// recordViolation increments the client's violation count and returns the new
// total.
func (r *banRegistry) recordViolation(ip string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.violations[ip]++
	return r.violations[ip]
}

// resetViolations clears a client's violation history. Called only when a
// client has been well behaved for a sustained period -- never on connect,
// which would let a client reset their ladder at will.
func (r *banRegistry) resetViolations(ip string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.violations, ip)
}

// violationCount returns the current violation total for a client.
func (r *banRegistry) violationCount(ip string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.violations[ip]
}

// banDurationFor returns the ban length for a client on their nth violation.
//
// With progressive banning on, each threshold crossing doubles the duration,
// starting at base and capped at max:
//
//	1st  ->  base
//	2nd  ->  base * 2
//	3rd  ->  base * 4
//	...
//	nth  ->  min(base * 2^(n-1), max)
//
// The cap is what MAX_BAN_DURATION is for. Without it the exponential
// overflows time.Duration and wraps negative, which would produce an
// already-expired ban -- a client punished with a *shorter* ban for more
// severe abuse.
//
// With progressive banning off, every violation gets base, preserving the
// previous fixed-duration behaviour.
func banDurationFor(violations int, base, max time.Duration, progressive bool) time.Duration {
	if base <= 0 {
		base = time.Minute
	}
	if !progressive {
		return base
	}
	if max <= 0 {
		max = 24 * time.Hour
	}
	if max < base {
		// A max below the base would make every ban shorter than the first.
		return base
	}

	// violations is 1-based, so the first ban is the base duration.
	d := base
	for i := 1; i < violations; i++ {
		if d >= max {
			break
		}
		d *= 2
		// Check before multiplying again to avoid overflow on a long-lived
		// offender: once d >= max the loop breaks next iteration, but the
		// multiplication itself must not wrap.
		if d >= max || d <= 0 {
			return max
		}
	}
	if d > max {
		return max
	}
	return d
}

// ban applies a ban for the given duration and returns the expiry.
func (r *banRegistry) ban(ip string, d time.Duration) time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()

	expiry := time.Now().Add(d)
	r.banned[ip] = expiry
	return expiry
}

// unban clears any ban and the violation history for a client.
func (r *banRegistry) unban(ip string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.banned, ip)
	delete(r.violations, ip)
}

// sweepExpired removes expired bans, returning how many were removed.
//
// Called periodically so the maps do not grow without bound as clients churn.
func (r *banRegistry) sweepExpired() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	n := 0
	for ip, expiry := range r.banned {
		if now.After(expiry) {
			delete(r.banned, ip)
			n++
		}
	}
	return n
}

// stats returns the current ban and violation counts, for logging and metrics.
func (r *banRegistry) stats() (banned, tracked int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.banned), len(r.violations)
}
