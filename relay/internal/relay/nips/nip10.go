package nips

import (
	"fmt"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-10: Text Notes and Threads
// https://github.com/nostr-protocol/nips/blob/master/10.md
//
// NIP-10 defines kind:1 as a simple plaintext note and specifies how reply
// threads work using "e" tags (with markers) and "p" tags.
//
// Marked "e" tags (PREFERRED):
//   ["e", <event-id>, <relay-url>, <marker>, <pubkey>]
//   - marker: "reply" or "root"
//
// Deprecated positional "e" tags:
//   - No "e" tag: not a reply
//   - One "e" tag: reply to that event
//   - Two "e" tags: [root-id, reply-id]
//   - Many "e" tags: [root-id, mention-id..., reply-id]
//
// "p" tags: Used to record who is involved in a reply thread.

// validReplyMarkers lists the valid markers for "e" tags in NIP-10.
var validReplyMarkers = map[string]bool{
	"reply": true,
	"root":  true,
}

// ValidateNIP10Reply validates a kind:1 text note for NIP-10 compliance.
// Checks:
//   - "e" tags have valid format (event-id, optional relay-url, optional marker, optional pubkey)
//   - Markers are valid ("reply" or "root")
//   - "p" tags have valid pubkey format
//   - At most one "root" marker and one "reply" marker
func ValidateNIP10Reply(evt *nostr.Event) error {
	if evt.Kind != 1 {
		return nil
	}

	rootCount := 0
	replyCount := 0

	for _, tag := range evt.Tags {
		if len(tag) < 2 {
			continue
		}

		switch tag[0] {
		case "e":
			// Validate event-id format (64 hex chars)
			if len(tag[1]) != 64 {
				return fmt.Errorf("invalid 'e' tag event-id: must be 64 hex chars, got %d", len(tag[1]))
			}
			if !isHexString51(tag[1]) {
				return fmt.Errorf("invalid 'e' tag event-id: not hex: %s", tag[1])
			}

			// Validate marker if present (tag[3])
			if len(tag) >= 4 && tag[3] != "" {
				marker := tag[3]
				if !validReplyMarkers[marker] {
					return fmt.Errorf("invalid 'e' tag marker: %s (must be 'reply' or 'root')", marker)
				}
				if marker == "root" {
					rootCount++
				} else if marker == "reply" {
					replyCount++
				}
				if rootCount > 1 {
					return fmt.Errorf("multiple 'root' markers in 'e' tags")
				}
				if replyCount > 1 {
					return fmt.Errorf("multiple 'reply' markers in 'e' tags")
				}
			}

			// Validate pubkey if present (tag[4])
			if len(tag) >= 5 && tag[4] != "" {
				if len(tag[4]) != 64 {
					return fmt.Errorf("invalid 'e' tag pubkey: must be 64 hex chars")
				}
				if !isHexString51(tag[4]) {
					return fmt.Errorf("invalid 'e' tag pubkey: not hex: %s", tag[4])
				}
			}

		case "p":
			// Validate pubkey format
			if len(tag[1]) != 64 {
				return fmt.Errorf("invalid 'p' tag pubkey: must be 64 hex chars")
			}
			if !isHexString51(tag[1]) {
				return fmt.Errorf("invalid 'p' tag pubkey: not hex: %s", tag[1])
			}
		}
	}

	return nil
}

// IsReply checks if a kind:1 event is a reply (has at least one "e" tag).
func IsReply(evt *nostr.Event) bool {
	if evt.Kind != 1 {
		return false
	}
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "e" {
			return true
		}
	}
	return false
}

// GetReplyRoot returns the event ID of the root of the reply thread, if any.
// Returns empty string if no root is found.
func GetReplyRoot(evt *nostr.Event) string {
	if evt.Kind != 1 {
		return ""
	}

	// First, look for marked "root" e-tag
	for _, tag := range evt.Tags {
		if len(tag) >= 4 && tag[0] == "e" && tag[3] == "root" {
			return tag[1]
		}
	}

	// Fallback: positional e-tags (deprecated)
	// Two e-tags: [root-id, reply-id]
	eTags := []string{}
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "e" {
			eTags = append(eTags, tag[1])
		}
	}

	if len(eTags) == 2 {
		// Positional: first is root, second is reply
		return eTags[0]
	}
	if len(eTags) > 2 {
		// Positional: first is root, last is reply, middle are mentions
		return eTags[0]
	}

	return ""
}

// GetReplyParent returns the event ID of the direct parent (reply), if any.
// Returns empty string if no parent is found.
func GetReplyParent(evt *nostr.Event) string {
	if evt.Kind != 1 {
		return ""
	}

	// First, look for marked "reply" e-tag
	for _, tag := range evt.Tags {
		if len(tag) >= 4 && tag[0] == "e" && tag[3] == "reply" {
			return tag[1]
		}
	}

	// Fallback: positional e-tags (deprecated)
	eTags := []string{}
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "e" {
			eTags = append(eTags, tag[1])
		}
	}

	if len(eTags) == 1 {
		// Single e-tag: reply to that event
		return eTags[0]
	}
	if len(eTags) == 2 {
		// Positional: second is reply
		return eTags[1]
	}
	if len(eTags) > 2 {
		// Positional: last is reply
		return eTags[len(eTags)-1]
	}

	return ""
}
