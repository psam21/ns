package nips

import (
	"fmt"
	"strings"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-48: Bridged Events
// https://github.com/nostr-protocol/nips/blob/master/48.md
//
// Nostr events bridged from other protocols (ActivityPub, AT Protocol, RSS, etc.)
// can link back to the source object by including a "proxy" tag:
//
//	["proxy", <id>, <protocol>]
//
// Where:
//   - <id> is the ID of the source object (varies by protocol, must be unique)
//   - <protocol> is the protocol name (e.g., "activitypub", "atproto", "rss", "web")
//
// Proxy tags may be added to any event kind, indicating the event did not
// originate on Nostr.

// SupportedBridgedProtocols lists the protocols defined in NIP-48.
var SupportedBridgedProtocols = map[string]bool{
	"activitypub": true,
	"atproto":     true,
	"rss":         true,
	"web":         true,
}

// MaxProxyIDLength limits the length of a proxy ID.
const MaxProxyIDLength = 2048

// ValidateProxyTag validates a single proxy tag.
// Returns nil if valid, error otherwise.
func ValidateProxyTag(tag nostr.Tag) error {
	if len(tag) != 3 {
		return fmt.Errorf("proxy tag must have exactly 3 elements [proxy, id, protocol], got %d", len(tag))
	}
	if tag[0] != "proxy" {
		return fmt.Errorf("tag must be 'proxy', got %q", tag[0])
	}
	id := tag[1]
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("proxy id cannot be empty")
	}
	if len(id) > MaxProxyIDLength {
		return fmt.Errorf("proxy id exceeds max length of %d chars", MaxProxyIDLength)
	}
	protocol := tag[2]
	if strings.TrimSpace(protocol) == "" {
		return fmt.Errorf("proxy protocol cannot be empty")
	}
	// Protocol should be lowercase ASCII letters
	for _, r := range protocol {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return fmt.Errorf("proxy protocol must be lowercase alphanumeric with - or _, got %q", protocol)
		}
	}
	return nil
}

// HasProxyTag returns true if the event has at least one proxy tag.
func HasProxyTag(evt *nostr.Event) bool {
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "proxy" {
			return true
		}
	}
	return false
}

// GetProxyTags returns all proxy tags on the event.
func GetProxyTags(evt *nostr.Event) []nostr.Tag {
	var proxies []nostr.Tag
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "proxy" {
			proxies = append(proxies, tag)
		}
	}
	return proxies
}

// GetProxyProtocols returns the unique protocols referenced by proxy tags.
func GetProxyProtocols(evt *nostr.Event) []string {
	seen := make(map[string]bool)
	var protocols []string
	for _, tag := range evt.Tags {
		if len(tag) >= 3 && tag[0] == "proxy" {
			protocol := tag[2]
			if !seen[protocol] {
				seen[protocol] = true
				protocols = append(protocols, protocol)
			}
		}
	}
	return protocols
}

// ValidateNIP48Event validates NIP-48 proxy tags on an event.
// Proxy tags may appear on any event kind. If present, they must be well-formed.
func ValidateNIP48Event(evt *nostr.Event) error {
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "proxy" {
			if err := ValidateProxyTag(tag); err != nil {
				return err
			}
		}
	}
	return nil
}
