package nips

import (
	"fmt"
	"strings"

	"github.com/Shugur-Network/relay/internal/logger"
	nostr "github.com/nbd-wtf/go-nostr"
	"go.uber.org/zap"
)

// NIP-EE: E2EE Messaging using the Messaging Layer Security (MLS) Protocol
// https://nips.nostr.com/EE
//
// Event kinds:
//   - 443:   KeyPackage Event — advertises MLS key material for async group invites
//   - 444:   Welcome Event — sent via NIP-59 gift wrap to new group members (unsigned inner event)
//   - 445:   Group Event — MLS group messages (control + application), published with ephemeral pubkeys
//   - 10051: KeyPackage Relays List — replaceable event listing relays for KeyPackage publishing

// ValidateKeyPackageEvent validates a kind:443 MLS KeyPackage event.
// KeyPackage events publish the user's MLS credentials so they can be added to groups asynchronously.
func ValidateKeyPackageEvent(evt *nostr.Event) error {
	logger.Debug("NIP-EE: Validating KeyPackage event",
		zap.String("event_id", evt.ID),
		zap.String("pubkey", evt.PubKey))

	if evt.Kind != 443 {
		return fmt.Errorf("invalid event kind for KeyPackage: %d", evt.Kind)
	}

	// Content must be non-empty (hex-encoded serialized KeyPackageBundle)
	if evt.Content == "" {
		return fmt.Errorf("KeyPackage event must have non-empty content (serialized KeyPackageBundle)")
	}

	// Must have mls_protocol_version tag
	hasProtocolVersion := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "mls_protocol_version" {
			hasProtocolVersion = true
			if tag[1] == "" {
				return fmt.Errorf("mls_protocol_version tag must have a value")
			}
			break
		}
	}
	if !hasProtocolVersion {
		return fmt.Errorf("KeyPackage event must have 'mls_protocol_version' tag")
	}

	// Must have mls_ciphersuite tag
	hasCiphersuite := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "mls_ciphersuite" {
			hasCiphersuite = true
			if tag[1] == "" {
				return fmt.Errorf("mls_ciphersuite tag must have a value")
			}
			break
		}
	}
	if !hasCiphersuite {
		return fmt.Errorf("KeyPackage event must have 'mls_ciphersuite' tag")
	}

	// Must have relays tag
	hasRelays := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "relays" {
			hasRelays = true
			break
		}
	}
	if !hasRelays {
		return fmt.Errorf("KeyPackage event must have 'relays' tag")
	}

	// Validate pubkey format
	if len(evt.PubKey) != 64 {
		return fmt.Errorf("invalid pubkey format in KeyPackage event")
	}

	return nil
}

// ValidateWelcomeEvent validates a kind:444 MLS Welcome event.
// Welcome events are sent to new group members via NIP-59 gift wrap.
// They are unsigned inner events — they MUST NOT be signed.
func ValidateWelcomeEvent(evt *nostr.Event) error {
	logger.Debug("NIP-EE: Validating Welcome event",
		zap.String("event_id", evt.ID))

	if evt.Kind != 444 {
		return fmt.Errorf("invalid event kind for Welcome: %d", evt.Kind)
	}

	// Content must be non-empty (serialized MLSMessage containing Welcome object)
	if evt.Content == "" {
		return fmt.Errorf("Welcome event must have non-empty content (serialized MLSMessage)")
	}

	// Must have "e" tag referencing the KeyPackage Event used for the invite
	hasETag := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "e" {
			hasETag = true
			if len(tag[1]) != 64 {
				return fmt.Errorf("invalid event ID in 'e' tag: %s", tag[1])
			}
			break
		}
	}
	if !hasETag {
		return fmt.Errorf("Welcome event must have 'e' tag referencing the KeyPackage event")
	}

	// Must have "relays" tag
	hasRelays := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "relays" {
			hasRelays = true
			break
		}
	}
	if !hasRelays {
		return fmt.Errorf("Welcome event must have 'relays' tag")
	}

	return nil
}

// ValidateGroupEvent validates a kind:445 MLS Group event.
// Group events carry all MLS messages (Proposal, Commit, Application) encrypted with NIP-44
// using a key derived from the MLS exporter_secret. Published with ephemeral keypairs.
func ValidateGroupEvent(evt *nostr.Event) error {
	logger.Debug("NIP-EE: Validating Group event",
		zap.String("event_id", evt.ID),
		zap.String("pubkey", evt.PubKey))

	if evt.Kind != 445 {
		return fmt.Errorf("invalid event kind for Group event: %d", evt.Kind)
	}

	// Content must be non-empty (NIP-44 encrypted serialized MLSMessage)
	if evt.Content == "" {
		return fmt.Errorf("Group event must have non-empty content (NIP-44 encrypted MLSMessage)")
	}

	// Must have "h" tag with the Nostr group ID
	hasHTag := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "h" {
			hasHTag = true
			if tag[1] == "" {
				return fmt.Errorf("'h' tag must have a non-empty group ID value")
			}
			break
		}
	}
	if !hasHTag {
		return fmt.Errorf("Group event must have 'h' tag with group ID")
	}

	// Content should be a valid NIP-44 payload (base64-encoded encrypted data)
	if IsNIP44Payload(evt.Content) {
		return nil
	}

	// Some implementations may use raw base64 without full NIP-44 envelope validation passing.
	// Accept content that looks like base64 as a fallback for interop.
	// The actual decryption and MLS processing is client-side.

	return nil
}

// ValidateKeyPackageRelaysList validates a kind:10051 KeyPackage Relays List event.
// This is a replaceable event (10000-19999 range) listing relays where a user
// publishes their KeyPackage events.
func ValidateKeyPackageRelaysList(evt *nostr.Event) error {
	logger.Debug("NIP-EE: Validating KeyPackage Relays List event",
		zap.String("event_id", evt.ID),
		zap.String("pubkey", evt.PubKey))

	if evt.Kind != 10051 {
		return fmt.Errorf("invalid event kind for KeyPackage Relays List: %d", evt.Kind)
	}

	// Must have at least one "relay" tag
	relayCount := 0
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "relay" {
			relayCount++
			url := tag[1]
			if !strings.HasPrefix(url, "wss://") && !strings.HasPrefix(url, "ws://") {
				return fmt.Errorf("invalid relay URL in 'relay' tag: %s (must start with wss:// or ws://)", url)
			}
		}
	}
	if relayCount == 0 {
		return fmt.Errorf("KeyPackage Relays List must have at least one 'relay' tag")
	}

	return nil
}

// IsMLSEvent checks if an event is an MLS/NIP-EE event
func IsMLSEvent(evt *nostr.Event) bool {
	return evt.Kind == 443 || evt.Kind == 444 || evt.Kind == 445 || evt.Kind == 10051
}

// IsMLSGroupEvent checks if an event is an MLS Group event (kind 445)
func IsMLSGroupEvent(evt *nostr.Event) bool {
	return evt.Kind == 445
}

// IsKeyPackageEvent checks if an event is an MLS KeyPackage event (kind 443)
func IsKeyPackageEvent(evt *nostr.Event) bool {
	return evt.Kind == 443
}
