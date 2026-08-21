package nips

import (
	"fmt"
	"strings"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-36: Sensitive Content / Content Warning
// https://github.com/nostr-protocol/nips/blob/master/36.md
//
// The "content-warning" tag enables users to specify if the event's content
// needs to be approved by readers to be shown. Clients can hide the content
// until the user acts on it.
//
// Tag format: ["content-warning", "<optional reason>"]
//
// Additionally, NIP-32 l/L tags MAY be used with "content-warning" namespace
// to support further qualification and querying.

// MaxContentWarningReasonLength limits the length of the reason text.
const MaxContentWarningReasonLength = 200

// ValidateContentWarningTag validates a single content-warning tag.
// Returns nil if valid, error otherwise.
func ValidateContentWarningTag(tag nostr.Tag) error {
	if len(tag) < 1 {
		return fmt.Errorf("content-warning tag must have at least 1 element")
	}
	if tag[0] != "content-warning" {
		return fmt.Errorf("tag must be 'content-warning', got %q", tag[0])
	}
	// Reason is optional but if present must be reasonable length
	if len(tag) >= 2 {
		reason := tag[1]
		if len(reason) > MaxContentWarningReasonLength {
			return fmt.Errorf("content-warning reason exceeds max length of %d chars", MaxContentWarningReasonLength)
		}
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("content-warning reason cannot be empty whitespace")
		}
	}
	return nil
}

// HasContentWarning returns true if the event has a content-warning tag.
func HasContentWarning(evt *nostr.Event) bool {
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "content-warning" {
			return true
		}
	}
	return false
}

// GetContentWarningReason returns the reason from the first content-warning tag.
// Returns empty string if no reason provided.
func GetContentWarningReason(evt *nostr.Event) string {
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "content-warning" {
			if len(tag) >= 2 {
				return tag[1]
			}
			return ""
		}
	}
	return ""
}

// ValidateNIP36Event validates NIP-36 content-warning tags on an event.
// This is a permissive validator — content-warning is optional and any
// well-formed content-warning tag is acceptable.
func ValidateNIP36Event(evt *nostr.Event) error {
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "content-warning" {
			if err := ValidateContentWarningTag(tag); err != nil {
				return err
			}
		}
	}
	return nil
}
