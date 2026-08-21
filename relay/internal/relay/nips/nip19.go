package nips

import (
	"fmt"

	"github.com/nbd-wtf/go-nostr/nip19"
)

// NIP-19: bech32-encoded entities
// https://github.com/nostr-protocol/nips/blob/master/19.md
//
// Provides encoding/decoding for:
// - npub: public key
// - nsec: private key
// - note: event ID
// - nprofile: profile pointer
// - nevent: event pointer
// - naddr: entity pointer (addressable event)

// Decode decodes a NIP-19 bech32 string into its components.
// Returns the prefix (npub, nsec, note, nprofile, nevent, naddr) and the decoded value.
func Decode(bech32string string) (string, interface{}, error) {
	prefix, value, err := nip19.Decode(bech32string)
	if err != nil {
		return "", nil, fmt.Errorf("failed to decode NIP-19 string: %w", err)
	}
	return prefix, value, nil
}

// EncodePubkey encodes a public key as npub
func EncodePubkey(pubkey string) (string, error) {
	if len(pubkey) != 64 {
		return "", fmt.Errorf("pubkey must be 64 hex characters")
	}
	return nip19.EncodePublicKey(pubkey)
}

// EncodePrivateKey encodes a private key as nsec
func EncodePrivateKey(privkey string) (string, error) {
	if len(privkey) != 64 {
		return "", fmt.Errorf("private key must be 64 hex characters")
	}
	return nip19.EncodePrivateKey(privkey)
}

// EncodeEventID encodes an event ID as note
func EncodeEventID(eventID string) (string, error) {
	if len(eventID) != 64 {
		return "", fmt.Errorf("event ID must be 64 hex characters")
	}
	return nip19.EncodeNote(eventID)
}

// EncodeProfilePointer encodes a profile pointer as nprofile
func EncodeProfilePointer(pubkey string, relays []string) (string, error) {
	if len(pubkey) != 64 {
		return "", fmt.Errorf("pubkey must be 64 hex characters")
	}
	return nip19.EncodeProfile(pubkey, relays)
}

// EncodeEventPointer encodes an event pointer as nevent
func EncodeEventPointer(eventID string, relays []string, author string, kind int) (string, error) {
	if len(eventID) != 64 {
		return "", fmt.Errorf("event ID must be 64 hex characters")
	}
	return nip19.EncodeEvent(eventID, relays, author)
}

// EncodeEntityPointer encodes an addressable event pointer as naddr
func EncodeEntityPointer(kind int, pubkey string, dTag string, relays []string) (string, error) {
	if len(pubkey) != 64 {
		return "", fmt.Errorf("pubkey must be 64 hex characters")
	}
	return nip19.EncodeEntity(pubkey, kind, dTag, relays)
}

// IsNIP19Prefix checks if a string is a valid NIP-19 prefix
func IsNIP19Prefix(prefix string) bool {
	switch prefix {
	case "npub", "nsec", "note", "nprofile", "nevent", "naddr":
		return true
	default:
		return false
	}
}

// ValidateNIP19String validates a NIP-19 encoded string
func ValidateNIP19String(bech32string string) error {
	_, _, err := Decode(bech32string)
	return err
}