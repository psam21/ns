package nips

import (
	"fmt"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-C7: Chats
// https://github.com/nostr-protocol/nips/blob/master/C7.md
//
// A chat message is a kind 9 event with arbitrary content.
// Replies use a "q" tag to quote the parent event:
//
//	["q", <event-id>, <relay-url>, <pubkey>]
//
// Clients that render a "chat view" as a stream of ordered events MUST only
// fetch kind 9 events to prevent missing context. Other content types MAY be
// quoted within a kind 9 following NIP-18.

// ChatMessageKind is the kind for chat messages.
const ChatMessageKind = 9

// MaxChatContentLength limits chat message content length.
const MaxChatContentLength = 10000

// ValidateQTag validates a "q" (quote) tag.
// Format: ["q", <event-id>, <relay-url>, <pubkey>]
func ValidateQTag(tag nostr.Tag) error {
	if len(tag) < 2 {
		return fmt.Errorf("q tag must have at least 2 elements, got %d", len(tag))
	}
	if tag[0] != "q" {
		return fmt.Errorf("tag must be 'q', got %q", tag[0])
	}
	eventID := tag[1]
	if len(eventID) != 64 {
		return fmt.Errorf("q tag event id must be 64 hex chars, got %d", len(eventID))
	}
	for _, c := range eventID {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return fmt.Errorf("q tag event id must be lowercase hex: %s", eventID)
		}
	}
	// relay-url is optional but if present must be a non-empty string
	if len(tag) >= 3 && tag[2] != "" {
		// Just check it's not empty - relay URL format validation is lax
	}
	// pubkey is optional but if present must be 64 hex chars
	if len(tag) >= 4 && tag[3] != "" {
		pk := tag[3]
		if len(pk) != 64 {
			return fmt.Errorf("q tag pubkey must be 64 hex chars, got %d", len(pk))
		}
		for _, c := range pk {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				return fmt.Errorf("q tag pubkey must be lowercase hex: %s", pk)
			}
		}
	}
	return nil
}

// IsChatMessage returns true if the event is a chat message (kind 9).
func IsChatMessage(evt *nostr.Event) bool {
	return evt.Kind == ChatMessageKind
}

// GetChatQuotedEvents returns the event IDs of all quoted events in the chat.
func GetChatQuotedEvents(evt *nostr.Event) []string {
	var ids []string
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "q" {
			ids = append(ids, tag[1])
		}
	}
	return ids
}

// ValidateNIPC7Event validates a NIP-C7 chat message event (kind 9).
func ValidateNIPC7Event(evt *nostr.Event) error {
	if evt.Kind != ChatMessageKind {
		return fmt.Errorf("not a NIP-C7 event kind: %d", evt.Kind)
	}
	if len(evt.Content) > MaxChatContentLength {
		return fmt.Errorf("chat content exceeds max length of %d chars", MaxChatContentLength)
	}
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "q" {
			if err := ValidateQTag(tag); err != nil {
				return err
			}
		}
	}
	return nil
}
