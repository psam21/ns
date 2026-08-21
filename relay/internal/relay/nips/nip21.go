package nips

import (
	"fmt"
	"regexp"
	"strings"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-21: nostr: URI scheme
// https://github.com/nostr-protocol/nips/blob/master/21.md
//
// NIP-21 standardizes the usage of a common URI scheme for maximum
// interoperability in the network. The scheme is "nostr:" and the
// identifiers that come after are expected to be the same as those
// defined in NIP-19 (except "nsec").
//
// Supported NIP-19 entities:
//   - npub1...: Public key
//   - nprofile1...: Profile with relay hints
//   - note1...: Event ID (deprecated, use nevent)
//   - nevent1...: Event with relay hints and author pubkey
//   - naddr1...: Addressable event (kind:pubkey:d-tag)

// nostrURIRegex21 matches nostr: URI schemes with valid NIP-19 entity prefixes.
// Per NIP-21, nsec is NOT allowed in nostr: URIs.
var nostrURIRegex21 = regexp.MustCompile(`^nostr:(npub1[0-9a-z]{58,}|nprofile1[0-9a-z]{58,}|nevent1[0-9a-z]{58,}|naddr1[0-9a-z]{58,})$`)

// ValidateNostrURI validates a nostr: URI string.
// Returns nil if valid, error otherwise.
func ValidateNostrURI(uri string) error {
	if uri == "" {
		return fmt.Errorf("nostr URI cannot be empty")
	}

	if !strings.HasPrefix(uri, "nostr:") {
		return fmt.Errorf("URI must start with 'nostr:' scheme")
	}

	// Check for nsec (not allowed in nostr: URIs per NIP-21)
	if strings.Contains(uri, "nsec1") {
		return fmt.Errorf("nsec is not allowed in nostr: URIs")
	}

	// Validate format
	if !nostrURIRegex21.MatchString(uri) {
		return fmt.Errorf("invalid nostr: URI format: %s", uri)
	}

	return nil
}

// ExtractNostrEntities extracts all nostr: URIs from a text string.
// Returns a slice of valid nostr: URIs found in the text.
func ExtractNostrEntities(text string) []string {
	// Find all nostr: URIs (relaxed regex for extraction)
	re := regexp.MustCompile(`nostr:(npub1[0-9a-z]{58,}|nprofile1[0-9a-z]{58,}|nevent1[0-9a-z]{58,}|naddr1[0-9a-z]{58,})`)
	matches := re.FindAllString(text, -1)
	return matches
}

// ValidateNIP21Content validates that any nostr: URI references in the event
// content are well-formed. This is used as part of content validation for
// events with readable text content (kind 1, kind 30023, etc.).
func ValidateNIP21Content(evt *nostr.Event) error {
	entities := ExtractNostrEntities(evt.Content)
	for _, entity := range entities {
		if err := ValidateNostrURI(entity); err != nil {
			return fmt.Errorf("invalid nostr: URI in content: %w", err)
		}
	}
	return nil
}

// NostrEntityType returns the type of a NIP-19 entity (npub, nprofile, nevent, naddr).
// Returns empty string if not a valid entity type.
func NostrEntityType(entity string) string {
	for _, prefix := range []string{"npub1", "nprofile1", "nevent1", "naddr1", "note1"} {
		if strings.HasPrefix(entity, prefix) {
			return strings.TrimSuffix(prefix, "1")
		}
	}
	return ""
}
