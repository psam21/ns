package relay

import (
	"context"
	"time"

	"github.com/Shugur-Network/relay/internal/constants"
	"github.com/Shugur-Network/relay/internal/logger"
	"github.com/Shugur-Network/relay/internal/metrics"
	"github.com/Shugur-Network/relay/internal/relay/nips"
	nostr "github.com/nbd-wtf/go-nostr"
	"go.uber.org/zap"
)

func (c *WsConnection) handleRequest(ctx context.Context, arr []interface{}) {
	// Log the start of request processing
	logger.Debug("Processing REQ command",
		zap.String("client", c.RemoteAddr()))

	// Validate array length
	if len(arr) < 3 {
		logger.Warn("Invalid REQ command: missing subscription ID or filter",
			zap.String("client", c.RemoteAddr()))
		c.sendNotice("REQ command missing subscription ID or filter")
		return
	}

	// Extract subscription ID
	subID, ok := arr[1].(string)
	if !ok || subID == "" {
		logger.Warn("Invalid REQ command: subscription ID must be a string",
			zap.String("client", c.RemoteAddr()))
		c.sendNotice("REQ command subscription ID must be a string")
		return
	}

	// Validate subscription ID length
	if len(subID) > 64 {
		c.sendNotice("Subscription ID too long (max 64 chars)")
		return
	}

	// Remove existing subscription if present
	if c.hasSubscription(subID) {
		logger.Debug("Replacing existing subscription",
			zap.String("sub_id", subID),
			zap.String("client", c.RemoteAddr()))
		c.removeSubscription(subID)
	}

	// Parse the filter with support for #tag syntax
	var f nostr.Filter
	if len(arr) >= 3 {
		filter, err := parseFilterFromRaw(arr[2])
		if err != nil {
			logger.Warn("Failed to parse filter",
				zap.String("sub_id", subID),
				zap.Error(err),
				zap.String("client", c.RemoteAddr()))
			c.sendNotice("Invalid filter: " + err.Error())
			return
		}
		f = filter
	} else {
		c.sendNotice("REQ command missing filter")
		return
	}

	// Apply cap to limit if needed
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 500
	}

	// Validate filter with the validator
	if err := c.node.GetValidator().ValidateFilter(f); err != nil {
		logger.Warn("Filter validation failed",
			zap.String("sub_id", subID),
			zap.Error(err),
			zap.String("client", c.RemoteAddr()))
		c.sendClosed(subID, nips.FormatErrorMessage(nips.ErrorCodeInvalidFilter, err.Error()))
		return
	}

	// Check special validation for specific filter types
	if len(f.Kinds) > 0 {
		switch {
		case containsKind(f.Kinds, nips.KindRelayList):
			if err := nips.ValidateRelayListFilter(f); err != nil {
				c.sendClosed(subID, nips.FormatErrorMessage(nips.ErrorCodeInvalidFilter, err.Error()))
				return
			}
		}
	}

	// Validate search if present
	if f.Search != "" {
		if err := nips.ValidateSearchFilter(f, nips.DefaultSearchOptions()); err != nil {
			c.sendClosed(subID, nips.FormatErrorMessage(nips.ErrorCodeInvalidFilter, err.Error()))
			return
		}
	}

	// NIP-17: Require AUTH for DM and gift-wrap queries to prevent leaking to non-recipients
	if len(f.Kinds) > 0 {
		requiresAuth := false
		for _, k := range f.Kinds {
			if k == 4 || k == 14 || k == 15 || k == 1059 {
				requiresAuth = true
				break
			}
		}
		if requiresAuth && !c.hasAuthentication() {
			c.sendClosed(subID, "auth-required: this query requires authentication")
			return
		}
	}

	// Enforce max subscription cap (finding #2)
	//
	// The count is read under the lock. Reading c.subscriptions directly, as
	// this did, races with Close(), which does
	// c.subscriptions = make(map[string][]nostr.Filter) under the same mutex
	// and expects this one to serialize against it. It is a data race, not
	// just an unsynchronized read, and the cap is advisory so the worst case
	// is one extra subscription rather than a crash -- but a map read racing
	// a map replacement is a concurrent map access panic waiting for a load.
	if c.subscriptionCount() >= constants.MaxSubscriptions {
		logger.Warn("Subscription cap reached",
			zap.String("client", c.RemoteAddr()),
			zap.Int("max", constants.MaxSubscriptions))
		c.sendClosed(subID, "subscription-limit: too many active subscriptions")
		return
	}

	// Store subscription.
	//
	// addSubscription reports whether subID was already registered, because a
	// duplicate REQ (NIP-01: a second REQ with the same subscription_id
	// replaces the first) must not increment the gauge again. Previously every
	// REQ incremented unconditionally, so a client that reuses one sub_id
	// inflated active_subscriptions without bound -- and Close() then
	// decremented by the map size, so the gauge never returned to zero. The
	// dashboard's "active subscriptions" figure drifted upward for the life of
	// the process and the leak was invisible because nothing asserted the
	// gauge against the map.
	if replaced := c.addSubscription(subID, []nostr.Filter{f}); !replaced {
		// Update metrics
		metrics.ActiveSubscriptions.Inc()
	}

	// Query DB and send events in a goroutine.
	//
	// Uses c.eventCtx, the per-connection context, not ctx. ctx here is the
	// server-wide context threaded down from HandleMessages, which is canceled
	// only at process shutdown -- so a REQ that arrived moments before the
	// client disconnected kept its database query running against the
	// now-dead connection.
	go c.processSubscription(c.eventCtx, subID, f)
}

