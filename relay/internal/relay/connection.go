package relay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Shugur-Network/relay/internal/config"
	"github.com/Shugur-Network/relay/internal/domain"
	"github.com/Shugur-Network/relay/internal/errors"
	"github.com/Shugur-Network/relay/internal/logger"
	"github.com/Shugur-Network/relay/internal/metrics"
	"github.com/Shugur-Network/relay/internal/relay/nips"
	"github.com/gorilla/websocket"
	nostr "github.com/nbd-wtf/go-nostr"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

var (
	// bans tracks banned clients and their violation history. Violations are
	// retained across connections so PROGRESSIVE_BAN can escalate; see ban.go
	// for why the previous per-connection reset defeated the ladder entirely.
	bans = newBanRegistry()
)

// extractRealClientIP extracts the real client IP. Forwarded headers
// (X-Real-IP, X-Forwarded-For) are only honored when the request originates
// from a CIDR listed in trustedProxies. Otherwise the value is taken from
// the socket peer so a client cannot spoof its source IP by sending its own
// headers.
//
// trustedProxies is a slice of CIDR strings (e.g. "127.0.0.0/8", "::1/128").
// An empty slice disables forwarded-header trust entirely.
func extractRealClientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	remoteIP := normalizeIP(r.RemoteAddr)
	if remoteIP == "" {
		// Could not parse RemoteAddr; fall back to whatever the header says.
		return strings.TrimSpace(r.Header.Get("X-Real-IP"))
	}

	// Only consult forwarded headers if the direct peer is trusted.
	if !ipMatchesAnyCIDR(remoteIP, trustedProxies) {
		logger.Debug("Client peer not in TrustedProxies; ignoring forwarded headers",
			zap.String("client_ip", remoteIP),
			zap.String("x_real_ip", r.Header.Get("X-Real-IP")),
			zap.String("x_forwarded_for", r.Header.Get("X-Forwarded-For")))
		return remoteIP
	}

	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}

	if forwardedFor := r.Header.Get("X-Forwarded-For"); forwardedFor != "" {
		parts := strings.Split(forwardedFor, ",")
		if len(parts) > 0 {
			extractedIP := strings.TrimSpace(parts[0])
			logger.Debug("Client IP extracted from X-Forwarded-For",
				zap.String("forwarded_ip", extractedIP),
				zap.String("full_header", forwardedFor),
				zap.String("raw_remote_addr", r.RemoteAddr))
			return extractedIP
		}
	}

	return remoteIP
}

// ipMatchesAnyCIDR returns true if ip parses and falls within any of the
// provided CIDR networks. An empty/nil trustedProxies slice yields false.
func ipMatchesAnyCIDR(ip string, cidrs []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, cidr := range cidrs {
		if cidr == nil {
			continue
		}
		if cidr.Contains(parsed) {
			return true
		}
	}
	return false
}

// normalizeIP converts a network address to a normalized IP string
func normalizeIP(addr string) string {
	// Extract the IP portion (remove port)
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// If splitting fails, assume addr is already an IP
		host = addr
	}

	// Normalize IPv4-mapped IPv6 addresses
	ip := net.ParseIP(host)
	if ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			return ipv4.String()
		}
		return ip.String()
	}

	return host
}

// parseTrustedProxies parses a slice of strings ("1.2.3.0/24", "::1/128")
// into a slice of *net.IPNet. Invalid entries are silently dropped after a
// warning so a bad config does not brick the relay at startup.
func parseTrustedProxies(raw []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		_, cidr, err := net.ParseCIDR(s)
		if err != nil {
			logger.Warn("Ignoring invalid TrustedProxies CIDR",
				zap.String("entry", s), zap.Error(err))
			continue
		}
		out = append(out, cidr)
	}
	return out
}

// generateClientID generates a unique client ID for event dispatcher.
// Uses 16 random bytes (128 bits) to keep the birthday-collision probability
// negligibly small at any realistic connection count.
func generateClientID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID if random generation fails
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// cleanExpiredBans periodically removes expired bans from the ban list
func cleanExpiredBans() {
	for {
		time.Sleep(10 * time.Minute)

		unbanned := bans.sweepExpired()
		active, tracked := bans.stats()

		if unbanned > 0 || active > 0 {
			logger.Debug("Ban list cleanup completed",
				zap.Int("unbanned_count", unbanned),
				zap.Int("active_bans", active),
				zap.Int("tracked_clients", tracked))
		}
	}
}

