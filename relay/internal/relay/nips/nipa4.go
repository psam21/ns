package nips

import (
	"fmt"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-A4: Public Messages
// https://github.com/nostr-protocol/nips/blob/master/A4.md
//
// NIP-A4 defines kind 24 as a simple plaintext message to one or more Nostr users.
// The .content contains the message. p tags identify one or more receivers.
//
// There are no message chains - no thread root, no chatroom concept.
// e tags MUST NOT be used.

// PublicMessageKind is the kind for public messages.
const PublicMessageKind = 24

// MaxPublicMessageLength limits the length of public messages.
const MaxPublicMessageLength = 10000

// ValidateNIPA4Event validates a NIP-A4 public message event (kind 24).
// Requirements:
//   - Must have at least one p tag (receiver)
//   - Must NOT have any e tags (no threading)
func ValidateNIPA4Event(evt *nostr.Event) error {
	if evt.Kind != PublicMessageKind {
		return fmt.Errorf("not a NIP-A4 event kind: %d", evt.Kind)
	}
	if len(evt.Content) > MaxPublicMessageLength {
		return fmt.Errorf("public message content exceeds max length of %d chars", MaxPublicMessageLength)
	}

	hasP := false
	for _, tag := range evt.Tags {
		if len(tag) < 1 {
			continue
		}
		switch tag[0] {
		case "p":
			hasP = true
			if len(tag) < 2 {
				return fmt.Errorf("p tag must have a pubkey")
			}
			if !hex64Regex.MatchString(tag[1]) {
				return fmt.Errorf("p tag pubkey must be 64 lowercase hex chars: %s", tag[1])
			}
		case "e":
			// e tags MUST NOT be used per NIP-A4
			return fmt.Errorf("NIP-A4 public messages MUST NOT include e tags (no threading)")
		}
	}

	if !hasP {
		return fmt.Errorf("NIP-A4 public message MUST include at least one p tag (receiver)")
	}
	return nil
}

// GetPublicMessageReceivers returns the pubkeys of all receivers.
func GetPublicMessageReceivers(evt *nostr.Event) []string {
	var receivers []string
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			receivers = append(receivers, tag[1])
		}
	}
	return receivers
}
