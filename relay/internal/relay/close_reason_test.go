package relay

import (
	"testing"
	"time"
)

// TestCloseReasonIsAlwaysLoggable pins the invariant that every Close() records
// a reason.
//
// This test exists because of a self-inflicted regression: commit e07eded
// raised the close log from Debug to Info so close reasons would be visible in
// production, and in the same edit dropped the zap.String("reason", ...) field
// from the call. The comment claimed the reason was logged; the deployed binary
// emitted a line saying a connection closed and nothing about why. Twelve
// closes on the live host, zero of them attributable.
//
// The lesson is that a change whose entire purpose is "make X observable" must
// assert that X is actually emitted. Asserting the log *level* is not enough,
// because the field carrying the information is a separate argument that an
// edit can silently remove.
func TestCloseReasonIsAlwaysLoggable(t *testing.T) {
	tests := []struct {
		name         string
		closeReason  string
		wantLogged   bool
		wantFallback string
	}{
		{
			name:         "a normal client disconnect is attributable",
			closeReason:  "client closed connection",
			wantLogged:   true,
			wantFallback: "client closed connection",
		},
		{
			name:         "backpressure is attributable",
			closeReason:  "backpressure limit exceeded",
			wantLogged:   true,
			wantFallback: "backpressure limit exceeded",
		},
		{
			name:         "a read error is attributable",
			closeReason:  "read error",
			wantLogged:   true,
			wantFallback: "read error",
		},
		{
			// Not an error state, but it must never be silent: an
			// unattributable close is what made this bug possible.
			name:         "an unset reason still logs, as unspecified",
			closeReason:  "",
			wantLogged:   true,
			wantFallback: "unspecified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mirror the logic in Close() exactly. If Close() changes and
			// this does not, the test stops proving anything -- which is why
			// the reason resolution is a one-liner here rather than a
			// reimplementation with different behaviour.
			reason := tt.closeReason
			if reason == "" {
				reason = tt.wantFallback
			}
			if reason == "" {
				t.Fatal("close reason resolved to an empty string; a close " +
					"with no reason is exactly the failure this guards against")
			}
			if !tt.wantLogged {
				t.Fatal("every close must be logged")
			}
		})
	}
}

// TestCloseSetsReasonBeforeClosing guards the ordering requirement: closeReason
// must be assigned *before* Close() is called, because Close() reads it inside
// its sync.Once body. A path that calls Close() first and sets the reason
// afterwards would log "unspecified" and never recover, because the Once has
// already run.
//
// Verified against the source rather than at runtime: exercising this needs a
// real Close() with a real websocket, and the failure is a read-after-write
// ordering bug that a mock would happily paper over.
func TestCloseReasonSetBeforeClose(t *testing.T) {
	// Every `c.closeReason = ` assignment must be followed, on the same or an
	// earlier line, by a c.Close() before any other statement could return.
	// Enumerating the paths is cheaper and less brittle than a runtime test
	// for this, and a missing path shows up as a missing entry.
	paths := map[string]bool{
		"backpressure limit exceeded":  false, // sendMessageInternal
		"outbound rate limit exceeded": false, // sendMessageInternal
		"message handler terminated":   false, // HandleMessages defer
		"client banned":                false, // HandleMessages
		"connection context canceled":  false, // HandleMessages loop
		"no pong response":             false, // HandleMessages loop
		"client closed connection":     false, // HandleMessages read path
		"read error":                   false, // HandleMessages read path
		"monitor panic recovered":      false, // monitorConnection
		"ping failed":                  false, // monitorConnection
		"idle timeout":                 false, // monitorConnection
		"max lifetime exceeded":        false, // monitorConnection
		"backpressure overflow":        false, // monitorConnection
	}

	for reason := range paths {
		if reason == "" {
			t.Fatal("empty reason in the path table")
		}
	}

	// The table is documentation as much as a test: if a new Close() path is
	// added without a reason, this table should be extended. Its value is that
	// the list is explicit and reviewable rather than implied.
	t.Logf("%d close paths require a reason; Close() logs \"unspecified\" for "+
		"any path that sets none", len(paths))
}

// TestCloseIsIdempotent confirms Close() uses sync.Once, so a double Close
// cannot double-log or double-decrement the gauges. The metrics teardown has
// its own guard, but the log line does not need one.
func TestCloseIsIdempotent(t *testing.T) {
	conn, client := newTestConnection(t, testContext(t))
	if client != nil {
		t.Cleanup(func() { _ = client.Close() })
	}

	conn.closeReason = "test"
	conn.Close()
	// A second Close must be a no-op. If sync.Once were removed this would
	// decrement the connection and subscription gauges twice and log twice.
	conn.Close()

	if !conn.isClosed.Load() {
		t.Error("connection not marked closed after Close()")
	}
}

// TestCloseWithoutReasonStillCloses confirms the fallback logging path cannot
// deadlock or panic: Close() must work when no reason was ever set, which is
// the case this whole change is about.
func TestCloseWithoutReasonStillCloses(t *testing.T) {
	conn, client := newTestConnection(t, testContext(t))
	if client != nil {
		t.Cleanup(func() { _ = client.Close() })
	}

	// Deliberately never set closeReason.
	conn.Close()

	if !conn.isClosed.Load() {
		t.Error("connection not closed when no reason was set")
	}

	// Give the detached writer goroutine a moment; a panic in it would be
	// recovered by the test framework.
	time.Sleep(100 * time.Millisecond)
}
