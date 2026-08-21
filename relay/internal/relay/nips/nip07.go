package nips

import (
	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-07: window.nostr capability for web browsers
// https://github.com/nostr-protocol/nips/blob/master/07.md
//
// NIP-07 is a browser-side specification that defines the `window.nostr`
// object exposed by browser extensions. It allows web clients to:
//   - getPublicKey(): Get the user's public key as hex
//   - signEvent(event): Sign an event with the user's private key
//   - nip04.encrypt/decrypt: NIP-04 encryption (deprecated)
//   - nip44.encrypt/decrypt: NIP-44 encryption
//
// As a relay, our role is limited to:
//   1. Advertising NIP-07 support in our NIP-11 relay information document
//   2. Validating events that come from NIP-07 signers (signature is verified
//      by the standard NIP-01 event validation)
//
// There are no relay-side validators specific to NIP-07 since the spec is
// purely client/browser-side. This file exists to document NIP-07 support
// and provide a stub for future relay-side enhancements.

// NIP07SupportedMethods lists the methods defined by the window.nostr object.
var NIP07SupportedMethods = []string{
	"getPublicKey",
	"signEvent",
	"nip04.encrypt",
	"nip04.decrypt",
	"nip44.encrypt",
	"nip44.decrypt",
}

// IsNIP07CompatibleEvent checks if an event is compatible with NIP-07 signing.
// NIP-07 signers can sign any event, but typically they sign kind 0 (metadata)
// and kind 1 (text notes). This function is a placeholder for future
// NIP-07-specific validation logic.
func IsNIP07CompatibleEvent(evt *nostr.Event) bool {
	// NIP-07 can sign any event kind, so all events are compatible
	_ = evt
	return true
}