// processSubscription handles the database query and sending events to the client
func (c *WsConnection) processSubscription(ctx context.Context, subID string, f nostr.Filter) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Recovered from panic in processSubscription",
				zap.Any("panic", r),
				zap.String("client", c.RemoteAddr()),
				zap.String("sub_id", subID))
		}
	}()
	// Bound the database query.
	//
	// The timeout context created here used to be discarded -- assigned to
	// `_` and immediately canceled by the deferred cancel, while the actual
	// call below used the unbounded ctx. The 30s bound existed on paper and
	// applied to nothing, so one slow query held a database connection and a
	// goroutine indefinitely. This is the query side of the same class as the
	// per-connection context leak: a bound that is written but not threaded
	// through to the operation it was meant to limit.
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Query events from the database
	start := time.Now()
	events, err := c.QueryEvents(queryCtx, f)
	duration := time.Since(start)

	// Log query performance
	logger.Debug("Query execution completed",
		zap.String("sub_id", subID),
		zap.Duration("duration", duration),
		zap.Int("events_count", len(events)),
		zap.String("client", c.RemoteAddr()))

	if err != nil {
		logger.Error("Failed to query events",
			zap.String("sub_id", subID),
			zap.Error(err),
			zap.String("client", c.RemoteAddr()))
		c.sendNotice(nips.ErrDatabaseError)
		return
	}

	// Check if client is still connected before proceeding
	if c.isClosed.Load() {
		return
	}

	// Apply special validation for specific event kinds
	if len(f.Kinds) == 1 {
		switch f.Kinds[0] {
		case nips.KindRelayList:
			// Filter out invalid relay list events
			validEvents := make([]nostr.Event, 0, len(events))
			for _, evt := range events {
				if err := nips.ValidateKind10002(evt); err == nil {
					validEvents = append(validEvents, evt)
				}
			}
			events = validEvents
		}
	}

	// Send events to the client
	sentCount := 0
	for _, evt := range events {
		// Check again if client is still connected
		if c.isClosed.Load() {
			return
		}

		// For DMs and gift wrap, only send events the authenticated user is party to
		if evt.Kind == 4 || evt.Kind == 14 || evt.Kind == 15 || evt.Kind == 1059 {
			authedPK := c.getAuthenticatedPubkey()
			if authedPK == "" {
				continue // Not authenticated, skip
			}
			// Check if the authed user is the author or a recipient
			if evt.PubKey != authedPK && !eventHasPTag(&evt, authedPK) {
				continue // Not their event, skip
			}
		}

		// Send the event
		c.SendEvent(subID, &evt)
		sentCount++
	}

	logger.Debug("Subscription events sent",
		zap.String("sub_id", subID),
		zap.Int("sent_count", sentCount),
		zap.String("client", c.RemoteAddr()))

	// Send EOSE (End of Stored Events)
	// Per NIP-67, include "finish" hint since the relay has sent every stored
	// event matching the subscription's filters (no internal cap is applied
	// beyond what the filter requested).
	if !c.isClosed.Load() {
		c.sendEOSE(subID, "finish")
	}
}

// isAuthorizedForDM checks if a client should receive a DM
func isAuthorizedForDM(evt *nostr.Event, filters []nostr.Filter) bool {
	// Skip authorization for non-DM events
	// Note: Gift wrap events (1059) are excluded as they handle access control via encryption
	if evt.Kind != 4 && evt.Kind != 14 && evt.Kind != 15 {
		return true
	}

	// Extract DM recipients from event tags
	recipients := make(map[string]bool)
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			recipients[tag[1]] = true
		}
	}
	// Sender can see their own DMs
	recipients[evt.PubKey] = true

	// Check if any filter authorizes the client to see this DM
	for _, filter := range filters {
		// If filter explicitly includes the event's author
		for _, author := range filter.Authors {
			if author == evt.PubKey {
				return true
			}
		}

		// If filter explicitly includes the client's pubkey as a recipient
		if pTags, ok := filter.Tags["p"]; ok {
			for _, pubkey := range pTags {
				if recipients[pubkey] {
					return true
				}
			}
		}
	}
	return false
}

// eventHasPTag checks if an event has a "p" tag with the given pubkey
func eventHasPTag(evt *nostr.Event, pubkey string) bool {
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "p" && tag[1] == pubkey {
			return true
		}
	}
	return false
}

