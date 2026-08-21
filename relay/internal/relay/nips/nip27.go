package nips

import (
	"fmt"
	"regexp"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-27: Text Note References
// https://github.com/nostr-protocol/nips/blob/master/27.md
//
// NIP-27 standardizes the treatment given by clients to inline references of
// other events and profiles inside the .content of any event that has readable
// text (such as kinds 1 and 30023).
//
// When creating an event, clients should include mentions using NIP-21 codes
// (e.g., "nostr:nprofile1qqs...", "nostr:nevent1...", "nostr:naddr1...",
// "nostr:npub1...", "nostr:nsec1...", "nostr:note1...").
//
// Including NIP-18 quote tags (["q", "<event-id> or <event-address>", ...])
// for each reference is optional.

// nostrURIRegex matches nostr: URI schemes in event content.
// It captures the bech32 entity (npub1, nsec1, note1, nprofile1, nevent1, naddr1).
var nostrURIRegex = regexp.MustCompile(`nostr:(npub1[0-9a-z]{58,}|nsec1[0-9a-z]{58,}|note1[0-9a-z]{58,}|nprofile1[0-9a-z]{58,}|nevent1[0-9a-z]{58,}|naddr1[0-9a-z]{58,})`)

// ValidateNIP27References validates that any inline nostr: URI references in
// the event content are well-formed. This is a soft validation - we only check
// that references follow the NIP-21 bech32 format. We do not require that
// references be present (they are optional).
//
// This validator should be applied to events with readable text content
// (kind 1 text notes, kind 30023 long-form articles, etc.).
func ValidateNIP27References(evt *nostr.Event) error {
	// Only validate events with readable text content
	if !hasReadableContent(evt.Kind) {
		return nil
	}

	// Find all nostr: URIs in content
	matches := nostrURIRegex.FindAllStringSubmatch(evt.Content, -1)
	if len(matches) == 0 {
		// No references - this is valid (references are optional)
		return nil
	}

	// Validate each reference is well-formed
	for _, match := range matches {
		entity := match[1]
		if !isValidBech32Entity(entity) {
			return fmt.Errorf("invalid NIP-27 reference: malformed bech32 entity '%s'", entity)
		}
	}

	return nil
}

// hasReadableContent returns true if the event kind typically has readable
// text content that may contain NIP-27 inline references.
func hasReadableContent(kind int) bool {
	switch kind {
	case 0, // Profile metadata
		1,     // Short text note
		30023: // Long-form article
		return true
	}
	return false
}

// isValidBech32Entity performs a basic structural check on a bech32 entity.
// Full bech32 validation (checksum) is not performed here as that requires
// additional dependencies and the relay should accept any well-formed prefix.
func isValidBech32Entity(entity string) bool {
	if len(entity) < 6 {
		return false
	}

	// Check valid prefix
	validPrefixes := []string{"npub1", "nsec1", "note1", "nprofile1", "nevent1", "naddr1"}
	for _, prefix := range validPrefixes {
		if len(entity) > len(prefix) && entity[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
