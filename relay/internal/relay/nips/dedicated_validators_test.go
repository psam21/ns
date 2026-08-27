package nips

import (
	"strings"
	"testing"

	nostr "github.com/nbd-wtf/go-nostr"
)

func TestDedicatedValidatorsRejectWrongKind(t *testing.T) {
	event := &nostr.Event{Kind: 99999, Content: "not a valid NIP event"}
	checks := []struct {
		name string
		fn   func(*nostr.Event) error
	}{
		{"NIP-46 request", ValidateNIP46Request},
		{"NIP-46 response", ValidateNIP46Response},
		{"NIP-47 info", ValidateNIP47Info},
		{"NIP-47 request", ValidateNIP47Request},
		{"NIP-47 response", ValidateNIP47Response},
		{"NIP-5A event", ValidateNIP5AEvent},
		{"NIP-87 cashu mint", ValidateNIP87CashuMint},
		{"NIP-87 fedimint", ValidateNIP87Fedimint},
		{"NIP-87 recommendation", ValidateNIP87Recommendation},
		{"NIP-87 event", ValidateNIP87Event},
		{"NIP-94 event", ValidateNIP94Event},
		{"NIP-A0 event", ValidateNIPA0Event},
		{"NIP-A4 event", ValidateNIPA4Event},
		{"NIP-B0 event", ValidateNIPB0Event},
		{"NIP-C0 event", ValidateNIPC0Event},
		{"NIP-C7 event", ValidateNIPC7Event},
		{"NIP-CC event", ValidateNIPCCEvent},
		{"NIP-F4 event", ValidateNIPF4Event},
	}

	for _, check := range checks {
		check := check
		t.Run(check.name, func(t *testing.T) {
			if err := check.fn(event); err == nil {
				t.Fatalf("validator accepted wrong event kind")
			}
		})
	}
}

func TestOptionalValidatorsRejectApplicableMalformedTags(t *testing.T) {
	checks := []struct {
		name  string
		event nostr.Event
		fn    func(*nostr.Event) error
	}{
		{
			name:  "NIP-05 metadata type",
			event: nostr.Event{Kind: 0, Content: `{"nip05":123}`},
			fn:    ValidateNIP05Metadata,
		},
		{
			name:  "NIP-10 event reference",
			event: nostr.Event{Kind: 1, Tags: nostr.Tags{nostr.Tag{"e", "bad"}}},
			fn:    ValidateNIP10Reply,
		},
		{
			name:  "NIP-36 content warning",
			event: nostr.Event{Kind: 1, Tags: nostr.Tags{nostr.Tag{"content-warning", strings.Repeat("x", 300)}}},
			fn:    ValidateNIP36Event,
		},
		{
			name:  "NIP-48 proxy tag",
			event: nostr.Event{Kind: 1, Tags: nostr.Tags{nostr.Tag{"proxy", "", "http"}}},
			fn:    ValidateNIP48Event,
		},
		{
			name:  "NIP-92 imeta tag",
			event: nostr.Event{Kind: 1063, Tags: nostr.Tags{nostr.Tag{"imeta", "url"}}},
			fn:    ValidateNIP92Event,
		},
	}

	for _, check := range checks {
		check := check
		t.Run(check.name, func(t *testing.T) {
			if err := check.fn(&check.event); err == nil {
				t.Fatalf("validator accepted malformed applicable fixture")
			}
		})
	}
}

func TestScalarNIPValidatorsRejectMalformedValues(t *testing.T) {
	checks := []struct {
		name string
		fn   func(string) error
	}{
		{"NIP-05 identifier", ValidateNIP05Identifier},
		{"NIP-19 string", ValidateNIP19String},
	}

	for _, check := range checks {
		check := check
		t.Run(check.name, func(t *testing.T) {
			if err := check.fn("not-a-valid-nostr-value"); err == nil {
				t.Fatalf("validator accepted malformed value")
			}
		})
	}
}

func TestNIP44RejectsMalformedPayload(t *testing.T) {
	if err := ValidateNIP44Payload(nostr.Event{Kind: 44, Content: "not-a-versioned-payload"}); err == nil {
		t.Fatal("NIP-44 validator accepted malformed payload")
	}
}
