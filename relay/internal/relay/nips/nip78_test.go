package nips

import (
	"strings"
	"testing"

	nostr "github.com/nbd-wtf/go-nostr"
)

func TestValidateApplicationSpecificDataAcceptsOpaqueContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		tags    nostr.Tags
	}{
		{
			name:    "encrypted content without recipient tag",
			content: "AgFzdG9yZWQtY2lwaGVydGV4dA==",
			tags:    nostr.Tags{nostr.Tag{"d", "user-prefs"}},
		},
		{
			name:    "arbitrary application data and tags",
			content: "application-specific payload",
			tags: nostr.Tags{
				nostr.Tag{"d", "private-data"},
				nostr.Tag{"p", "application-defined-value"},
				nostr.Tag{"custom", "opaque"},
			},
		},
		{
			name:    "JSON content",
			content: `{"name":"optional app data","data":{"key":"value"}}`,
			tags:    nostr.Tags{nostr.Tag{"d", "json-data"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &nostr.Event{Kind: 30078, Tags: tt.tags, Content: tt.content}
			if err := ValidateApplicationSpecificData(event); err != nil {
				t.Fatalf("valid NIP-78 event rejected: %v", err)
			}
		})
	}
}

func TestValidateApplicationSpecificDataRequiresDTag(t *testing.T) {
	event := &nostr.Event{Kind: 30078, Content: "opaque"}
	err := ValidateApplicationSpecificData(event)
	if err == nil || !strings.Contains(err.Error(), "'d' tag") {
		t.Fatalf("expected missing d-tag error, got %v", err)
	}
}

func TestValidateApplicationSpecificDataRejectsWrongKind(t *testing.T) {
	event := &nostr.Event{Kind: 1, Tags: nostr.Tags{nostr.Tag{"d", "not-app-data"}}}
	err := ValidateApplicationSpecificData(event)
	if err == nil || !strings.Contains(err.Error(), "invalid event kind") {
		t.Fatalf("expected wrong-kind error, got %v", err)
	}
}
