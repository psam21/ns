package nips

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-94: File Metadata
// https://github.com/nostr-protocol/nips/blob/master/94.md
//
// NIP-94 specifies the use of the 1063 event kind for file metadata.
// The event has a description in `content` and tags describing the file:
//
// Required tags:
//   - url: URL to download the file
//   - m: MIME type (lowercase)
//   - x: SHA-256 hex of the file
//
// Optional tags:
//   - ox: SHA-256 hex of the original file (before transformations)
//   - size: file size in bytes
//   - dim: dimensions in pixels (e.g., "1920x1080")
//   - magnet: magnet URI
//   - i: torrent infohash
//   - blurhash: blurhash value
//   - thumb: thumbnail URL with SHA-256
//   - image: preview image URL with SHA-256
//   - summary: text excerpt
//   - alt: accessibility description
//   - fallback: fallback file sources
//   - service: service type (e.g., NIP-96)

// FileMetadataKind is the kind for file metadata events.
const FileMetadataKind = 1063

// sha256HexRegex94 matches a 64-character lowercase hex sha256 hash.
var sha256HexRegex94 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// dimRegex matches dimensions in the format "WxH" (e.g., "1920x1080").
var dimRegex = regexp.MustCompile(`^[0-9]+x[0-9]+$`)

// ValidateFileMetadataTag validates a single file metadata tag.
// Returns nil if valid, error otherwise.
func ValidateFileMetadataTag(tag nostr.Tag) error {
	if len(tag) < 2 {
		return fmt.Errorf("file metadata tag must have at least 2 elements, got %d", len(tag))
	}
	name := tag[0]
	value := tag[1]

	switch name {
	case "url", "thumb", "image", "fallback":
		// URL tags: value must be non-empty
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s tag value cannot be empty", name)
		}
	case "m":
		// MIME type: lowercase, format like "image/png"
		if value == "" {
			return fmt.Errorf("m tag (MIME type) cannot be empty")
		}
		if value != strings.ToLower(value) {
			return fmt.Errorf("m tag (MIME type) must be lowercase: %s", value)
		}
		if !strings.Contains(value, "/") {
			return fmt.Errorf("m tag (MIME type) must contain '/': %s", value)
		}
	case "x", "ox":
		// SHA-256 hash
		if !sha256HexRegex94.MatchString(value) {
			return fmt.Errorf("%s tag must be 64 lowercase hex chars: %s", name, value)
		}
	case "size":
		// File size in bytes
		if _, err := strconv.ParseUint(value, 10, 64); err != nil {
			return fmt.Errorf("size tag must be a non-negative integer: %s", value)
		}
	case "dim":
		// Dimensions in pixels
		if !dimRegex.MatchString(value) {
			return fmt.Errorf("dim tag must match WxH format: %s", value)
		}
	case "magnet":
		// Magnet URI
		if !strings.HasPrefix(value, "magnet:") {
			return fmt.Errorf("magnet tag must start with 'magnet:': %s", value)
		}
	case "i":
		// Torrent infohash (40 hex chars for v1, 64 for v2)
		if len(value) != 40 && len(value) != 64 {
			return fmt.Errorf("i tag (infohash) must be 40 or 64 hex chars: %s", value)
		}
	case "blurhash":
		// Blurhash value (non-empty)
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("blurhash tag value cannot be empty")
		}
	case "summary", "alt":
		// Text fields (non-empty)
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s tag value cannot be empty", name)
		}
	case "service":
		// Service type (non-empty)
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("service tag value cannot be empty")
		}
	default:
		// Unknown tags are allowed but ignored
	}
	return nil
}

// ValidateNIP94Event validates a NIP-94 file metadata event (kind 1063).
// Required tags: url, m, x.
func ValidateNIP94Event(evt *nostr.Event) error {
	if evt.Kind != FileMetadataKind {
		return fmt.Errorf("not a NIP-94 event kind: %d", evt.Kind)
	}

	hasURL := false
	hasMIME := false
	hasHash := false

	for _, tag := range evt.Tags {
		if len(tag) < 1 {
			continue
		}
		switch tag[0] {
		case "url":
			hasURL = true
		case "m":
			hasMIME = true
		case "x":
			hasHash = true
		}
		if err := ValidateFileMetadataTag(tag); err != nil {
			return err
		}
	}

	if !hasURL {
		return fmt.Errorf("NIP-94 event MUST include a 'url' tag")
	}
	if !hasMIME {
		return fmt.Errorf("NIP-94 event MUST include an 'm' (MIME type) tag")
	}
	if !hasHash {
		return fmt.Errorf("NIP-94 event MUST include an 'x' (SHA-256 hash) tag")
	}
	return nil
}

// GetFileURL returns the URL from the file metadata event.
func GetFileURL(evt *nostr.Event) string {
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "url" {
			return tag[1]
		}
	}
	return ""
}

// GetFileMIME returns the MIME type from the file metadata event.
func GetFileMIME(evt *nostr.Event) string {
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "m" {
			return tag[1]
		}
	}
	return ""
}

// GetFileHash returns the SHA-256 hash from the file metadata event.
func GetFileHash(evt *nostr.Event) string {
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "x" {
			return tag[1]
		}
	}
	return ""
}
