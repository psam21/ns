package nips

import (
	"fmt"
	"strings"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-92: Media Attachments Metadata (imeta)
// https://github.com/nostr-protocol/nips/blob/master/92.md
//
// NIP-92 defines the "imeta" (inline metadata) tag for adding metadata about
// media URLs in event content. Each imeta tag is variadic with space-delimited
// key/value pairs. Each imeta tag MUST have a "url" and at least one other field.
//
// Standard fields (from NIP-94):
//   - url: The media URL
//   - m: MIME type
//   - x: SHA-256 hash
//   - size: File size in bytes
//   - dim: Dimensions (e.g., "3024x4032")
//   - alt: Alt text / description
//   - blurhash: BlurHash preview
//   - fallback: Fallback URL(s)
//   - magnet: Magnet URI
//   - torrent: Torrent info hash
//   - artist: Artist name
//   - title: Media title
//   - thumbnail: Thumbnail URL
//   - summary: Summary text

// imetaAllowedFields lists the known imeta tag fields.
var imetaAllowedFields = map[string]bool{
	"url":       true,
	"m":         true,
	"x":         true,
	"size":      true,
	"dim":       true,
	"alt":       true,
	"blurhash":  true,
	"fallback":  true,
	"magnet":    true,
	"torrent":   true,
	"artist":    true,
	"title":     true,
	"thumbnail": true,
	"summary":   true,
}

// ValidateImetaTag validates a single imeta tag.
// Returns an error if the tag is malformed.
func ValidateImetaTag(tag nostr.Tag) error {
	if len(tag) < 3 {
		return fmt.Errorf("imeta tag must have at least 2 fields (url + 1 other)")
	}

	hasURL := false
	fieldCount := 0
	seenFields := make(map[string]bool)

	for i := 1; i < len(tag); i++ {
		kv := strings.SplitN(tag[i], " ", 2)
		if len(kv) != 2 {
			return fmt.Errorf("imeta tag field must be space-delimited key/value pair, got: %s", tag[i])
		}

		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])

		if key == "" {
			return fmt.Errorf("imeta tag field key cannot be empty")
		}
		if value == "" {
			return fmt.Errorf("imeta tag field value cannot be empty for key: %s", key)
		}

		// Unknown fields are allowed (forward compatibility) but should be tracked
		if !imetaAllowedFields[key] {
			// Allow unknown fields but don't count them as required
			continue
		}

		// Prevent duplicate fields in the same imeta tag
		if seenFields[key] {
			return fmt.Errorf("duplicate imeta field: %s", key)
		}
		seenFields[key] = true
		fieldCount++

		if key == "url" {
			hasURL = true
		}
	}

	if !hasURL {
		return fmt.Errorf("imeta tag MUST have a 'url' field")
	}

	if fieldCount < 2 {
		return fmt.Errorf("imeta tag MUST have 'url' and at least one other field")
	}

	return nil
}

// ValidateNIP92Event validates that any imeta tags in the event are well-formed.
// imeta tags are typically used with kind 1 (text notes) and other readable
// content events, but the validator does not restrict by kind since imeta can
// appear on any event with media URLs.
func ValidateNIP92Event(evt *nostr.Event) error {
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "imeta" {
			if err := ValidateImetaTag(tag); err != nil {
				return fmt.Errorf("invalid imeta tag: %w", err)
			}
		}
	}
	return nil
}
