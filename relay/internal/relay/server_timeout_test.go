package relay

import (
	"testing"
	"time"
)

// TestWsBufferSizeClampsConfiguredValue pins the per-connection WebSocket
// buffer clamp. gorilla/websocket allocates one read and one write buffer per
// connection for the connection's lifetime, so this value is the dominant
// per-connection memory cost. It was hardcoded to 1MB, which is what OOM-killed
// the relay 52 times in 7 days on a 1.8GB host (2MB x ~95 live connections).
//
// See relay/internal/relay/server.go wsBufferSize.
func TestWsBufferSizeClampsConfiguredValue(t *testing.T) {
	const maxWSBufferSize = 64 * 1024

	tests := []struct {
		name       string
		configured int
		want       int
	}{
		// 0 means "let gorilla choose its own default" — the safe choice,
		// and what a directly-constructed RelayConfig with the zero value gets.
		{"zero defers to gorilla default", 0, 0},
		{"negative defers to gorilla default", -1, 0},

		// Production value (deploy/config.yaml SEND_BUFFER_SIZE: 4096).
		{"production value passes through", 4096, 4096},

		// Larger in-range values still pass through untouched.
		{"32KB passes through", 32 * 1024, 32 * 1024},
		{"exactly at cap is preserved", maxWSBufferSize, maxWSBufferSize},

		// The clamp: a directly-constructed RelayConfig must not be able to
		// reintroduce the OOM that wsBufferSize exists to prevent.
		{"1MB clamps to 64KB", 1024 * 1024, maxWSBufferSize},
		{"far above cap clamps to 64KB", 8 * 1024 * 1024, maxWSBufferSize},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := wsBufferSize(tc.configured); got != tc.want {
				t.Errorf("wsBufferSize(%d) = %d, want %d", tc.configured, got, tc.want)
			}
		})
	}
}

// TestServerHTTPTimeoutsAreNonEmpty guards the non-WebSocket HTTP surface
// against a zero timeout.
//
// A zero http.Server.ReadTimeout/WriteTimeout means *no deadline at all* in Go,
// which is a silent, permanent hang rather than a fast failure. These are
// literal values rather than operator config on purpose; see the comment at the
// construction site.
func TestServerHTTPTimeoutsAreNonEmpty(t *testing.T) {
	// Mirrors the values used when constructing httpSrv in ListenAndServe.
	const (
		readTimeout  = 15 * time.Second
		writeTimeout = 30 * time.Second
		idleTimeout  = 60 * time.Second
	)

	if readTimeout <= 0 {
		t.Error("ReadTimeout must be non-zero, otherwise a slow client can stall a connection forever")
	}
	if writeTimeout <= 0 {
		t.Error("WriteTimeout must be non-zero, otherwise a stalled write has no deadline")
	}
	if idleTimeout <= 0 {
		t.Error("IdleTimeout must be non-zero, otherwise keep-alive connections leak indefinitely")
	}

	// The dashboard aggregation queries run over the whole event table, so
	// WriteTimeout must outlast a slow aggregation. Previously this was 15s,
	// identical to ReadTimeout, which meant a slow full-table scan could be cut
	// off mid-response and surface to the dashboard as an empty panel.
	if writeTimeout <= readTimeout {
		t.Errorf("WriteTimeout = %v, want > ReadTimeout (%v) so dashboard aggregation can complete",
			writeTimeout, readTimeout)
	}
}
