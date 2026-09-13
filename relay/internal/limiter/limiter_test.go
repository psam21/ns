package limiter

import (
	"testing"
	"time"

	"github.com/Shugur-Network/relay/internal/config"
)

func testLimit() RateLimit {
	return RateLimit{MaxEvents: 2, WindowSize: time.Minute, BurstSize: 0, BanThreshold: 2, BanDuration: time.Hour}
}

func TestRateLimiterEnforcesLimitAndReset(t *testing.T) {
	limiter := NewRateLimiter(&config.Config{})
	limit := testLimit()
	limiter.SetLimit("client", limit)
	if !limiter.Allow("client", limit) || !limiter.Allow("client", limit) {
		t.Fatal("first two events should be allowed")
	}
	if limiter.Allow("client", limit) {
		t.Fatal("event beyond the configured limit was allowed")
	}
	limiter.Reset("client")
	if !limiter.Allow("client", limit) {
		t.Fatal("event should be allowed after reset")
	}
}

func TestRateLimiterMapsEmptyKeyToDefaultBucket(t *testing.T) {
	limiter := NewRateLimiter(&config.Config{})
	limit := testLimit()
	if !limiter.Allow("", limit) {
		t.Fatal("first empty-key event should be allowed")
	}
	if limiter.GetCounter("default") == nil {
		t.Fatal("empty key did not use the default bucket")
	}
}

func TestRateLimiterReturnsConfiguredAndFallbackLimits(t *testing.T) {
	limiter := NewRateLimiter(&config.Config{})
	configured := RateLimit{MaxEvents: 9, WindowSize: time.Second}
	limiter.SetLimit("special", configured)
	if got := limiter.GetLimit("special"); got != configured {
		t.Fatalf("configured limit = %#v, want %#v", got, configured)
	}
	fallback := limiter.GetLimit("missing")
	if fallback.WindowSize != time.Minute {
		t.Fatalf("fallback window = %s, want 1m", fallback.WindowSize)
	}
}