// handleWebSocketConnection handles the upgrade of an HTTP connection to WebSocket
func handleWebSocketConnection(ctx context.Context, w http.ResponseWriter, r *http.Request, upgrader websocket.Upgrader, node domain.NodeInterface, relayConfig config.RelayConfig) {
	clientIP := extractRealClientIP(r, parseTrustedProxies(relayConfig.TrustedProxies))

	logger.Debug("New WebSocket connection attempt",
		zap.String("client_ip", clientIP),
		zap.String("user_agent", r.Header.Get("User-Agent")),
		zap.String("origin", r.Header.Get("Origin")))

	// Check if client is banned
	banExpiry, banned := bans.isBanned(clientIP)

	if banned {
		// Use new error handling system
		banErr := errors.ClientBannedError("excessive messages", time.Until(banExpiry).String()).
			WithSeverity(errors.SeverityMedium)
		errors.HandleHTTPError(w, r, banErr)
		return
	}

	// NOTE: the violation count is deliberately NOT reset here.
	//
	// This used to delete clientExceededCount[clientIP] on every accepted
	// connection. That made the ban ladder unescapable-but-useless: a client
	// tripped the limiter, disconnected, reconnected with the count back at
	// zero, and could only ever be punished as a first offender. The count is
	// now cleared only by bans.resetViolations, and a well-behaved client's
	// history decays rather than being wiped by a reconnect.

	// Check global connection limit using metrics counter
	if metrics.GetActiveConnectionsCount() >= int64(relayConfig.ThrottlingConfig.MaxConnections) {
		// Use new error handling system
		limitErr := errors.ConnectionLimitError(
			int(metrics.GetActiveConnectionsCount()),
			relayConfig.ThrottlingConfig.MaxConnections).
			WithSeverity(errors.SeverityMedium)
		errors.HandleHTTPError(w, r, limitErr)
		return
	}
	// Track whether we successfully registered the connection in metrics.
	// The defer decrements only if the increment happened, so failed
	// WebSocket upgrades (e.g. bad Upgrade header, ALPN mismatch, client
	// disconnect mid-handshake) do not move the counter. Previously this
	// defer was registered BEFORE the increment and decremented an
	// already-zero counter on upgrade failure, drifting it negative
	// (issue #99).
	connectionAccepted := false
	defer func() {
		if connectionAccepted {
			metrics.DecrementActiveConnections()
		}
	}()

	// Upgrade the connection
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Use new error handling system
		upgradeErr := errors.WebSocketError("connection upgrade", err).
			WithSeverity(errors.SeverityMedium)
		errors.HandleWebSocketError(wsConn, "upgrade", upgradeErr)
		return
	}

	// Enable compression
	wsConn.EnableWriteCompression(true)
	_ = wsConn.SetCompressionLevel(2) // nolint:errcheck // compression level is non-critical

	// Update metrics
	metrics.IncrementActiveConnections()
	connectionAccepted = true

	// Create new connection and register it
	conn, err := NewWsConnection(ctx, wsConn, node, relayConfig, clientIP)
	if err != nil {
		// Challenge generation failure is fatal: an unauthenticated-capable
		// connection is unsafe (see issue #51). The connectionAccepted
		// flag is true, so the defer will decrement the metric we just
		// incremented (issue #99).
		initErr := errors.WebSocketError("auth challenge init", err).
			WithSeverity(errors.SeverityHigh)
		errors.HandleWebSocketError(wsConn, "init", initErr)
		return
	}
	node.RegisterConn(conn)

	logger.Debug("WebSocket connection established successfully",
		zap.String("client_ip", clientIP),
		zap.Int64("active_connections", metrics.GetActiveConnectionsCount()))

	// Handle messages in a goroutine
	go conn.HandleMessages(ctx, relayConfig)
}

// WsConnection represents a single WebSocket client connection
type WsConnection struct {
	ws           *websocket.Conn
	node         domain.NodeInterface
	realClientIP string // Real client IP (extracted from proxy headers)
	lastActivity time.Time
	idleTimeout  time.Duration
	maxLifetime  time.Duration // Maximum lifetime of a connection
	startTime    time.Time     // When the connection was established

	pingTicker *time.Ticker

	subMu         sync.RWMutex
	subscriptions map[string][]nostr.Filter

	writeMu        sync.Mutex
	closeMu        sync.Once
	limiter        *rate.Limiter
	requestLimiter *rate.Limiter
	isClosed       atomic.Bool
	// metricsDecremented guards the one-shot metrics teardown in Close so a
	// double Close cannot decrement the connection and subscription gauges
	// twice.
	metricsDecremented atomic.Bool
	closeReason        string

	exceededLimitCount int
	backpressureChan   chan struct{} // Channel for backpressure handling

	// Event dispatcher integration
	clientID    string
	eventChan   chan *nostr.Event
	eventCtx    context.Context
	eventCancel context.CancelFunc

	// NIP-42 AUTH
	authChallenge string
	// authedPubkeys maps authenticated pubkey -> expiry timestamp.
	// AUTHs expire after AuthTTL to limit the lifetime of a stolen session.
	authedPubkeys map[string]time.Time
	authMu        sync.RWMutex
	relayURL      string

	// NIP-77 Negentropy Syncing
	negSessions *negSessions
}

// AuthTTL is how long a successful NIP-42 AUTH remains valid on a connection.
// Default: 10 minutes. The connection itself is capped at maxLifetime (24h).
const AuthTTL = 10 * time.Minute

// Ensure WsConnection implements domain.WebSocketConnection
var _ domain.WebSocketConnection = (*WsConnection)(nil)

