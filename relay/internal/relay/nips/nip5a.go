package nips

import (
	"fmt"
	"regexp"
	"strings"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-5A: Static Websites (nsites)
// https://github.com/nostr-protocol/nips/blob/master/5A.md
//
// NIP-5A describes a method by which static websites can be hosted from Blossom
// assets. Site manifests are Nostr events that map paths to sha256 hashes of
// files stored on Blossom servers.
//
// Event kinds:
//   - 15128: Root site (replaceable, NO d tag)
//   - 35128: Named site (addressable, requires d tag)
//   - 5128: Manifest snapshot (regular event)

// NsiteKindRoot is the kind for root site manifests.
const NsiteKindRoot = 15128

// NsiteKindNamed is the kind for named site manifests.
const NsiteKindNamed = 35128

// NsiteKindSnapshot is the kind for manifest snapshots.
const NsiteKindSnapshot = 5128

// MaxNsiteDTagLength is the max length for named site d tag (1-13 chars).
const MaxNsiteDTagLength = 13

// nsiteDTagRegex matches valid d tag values per NIP-5A.
var nsiteDTagRegex = regexp.MustCompile(`^[a-z0-9-]{1,13}$`)

// sha256HexRegex matches a 64-character lowercase hex sha256 hash.
var sha256HexRegex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// IsNsiteManifestKind returns true if the event kind is a nsite manifest kind.
func IsNsiteManifestKind(kind int) bool {
	return kind == NsiteKindRoot || kind == NsiteKindNamed || kind == NsiteKindSnapshot
}

// ValidatePathTag validates a single path tag.
// Format: ["path", "/absolute/path", "sha256hash"]
func ValidatePathTag(tag nostr.Tag) error {
	if len(tag) != 3 {
		return fmt.Errorf("path tag must have exactly 3 elements [path, path, hash], got %d", len(tag))
	}
	if tag[0] != "path" {
		return fmt.Errorf("tag must be 'path', got %q", tag[0])
	}
	path := tag[1]
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must be absolute (start with /): %s", path)
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("path cannot contain '..': %s", path)
	}
	hash := tag[2]
	if !sha256HexRegex.MatchString(hash) {
		return fmt.Errorf("path hash must be 64 lowercase hex chars: %s", hash)
	}
	return nil
}

// ValidateAggregateTag validates an aggregate x tag.
// Format: ["x", "<sha256-hex>", "aggregate"]
func ValidateAggregateTag(tag nostr.Tag) error {
	if len(tag) != 3 {
		return fmt.Errorf("x tag must have exactly 3 elements, got %d", len(tag))
	}
	if tag[0] != "x" {
		return fmt.Errorf("tag must be 'x', got %q", tag[0])
	}
	if tag[2] != "aggregate" {
		return fmt.Errorf("x tag third element must be 'aggregate', got %q", tag[2])
	}
	if !sha256HexRegex.MatchString(tag[1]) {
		return fmt.Errorf("aggregate hash must be 64 lowercase hex chars: %s", tag[1])
	}
	return nil
}

// ValidateSourceTag validates a source tag.
// Format: ["source", "<url>"]
func ValidateSourceTag(tag nostr.Tag) error {
	if len(tag) != 2 {
		return fmt.Errorf("source tag must have exactly 2 elements, got %d", len(tag))
	}
	if tag[0] != "source" {
		return fmt.Errorf("tag must be 'source', got %q", tag[0])
	}
	url := tag[1]
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "nostr://") {
		return fmt.Errorf("source URL must start with https:// or nostr://, got %q", url)
	}
	return nil
}

// ValidateServerTag validates a server tag (Blossom server URL).
func ValidateServerTag(tag nostr.Tag) error {
	if len(tag) != 2 {
		return fmt.Errorf("server tag must have exactly 2 elements, got %d", len(tag))
	}
	if tag[0] != "server" {
		return fmt.Errorf("tag must be 'server', got %q", tag[0])
	}
	url := tag[1]
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("server URL must start with https://, got %q", url)
	}
	return nil
}

// ValidateNIP5AEvent validates NIP-5A site manifest events.
// For kind 15128 (root): no d tag allowed.
// For kind 35128 (named): d tag required, 1-13 chars matching ^[a-z0-9-]+$ and not ending with -.
// For kind 5128 (snapshot): a tag referencing parent required.
// All path tags must be well-formed.
func ValidateNIP5AEvent(evt *nostr.Event) error {
	switch evt.Kind {
	case NsiteKindRoot:
		// Root site MUST NOT include a d tag
		for _, tag := range evt.Tags {
			if len(tag) >= 1 && tag[0] == "d" {
				return fmt.Errorf("root site (kind 15128) MUST NOT include a d tag")
			}
		}
	case NsiteKindNamed:
		// Named site MUST include a d tag
		dTagFound := false
		for _, tag := range evt.Tags {
			if len(tag) >= 1 && tag[0] == "d" {
				dTagFound = true
				if len(tag) >= 2 {
					dVal := tag[1]
					if !nsiteDTagRegex.MatchString(dVal) {
						return fmt.Errorf("d tag must match ^[a-z0-9-]{1,13}$, got %q", dVal)
					}
					if strings.HasSuffix(dVal, "-") {
						return fmt.Errorf("d tag must not end with '-': %s", dVal)
					}
				} else {
					return fmt.Errorf("d tag must have a value")
				}
			}
		}
		if !dTagFound {
			return fmt.Errorf("named site (kind 35128) MUST include a d tag")
		}
	case NsiteKindSnapshot:
		// Snapshot MUST include an a tag referencing parent
		aTagFound := false
		for _, tag := range evt.Tags {
			if len(tag) >= 1 && tag[0] == "a" {
				aTagFound = true
				break
			}
		}
		if !aTagFound {
			return fmt.Errorf("manifest snapshot (kind 5128) MUST include an 'a' tag referencing parent")
		}
	default:
		return fmt.Errorf("not a NIP-5A event kind: %d", evt.Kind)
	}

	// Validate path tags
	hasPath := false
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "path" {
			hasPath = true
			if err := ValidatePathTag(tag); err != nil {
				return err
			}
		}
	}
	if !hasPath {
		return fmt.Errorf("nsite event MUST include at least one path tag")
	}

	// Validate x tags (aggregate hash)
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "x" {
			if err := ValidateAggregateTag(tag); err != nil {
				return err
			}
		}
	}

	// Validate source tags
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "source" {
			if err := ValidateSourceTag(tag); err != nil {
				return err
			}
		}
	}

	// Validate server tags
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "server" {
			if err := ValidateServerTag(tag); err != nil {
				return err
			}
		}
	}

	return nil
}

// GetNsitePaths returns all path-to-hash mappings from the event.
func GetNsitePaths(evt *nostr.Event) map[string]string {
	paths := make(map[string]string)
	for _, tag := range evt.Tags {
		if len(tag) == 3 && tag[0] == "path" {
			paths[tag[1]] = tag[2]
		}
	}
	return paths
}

// GetNsiteAggregateHash returns the aggregate hash from the x tag, or empty if not present.
func GetNsiteAggregateHash(evt *nostr.Event) string {
	for _, tag := range evt.Tags {
		if len(tag) == 3 && tag[0] == "x" && tag[2] == "aggregate" {
			return tag[1]
		}
	}
	return ""
}
