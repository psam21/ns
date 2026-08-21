package nips

import (
	"fmt"
	"strconv"
	"strings"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-B0: Web Bookmarking
// https://github.com/nostr-protocol/nips/blob/master/B0.md
//
// NIP-B0 defines kind 39701 for a URI as editable web bookmark.
//
// The d tag is the URI. All characters before the hostname should be omitted
// if the scheme is https.
//
// Optional tags:
//   - t: hashtags/topics
//   - published_at: unix timestamp of first publish
//   - title: title for the bookmark
//
// Replies use kind 1111 events as comments with NIP-22.

// WebBookmarkKind is the kind for web bookmarks.
const WebBookmarkKind = 39701

// MaxWebBookmarkTitleLength limits the title length.
const MaxWebBookmarkTitleLength = 500

// ValidateWebBookmarkDTag validates the d tag value (URI without scheme).
func ValidateWebBookmarkDTag(value string) error {
	if value == "" {
		return fmt.Errorf("d tag (URI) cannot be empty")
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return fmt.Errorf("d tag should not include scheme (https://) per NIP-B0")
	}
	if strings.Contains(value, " ") {
		return fmt.Errorf("d tag (URI) cannot contain spaces: %s", value)
	}
	return nil
}

// ValidateNIPB0Event validates a NIP-B0 web bookmark event (kind 39701).
// Required: d tag with URI value.
func ValidateNIPB0Event(evt *nostr.Event) error {
	if evt.Kind != WebBookmarkKind {
		return fmt.Errorf("not a NIP-B0 event kind: %d", evt.Kind)
	}

	hasD := false
	for _, tag := range evt.Tags {
		if len(tag) < 1 {
			continue
		}
		switch tag[0] {
		case "d":
			hasD = true
			if len(tag) >= 2 {
				if err := ValidateWebBookmarkDTag(tag[1]); err != nil {
					return err
				}
			}
		case "title":
			if len(tag) >= 2 && len(tag[1]) > MaxWebBookmarkTitleLength {
				return fmt.Errorf("title exceeds max length of %d chars", MaxWebBookmarkTitleLength)
			}
		case "published_at":
			if len(tag) >= 2 {
				if _, err := strconv.ParseInt(tag[1], 10, 64); err != nil {
					return fmt.Errorf("published_at must be a unix timestamp: %s", tag[1])
				}
			}
		}
	}

	if !hasD {
		return fmt.Errorf("NIP-B0 web bookmark MUST include a d tag with the URI")
	}
	return nil
}

// GetWebBookmarkURI returns the d tag value (URI).
func GetWebBookmarkURI(evt *nostr.Event) string {
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "d" {
			return tag[1]
		}
	}
	return ""
}