func (c *WsConnection) handleClose(arr []interface{}) {
	// Log the start of close processing
	logger.Debug("Processing CLOSE command",
		zap.String("client", c.RemoteAddr()))

	// Validate array length
	if len(arr) < 2 {
		logger.Warn("Invalid CLOSE command: missing subscription ID",
			zap.String("client", c.RemoteAddr()))
		c.sendNotice("CLOSE command missing subscription ID")
		return
	}

	// Extract and validate subscription ID
	subID, ok := arr[1].(string)
	if !ok {
		logger.Warn("Invalid CLOSE command: subscription ID must be a string",
			zap.String("client", c.RemoteAddr()))
		c.sendNotice("CLOSE command subscription ID must be a string")
		return
	}

	// Check if subscription exists before attempting to close
	if !c.hasSubscription(subID) {
		logger.Debug("Attempted to close non-existent subscription",
			zap.String("sub_id", subID),
			zap.String("client", c.RemoteAddr()))
		c.sendClosed(subID, "subscription not found")
		return
	}

	// Log subscription closure
	logger.Debug("Closing subscription",
		zap.String("sub_id", subID),
		zap.String("client", c.RemoteAddr()))

	// Remove subscription and send confirmation
	c.removeSubscription(subID)
	c.sendClosed(subID, "subscription closed")

	// Update metrics
	metrics.ActiveSubscriptions.Dec()

	// Log successful closure
	logger.Debug("Subscription successfully closed",
		zap.String("sub_id", subID),
		zap.String("client", c.RemoteAddr()))
}

// handleCountRequest processes COUNT commands for NIP-45
func (c *WsConnection) handleCountRequest(ctx context.Context, arr []interface{}) {
	// Log the start of count request processing
	logger.Debug("Starting count request processing",
		zap.String("client", c.RemoteAddr()))

	// Parse the COUNT command using NIP-45 module
	countCmd, err := nips.ParseCountCommand(arr)
	if err != nil {
		logger.Warn("Invalid COUNT command",
			zap.Error(err),
			zap.String("client", c.RemoteAddr()))
		c.sendNotice("Invalid COUNT command: " + err.Error())
		return
	}

	// Parse the filter using existing parseFilterFromRaw
	if len(arr) >= 3 {
		filter, err := parseFilterFromRaw(arr[2])
		if err != nil {
			logger.Warn("Failed to parse filter for COUNT",
				zap.String("sub_id", countCmd.SubID),
				zap.Error(err),
				zap.String("client", c.RemoteAddr()))
			c.sendNotice("Invalid filter: " + err.Error())
			return
		}
		countCmd.Filter = filter
	} else {
		c.sendNotice("COUNT command missing filter")
		return
	}

	// Process count in a goroutine
	go func() {
		// Create a context with timeout for the count operation
		countCtx, cancel := context.WithTimeout(ctx, nips.CountTimeout)
		defer cancel()

		// Use the new HandleCountRequest which handles validation, counting, and HLL
		response, err := nips.HandleCountRequest(countCtx, countCmd.SubID, countCmd.Filter, c.node.DB())
		if err != nil {
			logger.Warn("COUNT request failed",
				zap.String("sub_id", countCmd.SubID),
				zap.Error(err),
				zap.String("client", c.RemoteAddr()))
			c.sendNotice("Invalid COUNT filter: " + err.Error())
			return
		}

		c.sendMessage("COUNT", countCmd.SubID, response)
	}()
}

// Subscription management helpers
func (c *WsConnection) hasSubscription(subID string) bool {
	c.subMu.RLock()
	defer c.subMu.RUnlock()
	_, ok := c.subscriptions[subID]
	return ok
}

// addSubscription registers filters under subID and reports whether subID was
// already registered, so callers can keep the ActiveSubscriptions gauge in
// step with the map.
func (c *WsConnection) addSubscription(subID string, filters []nostr.Filter) bool {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	_, replaced := c.subscriptions[subID]
	c.subscriptions[subID] = filters
	return replaced
}

// subscriptionCount returns the number of registered subscriptions.
//
// Exists so callers can enforce MaxSubscriptions under the lock rather than
// reading the map directly.
func (c *WsConnection) subscriptionCount() int {
	c.subMu.RLock()
	defer c.subMu.RUnlock()
	return len(c.subscriptions)
}

func (c *WsConnection) removeSubscription(subID string) {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	delete(c.subscriptions, subID)
}

func (c *WsConnection) getSubscriptionFilters(subID string) []nostr.Filter {
	c.subMu.RLock()
	defer c.subMu.RUnlock()
	filters, ok := c.subscriptions[subID]
	if !ok {
		return nil
	}
	return filters
}

// GetSubscriptions returns all active subscriptions
func (c *WsConnection) GetSubscriptions() map[string][]nostr.Filter {
	c.subMu.RLock()
	defer c.subMu.RUnlock()

	// Create a copy to avoid concurrent access issues
	cp := make(map[string][]nostr.Filter, len(c.subscriptions))
	for k, v := range c.subscriptions {
		cp[k] = v
	}
	return cp
}

// SendEvent sends a Nostr event to the client for a specific subscription
func (c *WsConnection) SendEvent(subID string, evt *nostr.Event) {
	// Check if subscription exists
	if !c.HasSubscription(subID) {
		return
	}

	// Send the event
	c.sendMessage("EVENT", subID, evt)
}

// containsKind checks if a slice of kinds contains a specific kind
func containsKind(kinds []int, kind int) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}
