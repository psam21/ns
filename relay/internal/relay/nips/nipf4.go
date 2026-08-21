package nips

import (
	"fmt"
	"regexp"
	"strings"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-F4: Podcasts
// https://github.com/nostr-protocol/nips/blob/master/F4.md
//
// NIP-F4 defines how podcast episodes can be fetched from relays.
//
// Event kinds:
//   - 10154: Podcast metadata (replaceable)
//   - 10064: Authored podcasts (replaceable)
//   - 54: Podcast episode
//   - 10054: Favorite podcasts (NIP-51 list)

// PodcastMetadataKind is the kind for podcast metadata.
const PodcastMetadataKind = 10154

// AuthoredPodcastsKind is the kind for authored podcasts list.
const AuthoredPodcastsKind = 10064

// PodcastEpisodeKind is the kind for podcast episodes.
const PodcastEpisodeKind = 54

// FavoritePodcastsKind is the kind for favorite podcasts list.
const FavoritePodcastsKind = 10054

// ValidPodcastRoles lists valid podcast author roles.
var ValidPodcastRoles = map[string]bool{
	"host":   true,
	"cohost": true,
	"editor": true,
}

// hex64Regex matches 64-character hex strings (pubkeys/event IDs).
var hex64Regex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidatePodcastMetadata validates a kind 10154 podcast metadata event.
func ValidatePodcastMetadata(evt *nostr.Event) error {
	hasTitle := false
	hasImage := false
	hasDescription := false

	for _, tag := range evt.Tags {
		if len(tag) < 1 {
			continue
		}
		switch tag[0] {
		case "title":
			if len(tag) < 2 || strings.TrimSpace(tag[1]) == "" {
				return fmt.Errorf("title tag must have a non-empty value")
			}
			hasTitle = true
		case "image":
			if len(tag) < 2 || strings.TrimSpace(tag[1]) == "" {
				return fmt.Errorf("image tag must have a non-empty URL")
			}
			hasImage = true
		case "description":
			if len(tag) < 2 {
				return fmt.Errorf("description tag must have a value")
			}
			hasDescription = true
		case "website":
			if len(tag) < 2 || strings.TrimSpace(tag[1]) == "" {
				return fmt.Errorf("website tag must have a non-empty URL")
			}
		case "p":
			// p tag: pubkey, optional role
			if len(tag) < 2 {
				return fmt.Errorf("p tag must have at least a pubkey")
			}
			if !hex64Regex.MatchString(tag[1]) {
				return fmt.Errorf("p tag pubkey must be 64 lowercase hex chars: %s", tag[1])
			}
			if len(tag) >= 3 && tag[2] != "" && !ValidPodcastRoles[tag[2]] {
				return fmt.Errorf("p tag role must be host, cohost, or editor, got %q", tag[2])
			}
		}
	}

	if !hasTitle {
		return fmt.Errorf("podcast metadata MUST include a title tag")
	}
	if !hasImage {
		return fmt.Errorf("podcast metadata MUST include an image tag")
	}
	if !hasDescription {
		return fmt.Errorf("podcast metadata MUST include a description tag")
	}
	return nil
}

// ValidateAuthoredPodcasts validates a kind 10064 authored podcasts event.
func ValidateAuthoredPodcasts(evt *nostr.Event) error {
	hasP := false
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "p" {
			hasP = true
			if len(tag) < 2 {
				return fmt.Errorf("p tag must have a pubkey")
			}
			if !hex64Regex.MatchString(tag[1]) {
				return fmt.Errorf("p tag pubkey must be 64 lowercase hex chars: %s", tag[1])
			}
		}
	}
	if !hasP {
		return fmt.Errorf("authored podcasts event MUST include at least one p tag")
	}
	return nil
}

// ValidatePodcastEpisode validates a kind 54 podcast episode event.
func ValidatePodcastEpisode(evt *nostr.Event) error {
	hasTitle := false
	hasAudio := false

	for _, tag := range evt.Tags {
		if len(tag) < 1 {
			continue
		}
		switch tag[0] {
		case "title":
			if len(tag) < 2 || strings.TrimSpace(tag[1]) == "" {
				return fmt.Errorf("title tag must have a non-empty value")
			}
			hasTitle = true
		case "audio":
			if len(tag) < 2 || strings.TrimSpace(tag[1]) == "" {
				return fmt.Errorf("audio tag must have a non-empty URL")
			}
			hasAudio = true
		case "image", "description":
			if len(tag) < 2 {
				return fmt.Errorf("%s tag must have a value", tag[0])
			}
		}
	}

	if !hasTitle {
		return fmt.Errorf("podcast episode MUST include a title tag")
	}
	if !hasAudio {
		return fmt.Errorf("podcast episode MUST include at least one audio tag")
	}
	return nil
}

// ValidateNIPF4Event dispatches to the appropriate validator based on event kind.
func ValidateNIPF4Event(evt *nostr.Event) error {
	switch evt.Kind {
	case PodcastMetadataKind:
		return ValidatePodcastMetadata(evt)
	case AuthoredPodcastsKind:
		return ValidateAuthoredPodcasts(evt)
	case PodcastEpisodeKind:
		return ValidatePodcastEpisode(evt)
	case FavoritePodcastsKind:
		// Favorite podcasts use NIP-51 list format; basic validation only
		return nil
	default:
		return fmt.Errorf("not a NIP-F4 event kind: %d", evt.Kind)
	}
}

// IsNIPF4Kind returns true if the event kind is a NIP-F4 podcast kind.
func IsNIPF4Kind(kind int) bool {
	return kind == PodcastMetadataKind || kind == AuthoredPodcastsKind ||
		kind == PodcastEpisodeKind || kind == FavoritePodcastsKind
}
