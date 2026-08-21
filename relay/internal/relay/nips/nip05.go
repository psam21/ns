package nips

import (
	"encoding/json"
	"fmt"
	"regexp"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-05: Mapping Nostr keys to DNS-based internet identifiers
// https://github.com/nostr-protocol/nips/blob/master/05.md
//
// NIP-05 allows users to map their Nostr public key to a DNS-based internet
// identifier (an email-like address). The local-part MUST only use characters
// a-z0-9-_. and the identifier is split into <local-part> and <domain>.
//
// The relay's role is to validate that the nip05 field in kind 0 (user
// metadata) events is well-formed. Actual DNS resolution is performed by
// clients.

// nip05LocalPartRegex validates the local-part of a NIP-05 identifier.
// Per spec: "the <local-part> part MUST only use characters a-z0-9-_."
var nip05LocalPartRegex = regexp.MustCompile(`^[a-z0-9\-_.]+$`)

// nip05DomainRegex validates the domain part of a NIP-05 identifier.
// Allows standard domain characters.
var nip05DomainRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?)+$`)

// ValidateNIP05Identifier validates a NIP-05 identifier string format.
// Returns nil if valid, error otherwise.
//
// Valid formats:
//   - "_@domain" (root identifier, displayed as just "domain")
//   - "name@domain" (regular identifier)
func ValidateNIP05Identifier(identifier string) error {
	if identifier == "" {
		return fmt.Errorf("NIP-05 identifier cannot be empty")
	}

	// Find the @ separator
	atIdx := -1
	for i, c := range identifier {
		if c == '@' {
			atIdx = i
			break
		}
	}

	if atIdx == -1 {
		return fmt.Errorf("NIP-05 identifier must contain '@': %s", identifier)
	}

	localPart := identifier[:atIdx]
	domain := identifier[atIdx+1:]

	if localPart == "" {
		return fmt.Errorf("NIP-05 local-part cannot be empty")
	}
	if domain == "" {
		return fmt.Errorf("NIP-05 domain cannot be empty")
	}

	// Validate local-part
	if !nip05LocalPartRegex.MatchString(localPart) {
		return fmt.Errorf("invalid NIP-05 local-part '%s': must only contain a-z0-9-_.", localPart)
	}

	// Validate domain
	if !nip05DomainRegex.MatchString(domain) {
		return fmt.Errorf("invalid NIP-05 domain '%s'", domain)
	}

	return nil
}

// ValidateNIP05Metadata validates that any nip05 field in a kind 0 (user
// metadata) event is well-formed.
func ValidateNIP05Metadata(evt *nostr.Event) error {
	if evt.Kind != 0 {
		return nil
	}

	if evt.Content == "" {
		return nil
	}

	// Parse the metadata JSON
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(evt.Content), &metadata); err != nil {
		// Don't fail on JSON parse errors - other validators handle that
		return nil
	}

	// Check for nip05 field
	nip05Val, ok := metadata["nip05"]
	if !ok {
		return nil
	}

	nip05Str, ok := nip05Val.(string)
	if !ok {
		return fmt.Errorf("nip05 field must be a string")
	}

	return ValidateNIP05Identifier(nip05Str)
}

// ResolveNIP05Address is a placeholder for future NIP-05 DNS resolution.
// This would fetch https://<domain>/.well-known/nostr.json?name=<local-part>
// and verify the pubkey mapping. Actual implementation requires HTTP client
// setup and is left as a future enhancement.
func ResolveNIP05Address(identifier, expectedPubkey string) error {
	// Validate format first
	if err := ValidateNIP05Identifier(identifier); err != nil {
		return err
	}
	// DNS resolution not implemented in relay - this is client-side
	return fmt.Errorf("NIP-05 DNS resolution not implemented in relay (client-side feature)")
}
