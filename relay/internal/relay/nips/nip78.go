package nips

import (
	"fmt"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-78: Application-specific data
// https://github.com/nostr-protocol/nips/blob/master/78.md
//
// Kind 30078 is an addressable event identified by its "d" tag.
// NIP-78 explicitly leaves content and all other tags application-defined;
// clients may store encrypted or otherwise non-JSON content here.

// ValidateApplicationSpecificData validates NIP-78 application-specific data events (kind 30078)
func ValidateApplicationSpecificData(evt *nostr.Event) error {
	if evt.Kind != 30078 {
		return fmt.Errorf("invalid event kind for application-specific data: %d", evt.Kind)
	}

	// The d tag identifies the application data address.
	for _, tag := range evt.Tags {
		if len(tag) >= 2 {
			if tag[0] == "d" {
				return nil
			}
		}
	}

	return fmt.Errorf("application-specific data must have 'd' tag")
}

// IsApplicationSpecificData checks if an event is application-specific data
func IsApplicationSpecificData(evt *nostr.Event) bool {
	return evt.Kind == 30078
}

// GetApplicationDataIdentifier returns the "d" tag value (application identifier)
func GetApplicationDataIdentifier(evt *nostr.Event) string {
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "d" {
			return tag[1]
		}
	}
	return ""
}

// GetApplicationDataTarget returns the "p" tag value (target pubkey)
func GetApplicationDataTarget(evt *nostr.Event) string {
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			return tag[1]
		}
	}
	return ""
}
