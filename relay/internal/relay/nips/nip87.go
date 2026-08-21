package nips

import (
	"fmt"
	"regexp"
	"strings"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-87: Cashu and Fedimint Discoverability
// https://github.com/nostr-protocol/nips/blob/master/87.md
//
// NIP-87 describes a way to discover ecash mints (Cashu and Fedimint), their
// capabilities, and people who recommend them.
//
// Event kinds:
//   - 38172: Cashu mint announcement (replaceable, requires d tag = mint pubkey)
//   - 38173: Fedimint announcement (replaceable, requires d tag = federation id)
//   - 38000: Mint recommendation (parameterized replaceable, d tag = mint event id)

// CashuMintKind is the kind for cashu mint announcements.
const CashuMintKind = 38172

// FedimintKind is the kind for fedimint announcements.
const FedimintKind = 38173

// MintRecommendationKind is the kind for mint recommendations.
const MintRecommendationKind = 38000

// CashuMintPubkeyRegex matches valid cashu mint pubkey format (used as d tag).
var CashuMintPubkeyRegex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// FedimintIDRegex matches fedimint federation ID format (fed11...).
var FedimintIDRegex = regexp.MustCompile(`^fed1[0-9a-z]{6,}$`)

// ValidNets lists valid network identifiers.
var ValidNets = map[string]bool{
	"mainnet": true,
	"testnet": true,
	"signet":  true,
	"regtest": true,
}

// IsNIP87Kind returns true if the event kind is a NIP-87 ecash mint kind.
func IsNIP87Kind(kind int) bool {
	return kind == CashuMintKind || kind == FedimintKind || kind == MintRecommendationKind
}

// ValidateNetsTag validates an "n" (network) tag.
func ValidateNetsTag(tag nostr.Tag) error {
	if len(tag) != 2 {
		return fmt.Errorf("n tag must have exactly 2 elements, got %d", len(tag))
	}
	if tag[0] != "n" {
		return fmt.Errorf("tag must be 'n', got %q", tag[0])
	}
	if !ValidNets[tag[1]] {
		return fmt.Errorf("invalid network: %s (must be mainnet, testnet, signet, or regtest)", tag[1])
	}
	return nil
}

// ValidateKTag validates a "k" tag (kind reference).
func ValidateKTag(tag nostr.Tag) error {
	if len(tag) != 2 {
		return fmt.Errorf("k tag must have exactly 2 elements, got %d", len(tag))
	}
	if tag[0] != "k" {
		return fmt.Errorf("tag must be 'k', got %q", tag[0])
	}
	kindStr := tag[1]
	kind := 0
	for _, c := range kindStr {
		if c < '0' || c > '9' {
			return fmt.Errorf("k tag value must be a number, got %q", kindStr)
		}
		kind = kind*10 + int(c-'0')
	}
	if !IsNIP87Kind(kind) {
		return fmt.Errorf("k tag must reference a NIP-87 kind (38172, 38173, 38000), got %d", kind)
	}
	return nil
}

// ValidateUTag validates a "u" tag (URL/invite code).
func ValidateUTag(tag nostr.Tag) error {
	if len(tag) < 2 {
		return fmt.Errorf("u tag must have at least 2 elements, got %d", len(tag))
	}
	if tag[0] != "u" {
		return fmt.Errorf("tag must be 'u', got %q", tag[0])
	}
	url := tag[1]
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("u tag value cannot be empty")
	}
	return nil
}

// ValidateNIP87CashuMint validates a kind 38172 (cashu mint) event.
func ValidateNIP87CashuMint(evt *nostr.Event) error {
	// Must have d tag = mint pubkey
	dFound := false
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "d" {
			dFound = true
			if len(tag) >= 2 {
				if !CashuMintPubkeyRegex.MatchString(tag[1]) {
					return fmt.Errorf("d tag for cashu mint must be 64-char hex pubkey, got %q", tag[1])
				}
			} else {
				return fmt.Errorf("d tag must have a value")
			}
		}
	}
	if !dFound {
		return fmt.Errorf("cashu mint (kind 38172) MUST include a d tag with the mint pubkey")
	}

	// Validate all n tags
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "n" {
			if err := ValidateNetsTag(tag); err != nil {
				return err
			}
		}
	}

	// Validate all u tags
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "u" {
			if err := ValidateUTag(tag); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateNIP87Fedimint validates a kind 38173 (fedimint) event.
func ValidateNIP87Fedimint(evt *nostr.Event) error {
	// Must have d tag = federation id
	dFound := false
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "d" {
			dFound = true
			if len(tag) >= 2 {
				if !FedimintIDRegex.MatchString(tag[1]) {
					return fmt.Errorf("d tag for fedimint must be federation id (fed11...), got %q", tag[1])
				}
			} else {
				return fmt.Errorf("d tag must have a value")
			}
		}
	}
	if !dFound {
		return fmt.Errorf("fedimint (kind 38173) MUST include a d tag with the federation id")
	}

	// Validate all n tags
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "n" {
			if err := ValidateNetsTag(tag); err != nil {
				return err
			}
		}
	}

	// Validate all u tags (invite codes)
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "u" {
			if err := ValidateUTag(tag); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateNIP87Recommendation validates a kind 38000 (mint recommendation) event.
func ValidateNIP87Recommendation(evt *nostr.Event) error {
	// Must have k tag referencing a NIP-87 kind
	kFound := false
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "k" {
			kFound = true
			if err := ValidateKTag(tag); err != nil {
				return err
			}
		}
	}
	if !kFound {
		return fmt.Errorf("mint recommendation (kind 38000) MUST include a k tag referencing 38172 or 38173")
	}

	// Must have d tag (mint event identifier)
	dFound := false
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "d" {
			dFound = true
			if len(tag) < 2 {
				return fmt.Errorf("d tag must have a value")
			}
		}
	}
	if !dFound {
		return fmt.Errorf("mint recommendation (kind 38000) MUST include a d tag with the mint event identifier")
	}

	// Validate all u tags
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "u" {
			if err := ValidateUTag(tag); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateNIP87Event dispatches to the appropriate validator based on event kind.
func ValidateNIP87Event(evt *nostr.Event) error {
	switch evt.Kind {
	case CashuMintKind:
		return ValidateNIP87CashuMint(evt)
	case FedimintKind:
		return ValidateNIP87Fedimint(evt)
	case MintRecommendationKind:
		return ValidateNIP87Recommendation(evt)
	default:
		return fmt.Errorf("not a NIP-87 event kind: %d", evt.Kind)
	}
}