// NewWsConnection initializes a new WebSocket connection.
// Returns an error if the NIP-42 AUTH challenge cannot be generated; callers
// must reject the upgrade in that case to prevent unprotected operations
// on the connection.
func NewWsConnection(
	ctx context.Context,
	ws *websocket.Conn,
	node domain.NodeInterface,
	cfg config.RelayConfig,
	realClientIP string,
) (*WsConnection, error) {
	// Basic rate limiter
	limiter := rate.NewLimiter(
		rate.Limit(cfg.ThrottlingConfig.RateLimit.MaxEventsPerSecond),
		cfg.ThrottlingConfig.RateLimit.BurstSize,
	)

	// Separate limiter for the read-side commands.
	//
	// Deliberately NOT the same limiter as `limiter`. That one is sized for
	// events (MAX_EVENTS_PER_SECOND, default 30) and a client legitimately
	// publishing 30 events/second would then have no budget left to read
	// with, so sharing the bucket would rate-limit honest publishers on their
	// own writes. Two buckets also stop a REQ flood from starving EVENT
	// delivery, and vice versa.
	//
	// This exists because MAX_REQUESTS_PER_SECOND was configured (60 in
	// deploy/config.yaml) and validated but never read anywhere in the
	// codebase, so an operator tuning it saw no behaviour change and no
	// warning. Same class of defect as the goroutine leak fixed in 68b5b7a: a
	// limit that is written but not threaded through to the operation it is
	// meant to bound.
	//
	// Without it a single socket could send unlimited REQ commands. Each one
	// spawns processSubscription -- a goroutine running a database query --
	// and MaxSubscriptions only caps *concurrently registered* subs per
	// connection, not the rate of new ones and not the goroutines already
	// querying. That is a one-connection DoS against both the relay and
	// PostgreSQL.
	//
	// rate.Limit of 0 means "unlimited" to x/time/rate, which is the correct
	// reading of MAX_REQUESTS_PER_SECOND: 0 disables the limit. The burst is
	// floored at 1, because a 0 burst with an empty bucket rejects everything.
	reqBurst := cfg.ThrottlingConfig.RateLimit.BurstSize
	if reqBurst < 1 {
		reqBurst = 1
	}
	requestLimiter := rate.NewLimiter(
		rate.Limit(cfg.ThrottlingConfig.RateLimit.MaxRequestsPerSecond),
		reqBurst,
	)

	// Create context for event handling
	eventCtx, eventCancel := context.WithCancel(ctx)

	conn := &WsConnection{
		ws:               ws,
		node:             node,
		realClientIP:     realClientIP,
		idleTimeout:      cfg.IdleTimeout,
		maxLifetime:      24 * time.Hour, // Maximum connection lifetime
		startTime:        time.Now(),
		lastActivity:     time.Now(),
		subscriptions:    make(map[string][]nostr.Filter),
		pingTicker:       time.NewTicker(15 * time.Second),
		limiter:          limiter,
		requestLimiter:   requestLimiter,
		backpressureChan: make(chan struct{}, 100), // Buffer for backpressure
		// Event dispatcher integration
		clientID:    generateClientID(),
		eventCtx:    eventCtx,
		eventCancel: eventCancel,
		// NIP-42 AUTH
		authedPubkeys: make(map[string]time.Time),
		negSessions:   newNegSessions(),
		relayURL:      cfg.PublicURL,
	}

	// Generate NIP-42 auth challenge. If this fails, fail closed: do not
	// hand back a connection that can perform protected operations.
	challenge, err := nips.GenerateAuthChallenge()
	if err != nil {
		return nil, fmt.Errorf("failed to generate NIP-42 auth challenge: %w", err)
	}
	conn.authChallenge = challenge

	// Note: dispatcher subscription is now lazy. The connection only
	// registers with the event dispatcher once it has at least one
	// subscription (issue #96). See subscribeDispatcher and
	// unsubscribeDispatcher.

	// WebSocket compression
	ws.EnableWriteCompression(true)
	_ = ws.SetCompressionLevel(2) // nolint:errcheck // compression level is non-critical

	// Deadlines + read limit
	_ = ws.SetReadDeadline(time.Now().Add(60 * time.Second)) // nolint:errcheck // deadline is non-critical

	// Set WebSocket read limit based on configured content length with buffer for JSON overhead
	readLimitBytes := int64(cfg.ThrottlingConfig.MaxContentLen * 2) // 2x buffer for JSON overhead
	if readLimitBytes < 1024*1024 {                                 // Minimum 1MB
		readLimitBytes = 1024 * 1024
	}
	if readLimitBytes > 32*1024*1024 { // Maximum 32MB
		readLimitBytes = 32 * 1024 * 1024
	}
	ws.SetReadLimit(readLimitBytes)

	// Ping handler - must echo back the same data
	ws.SetPingHandler(func(appData string) error {
		conn.lastActivity = time.Now()
		conn.writeMu.Lock()
		defer conn.writeMu.Unlock()
		// Echo back the same ping data in the pong response
		_ = conn.ws.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(5*time.Second))
		return nil
	})

	// Start monitoring.
	//
	// Uses conn.eventCtx for the same reason as the negentropy sweeper below:
	// ctx is the server-wide context, canceled only at process shutdown, so
	// monitoring every connection with it left one goroutine alive per closed
	// connection.
	go conn.monitorConnection(conn.eventCtx)

	// Start negentropy idle-session sweeper.
	//
	// Must be given eventCtx, the per-connection context, not ctx. ctx here
	// is the server-wide context threaded down from ListenAndServe, so it is
	// only canceled at process shutdown. Passing it meant one sweeper
	// goroutine outlived every closed WebSocket: measured on the production
	// host 2026-10-02, 55 of 148 live goroutines were
	// startNegSweeper.func1 parked on a 30s ticker, and the count grew with
	// every connection the relay had ever accepted.
	//
	// That is the leak behind the RSS climb from 64MB to 124MB over ~3.6
	// hours on a single process (watch.jsonl). Each abandoned goroutine also
	// pins its WsConnection, so the websocket, its buffers and its
	// subscription map are all kept alive well past Close().
	conn.startNegSweeper(conn.eventCtx)

	return conn, nil
}

// RemoteAddr returns the client's real remote address (extracted from proxy headers)
func (c *WsConnection) RemoteAddr() string {
	return c.realClientIP
}

// SendMessage handles backpressure and rate limiting
func (c *WsConnection) SendMessage(msg []byte) {
	c.sendMessageInternal(msg, true)
}

