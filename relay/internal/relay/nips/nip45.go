package nips

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/bits"
	"strings"
	"time"

	"github.com/Shugur-Network/relay/internal/logger"
	nostr "github.com/nbd-wtf/go-nostr"
	"go.uber.org/zap"
)

// NIP-45: COUNT Command with HyperLogLog support
// https://github.com/nostr-protocol/nips/blob/master/45.md

const (
	// CountTimeout is the maximum time allowed for a COUNT operation
	CountTimeout = 5 * time.Second
	// HLLRegisters is the number of HyperLogLog registers (256)
	HLLRegisters = 256
	// HLLHexLength is the hex-encoded length of 256 registers (each 1 byte)
	HLLHexLength = 512
)

// CountCommand represents a parsed COUNT command
type CountCommand struct {
	SubID  string
	Filter nostr.Filter
}

// CountResponse represents the response to a COUNT command
type CountResponse struct {
	Count       int64  `json:"count"`
	Approximate *bool  `json:"approximate,omitempty"`
	HLL         string `json:"hll,omitempty"`
}

// ParseCountCommand parses a COUNT command from raw message array
// Note: This function expects the filter to be parsed externally using parseFilterFromRaw
func ParseCountCommand(arr []interface{}) (*CountCommand, error) {
	// Validate array length
	if len(arr) < 3 {
		return nil, fmt.Errorf("COUNT command missing subscription ID or filter")
	}

	// Extract subscription ID
	subID, ok := arr[1].(string)
	if !ok || subID == "" {
		return nil, fmt.Errorf("COUNT command subscription ID must be a string")
	}

	// The filter parsing will be handled externally
	return &CountCommand{
		SubID: subID,
	}, nil
}

// ValidateCountFilter validates a filter for COUNT operations
func ValidateCountFilter(filter nostr.Filter) error {
	if len(filter.Authors) > 100 {
		return fmt.Errorf("too many authors in filter (max 100)")
	}
	if len(filter.Kinds) > 20 {
		return fmt.Errorf("too many kinds in filter (max 20)")
	}
	for _, kind := range filter.Kinds {
		if kind < 0 || kind > 65535 {
			return fmt.Errorf("invalid event kind: %d", kind)
		}
	}
	for _, author := range filter.Authors {
		if !nostr.IsValid32ByteHex(author) {
			return fmt.Errorf("invalid author pubkey: %s", author)
		}
	}
	return nil
}

// HandleCountRequest validates and processes a COUNT request
func HandleCountRequest(ctx context.Context, subID string, filter nostr.Filter) (*CountResponse, error) {
	if err := ValidateCountFilter(filter); err != nil {
		logger.Warn("COUNT filter validation failed",
			zap.String("sub_id", subID),
			zap.Error(err))
		return nil, err
	}
	return &CountResponse{Count: 0}, nil
}

// FormatCountResponse formats a count response for sending to client
func FormatCountResponse(subID string, count int64) []interface{} {
	return []interface{}{
		"COUNT",
		subID,
		map[string]int64{"count": count},
	}
}

// --- HyperLogLog ---

// IsHLLEligible checks if a filter is eligible for HyperLogLog computation.
// Per NIP-45 spec: must have exactly one tag attribute with a single value.
func IsHLLEligible(filter nostr.Filter) bool {
	if len(filter.Tags) != 1 {
		return false
	}
	for _, values := range filter.Tags {
		if len(values) != 1 {
			return false
		}
	}
	return true
}

// ComputeHLLOffset computes the deterministic offset for HLL from a filter.
// Per NIP-45 spec:
//  1. Take the first tag attribute's first item
//  2. Get a 32-byte hex string from it (use as-is if hex, sha256 otherwise)
//  3. Take the character at position 32 of the 64-char hex string
//  4. Read as base-16 number
//  5. Add 8
func ComputeHLLOffset(filter nostr.Filter) (int, error) {
	if !IsHLLEligible(filter) {
		return 0, fmt.Errorf("filter not eligible for HLL")
	}

	var firstValue string
	for _, values := range filter.Tags {
		firstValue = values[0]
		break
	}

	// Get 64-char hex string
	var hexStr string
	if len(firstValue) == 64 && isHex64(firstValue) {
		// Event ID or pubkey — use as-is
		hexStr = strings.ToLower(firstValue)
	} else if strings.Contains(firstValue, ":") {
		// Address (kind:pubkey:d-tag) — extract pubkey part
		parts := strings.SplitN(firstValue, ":", 3)
		if len(parts) >= 2 && len(parts[1]) == 64 && isHex64(parts[1]) {
			hexStr = strings.ToLower(parts[1])
		} else {
			hexStr = sha256Hex(firstValue)
		}
	} else {
		// Anything else — hash with SHA256
		hexStr = sha256Hex(firstValue)
	}

	if len(hexStr) < 64 {
		return 0, fmt.Errorf("invalid hex string length")
	}

	// Character at position 32 (0-indexed) in the 64-char hex
	charAtPos32 := hexStr[32]
	var val int
	if charAtPos32 >= '0' && charAtPos32 <= '9' {
		val = int(charAtPos32 - '0')
	} else if charAtPos32 >= 'a' && charAtPos32 <= 'f' {
		val = int(charAtPos32-'a') + 10
	} else {
		return 0, fmt.Errorf("invalid hex character at position 32: %c", charAtPos32)
	}

	return val + 8, nil
}

// ComputeHLL computes a HyperLogLog value from a list of pubkeys and an offset.
// Returns a 512-character hex string representing 256 uint8 registers.
func ComputeHLL(pubkeys []string, offset int) string {
	registers := make([]uint8, HLLRegisters)

	for _, pk := range pubkeys {
		pkBytes, err := hex.DecodeString(pk)
		if err != nil || len(pkBytes) < 32 {
			continue
		}

		// Register index: the byte at position `offset` of the pubkey
		if offset >= len(pkBytes) {
			continue
		}
		ri := pkBytes[offset]

		// Count leading zero bits starting at position offset+1
		lzCount := countLeadingZeroBitsFromOffset(pkBytes, offset+1) + 1

		if lzCount > registers[ri] {
			registers[ri] = lzCount
		}
	}

	return hex.EncodeToString(registers)
}

// countLeadingZeroBitsFromOffset counts the number of leading zero bits
// in the byte slice starting from the given byte offset.
func countLeadingZeroBitsFromOffset(data []byte, byteOffset int) uint8 {
	var count uint8
	for i := byteOffset; i < len(data); i++ {
		if data[i] == 0 {
			count += 8
		} else {
			count += uint8(bits.LeadingZeros8(data[i]))
			break
		}
	}
	// Cap at 255 (uint8 max)
	if count > 255 {
		count = 255
	}
	return count
}

func isHex64(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
