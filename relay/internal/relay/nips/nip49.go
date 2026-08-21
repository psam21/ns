package nips

import (
	"fmt"
	"regexp"

	"github.com/nbd-wtf/go-nostr/nip19"
)

// NIP-49: Private Key Encryption (ncryptsec)
// https://github.com/nostr-protocol/nips/blob/master/49.md
//
// NIP-49 defines a method by which clients can encrypt (and decrypt) a user's
// private key with a password. The encrypted format uses:
//   - scrypt for password-based key derivation
//   - XChaCha20-Poly1305 for symmetric encryption
//   - bech32 encoding with "ncryptsec" prefix
//
// Format (91 bytes before bech32):
//   VERSION_NUMBER (1) | LOG_N (1) | SALT (16) | NONCE (24) | KEY_SECURITY (1) | CIPHERTEXT (48)
//
// The relay does not perform encryption/decryption but can validate ncryptsec
// strings when they appear in events or other contexts.

// ncryptsecRegex matches valid ncryptsec bech32 strings.
// ncryptsec1 followed by bech32 characters.
var ncryptsecRegex = regexp.MustCompile(`^ncryptsec1[0-9a-z]{100,}$`)

// NIP49Version is the current version byte.
const NIP49Version byte = 0x02

// KeySecurityByte values per NIP-49.
const (
	KeySecurityInsecure     byte = 0x00
	KeySecuritySecure       byte = 0x01
	KeySecurityUntracked    byte = 0x02
)

// RecommendedLogN is the recommended scrypt log_n value (2^16 = 64MiB).
const RecommendedLogN byte = 16

// ValidateNcryptsec validates that a string is a well-formed ncryptsec
// bech32-encoded encrypted private key.
// Returns the decoded bytes if valid, error otherwise.
func ValidateNcryptsec(ncryptsec string) ([]byte, error) {
	if ncryptsec == "" {
		return nil, fmt.Errorf("ncryptsec cannot be empty")
	}
	prefix, decoded, err := nip19.Decode(ncryptsec)
	if err != nil {
		return nil, fmt.Errorf("failed to decode ncryptsec: %w", err)
	}
	if prefix != "ncryptsec" {
		return nil, fmt.Errorf("invalid prefix: expected 'ncryptsec', got %q", prefix)
	}
	// Type assert to []byte
	payload, ok := decoded.([]byte)
	if !ok {
		return nil, fmt.Errorf("ncryptsec payload is not bytes")
	}
	// Per NIP-49: output prior to bech32 encoding should be 91 bytes long.
	if len(payload) != 91 {
		return nil, fmt.Errorf("ncryptsec payload must be 91 bytes, got %d", len(payload))
	}
	return payload, nil
}

// IsNcryptsec returns true if the string appears to be a ncryptsec bech32 string.
func IsNcryptsec(s string) bool {
	return ncryptsecRegex.MatchString(s)
}

// ParseNcryptsecMetadata extracts metadata from a ncryptsec payload without
// performing decryption.
// Returns version, log_n, salt, nonce, key_security bytes.
func ParseNcryptsecMetadata(payload []byte) (version, logN, keySecurity byte, salt, nonce []byte, err error) {
	if len(payload) != 91 {
		return 0, 0, 0, nil, nil, fmt.Errorf("ncryptsec payload must be 91 bytes, got %d", len(payload))
	}
	version = payload[0]
	logN = payload[1]
	salt = payload[2:18]   // 16 bytes
	nonce = payload[18:42] // 24 bytes
	keySecurity = payload[42]
	// CIPHERTEXT = payload[43:91] (48 bytes) - skipped for metadata
	return version, logN, keySecurity, salt, nonce, nil
}

// ValidateNcryptsecFormat validates the format of a ncryptsec string including
// version and log_n checks.
func ValidateNcryptsecFormat(ncryptsec string) error {
	payload, err := ValidateNcryptsec(ncryptsec)
	if err != nil {
		return err
	}
	version, logN, keySecurity, _, _, err := ParseNcryptsecMetadata(payload)
	if err != nil {
		return err
	}
	if version != NIP49Version {
		return fmt.Errorf("unsupported ncryptsec version: 0x%02x (expected 0x%02x)", version, NIP49Version)
	}
	if logN < 16 || logN > 22 {
		return fmt.Errorf("log_n must be between 16 and 22, got %d", logN)
	}
	if keySecurity > KeySecurityUntracked {
		return fmt.Errorf("invalid key security byte: 0x%02x", keySecurity)
	}
	return nil
}
