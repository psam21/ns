package nips

import (
	"fmt"
	"strings"

	"github.com/Shugur-Network/relay/internal/logger"
	"github.com/Shugur-Network/relay/internal/relay/nips/common"
	nostr "github.com/nbd-wtf/go-nostr"
	"go.uber.org/zap"
)

// NIP-09: Event Deletion
// https://github.com/nostr-protocol/nips/blob/master/09.md

// ValidateEventDeletion validates NIP-09 event deletion events (kind 5)
func ValidateEventDeletion(evt *nostr.Event) error {
	return common.ValidateEventWithCallback(
		evt,
		"09",             // NIP number
		5,                // Expected event kind
		"event deletion", // Event name for logging
		func(helper *common.ValidationHelper, event *nostr.Event) error {
			// Must have at least one "e" or "a" tag referencing events to delete (finding #7)
			hasTag := false
			for _, tag := range event.Tags {
				if len(tag) >= 1 && (tag[0] == "e" || tag[0] == "a") {
					hasTag = true
					break
				}
			}
			if !hasTag {
				return helper.ErrorFormatter.FormatError("deletion event must reference at least one event with 'e' or 'a' tag")
			}

			// Validate event ID format in "e" tags and count them
			eventCount := 0
			for _, tag := range event.Tags {
				if len(tag) >= 2 && tag[0] == "e" {
					eventCount++
					if err := helper.ValidateEventID(tag[1]); err != nil {
						logger.Warn("NIP-09: Invalid event ID in 'e' tag",
							zap.String("deletion_event_id", event.ID),
							zap.String("invalid_event_id", tag[1]))
						return helper.FormatTagError("e", "invalid event ID: %v", err)
					}
				} else if len(tag) >= 2 && tag[0] == "a" {
					eventCount++
					// Addressable event reference format: kind:pubkey:d-tag
					parts := strings.Split(tag[1], ":")
					if len(parts) < 2 || len(parts) > 3 {
						return helper.FormatTagError("a", "invalid addressable event reference: %s (expected kind:pubkey:d-tag)", tag[1])
					}
				}
			}

			// Log the count of target events for debugging
			logger.Debug("NIP-09: Valid deletion event",
				zap.String("event_id", event.ID),
				zap.Int("target_events", eventCount))

			return nil
		},
	)
}

// ValidateDeletionAuth returns an error if any "e"‑tagged event in `tags`
// is ALREADY KNOWN (lookup(id) ⇒ author) and its author differs from `deleter`.
func ValidateDeletionAuth(
	tags []nostr.Tag,
	deleter string,
	lookup func(evt string) (event nostr.Event, ok bool),
) error {
	for _, t := range tags {
		if len(t) >= 2 && t[0] == "e" {
			id := t[1]
			if event, ok := lookup(id); !ok {
				return fmt.Errorf("deletion target event not found: %s", id)
			} else if event.PubKey != deleter {
				return fmt.Errorf("unauthorized delete of %s", id)
			}
		} else if len(t) >= 2 && t[0] == "a" {
			// Addressable event reference: kind:pubkey:d-tag
			// Authorization requires the event author matches the deleter
			ref := t[1]
			parts := strings.Split(ref, ":")
			if len(parts) >= 2 {
				author := parts[1]
				if author != deleter {
					return fmt.Errorf("unauthorized delete of addressable event %s (author %s != %s)", ref, author, deleter)
				}
			}
		}
	}
	return nil
}

func IsDeletionEvent(evt nostr.Event) bool {
	return evt.Kind == 5
}
