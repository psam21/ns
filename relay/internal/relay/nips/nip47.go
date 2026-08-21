package nips

import (
	"encoding/base64"
	"fmt"
	"strings"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-47: Nostr Wallet Connect (NWC)
// https://github.com/nostr-protocol/nips/blob/master/47.md
//
// SPEC UPDATE (3 weeks ago): Simplified core spec and added extensions
// The spec was simplified to define only the core protocol with optional
// extensions defined in separate specifications at:
// https://github.com/nostr-wallet-connect/nwc
//
// Event kinds:
//   - 13194: Info event (replaceable)
//   - 23194: Request event
//   - 23195: Response event

// NIP47 core methods defined in the simplified spec.
var nip47CoreMethods = map[string]bool{
	"pay_invoice":    true,
	"make_invoice":   true,
	"lookup_invoice": true,
	"get_balance":    true,
	"get_info":       true,
}

// NIP47 supported encryption schemes.
var nip47EncryptionSchemes = map[string]bool{
	"nip44_v2": true,
	"nip04":    true, // deprecated, backward compatibility
}

// ValidateNIP47Info validates a NIP-47 info event (kind 13194).
// The info event advertises wallet service capabilities.
func ValidateNIP47Info(evt *nostr.Event) error {
	if evt.Kind != 13194 {
		return fmt.Errorf("invalid event kind for NIP-47 info: %d", evt.Kind)
	}

	// Validate encryption tag if present
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "encryption" {
			schemes := strings.Fields(tag[1])
			if len(schemes) == 0 {
				return fmt.Errorf("'encryption' tag cannot be empty")
			}
			for _, scheme := range schemes {
				if !nip47EncryptionSchemes[scheme] {
					return fmt.Errorf("unsupported encryption scheme: %s", scheme)
				}
			}
		}

		// Validate extensions tag if present
		if len(tag) >= 2 && tag[0] == "extensions" {
			// Extensions are space-separated identifiers (e.g., "02 03 04")
			// We just validate format, not specific extension names
			exts := strings.Fields(tag[1])
			for _, ext := range exts {
				if ext == "" {
					return fmt.Errorf("empty extension identifier in 'extensions' tag")
				}
			}
		}
	}

	// Content should be space-separated list of supported methods
	if evt.Content != "" {
		methods := strings.Fields(evt.Content)
		for _, method := range methods {
			// Core methods are validated; extensions are allowed but not validated here
			if !nip47CoreMethods[method] && !isExtensionMethod(method) {
				// Allow unknown methods (forward compatibility with extensions)
				continue
			}
		}
	}

	return nil
}

// ValidateNIP47Request validates a NIP-47 request event (kind 23194).
func ValidateNIP47Request(evt *nostr.Event) error {
	if evt.Kind != 23194 {
		return fmt.Errorf("invalid event kind for NIP-47 request: %d", evt.Kind)
	}

	// Must have "p" tag with wallet service pubkey
	hasPTag := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			if len(tag[1]) != 64 {
				return fmt.Errorf("invalid pubkey in 'p' tag: %s", tag[1])
			}
			hasPTag = true
			break
		}
	}

	if !hasPTag {
		return fmt.Errorf("NIP-47 request must have 'p' tag with wallet service pubkey")
	}

	// Validate encryption tag if present
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "encryption" {
			scheme := tag[1]
			if !nip47EncryptionSchemes[scheme] {
				return fmt.Errorf("unsupported encryption scheme: %s", scheme)
			}
		}
	}

	// Content must be encrypted (NIP-44 or NIP-04)
	if evt.Content == "" {
		return fmt.Errorf("NIP-47 request must have encrypted content")
	}

	// Try to decode as base64 (basic format check)
	if _, err := base64.StdEncoding.DecodeString(evt.Content); err != nil {
		return fmt.Errorf("NIP-47 request content must be base64 encoded: %w", err)
	}

	return nil
}

// ValidateNIP47Response validates a NIP-47 response event (kind 23195).
func ValidateNIP47Response(evt *nostr.Event) error {
	if evt.Kind != 23195 {
		return fmt.Errorf("invalid event kind for NIP-47 response: %d", evt.Kind)
	}

	// Must have "p" tag with client pubkey
	hasPTag := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			if len(tag[1]) != 64 {
				return fmt.Errorf("invalid pubkey in 'p' tag: %s", tag[1])
			}
			hasPTag = true
			break
		}
	}

	if !hasPTag {
		return fmt.Errorf("NIP-47 response must have 'p' tag with client pubkey")
	}

	// Should have "e" tag with request event id
	hasETag := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "e" {
			if len(tag[1]) != 64 {
				return fmt.Errorf("invalid event id in 'e' tag: %s", tag[1])
			}
			hasETag = true
			break
		}
	}

	if !hasETag {
		return fmt.Errorf("NIP-47 response should have 'e' tag with request event id")
	}

	// Content must be encrypted
	if evt.Content == "" {
		return fmt.Errorf("NIP-47 response must have encrypted content")
	}

	// Try to decode as base64 (basic format check)
	if _, err := base64.StdEncoding.DecodeString(evt.Content); err != nil {
		return fmt.Errorf("NIP-47 response content must be base64 encoded: %w", err)
	}

	return nil
}

// isExtensionMethod checks if a method name looks like an extension method.
// Extension methods are defined in separate specs starting with "02.md".
// This is a permissive check - we allow any non-core method as a potential extension.
func isExtensionMethod(method string) bool {
	return !nip47CoreMethods[method]
}