// requestLimiterApplies reports whether an inbound command should be charged
// against the read-side request limiter (MAX_REQUESTS_PER_SECOND).
//
// The split is by cost, not by verb:
//
//   - REQ, COUNT, NEG-OPEN, NEG-MSG each spawn a goroutine, a database query,
//     or negentropy reconciliation. They are the abuse vector and are limited.
//   - CLOSE, AUTH and NEG-CLOSE are not. Rejecting CLOSE would strand a
//     subscription; rejecting AUTH would stop a client from ever becoming
//     authenticated; rejecting NEG-CLOSE would leak a negentropy session. All
//     three are cheap, and denying them does more harm than permitting them.
//   - EVENT is excluded here because it is charged to c.limiter, which is sized
//     by MAX_EVENTS_PER_SECOND and escalates to a ban. Charging it twice would
//     make an honest publisher's reads fail because of its own writes.
//
// Kept as a named function rather than an inline switch so the policy is
// testable and so a newly added command does not silently default to unlimited.
func requestLimiterApplies(cmdType string) bool {
	switch cmdType {
	case "REQ", "COUNT", "NEG-OPEN", "NEG-MSG":
		return true
	default:
		return false
	}
}

// SendMessageNoRateLimit sends a message without rate limiting (for subscription responses)
func (c *WsConnection) SendMessageNoRateLimit(msg []byte) {
	c.sendMessageInternal(msg, false)
}

// sendMessageInternal handles the actual message sending with optional rate limiting
func (c *WsConnection) sendMessageInternal(msg []byte, applyRateLimit bool) {
	if c.isClosed.Load() {
		return
	}

	// Check backpressure
	select {
	case c.backpressureChan <- struct{}{}:
		defer func() { <-c.backpressureChan }()
	default:
		// Backpressure is too high, close connection.
		//
		// This used to close silently -- no log, no closeReason -- so a
		// connection dropped here was indistinguishable from a network
		// fault. Diagnosing that on the live host meant an empty journal and
		// a broken pipe in the client with no server-side explanation.
		// Backpressure is the *expected* outcome when a client stops reading
		// during a burst, so it is a normal event worth recording, not an
		// error, but it must be visible.
		c.closeReason = "backpressure limit exceeded"
		logger.Warn("Closing connection: outbound backpressure limit exceeded",
			zap.Int("limit", cap(c.backpressureChan)),
			zap.String("client", c.RemoteAddr()),
			zap.String("real_client_ip", c.realClientIP))
		metrics.IncrementErrorCount()
		c.Close()
		return
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.isClosed.Load() {
		return
	}

	// Apply rate limiting only if requested.
	//
	// Reaching the limit is not fatal on its own: a client that cannot keep up
	// simply gets fewer messages and backfills on its next REQ. Only a
	// sustained run of rejections means the connection is not draining at all.
	if applyRateLimit && !c.limiter.Allow() {
		c.exceededLimitCount++
		if c.exceededLimitCount > 5 {
			// Previously closed silently, leaving an empty journal and an
			// unexplained broken pipe on the client.
			c.closeReason = "outbound rate limit exceeded repeatedly"
			logger.Warn("Closing connection: outbound rate limit exceeded repeatedly",
				zap.Int("consecutive_rejections", c.exceededLimitCount),
				zap.String("client", c.RemoteAddr()),
				zap.String("real_client_ip", c.realClientIP))
			metrics.IncrementErrorCount()
			c.Close()
		}
		return
	}

	// Reset exceeded count on successful send
	c.exceededLimitCount = 0

	// Set write deadline
	_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second)) // nolint:errcheck // deadline is non-critical
	if err := c.ws.WriteMessage(websocket.TextMessage, msg); err != nil {
		logger.Error("Failed to write message", zap.Error(err))
		metrics.IncrementErrorCount()
		c.Close()
	}

	// Update metrics
	metrics.IncrementMessagesSent()
	metrics.MessageSizeBytesSent.Observe(float64(len(msg)))
}

// sendMessage marshals a top-level array like ["NOTICE", "xyz"] or ["CLOSED", subID, reason].
func (c *WsConnection) sendMessage(msgType string, args ...interface{}) {
	data := append([]interface{}{msgType}, args...)
	raw, err := json.Marshal(data)
	if err != nil {
		logger.Warn("Failed to marshal message", zap.Error(err))
		return
	}

	// Bypass rate limiting for EVENT and COUNT responses (subscription data)
	if msgType == "EVENT" || msgType == "COUNT" {
		c.SendMessageNoRateLimit(raw)
	} else {
		c.SendMessage(raw)
	}
}

// sendNotice is a convenience for sending ["NOTICE", <message>].
func (c *WsConnection) sendNotice(message string) {
	c.sendMessage("NOTICE", message)
}

// sendClosed is a convenience for sending ["CLOSED", <subID>, <reason>].
func (c *WsConnection) sendClosed(subID, reason string) {
	c.sendMessage("CLOSED", subID, reason)
}

// sendOK sends an OK response for an event with status and message
func (c *WsConnection) sendOK(eventID string, accepted bool, message string) {
	msg := []interface{}{"OK", eventID, accepted, message}
	data, _ := json.Marshal(msg)
	c.SendMessage(data)
}

// sendEOSE sends an EOSE (End of Stored Events) message
// Per NIP-67, may include a third element with hint strings:
//   - ["finish"]: relay has sent every stored event matching the subscription's filters
//   - ["more"]: relay holds more matching stored events than it has sent
//   - [] or omitted: legacy behavior
func (c *WsConnection) sendEOSE(subID string, hints ...string) {
	if len(hints) > 0 {
		// NIP-67 EOSE with hints
		msg := []interface{}{"EOSE", subID, hints}
		data, _ := json.Marshal(msg)
		c.SendMessage(data)
	} else {
		c.sendMessage("EOSE", subID)
	}
}

