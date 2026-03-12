package storage

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/Shugur-Network/relay/internal/logger"
	nostr "github.com/nbd-wtf/go-nostr"
	"go.uber.org/zap"
)

// EventRowData represents the event data structure from the database
type EventRowData struct {
	ID        string          `json:"id"`
	PubKey    string          `json:"pubkey"`
	CreatedAt int64           `json:"created_at"`
	Kind      int             `json:"kind"`
	Tags      json.RawMessage `json:"tags"`
	Content   string          `json:"content"`
	Sig       string          `json:"sig"`
}

// ToNostrEvent converts EventRowData to a nostr.Event
func (e *EventRowData) ToNostrEvent() (*nostr.Event, error) {
	evt := &nostr.Event{
		ID:        e.ID,
		PubKey:    e.PubKey,
		CreatedAt: nostr.Timestamp(e.CreatedAt),
		Kind:      e.Kind,
		Content:   e.Content,
		Sig:       e.Sig,
	}

	// Parse tags if they exist
	if len(e.Tags) > 0 {
		if err := json.Unmarshal(e.Tags, &evt.Tags); err != nil {
			logger.Warn("Failed to unmarshal tags in event row", zap.Error(err))
			evt.Tags = []nostr.Tag{}
		}
	}

	return evt, nil
}

// EventDispatcher manages real-time event distribution across relay instances
type EventDispatcher struct {
	db          *DB
	clients     map[string]chan *nostr.Event
	clientsMu   sync.RWMutex
	eventBuffer chan *nostr.Event
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewEventDispatcher creates a new event dispatcher for real-time events
func NewEventDispatcher(db *DB) *EventDispatcher {
	ctx, cancel := context.WithCancel(context.Background())

	return &EventDispatcher{
		db:          db,
		clients:     make(map[string]chan *nostr.Event),
		eventBuffer: make(chan *nostr.Event, 1000),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start begins processing events for local clients
func (ed *EventDispatcher) Start() error {
	if !ed.db.isConnected() {
		return logger.NewError("database is not connected")
	}

	logger.Info("Starting event dispatcher...")
	go ed.processEvents()
	logger.Info("✅ Event dispatcher started")
	return nil
}

// Stop stops the event dispatcher
func (ed *EventDispatcher) Stop() {
	logger.Info("Stopping event dispatcher...")
	ed.cancel()

	// Close all client channels
	ed.clientsMu.Lock()
	for clientID, clientChan := range ed.clients {
		close(clientChan)
		delete(ed.clients, clientID)
	}
	ed.clientsMu.Unlock()

	close(ed.eventBuffer)
	logger.Info("✅ Event dispatcher stopped")
}

// AddClient registers a new client for event notifications
func (ed *EventDispatcher) AddClient(clientID string) chan *nostr.Event {
	ed.clientsMu.Lock()
	defer ed.clientsMu.Unlock()

	clientChan := make(chan *nostr.Event, 100)
	ed.clients[clientID] = clientChan

	logger.Debug("Added event dispatcher client", zap.String("client_id", clientID))
	return clientChan
}

// RemoveClient unregisters a client from event notifications
func (ed *EventDispatcher) RemoveClient(clientID string) {
	ed.clientsMu.Lock()
	defer ed.clientsMu.Unlock()

	if clientChan, exists := ed.clients[clientID]; exists {
		close(clientChan)
		delete(ed.clients, clientID)
		logger.Debug("Removed event dispatcher client", zap.String("client_id", clientID))
	}
}

// GetClientCount returns the number of active clients
func (ed *EventDispatcher) GetClientCount() int {
	ed.clientsMu.RLock()
	defer ed.clientsMu.RUnlock()
	return len(ed.clients)
}

// processEvents processes events from the buffer and broadcasts them to clients
func (ed *EventDispatcher) processEvents() {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	var batch []*nostr.Event

	for {
		select {
		case <-ed.ctx.Done():
			return
		case event := <-ed.eventBuffer:
			batch = append(batch, event)
		case <-ticker.C:
			if len(batch) > 0 {
				ed.broadcastEvents(batch)
				batch = batch[:0] // Clear batch
			}
		}
	}
}

// broadcastEvents sends events to all registered clients
func (ed *EventDispatcher) broadcastEvents(events []*nostr.Event) {
	ed.clientsMu.RLock()
	clientCount := len(ed.clients)
	ed.clientsMu.RUnlock()

	if len(events) > 0 {
		logger.Info("Broadcasting events to clients",
			zap.Int("event_count", len(events)),
			zap.Int("client_count", clientCount))
	}

	ed.clientsMu.RLock()
	defer ed.clientsMu.RUnlock()

	for clientID, clientChan := range ed.clients {
		for _, event := range events {
			select {
			case clientChan <- event:
				logger.Debug("Event sent to client successfully",
					zap.String("client_id", clientID),
					zap.String("event_id", event.ID))
			default:
				// Client buffer is full, drop the event
				logger.Warn("Dropped event for client - buffer full",
					zap.String("client_id", clientID),
					zap.String("event_id", event.ID))
			}
		}
	}
}
