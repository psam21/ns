package nips

import (
	"encoding/json"
	"fmt"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-59: Gift Wrap
// https://github.com/nostr-protocol/nips/blob/master/59.md

// ValidateGiftWrapEvent validates NIP-59 gift wrap events
func ValidateGiftWrapEvent(evt *nostr.Event) error {
	switch evt.Kind {
	case 1059:
		return validateGiftWrapOuter(evt)
	case 13194:
		return validateWalletConnectEvent(evt)
	default:
		return fmt.Errorf("invalid event kind for gift wrap: %d", evt.Kind)
	}
}

// validateGiftWrapOuter validates outer gift wrap events (kind 1059)
func validateGiftWrapOuter(evt *nostr.Event) error {
	if evt.Kind != 1059 {
		return fmt.Errorf("invalid event kind for gift wrap: %d", evt.Kind)
	}

	// Must have "p" tag with recipient pubkey
	hasPTag := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			hasPTag = true
			// Validate pubkey format
			if len(tag[1]) != 64 {
				return fmt.Errorf("invalid pubkey in 'p' tag: %s", tag[1])
			}
			break
		}
	}

	if !hasPTag {
		return fmt.Errorf("gift wrap must have 'p' tag with recipient")
	}

	// Content must be encrypted (non-empty)
	if evt.Content == "" {
		return fmt.Errorf("gift wrap must have encrypted content")
	}

	// Validate NIP-44 format
	if !IsNIP44Payload(evt.Content) {
		return fmt.Errorf("invalid NIP-44 content in gift wrap")
	}

	// CreatedAt should be randomized for privacy
	// We can't validate this strictly, but we can check it's reasonable
	if evt.CreatedAt == 0 {
		return fmt.Errorf("gift wrap must have created_at timestamp")
	}

	return nil
}

// validateWalletConnectEvent validates wallet connect events (kind 13194)
// NIP-47: Nostr Wallet Connect - simplified core spec with extensions
func validateWalletConnectEvent(evt *nostr.Event) error {
	if evt.Kind != 13194 {
		return fmt.Errorf("invalid event kind for wallet connect: %d", evt.Kind)
	}

	// NIP-47 wallet connect events should have:
	// - "p" tag with the wallet pubkey
	// - "method" tag with the RPC method name
	// - "params" tag with JSON-encoded parameters
	// - "id" tag with request ID (for requests)
	// - "result" or "error" tag for responses

	hasPTag := false
	hasMethodTag := false
	hasParamsTag := false

	for _, tag := range evt.Tags {
		if len(tag) >= 2 {
			switch tag[0] {
			case "p":
				hasPTag = true
				// Validate pubkey format
				if len(tag[1]) != 64 {
					return fmt.Errorf("invalid pubkey in 'p' tag: %s", tag[1])
				}
			case "method":
				hasMethodTag = true
				if tag[1] == "" {
					return fmt.Errorf("method tag must have a value")
				}
			case "params":
				hasParamsTag = true
				// Validate JSON format
				var params interface{}
				if err := json.Unmarshal([]byte(tag[1]), &params); err != nil {
					return fmt.Errorf("invalid JSON in 'params' tag: %v", err)
				}
			case "id":
				// Request ID - optional but recommended
				if tag[1] == "" {
					return fmt.Errorf("id tag must have a value")
				}
			case "result":
				// Response result - validate JSON
				var result interface{}
				if err := json.Unmarshal([]byte(tag[1]), &result); err != nil {
					return fmt.Errorf("invalid JSON in 'result' tag: %v", err)
				}
			case "error":
				// Error response - validate JSON
				var errorObj interface{}
				if err := json.Unmarshal([]byte(tag[1]), &errorObj); err != nil {
					return fmt.Errorf("invalid JSON in 'error' tag: %v", err)
				}
			}
		}
	}

	// For requests, method and params are required
	// For responses, either result or error is required
	// We can't easily distinguish without more context, so we validate what's present

	if !hasPTag {
		return fmt.Errorf("wallet connect event must have 'p' tag with wallet pubkey")
	}

	if !hasMethodTag {
		return fmt.Errorf("wallet connect event must have 'method' tag")
	}

	if !hasParamsTag {
		return fmt.Errorf("wallet connect event must have 'params' tag")
	}

	// Validate NIP-44 format for encrypted content
	if !IsNIP44Payload(evt.Content) {
		return fmt.Errorf("invalid NIP-44 content in wallet connect event")
	}

	// CreatedAt should be present
	if evt.CreatedAt == 0 {
		return fmt.Errorf("wallet connect event must have created_at timestamp")
	}

	return nil
}

// IsGiftWrapEvent checks if an event is a gift wrap event
func IsGiftWrapEvent(evt *nostr.Event) bool {
	return evt.Kind == 13 || evt.Kind == 1059 || evt.Kind == 13194
}

// IsSealEvent checks if an event is a seal event (kind 13)
func IsSealEvent(evt *nostr.Event) bool {
	return evt.Kind == 13
}

// IsOuterGiftWrap checks if an event is an outer gift wrap (kind 1059)
func IsOuterGiftWrap(evt *nostr.Event) bool {
	return evt.Kind == 1059
}

// IsWalletConnectEvent checks if an event is a wallet connect event (kind 13194)
func IsWalletConnectEvent(evt *nostr.Event) bool {
	return evt.Kind == 13194
}
