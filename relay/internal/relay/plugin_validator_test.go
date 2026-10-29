package relay

import (
	"context"
	"strings"
	"testing"

	"github.com/Shugur-Network/relay/internal/config"
	nostr "github.com/nbd-wtf/go-nostr"
)

func TestValidateEventRejectsMalformedExpirationTag(t *testing.T) {
	cfg := &config.Config{
		Relay: config.RelayConfig{
			MinPowDifficulty: 0,
			ThrottlingConfig: config.ThrottlingConfig{MaxContentLen: 64000},
		},
	}
	validator := NewPluginValidator(cfg, nil)
	secretKey := nostr.GeneratePrivateKey()
	event := nostr.Event{
		Kind:      1,
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{nostr.Tag{"expiration", "not-a-timestamp"}},
		Content:   "malformed expiration regression",
	}
	if err := event.Sign(secretKey); err != nil {
		t.Fatalf("sign test event: %v", err)
	}

	valid, reason := validator.ValidateEvent(context.Background(), event)
	if valid {
		t.Fatalf("malformed expiration event was accepted: %s", reason)
	}
	if !strings.Contains(reason, "invalid expiration tag") {
		t.Fatalf("unexpected rejection reason: %s", reason)
	}
}

func TestValidateEventAllowsAddressableKeyPackageKind(t *testing.T) {
	cfg := &config.Config{
		Relay: config.RelayConfig{
			MinPowDifficulty: 0,
			ThrottlingConfig: config.ThrottlingConfig{MaxContentLen: 64000},
		},
	}
	validator := NewPluginValidator(cfg, nil)
	secretKey := nostr.GeneratePrivateKey()
	event := nostr.Event{
		Kind:      30443,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			nostr.Tag{"d", "test-key-package"},
			nostr.Tag{"encoding", "base64"},
		},
		Content: "SGVsbG8=",
	}
	if err := event.Sign(secretKey); err != nil {
		t.Fatalf("sign test event: %v", err)
	}

	valid, reason := validator.ValidateEvent(context.Background(), event)
	if !valid {
		t.Fatalf("addressable KeyPackage event rejected: %s", reason)
	}
}

func TestValidateEventAllowsOpaqueNIP78ContentWithoutPTag(t *testing.T) {
	cfg := &config.Config{
		Relay: config.RelayConfig{
			MinPowDifficulty: 0,
			ThrottlingConfig: config.ThrottlingConfig{MaxContentLen: 64000},
		},
	}
	validator := NewPluginValidator(cfg, nil)
	secretKey := nostr.GeneratePrivateKey()
	event := nostr.Event{
		Kind:      30078,
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{nostr.Tag{"d", "user-prefs"}},
		Content:   "AgFzdG9yZWQtY2lwaGVydGV4dA==",
	}
	if err := event.Sign(secretKey); err != nil {
		t.Fatalf("sign test event: %v", err)
	}

	valid, reason := validator.ValidateEvent(context.Background(), event)
	if !valid {
		t.Fatalf("opaque NIP-78 event rejected: %s", reason)
	}
}