// HandleMessages processes incoming messages from the client
func (c *WsConnection) HandleMessages(ctx context.Context, cfg config.RelayConfig) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Recovered from panic in HandleMessages",
				zap.Any("panic", r),
				zap.String("client", c.RemoteAddr()),
			)
		}
		// Always ensure connection is properly closed and unregistered
		c.closeReason = "message handler terminated"
		c.Close()
		c.node.UnregisterConn(c)
	}()

	clientIP := c.realClientIP

	logger.Debug("Starting message handler",
		zap.String("real_client_ip", clientIP),
		zap.String("websocket_remote_addr", c.ws.RemoteAddr().String()),
		zap.String("client_id", c.clientID))

	// Send NIP-42 AUTH challenge
	if c.authChallenge != "" {
		authMsg, _ := json.Marshal([]interface{}{"AUTH", c.authChallenge})
		c.SendMessage(authMsg)
		logger.Debug("Sent NIP-42 AUTH challenge",
			zap.String("client", c.RemoteAddr()),
			zap.String("challenge", c.authChallenge[:16]+"..."))
	}

	// Check if client is banned.
	//
	// Re-checked here, not only at handshake time: a client can be banned by
	// traffic on a *different* connection while this socket is already open,
	// and without this a banned client keeps its established connection.
	banExpiry, banned := bans.isBanned(clientIP)

	if banned {
		logger.Warn("Banned client attempted to send messages",
			zap.String("client_ip", clientIP),
			zap.Time("ban_expires", banExpiry))
		c.closeReason = "client banned"
		c.sendNotice("You are temporarily banned due to excessive messages.")
		c.Close()
		return
	}

	// Set WebSocket read limit based on configured content length with buffer for JSON overhead
	readLimitBytes := int64(cfg.ThrottlingConfig.MaxContentLen * 2) // 2x buffer for JSON overhead
	if readLimitBytes < 1024*1024 {                                 // Minimum 1MB
		readLimitBytes = 1024 * 1024
	}
	if readLimitBytes > 32*1024*1024 { // Maximum 32MB
		readLimitBytes = 32 * 1024 * 1024
	}
	c.ws.SetReadLimit(readLimitBytes)

	lastPong := time.Now()
	c.ws.SetPongHandler(func(string) error {
		c.lastActivity = time.Now()
		lastPong = time.Now()
		return nil
	})

	connCtx, cancel := context.WithTimeout(ctx, 24*time.Hour)
	defer cancel()

	for {
		select {
		case <-connCtx.Done():
			c.closeReason = "connection context canceled"
			return
		default:
			// Keep going
		}

		_ = c.ws.SetReadDeadline(time.Now().Add(60 * time.Second)) // nolint:errcheck // deadline is non-critical
		if time.Since(lastPong) > 90*time.Second {
			logger.Debug("No pong response in 90s, closing connection",
				zap.String("client", c.RemoteAddr()))
			c.closeReason = "no pong response"
			return
		}

		// Read message
		_, rawMsg, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				c.closeReason = "client closed connection"
				logger.Debug("Client closed connection normally",
					zap.String("client", c.RemoteAddr()))
			} else {
				c.closeReason = "read error"
				logger.Debug("WS read error, disconnecting client",
					zap.Error(err),
					zap.String("client", c.RemoteAddr()))
			}
			return
		}

		// Update metrics
		metrics.IncrementMessagesProcessed() // This handles both counter and local tracking
		messageSize := float64(len(rawMsg))
		metrics.MessageSizeBytes.Observe(messageSize)

		_ = c.ws.SetReadDeadline(time.Time{}) // nolint:errcheck // deadline reset is non-critical
		c.lastActivity = time.Now()

		var arr []interface{}
		if err := json.Unmarshal(rawMsg, &arr); err != nil {
			c.sendNotice("invalid: malformed JSON from client")
			continue
		}
		if len(arr) == 0 {
			c.sendNotice("invalid: empty command array")
			continue
		}

		cmdType, ok := arr[0].(string)
		if !ok {
			c.sendNotice("invalid: command must be a string")
			continue
		}

		if cmdType == "EVENT" {
			if !c.limiter.Allow() {
				// Track repeated violations
				count := bans.recordViolation(clientIP)

				logger.Debug("Client rate limit violation",
					zap.String("client_ip", clientIP),
					zap.Int("violation_count", count),
					zap.Int("ban_threshold", cfg.ThrottlingConfig.BanThreshold),
					zap.String("real_client_ip", c.realClientIP),
					zap.String("websocket_remote_addr", c.ws.RemoteAddr().String()))

				c.sendNotice("Rate limit exceeded: too many messages")

				if count >= cfg.ThrottlingConfig.BanThreshold {
					// Progressive banning: each threshold crossing doubles the
					// duration, capped at MAX_BAN_DURATION. This used to be a
					// flat ThrottlingConfig.BanDuration, so PROGRESSIVE_BAN and
					// MAX_BAN_DURATION were configured in deploy/config.yaml and
					// read nowhere in the codebase.
					//
					// The violation count is deliberately NOT cleared on ban.
					// Clearing it here would make every ban a first offence, so
					// the escalation could never reach its second rung. It decays
					// via bans.resetViolations once a client behaves.
					banDuration := banDurationFor(
						count,
						time.Duration(cfg.ThrottlingConfig.BanDuration)*time.Second,
						cfg.ThrottlingConfig.RateLimit.MaxBanDuration,
						cfg.ThrottlingConfig.RateLimit.ProgressiveBan,
					)
					banExpires := bans.ban(clientIP, banDuration)

					logger.Warn("BANNING CLIENT due to repeated rate limit violations",
						zap.String("client_ip", clientIP),
						zap.Int("violation_count", count),
						zap.Bool("progressive", cfg.ThrottlingConfig.RateLimit.ProgressiveBan),
						zap.Duration("ban_duration", banDuration),
						zap.String("real_client_ip", c.realClientIP),
						zap.Time("ban_expires", banExpires))

					c.sendNotice("You have been temporarily banned.")
					c.Close()
					return
				}
				continue
			}
			// Reset exceeded count on successful message
			c.exceededLimitCount = 0
		}

		// Update command metrics
		metrics.CommandsReceived.WithLabelValues(cmdType).Inc()

		// Rate-limit the read-side commands. See requestLimiterApplies for
		// which commands and why.
		//
		// Rejection uses `continue` rather than closing the connection: NIP-01
		// defines no notice type for "too fast", and a client syncing a large
		// backlog will legitimately burst past a per-second limit. Dropping the
		// frame and letting the token bucket refill is the standard answer. The
		// EVENT path escalates to a ban instead, because publishing is the
		// expensive, abusable direction.
		if requestLimiterApplies(cmdType) && c.requestLimiter != nil {
			if !c.requestLimiter.Allow() {
				metrics.RateLimited.WithLabelValues(cmdType).Inc()
				logger.Debug("Client request rate limit exceeded",
					zap.String("client_ip", clientIP),
					zap.String("command", cmdType),
					zap.Int("limit_per_sec", cfg.ThrottlingConfig.RateLimit.MaxRequestsPerSecond),
					zap.String("real_client_ip", c.realClientIP))
				continue
			}
		}

		// Process the command
		start := time.Now()
		switch cmdType {
		case "EVENT":
			c.handleEvent(ctx, arr)
		case "REQ":
			c.handleRequest(ctx, arr)
		case "COUNT":
			c.handleCountRequest(ctx, arr)
		case "CLOSE":
			c.handleClose(arr)
		case "AUTH":
			c.handleAuth(arr)
		case "NEG-OPEN":
			c.handleNegOpen(ctx, arr)
		case "NEG-MSG":
			c.handleNegMsg(arr)
		case "NEG-CLOSE":
			c.handleNegClose(arr)
		default:
			c.sendNotice("invalid: unknown command '" + cmdType + "'")
		}
		metrics.CommandProcessingDuration.WithLabelValues(cmdType).Observe(time.Since(start).Seconds())
	}
}

