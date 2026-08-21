package nips

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-69: Peer-to-peer Order events
// https://github.com/nostr-protocol/nips/blob/master/69.md
//
// SPEC UPDATE (9 months ago): Added order expiration support
// Added "expires_at" and "expiration" tags for order expiration handling.
//
// Event kind: 38383 (P2P Order)

// ValidateP2POrder validates NIP-69 P2P Order events (kind 38383)
func ValidateP2POrder(evt *nostr.Event) error {
	if evt.Kind != 38383 {
		return fmt.Errorf("invalid event kind for P2P order: %d", evt.Kind)
	}

	// Must have "d" tag for addressable event
	hasDTag := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "d" {
			hasDTag = true
			break
		}
	}

	if !hasDTag {
		return fmt.Errorf("P2P order must have 'd' tag for order ID")
	}

	// Validate "k" tag (order type: sell or buy)
	hasKTag := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "k" {
			if tag[1] != "sell" && tag[1] != "buy" {
				return fmt.Errorf("invalid order type in 'k' tag: %s (must be 'sell' or 'buy')", tag[1])
			}
			break
		}
	}

	if !hasKTag {
		return fmt.Errorf("P2P order must have 'k' tag (sell or buy)")
	}

	// Validate "f" tag (currency - ISO 4217)
	hasFTag := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "f" {
			if len(tag[1]) != 3 {
				return fmt.Errorf("invalid currency in 'f' tag: %s (must be ISO 4217 3-letter code)", tag[1])
			}
			break
		}
	}

	if !hasFTag {
		return fmt.Errorf("P2P order must have 'f' tag (currency)")
	}

	// Validate "s" tag (status)
	hasSTag := false
	validStatuses := map[string]bool{
		"pending":     true,
		"canceled":    true,
		"in-progress": true,
		"success":     true,
		"expired":     true,
	}
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "s" {
			if !validStatuses[tag[1]] {
				return fmt.Errorf("invalid status in 's' tag: %s", tag[1])
			}
			break
		}
	}

	if !hasSTag {
		return fmt.Errorf("P2P order must have 's' tag (status)")
	}

	// Validate "amt" tag (amount in satoshis)
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "amt" {
			amt, err := strconv.ParseInt(tag[1], 10, 64)
			if err != nil || amt < 0 {
				return fmt.Errorf("invalid amount in 'amt' tag: %s", tag[1])
			}
			break
		}
	}

	// Validate "fa" tag (fiat amount)
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "fa" {
			parts := strings.Split(tag[1], ",")
			if len(parts) > 2 {
				return fmt.Errorf("invalid fiat amount in 'fa' tag: %s (max 2 values)", tag[1])
			}
			for _, p := range parts {
				if _, err := strconv.ParseFloat(p, 64); err != nil {
					return fmt.Errorf("invalid fiat amount in 'fa' tag: %s", tag[1])
				}
			}
			break
		}
	}

	// Validate "pm" tag (payment method)
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "pm" {
			if tag[1] == "" {
				return fmt.Errorf("empty payment method in 'pm' tag")
			}
			break
		}
	}

	// Validate "premium" tag (percentage)
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "premium" {
			premium, err := strconv.ParseFloat(tag[1], 64)
			if err != nil || premium < 0 {
				return fmt.Errorf("invalid premium in 'premium' tag: %s", tag[1])
			}
			break
		}
	}

	// Validate "source" tag (URL)
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "source" {
			if tag[1] != "" && !strings.HasPrefix(tag[1], "http://") && !strings.HasPrefix(tag[1], "https://") {
				return fmt.Errorf("invalid URL in 'source' tag: %s", tag[1])
			}
			break
		}
	}

	// Validate "rating" tag (JSON)
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "rating" {
			var rating map[string]interface{}
			if err := json.Unmarshal([]byte(tag[1]), &rating); err != nil {
				return fmt.Errorf("invalid JSON in 'rating' tag: %v", err)
			}
			break
		}
	}

	// Validate "network" tag
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "network" {
			validNetworks := map[string]bool{
				"mainnet": true,
				"testnet": true,
				"signet":  true,
			}
			if !validNetworks[tag[1]] {
				return fmt.Errorf("invalid network in 'network' tag: %s", tag[1])
			}
			break
		}
	}

	// Validate "layer" tag
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "layer" {
			validLayers := map[string]bool{
				"onchain":  true,
				"lightning": true,
				"liquid":   true,
			}
			if !validLayers[tag[1]] {
				return fmt.Errorf("invalid layer in 'layer' tag: %s", tag[1])
			}
			break
		}
	}

	// Validate "g" tag (geohash)
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "g" {
			if tag[1] == "" {
				return fmt.Errorf("empty geohash in 'g' tag")
			}
			break
		}
	}

	// Validate "bond" tag
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "bond" {
			bond, err := strconv.ParseInt(tag[1], 10, 64)
			if err != nil || bond < 0 {
				return fmt.Errorf("invalid bond in 'bond' tag: %s", tag[1])
			}
			break
		}
	}

	// SPEC UPDATE (9 months ago): Added order expiration support
	// Validate "expires_at" tag (expiration date for pending status)
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "expires_at" {
			expiresAt, err := strconv.ParseInt(tag[1], 10, 64)
			if err != nil || expiresAt <= 0 {
				return fmt.Errorf("invalid 'expires_at' tag: %s", tag[1])
			}
			// Check if expires_at is in the future
			if time.Unix(expiresAt, 0).Before(time.Now()) {
				return fmt.Errorf("expires_at is in the past: %d", expiresAt)
			}
			break
		}
	}

	// Validate "expiration" tag (NIP-40 expiration)
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "expiration" {
			expiration, err := strconv.ParseInt(tag[1], 10, 64)
			if err != nil || expiration <= 0 {
				return fmt.Errorf("invalid 'expiration' tag: %s", tag[1])
			}
			// Check if expiration is in the future
			if time.Unix(expiration, 0).Before(time.Now()) {
				return fmt.Errorf("expiration is in the past: %d", expiration)
			}
			break
		}
	}

	// Validate "y" tag (platform)
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "y" {
			if tag[1] == "" {
				return fmt.Errorf("empty platform in 'y' tag")
			}
			break
		}
	}

	// Validate "z" tag (document type)
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "z" {
			if tag[1] != "order" {
				return fmt.Errorf("invalid document type in 'z' tag: %s (must be 'order')", tag[1])
			}
			break
		}
	}

	// Validate "name" tag
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "name" {
			if tag[1] == "" {
				return fmt.Errorf("empty name in 'name' tag")
			}
			break
		}
	}

	// Validate "g" tag (geohash)
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "g" {
			if tag[1] == "" {
				return fmt.Errorf("empty geohash in 'g' tag")
			}
			break
		}
	}

	// Validate "bond" tag
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "bond" {
			bond, err := strconv.ParseInt(tag[1], 10, 64)
			if err != nil || bond < 0 {
				return fmt.Errorf("invalid bond in 'bond' tag: %s", tag[1])
			}
			break
		}
	}

	return nil
}

// IsP2POrder checks if an event is a P2P order event
func IsP2POrder(evt *nostr.Event) bool {
	return evt.Kind == 38383
}