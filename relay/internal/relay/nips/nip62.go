package nips

import (
	"fmt"
	"strings"

	"github.com/Shugur-Network/relay/internal/logger"
	nostr "github.com/nbd-wtf/go-nostr"
	"go.uber.org/zap"
)

// NIP-62: Request to Vanish
// https://github.com/nostr-protocol/nips/blob/master/62.md

// ValidateVanishEvent validates a NIP-62 vanish request (kind 62).
func ValidateVanishEvent(evt *nostr.Event) error {
	if evt.Kind != 62 {
		return fmt.Errorf("expected kind 62, got %d", evt.Kind)
	}

	// Must have at least one "relay" tag
	hasRelay := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "relay" {
			hasRelay = true
			break
		}
	}
	if !hasRelay {
		return fmt.Errorf("vanish request must include at least one 'relay' tag")
	}

	return nil
}

// IsVanishEvent returns true if the event is a NIP-62 vanish request.
func IsVanishEvent(evt nostr.Event) bool {
	return evt.Kind == 62
}

// VanishTargetsRelay checks if a vanish request targets a specific relay URL or ALL_RELAYS.
func VanishTargetsRelay(evt *nostr.Event, relayURL string) bool {
	normalizedRelayURL := strings.TrimSuffix(strings.ToLower(relayURL), "/")

	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "relay" {
			if tag[1] == "ALL_RELAYS" {
				logger.Info("NIP-62: Global vanish request received",
					zap.String("pubkey", evt.PubKey),
					zap.String("content", evt.Content))
				return true
			}
			normalizedTag := strings.TrimSuffix(strings.ToLower(tag[1]), "/")
			if normalizedTag == normalizedRelayURL {
				logger.Info("NIP-62: Targeted vanish request received",
					zap.String("pubkey", evt.PubKey),
					zap.String("relay", tag[1]))
				return true
			}
		}
	}
	return false
}
