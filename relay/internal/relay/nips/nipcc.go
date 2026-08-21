package nips

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-CC: Geocaching Events
// https://github.com/nostr-protocol/nips/blob/master/CC.md
//
// NIP-CC defines event kinds for geocaching on Nostr.
//
// Event kinds:
//   - 37516: Geocache listing (addressable, requires d tag)
//   - 37517: Geocache curation list (addressable, requires d tag)
//   - 7516: Found log (regular event)
//   - 7517: Geocache verification event

// GeocacheListingKind is the kind for geocache listings.
const GeocacheListingKind = 37516

// GeocacheCurationListKind is the kind for geocache curation lists.
const GeocacheCurationListKind = 37517

// GeocacheFoundLogKind is the kind for found logs.
const GeocacheFoundLogKind = 7516

// GeocacheVerificationKind is the kind for verification events.
const GeocacheVerificationKind = 7517

// ValidGeocacheSizes lists valid cache sizes.
var ValidGeocacheSizes = map[string]bool{
	"micro":   true,
	"small":   true,
	"regular": true,
	"large":   true,
	"other":   true,
}

// ValidGeocacheTypes lists valid cache types.
var ValidGeocacheTypes = map[string]bool{
	"traditional": true,
	"multi":       true,
	"mystery":     true,
}

// geohashRegex matches valid geohash strings (3-12 chars, base32).
var geohashRegex = regexp.MustCompile(`^[0-9a-z]{3,12}$`)

// ValidateGeocacheListing validates a kind 37516 geocache listing event.
func ValidateGeocacheListing(evt *nostr.Event) error {
	hasD := false
	hasName := false
	hasG := false
	hasD2 := false // difficulty
	hasT := false  // terrain
	hasS := false  // size

	for _, tag := range evt.Tags {
		if len(tag) < 1 {
			continue
		}
		switch tag[0] {
		case "d":
			hasD = true
			if len(tag) < 2 || strings.TrimSpace(tag[1]) == "" {
				return fmt.Errorf("d tag must have a non-empty cache identifier")
			}
		case "name":
			hasName = true
			if len(tag) < 2 || strings.TrimSpace(tag[1]) == "" {
				return fmt.Errorf("name tag must have a non-empty value")
			}
		case "g":
			hasG = true
			if len(tag) < 2 || !geohashRegex.MatchString(tag[1]) {
				return fmt.Errorf("g tag must be a valid geohash (3-12 base32 chars): %s", tag[1])
			}
		case "D":
			hasD2 = true
			if len(tag) < 2 {
				return fmt.Errorf("D tag (difficulty) must have a value")
			}
			d, err := strconv.Atoi(tag[1])
			if err != nil || d < 1 || d > 5 {
				return fmt.Errorf("D tag (difficulty) must be integer 1-5: %s", tag[1])
			}
		case "T":
			hasT = true
			if len(tag) < 2 {
				return fmt.Errorf("T tag (terrain) must have a value")
			}
			t, err := strconv.Atoi(tag[1])
			if err != nil || t < 1 || t > 5 {
				return fmt.Errorf("T tag (terrain) must be integer 1-5: %s", tag[1])
			}
		case "S":
			hasS = true
			if len(tag) < 2 || !ValidGeocacheSizes[tag[1]] {
				return fmt.Errorf("S tag (size) must be one of micro/small/regular/large/other, got %q", tag[1])
			}
		case "t":
			if len(tag) >= 2 && tag[1] != "" && !ValidGeocacheTypes[tag[1]] {
				// Unknown cache type - allow but log
			}
		case "verification":
			if len(tag) < 2 || !hex64Regex.MatchString(tag[1]) {
				return fmt.Errorf("verification tag must be 64 hex chars: %s", tag[1])
			}
		case "F":
			if len(tag) < 2 || !hex64Regex.MatchString(tag[1]) {
				return fmt.Errorf("F tag (winner pubkey) must be 64 hex chars: %s", tag[1])
			}
		}
	}

	if !hasD {
		return fmt.Errorf("geocache listing MUST include a d tag (cache identifier)")
	}
	if !hasName {
		return fmt.Errorf("geocache listing MUST include a name tag")
	}
	if !hasG {
		return fmt.Errorf("geocache listing MUST include at least one g tag (geohash)")
	}
	if !hasD2 {
		return fmt.Errorf("geocache listing MUST include a D tag (difficulty)")
	}
	if !hasT {
		return fmt.Errorf("geocache listing MUST include a T tag (terrain)")
	}
	if !hasS {
		return fmt.Errorf("geocache listing MUST include an S tag (size)")
	}
	return nil
}

// ValidateGeocacheFoundLog validates a kind 7516 found log event.
func ValidateGeocacheFoundLog(evt *nostr.Event) error {
	hasA := false
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "a" {
			hasA = true
			if len(tag) < 2 {
				return fmt.Errorf("a tag must reference a geocache listing")
			}
			// Format: 37516:<pubkey>:<d-tag>
			parts := strings.SplitN(tag[1], ":", 3)
			if len(parts) < 3 {
				return fmt.Errorf("a tag must be 37516:<pubkey>:<d-tag>, got %s", tag[1])
			}
			if parts[0] != "37516" {
				return fmt.Errorf("a tag must reference kind 37516, got %s", parts[0])
			}
			if !hex64Regex.MatchString(parts[1]) {
				return fmt.Errorf("a tag pubkey must be 64 hex chars: %s", parts[1])
			}
		}
	}
	if !hasA {
		return fmt.Errorf("geocache found log MUST include an a tag referencing the cache")
	}
	return nil
}

// ValidateGeocacheVerification validates a kind 7517 verification event.
func ValidateGeocacheVerification(evt *nostr.Event) error {
	if !strings.HasPrefix(evt.Content, "Geocache verification for ") {
		return fmt.Errorf("verification event content must start with 'Geocache verification for '")
	}
	hasA := false
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "a" {
			hasA = true
			if len(tag) < 2 {
				return fmt.Errorf("a tag must have a value")
			}
		}
	}
	if !hasA {
		return fmt.Errorf("geocache verification MUST include an a tag")
	}
	return nil
}

// ValidateNIPCCEvent dispatches to the appropriate validator based on event kind.
func ValidateNIPCCEvent(evt *nostr.Event) error {
	switch evt.Kind {
	case GeocacheListingKind:
		return ValidateGeocacheListing(evt)
	case GeocacheCurationListKind:
		// Curation lists require d, title, and at least one a tag
		hasD := false
		hasTitle := false
		hasA := false
		for _, tag := range evt.Tags {
			if len(tag) < 1 {
				continue
			}
			switch tag[0] {
			case "d":
				hasD = true
			case "title":
				hasTitle = true
			case "a":
				hasA = true
			}
		}
		if !hasD {
			return fmt.Errorf("geocache curation list MUST include a d tag")
		}
		if !hasTitle {
			return fmt.Errorf("geocache curation list MUST include a title tag")
		}
		if !hasA {
			return fmt.Errorf("geocache curation list MUST include at least one a tag")
		}
		return nil
	case GeocacheFoundLogKind:
		return ValidateGeocacheFoundLog(evt)
	case GeocacheVerificationKind:
		return ValidateGeocacheVerification(evt)
	default:
		return fmt.Errorf("not a NIP-CC event kind: %d", evt.Kind)
	}
}

// IsNIPCCKind returns true if the event kind is a NIP-CC geocaching kind.
func IsNIPCCKind(kind int) bool {
	return kind == GeocacheListingKind || kind == GeocacheCurationListKind ||
		kind == GeocacheFoundLogKind || kind == GeocacheVerificationKind
}
