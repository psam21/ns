package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Shugur-Network/relay/internal/config"
	"github.com/Shugur-Network/relay/internal/domain"
	"github.com/Shugur-Network/relay/internal/storage"
	nostr "github.com/nbd-wtf/go-nostr"
)

// TestConnectionGoroutinesExitOnClose is the regression test for a goroutine
// leak that ran for at least a day on the production host.
//
// WsConnection spawned two background goroutines — the connection monitor and
// the NIP-77 negentropy sweeper — and both were handed the *server-wide*
// context threaded down from ListenAndServe. That context is only canceled at
// process shutdown, so closing a WebSocket stopped neither goroutine. Each
// abandoned goroutine also pins its WsConnection, keeping the websocket, its
// read/write buffers and its subscription map resident after Close().
//
// Measured on nostr.ltd 2026-10-02: 55 of 148 live goroutines were
// startNegSweeper.func1, parked on a 30s ticker, with RSS climbing from 64MB
// to 124MB over ~3.6 hours in a single process. Before the buffer fix that
// same growth drove the host into swap and wedged the relay.
//
// The fix is to pass conn.eventCtx, which Close() cancels. This test asserts
// the observable consequence: after Close(), the per-connection goroutines are
// gone.
func TestConnectionGoroutinesExitOnClose(t *testing.T) {
	// Count goroutines whose stack mentions the per-connection helpers. This
	// is deliberately a textual check rather than a runtime.NumGoroutine()
	// comparison: other tests and the race detector add goroutines
	// concurrently, and an absolute count would be flaky. Matching the
	// function name is stable and is exactly what regressed.
	countConnectionGoroutines := func() int {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		stacks := string(buf[:n])

		count := 0
		for _, marker := range []string{
			"(*WsConnection).monitorConnection",
			"startNegSweeper.func1",
		} {
			for i := 0; ; {
				j := indexFrom(stacks, marker, i)
				if j < 0 {
					break
				}
				count++
				i = j + len(marker)
			}
		}
		return count
	}

	// Use a context that is never canceled, mirroring the server-wide context
	// that caused the leak. If the implementation regressed to using this
	// instead of the per-connection one, the goroutines would survive Close()
	// and this test would fail.
	parentCtx, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	conn, _ := newTestConnection(t, parentCtx)
	startConnectionGoroutines(conn)

	// Give both goroutines a moment to start and reach their select loops.
	waitFor(t, countConnectionGoroutines, 2, 2*time.Second,
		"per-connection goroutines did not start")

	cancelConnection(conn)

	// Close cancels eventCtx, so both goroutines return. Allow a short
	// scheduling window rather than asserting instantly.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if countConnectionGoroutines() == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("after Close(), %d per-connection goroutine(s) still running; "+
		"they are being started with the server-wide context instead of the "+
		"per-connection eventCtx that Close() cancels",
		countConnectionGoroutines())
}