// processDispatcherEvents handles real-time events from the event dispatcher
func (c *WsConnection) processDispatcherEvents() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Recovered from panic in processDispatcherEvents",
				zap.Any("panic", r),
				zap.String("client", c.RemoteAddr()))
		}
	}()
	if c.eventChan == nil {
		return
	}

	for {
		select {
		case <-c.eventCtx.Done():
			return
		case event := <-c.eventChan:
			if event == nil {
				return // Channel closed
			}

			// Check if connection is still active
			if c.isClosed.Load() {
				return
			}

			// Check if any subscription matches this event
			c.subMu.RLock()
			for subID, filters := range c.subscriptions {
				for _, filter := range filters {
					if c.eventMatchesFilter(event, filter) {
						// Send event to client
						c.sendMessage("EVENT", subID, event)
						logger.Debug("Sent real-time event to client",
							zap.String("sub_id", subID),
							zap.String("event_id", event.ID),
							zap.String("client", c.RemoteAddr()))
						break // Only send once per subscription
					}
				}
			}
			c.subMu.RUnlock()
		}
	}
}

// eventMatchesFilter checks if an event matches a subscription filter
func (c *WsConnection) eventMatchesFilter(event *nostr.Event, filter nostr.Filter) bool {
	// Check IDs
	if len(filter.IDs) > 0 {
		found := false
		for _, id := range filter.IDs {
			if event.ID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check authors
	if len(filter.Authors) > 0 {
		found := false
		for _, author := range filter.Authors {
			if event.PubKey == author {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check kinds
	if len(filter.Kinds) > 0 {
		found := false
		for _, kind := range filter.Kinds {
			if event.Kind == kind {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check since
	if filter.Since != nil && event.CreatedAt < *filter.Since {
		return false
	}

	// Check until
	if filter.Until != nil && event.CreatedAt > *filter.Until {
		return false
	}

	// Check tags
	for tagName, tagValues := range filter.Tags {
		if len(tagValues) > 0 {
			found := false
			for _, tag := range event.Tags {
				if len(tag) >= 2 && tag[0] == tagName {
					for _, value := range tagValues {
						if tag[1] == value {
							found = true
							break
						}
					}
					if found {
						break
					}
				}
			}
			if !found {
				return false
			}
		}
	}

	return true
}

// Close gracefully shuts down the WebSocket
func (c *WsConnection) Close() {
	c.closeMu.Do(func() {
		c.isClosed.Store(true)

		if c.closeReason != "" {
			// Info, not Debug.
			//
			// This logged at Debug, and production runs at Info, so *every* close
			// reason was invisible on the live host. That is why diagnosing a
			// client-side broken pipe produced an empty journal: the relay knew
			// why it had closed the socket and was not writing it down anywhere
			// the operator could see.
			//
			// Info is the right level because closeReason is set on the
			// abnormal paths -- backpressure, repeated rate limiting, banned
			// client, no pong, malformed traffic. Those are exactly the events an
			// operator is trying to explain after the fact. Normal client
			// disconnects set "client closed connection normally" and are also
			// logged, which is the one acceptable cost of this level.
			logger.Info("WebSocket connection closed",
				zap.String("client_ip", c.RemoteAddr()),
				zap.String("real_client_ip", c.realClientIP),
				zap.Duration("connection_duration", time.Since(c.startTime)))
		}

		// Stop event dispatcher processing
		if c.eventCancel != nil {
			c.eventCancel()
		}

		// Unregister from event dispatcher
		if eventDispatcher := c.node.GetEventDispatcher(); eventDispatcher != nil && c.clientID != "" {
			eventDispatcher.RemoveClient(c.clientID)
		}

		// Clear any subscriptions
		c.subMu.Lock()
		oldSubs := len(c.subscriptions)
		c.subscriptions = make(map[string][]nostr.Filter)
		c.subMu.Unlock()

		// Clean up NIP-77 negentropy sessions
		if c.negSessions != nil {
			c.negSessions.closeAll()
		}

		// Update metrics - only decrement once
		if !c.metricsDecremented.Swap(true) {
			metrics.ActiveSubscriptions.Sub(float64(oldSubs))
			metrics.DecrementActiveConnections()
		}

		if c.pingTicker != nil {
			c.pingTicker.Stop()
		}

		// Attempt a polite close
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		closeChan := make(chan struct{})
		go func() {
			msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, c.closeReason)
			c.writeMu.Lock()
			_ = c.ws.SetWriteDeadline(time.Now().Add(time.Second))
			_ = c.ws.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second))
			_ = c.ws.SetWriteDeadline(time.Time{})
			c.writeMu.Unlock()
			close(closeChan)
		}()

		select {
		case <-closeChan:
		case <-closeCtx.Done():
			logger.Debug("Close message timeout",
				zap.String("client", c.RemoteAddr()))
		}

		// Unregister
		c.node.UnregisterConn(c)

		// Finally close
		_ = c.ws.Close()
		logger.Debug("WebSocket connection cleanup completed",
			zap.String("client", c.RemoteAddr()))
	})
}

// monitorConnection handles connection timeouts and cleanup
func (c *WsConnection) monitorConnection(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Recovered from panic in monitorConnection",
				zap.Any("panic", r),
				zap.String("client", c.RemoteAddr()))
			c.closeReason = "monitor panic recovered"
			c.Close()
		}
	}()
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.Close()
			return
		case <-c.pingTicker.C:
			// Send ping to keep connection alive
			c.writeMu.Lock()
			if !c.isClosed.Load() {
				_ = c.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
				err := c.ws.WriteControl(websocket.PingMessage, []byte("keepalive"), time.Now().Add(5*time.Second))
				_ = c.ws.SetWriteDeadline(time.Time{})
				if err != nil {
					logger.Debug("Failed to send ping, closing connection",
						zap.Error(err),
						zap.String("client", c.RemoteAddr()))
					c.writeMu.Unlock()
					c.closeReason = "ping failed"
					c.Close()
					return
				}
				logger.Debug("Sent ping to client", zap.String("client", c.RemoteAddr()))
			}
			c.writeMu.Unlock()
		case <-ticker.C:
			now := time.Now()
			c.writeMu.Lock()

			// Check idle timeout
			if now.Sub(c.lastActivity) > c.idleTimeout {
				c.writeMu.Unlock()
				c.closeReason = "idle timeout"
				c.Close()
				return
			}

			// Check max lifetime
			if now.Sub(c.startTime) > c.maxLifetime {
				c.writeMu.Unlock()
				c.closeReason = "max lifetime exceeded"
				c.Close()
				return
			}

			// Check backpressure
			if len(c.backpressureChan) > 90 { // 90% of buffer capacity
				c.writeMu.Unlock()
				c.closeReason = "backpressure overflow"
				c.Close()
				return
			}

			c.writeMu.Unlock()
		}
	}
}

