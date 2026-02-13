package nips

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/Shugur-Network/relay/internal/logger"
	nostr "github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip42"
	"go.uber.org/zap"
)

// GenerateAuthChallenge creates a random hex challenge string for NIP-42 AUTH.
func GenerateAuthChallenge() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate auth challenge: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ValidateAuthEvent validates a NIP-42 AUTH event from a client.
// Returns the authenticated pubkey on success.
func ValidateAuthEvent(event *nostr.Event, challenge string, relayURL string) (string, bool) {
	pubkey, ok := nip42.ValidateAuthEvent(event, challenge, relayURL)
	if !ok {
		logger.Debug("NIP-42: AUTH validation failed",
			zap.String("event_id", event.ID),
			zap.String("pubkey", event.PubKey),
			zap.String("challenge", challenge))
		return "", false
	}

	logger.Info("NIP-42: Client authenticated",
		zap.String("pubkey", pubkey))
	return pubkey, true
}

// IsProtectedEvent checks if an event has the NIP-70 "-" tag (protected event).
func IsProtectedEvent(evt *nostr.Event) bool {
	for _, tag := range evt.Tags {
		if len(tag) == 1 && tag[0] == "-" {
			return true
		}
	}
	return false
}

// IsAuthEvent returns true if the event is a NIP-42 auth event (kind 22242).
func IsAuthEvent(evt *nostr.Event) bool {
	return evt.Kind == nostr.KindClientAuthentication
}
