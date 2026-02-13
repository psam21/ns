package nips

import (
	"fmt"
	"strconv"

	nostr "github.com/nbd-wtf/go-nostr"
)

// CountLeadingZeroBits counts the number of leading zero bits in a hex event ID.
// NIP-13 defines difficulty as the number of leading zero bits in the event ID.
func CountLeadingZeroBits(hexID string) int {
	count := 0
	for _, c := range hexID {
		nibble := hexToNibble(byte(c))
		if nibble < 0 {
			break
		}
		if nibble == 0 {
			count += 4
		} else {
			// Count leading zeros in this nibble (0-3)
			for bit := 3; bit >= 0; bit-- {
				if nibble&(1<<uint(bit)) != 0 {
					return count
				}
				count++
			}
		}
	}
	return count
}

// hexToNibble converts a hex character to its 4-bit value, returns -1 for invalid chars.
func hexToNibble(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c - 'a' + 10)
	case c >= 'A' && c <= 'F':
		return int(c - 'A' + 10)
	default:
		return -1
	}
}

// GetNonceCommitment extracts the committed target difficulty from a nonce tag.
// Returns (targetDifficulty, hasCommitment).
// Nonce tag format: ["nonce", "<counter>", "<target_difficulty>"]
func GetNonceCommitment(evt nostr.Event) (int, bool) {
	for _, tag := range evt.Tags {
		if len(tag) >= 3 && tag[0] == "nonce" {
			target, err := strconv.Atoi(tag[2])
			if err == nil && target > 0 {
				return target, true
			}
		}
	}
	return 0, false
}

// ValidatePoW validates an event's proof of work against a minimum difficulty requirement.
// If minDifficulty is 0, PoW is not required (but committed targets are still enforced).
// Returns nil if valid, or an error describing the failure.
func ValidatePoW(evt nostr.Event, minDifficulty int) error {
	actualDifficulty := CountLeadingZeroBits(evt.ID)
	committedTarget, hasCommitment := GetNonceCommitment(evt)

	// If relay requires minimum PoW difficulty
	if minDifficulty > 0 {
		if !hasCommitment {
			return fmt.Errorf("pow: relay requires minimum difficulty %d, but event has no nonce tag", minDifficulty)
		}
		if committedTarget < minDifficulty {
			return fmt.Errorf("pow: committed target %d is below relay minimum %d", committedTarget, minDifficulty)
		}
		if actualDifficulty < minDifficulty {
			return fmt.Errorf("pow: difficulty %d is below relay minimum %d", actualDifficulty, minDifficulty)
		}
	}

	// If event has a committed target, verify actual difficulty meets it
	if hasCommitment {
		if actualDifficulty < committedTarget {
			return fmt.Errorf("pow: actual difficulty %d does not meet committed target %d", actualDifficulty, committedTarget)
		}
	}

	return nil
}