// Subscription management methods

// HasSubscription checks if a subscription exists
func (c *WsConnection) HasSubscription(subID string) bool {
	c.subMu.RLock()
	defer c.subMu.RUnlock()
	_, ok := c.subscriptions[subID]
	return ok
}

// subscribeDispatcher registers the connection with the event
// dispatcher and starts the read loop, if not already subscribed.
// Caller must hold c.subMu (write) OR have c.eventChan already
// guaranteed non-nil. The first subscription added to a connection
// triggers the subscribe; the last removed triggers the unsubscribe
// (issue #96).
func (c *WsConnection) subscribeDispatcher() {
	if c.eventChan != nil {
		return
	}
	ed := c.node.GetEventDispatcher()
	if ed == nil {
		return
	}
	c.eventChan = ed.AddClient(c.clientID)
	go c.processDispatcherEvents()
}

// unsubscribeDispatcher removes the connection from the dispatcher if
// it has no remaining subscriptions. Caller must hold c.subMu (write).
func (c *WsConnection) unsubscribeDispatcher() {
	if c.eventChan == nil {
		return
	}
	if ed := c.node.GetEventDispatcher(); ed != nil {
		ed.RemoveClient(c.clientID)
	}
	c.eventChan = nil
}

// AddSubscription adds a new subscription. The first subscription on
// a connection registers it with the event dispatcher.
func (c *WsConnection) AddSubscription(subID string, filters []nostr.Filter) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	first := len(c.subscriptions) == 0
	c.subscriptions[subID] = filters
	if first {
		c.subscribeDispatcher()
	}
	metrics.IncrementActiveSubscriptions()
}

// RemoveSubscription removes a subscription. The last subscription
// on a connection unregisters it from the event dispatcher.
func (c *WsConnection) RemoveSubscription(subID string) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	if _, exists := c.subscriptions[subID]; !exists {
		return
	}
	delete(c.subscriptions, subID)
	metrics.DecrementActiveSubscriptions()
	if len(c.subscriptions) == 0 {
		c.unsubscribeDispatcher()
	}
}

