package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Shugur-Network/relay/internal/logger"
	"github.com/Shugur-Network/relay/internal/metrics"
	nostr "github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip77/negentropy"
	"github.com/nbd-wtf/go-nostr/nip77/negentropy/storage/vector"
	"go.uber.org/zap"
)

const (
	maxNegSessions       = 5              // Max concurrent negentropy sessions per connection
	maxNegRecords        = 500000         // Max records to process per session
	negSessionTimeout    = 2 * time.Minute // Session idle timeout
	negFrameSizeLimit    = 128 * 1024      // 128KB frame size limit
)

// negSession holds state for an active negentropy sync session
type negSession struct {
	neg       *negentropy.Negentropy
	createdAt time.Time
	lastUsed  time.Time
}

// negSessions manages per-connection negentropy sessions
type negSessions struct {
	mu       sync.Mutex
	sessions map[string]*negSession
}

func newNegSessions() *negSessions {
	return &negSessions{
		sessions: make(map[string]*negSession),
	}
}

func (ns *negSessions) get(subID string) (*negSession, bool) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	s, ok := ns.sessions[subID]
	if ok {
		s.lastUsed = time.Now()
	}
	return s, ok
}

func (ns *negSessions) set(subID string, s *negSession) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.sessions[subID] = s
}

func (ns *negSessions) remove(subID string) {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	delete(ns.sessions, subID)
}

func (ns *negSessions) count() int {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	return len(ns.sessions)
}

func (ns *negSessions) closeAll() {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	// Just clear the map; negentropy instances will be GC'd
	for k := range ns.sessions {
		delete(ns.sessions, k)
	}
}

// handleNegOpen processes a NEG-OPEN message
func (c *WsConnection) handleNegOpen(ctx context.Context, arr []interface{}) {
	// Parse: ["NEG-OPEN", <subID>, <filter>, <initialMessage>]
	if len(arr) < 4 {
		c.sendNegErr("", "error: NEG-OPEN requires subscription ID, filter, and initial message")
		return
	}

	subID, ok := arr[1].(string)
	if !ok || subID == "" {
		c.sendNegErr("", "error: invalid subscription ID")
		return
	}

	// Check concurrent session limit
	if c.negSessions.count() >= maxNegSessions {
		c.sendNegErr(subID, "blocked: too many concurrent sync sessions")
		return
	}

	// Close existing session with same ID if any
	c.negSessions.remove(subID)

	// Parse filter
	filterJSON, err := json.Marshal(arr[2])
	if err != nil {
		c.sendNegErr(subID, "error: invalid filter")
		return
	}

	var filter nostr.Filter
	if err := json.Unmarshal(filterJSON, &filter); err != nil {
		c.sendNegErr(subID, "error: invalid filter: "+err.Error())
		return
	}

	initialMsg, ok := arr[3].(string)
	if !ok {
		c.sendNegErr(subID, "error: initial message must be a hex string")
		return
	}

	// Validate filter
	if err := c.node.GetValidator().ValidateFilter(filter); err != nil {
		c.sendNegErr(subID, "error: "+err.Error())
		return
	}

	// Query events matching the filter from DB
	events, err := c.QueryEvents(ctx, filter)
	if err != nil {
		logger.Error("NIP-77: failed to query events for negentropy",
			zap.String("sub_id", subID),
			zap.Error(err))
		c.sendNegErr(subID, "error: database query failed")
		return
	}

	// Check record limit
	if len(events) > maxNegRecords {
		c.sendNegErr(subID, fmt.Sprintf("blocked: filter matches too many events (%d > %d)", len(events), maxNegRecords))
		return
	}

	// Build vector storage from events
	vec := vector.New()
	for _, evt := range events {
		vec.Insert(evt.CreatedAt, evt.ID)
	}
	vec.Seal()

	// Create negentropy instance (server mode)
	neg := negentropy.New(vec, negFrameSizeLimit)

	// Process initial message
	responseMsg, err := neg.Reconcile(initialMsg)
	if err != nil {
		logger.Error("NIP-77: initial reconciliation failed",
			zap.String("sub_id", subID),
			zap.Error(err))
		c.sendNegErr(subID, "error: reconciliation failed")
		return
	}

	// Store session
	now := time.Now()
	c.negSessions.set(subID, &negSession{
		neg:       neg,
		createdAt: now,
		lastUsed:  now,
	})

	// Send response
	if responseMsg != "" {
		c.sendNegMsg(subID, responseMsg)
	}

	logger.Debug("NIP-77: NEG-OPEN processed",
		zap.String("sub_id", subID),
		zap.Int("events_count", len(events)),
		zap.String("client", c.RemoteAddr()))

	metrics.IncrementMessagesProcessed()
}

// handleNegMsg processes a NEG-MSG message
func (c *WsConnection) handleNegMsg(arr []interface{}) {
	// Parse: ["NEG-MSG", <subID>, <message>]
	if len(arr) < 3 {
		c.sendNegErr("", "error: NEG-MSG requires subscription ID and message")
		return
	}

	subID, ok := arr[1].(string)
	if !ok || subID == "" {
		c.sendNegErr("", "error: invalid subscription ID")
		return
	}

	msg, ok := arr[2].(string)
	if !ok {
		c.sendNegErr(subID, "error: message must be a hex string")
		return
	}

	// Find session
	session, exists := c.negSessions.get(subID)
	if !exists {
		c.sendNegErr(subID, "closed: no active session for this subscription ID")
		return
	}

	// Check session timeout
	if time.Since(session.lastUsed) > negSessionTimeout {
		c.negSessions.remove(subID)
		c.sendNegErr(subID, "closed: session timed out")
		return
	}

	// Process message
	responseMsg, err := session.neg.Reconcile(msg)
	if err != nil {
		logger.Error("NIP-77: reconciliation failed",
			zap.String("sub_id", subID),
			zap.Error(err))
		c.negSessions.remove(subID)
		c.sendNegErr(subID, "error: reconciliation failed")
		return
	}

	// Send response
	if responseMsg != "" {
		c.sendNegMsg(subID, responseMsg)
	} else {
		// Empty response means reconciliation is complete
		c.negSessions.remove(subID)
	}

	logger.Debug("NIP-77: NEG-MSG processed",
		zap.String("sub_id", subID),
		zap.String("client", c.RemoteAddr()))
}

// handleNegClose processes a NEG-CLOSE message
func (c *WsConnection) handleNegClose(arr []interface{}) {
	if len(arr) < 2 {
		return
	}

	subID, ok := arr[1].(string)
	if !ok {
		return
	}

	c.negSessions.remove(subID)

	logger.Debug("NIP-77: NEG-CLOSE processed",
		zap.String("sub_id", subID),
		zap.String("client", c.RemoteAddr()))
}

// sendNegMsg sends a NEG-MSG to the client
func (c *WsConnection) sendNegMsg(subID, msg string) {
	raw := fmt.Sprintf(`["NEG-MSG","%s","%s"]`, subID, msg)
	c.SendMessageNoRateLimit([]byte(raw))
}

// sendNegErr sends a NEG-ERR to the client
func (c *WsConnection) sendNegErr(subID, reason string) {
	raw := fmt.Sprintf(`["NEG-ERR","%s","%s"]`, subID, reason)
	c.SendMessageNoRateLimit([]byte(raw))
}