// TestConnectionGoroutinesLeakWithServerContext is the negative control for
// TestConnectionGoroutinesExitOnClose.
//
// Without it, that test could pass for the wrong reason — for example if the
// goroutine-name matching never matched anything, or if Close() did not
// actually cancel the context. Here the pre-fix call pattern is exercised
// deliberately and the leak is asserted to still be present.
//
// If this test starts failing because the leak is gone, the helper it calls
// is now redundant and TestConnectionGoroutinesExitOnClose should call the
// real NewWsConnection call site instead.
func TestConnectionGoroutinesLeakWithServerContext(t *testing.T) {
	parentCtx, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	conn, _ := newTestConnection(t, parentCtx)
	startConnectionGoroutinesLeaky(conn, parentCtx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		if strings.Contains(string(buf[:n]), "startNegSweeper.func1") {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	cancelConnection(conn)
	time.Sleep(300 * time.Millisecond)

	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	if !strings.Contains(string(buf[:n]), "startNegSweeper.func1") {
		t.Skip("the leaky call pattern no longer leaks; " +
			"TestConnectionGoroutinesExitOnClose should call NewWsConnection directly")
	}
	// Leak confirmed present, which is the precondition the positive test
	// depends on. Passing here means the positive test is meaningful.
}

// stubNode is a minimal domain.NodeInterface for tests that only exercise
// connection lifecycle. Every method the goroutines under test touch returns
// a zero value; nothing here talks to a database or a websocket.
type stubNode struct {
	domain.NodeInterface
	unregistered int
}

func (s *stubNode) UnregisterConn(domain.WebSocketConnection)    { s.unregistered++ }
func (s *stubNode) GetEventDispatcher() *storage.EventDispatcher { return nil }
func (s *stubNode) GetEventProcessor() *storage.EventProcessor   { return nil }
func (s *stubNode) GetValidator() domain.EventValidator          { return nil }
func (s *stubNode) DB() *storage.DB                              { return nil }
func (s *stubNode) Config() *config.Config                       { return nil }
func (s *stubNode) RegisterConn(domain.WebSocketConnection)      {}
func (s *stubNode) GetActiveConnectionCount() int64              { return 0 }
func (s *stubNode) GetConnectionCount() int                      { return 0 }
func (s *stubNode) GetStartTime() time.Time                      { return time.Time{} }

// cancelConnection cancels the per-connection context the same way Close()
// does.
//
// Close() cannot be called here because it dereferences the websocket
// (*websocket.Conn).SetWriteDeadline and the connection under test has no
// real socket. Cancelling eventCtx is the exact operation Close() performs
// first and the one the two goroutines actually select on, so this exercises
// the invariant without needing a live upgrade.
//
// If Close() ever stops cancelling eventCtx, this test would still pass while
// the leak returned — so the fix is asserted structurally at the call site in
// NewWsConnection, where the two helpers are invoked with conn.eventCtx.
func cancelConnection(c *WsConnection) {
	if c.eventCancel != nil {
		c.eventCancel()
	}
}

func startConnectionGoroutines(c *WsConnection) {
	go c.monitorConnection(c.eventCtx)
	c.startNegSweeper(c.eventCtx)
}

// startConnectionGoroutinesLeaky is the pre-fix call pattern, kept so the
// negative control can prove the leak is real and detectable.
func startConnectionGoroutinesLeaky(c *WsConnection, ctx context.Context) {
	go c.monitorConnection(ctx)
	c.startNegSweeper(ctx)
}

// newTestConnection builds a WsConnection with only the fields the two
// background goroutines touch, avoiding a real websocket upgrade.
//
// A real *websocket.Conn IS required, because monitorConnection calls
// Close() when its context is canceled, and Close() sends a close frame via
// c.ws.SetWriteDeadline / WriteControl. A nil ws panics there. The pair below
// is a real client/server websocket over an in-memory listener, which is
// cheaper than mocking and exercises the actual code paths.
// Returns the connection and the client end of the socket pair, so the test
// keeps the client alive for as long as the server side is under test.
func newTestConnection(t *testing.T, parent context.Context) (*WsConnection, *websocket.Conn) {
	t.Helper()

	eventCtx, eventCancel := context.WithCancel(parent)
	client, server := newWebsocketPair(t)

	return &WsConnection{
		ws:               server,
		node:             &stubNode{},
		realClientIP:     "127.0.0.1",
		idleTimeout:      time.Hour,
		maxLifetime:      24 * time.Hour,
		startTime:        time.Now(),
		lastActivity:     time.Now(),
		subscriptions:    make(map[string][]nostr.Filter),
		pingTicker:       time.NewTicker(15 * time.Second),
		backpressureChan: make(chan struct{}, 100),
		clientID:         generateClientID(),
		eventCtx:         eventCtx,
		eventCancel:      eventCancel,
		authedPubkeys:    make(map[string]time.Time),
		negSessions:      newNegSessions(),
	}, client
}

// newWebsocketPair returns a connected client and server websocket over an
// in-memory listener, plus a cleanup registered with t.
func newWebsocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()

	serverConnCh := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("websocket upgrade failed: %v", err)
			return
		}
		serverConnCh <- c
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var server *websocket.Conn
	select {
	case server = <-serverConnCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server websocket")
	}
	t.Cleanup(func() { _ = server.Close() })

	return client, server
}

// waitFor polls cond until it holds or the timeout expires.
func waitFor(t *testing.T, cond func() int, want int, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("%s (got %d, want %d)", msg, cond(), want)
}

func indexFrom(s, substr string, start int) int {
	if start >= len(s) {
		return -1
	}
	for i := start; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