// handleEvent processes EVENT commands
func (c *WsConnection) handleEvent(ctx context.Context, arr []interface{}) {
	if len(arr) < 2 {
		c.sendNotice("Invalid event message: not enough elements")
		return
	}

	// Marshal the event data back to JSON
	eventData, err := json.Marshal(arr[1])
	if err != nil {
		c.sendNotice("Invalid event: " + err.Error())
		return
	}

	var evt nostr.Event
	if err := json.Unmarshal(eventData, &evt); err != nil {
		c.sendNotice("Invalid event: " + err.Error())
		return
	}

	// Use ValidateAndProcessEvent for comprehensive validation
	valid, msg, err := c.node.GetValidator().ValidateAndProcessEvent(ctx, evt)
	if err != nil {
		c.sendOK(evt.ID, false, "error: "+err.Error())
		return
	}
	if !valid {
		c.sendOK(evt.ID, false, msg)
		return
	}

	// NIP-70: Reject protected events unless the author is authenticated on this connection
	if nips.IsProtectedEvent(&evt) {
		if !c.isAuthenticated(evt.PubKey) {
			c.sendOK(evt.ID, false, "auth-required: this event may only be published by its author")
			return
		}
	}

	// NIP-29: Validate and process group events
	if IsGroupEvent(&evt) {
		gs := GetGroupStore()
		if gs != nil {
			ok, reason := gs.ValidateGroupEvent(&evt)
			if !ok {
				c.sendOK(evt.ID, false, "blocked: "+reason)
				return
			}
			// Process group state changes and get relay-generated events
			relayEvents := gs.ProcessGroupEvent(&evt)
			for _, relayEvt := range relayEvents {
				// Store relay-generated metadata events
				if relayEvt != nil {
					c.node.GetEventProcessor().QueueEvent(*relayEvt)
				}
			}
		}
	}

	// NIP-43: Validate and process relay access metadata events
	if IsNIP43Event(&evt) {
		ms := GetMembershipStore()
		accepted, msg, relayEvents := ms.HandleNIP43Event(&evt)
		if !accepted {
			c.sendOK(evt.ID, false, msg)
			return
		}
		// Store relay-generated events (membership list updates, add/remove)
		for _, relayEvt := range relayEvents {
			if relayEvt != nil {
				c.node.GetEventProcessor().QueueEvent(*relayEvt)
			}
		}
		if msg != "" {
			// Join/leave messages like "info: welcome!" or "duplicate: already a member"
			c.sendOK(evt.ID, true, msg)
			// Don't store join/leave requests themselves — only relay-generated events
			if evt.Kind == 28934 || evt.Kind == 28936 {
				return
			}
		}
	}

	// Queue the event for processing
	if ok := c.node.GetEventProcessor().QueueEvent(evt); !ok {
		c.sendOK(evt.ID, false, "server busy, try again")
		return
	}

	// Update metrics for successful event
	metrics.EventsProcessed.WithLabelValues(fmt.Sprintf("%d", evt.Kind)).Inc()

	// Send successful response
	c.sendOK(evt.ID, true, "")
}

// QueryEvents reads events from storage that match a given Nostr filter.
func (c *WsConnection) QueryEvents(ctx context.Context, f nostr.Filter) ([]nostr.Event, error) {
	logger.Debug("QueryEvents called with filter", zap.Any("filter", f))

	results, err := c.node.DB().GetEvents(ctx, f)
	if err != nil {
		logger.Error("Error retrieving events from storage", zap.Error(err))
		return nil, err
	}
	return results, nil
}

// handleAuth processes AUTH commands (NIP-42)
func (c *WsConnection) handleAuth(arr []interface{}) {
	if len(arr) < 2 {
		c.sendNotice("invalid: AUTH message requires an event")
		return
	}

	// Marshal the event data back to JSON
	eventData, err := json.Marshal(arr[1])
	if err != nil {
		c.sendNotice("invalid: malformed AUTH event")
		return
	}

	var evt nostr.Event
	if err := json.Unmarshal(eventData, &evt); err != nil {
		c.sendNotice("invalid: malformed AUTH event: " + err.Error())
		return
	}

	if c.authChallenge == "" {
		c.sendOK(evt.ID, false, "error: no auth challenge was issued")
		return
	}

	// Validate the AUTH event using NIP-42
	pubkey, ok := nips.ValidateAuthEvent(&evt, c.authChallenge, c.relayURL)
	if !ok {
		c.sendOK(evt.ID, false, "error: auth event validation failed")
		return
	}

	// Mark this pubkey as authenticated on this connection with a TTL.
	// After AuthTTL the AUTH record is considered expired and the client
	// must re-authenticate.
	c.authMu.Lock()
	c.authedPubkeys[pubkey] = time.Now().Add(AuthTTL)
	c.authMu.Unlock()

	logger.Info("NIP-42: Client authenticated successfully",
		zap.String("pubkey", pubkey),
		zap.String("client", c.RemoteAddr()))

	c.sendOK(evt.ID, true, "")
}

// isAuthenticated checks if a pubkey has been authenticated on this connection
// via NIP-42, AND that the AUTH record has not yet expired.
func (c *WsConnection) isAuthenticated(pubkey string) bool {
	c.authMu.RLock()
	expiry, ok := c.authedPubkeys[pubkey]
	c.authMu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		// Expired: clean up so the map does not grow without bound.
		c.authMu.Lock()
		delete(c.authedPubkeys, pubkey)
		c.authMu.Unlock()
		return false
	}
	return true
}

// hasAuthentication checks if any pubkey has been authenticated on this connection
// (and not yet expired).
func (c *WsConnection) hasAuthentication() bool {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	now := time.Now()
	for _, expiry := range c.authedPubkeys {
		if now.Before(expiry) {
			return true
		}
	}
	return false
}

// getAuthenticatedPubkey returns the first authenticated pubkey on this connection, or empty string
// getAuthenticatedPubkey returns the first authenticated pubkey on this connection
// (whose AUTH record has not expired), or empty string.
func (c *WsConnection) getAuthenticatedPubkey() string {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	now := time.Now()
	for pk, expiry := range c.authedPubkeys {
		if now.Before(expiry) {
			return pk
		}
	}
	return ""
}
