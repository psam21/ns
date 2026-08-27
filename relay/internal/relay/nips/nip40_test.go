package nips

import (
	"strconv"
	"testing"
	"time"

	nostr "github.com/nbd-wtf/go-nostr"
)

func TestExpirationHelpers(t *testing.T) {
	now := time.Now().Unix()
	tests := []struct {
		name        string
		tags        nostr.Tags
		wantFound   bool
		wantExpired bool
		wantErr     bool
	}{
		{
			name:        "future expiration",
			tags:        nostr.Tags{nostr.Tag{"expiration", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)}},
			wantFound:   true,
			wantExpired: false,
		},
		{
			name:        "past expiration",
			tags:        nostr.Tags{nostr.Tag{"expiration", "1"}},
			wantFound:   true,
			wantExpired: true,
		},
		{
			name:        "malformed expiration",
			tags:        nostr.Tags{nostr.Tag{"expiration", "not-a-timestamp"}},
			wantFound:   false,
			wantExpired: false,
			wantErr:     true,
		},
		{
			name:        "no expiration",
			tags:        nostr.Tags{nostr.Tag{"t", "nostr"}},
			wantFound:   false,
			wantExpired: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := nostr.Event{Tags: tt.tags}
			if _, found := GetExpirationTime(event); found != tt.wantFound {
				t.Fatalf("GetExpirationTime found=%v, want %v", found, tt.wantFound)
			}
			if expired := IsExpired(event); expired != tt.wantExpired {
				t.Fatalf("IsExpired=%v, want %v", expired, tt.wantExpired)
			}
			err := ValidateExpirationTag(event)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateExpirationTag error=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}

	if now <= 0 {
		t.Fatal("clock returned invalid Unix timestamp")
	}
}

func TestValidateExpirationTagRejectsMalformedDuplicate(t *testing.T) {
	event := nostr.Event{Tags: nostr.Tags{
		nostr.Tag{"expiration", "1787827475"},
		nostr.Tag{"expiration", "bad"},
	}}
	if err := ValidateExpirationTag(event); err == nil {
		t.Fatal("expected malformed duplicate expiration tag to be rejected")
	}
}
