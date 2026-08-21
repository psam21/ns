package nips

import (
	"context"
	"fmt"
	"time"

	nostr "github.com/nbd-wtf/go-nostr"
)

// NIP-46: Nostr Remote Signing
// https://github.com/nostr-protocol/nips/blob/master/46.md
//
// SPEC UPDATE (last month): Avoid silent timeouts
// Added timeout handling for NIP-46 requests to prevent silent failures.
//
// Event kinds:
//   - 24133: Request/Response events (NIP-44 encrypted)
//   - 24242: Auth challenges

const (
	// NIP46RequestTimeout is the maximum time allowed for a NIP-46 request
	NIP46RequestTimeout = 30 * time.Second
	// NIP46PingInterval is the interval for ping/pong keepalive
	NIP46PingIntervalConst = 60 * time.Second
	// NIP46SessionTimeout is the maximum session duration
	NIP46SessionTimeoutConst = 24 * time.Hour
)

// NIP46Method represents a NIP-46 method name
type NIP46Method string

const (
	NIP46MethodConnect        NIP46Method = "connect"
	NIP46MethodSignEvent      NIP46Method = "sign_event"
	NIP46MethodPing           NIP46Method = "ping"
	NIP46MethodGetPublicKey   NIP46Method = "get_public_key"
	NIP46MethodNIP04Encrypt   NIP46Method = "nip04_encrypt"
	NIP46MethodNIP04Decrypt   NIP46Method = "nip04_decrypt"
	NIP46MethodNIP44Encrypt   NIP46Method = "nip44_encrypt"
	NIP46MethodNIP44Decrypt   NIP46Method = "nip44_decrypt"
	NIP46MethodSwitchRelays   NIP46Method = "switch_relays"
	NIP46MethodLogout         NIP46Method = "logout"
)

// NIP46Request represents a parsed NIP-46 request
type NIP46Request struct {
	ID     string        `json:"id"`
	Method NIP46Method   `json:"method"`
	Params []string      `json:"params"`
}

// NIP46Response represents a NIP-46 response
type NIP46Response struct {
	ID     string `json:"id"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// NIP46Permissions represents requested permissions
type NIP46Permissions struct {
	Methods []string `json:"methods"`
}

// NIP46ClientMetadata represents client metadata
type NIP46ClientMetadata struct {
	Name  string `json:"name,omitempty"`
	URL   string `json:"url,omitempty"`
	Image string `json:"image,omitempty"`
}

// ValidateNIP46Request validates a NIP-46 request event (kind 24133)
func ValidateNIP46Request(evt *nostr.Event) error {
	if evt.Kind != 24133 {
		return fmt.Errorf("invalid event kind for NIP-46 request: %d", evt.Kind)
	}

	// Must have "p" tag with remote-signer pubkey
	hasPTag := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			if len(tag[1]) != 64 {
				return fmt.Errorf("invalid pubkey in 'p' tag: %s", tag[1])
			}
			hasPTag = true
			break
		}
	}

	if !hasPTag {
		return fmt.Errorf("NIP-46 request must have 'p' tag with remote-signer pubkey")
	}

	// Content must be NIP-44 encrypted
	if evt.Content == "" {
		return fmt.Errorf("NIP-46 request must have encrypted content")
	}

	// Validate NIP-44 format
	if !IsNIP44Payload(evt.Content) {
		return fmt.Errorf("invalid NIP-44 content in NIP-46 request")
	}

	// CreatedAt should be recent (within 5 minutes for requests)
	if time.Since(time.Unix(int64(evt.CreatedAt), 0)) > 5*time.Minute {
		return fmt.Errorf("NIP-46 request timestamp too old")
	}

	return nil
}

// ValidateNIP46Response validates a NIP-46 response event (kind 24133)
func ValidateNIP46Response(evt *nostr.Event) error {
	if evt.Kind != 24133 {
		return fmt.Errorf("invalid event kind for NIP-46 response: %d", evt.Kind)
	}

	// Must have "p" tag with client pubkey
	hasPTag := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			if len(tag[1]) != 64 {
				return fmt.Errorf("invalid pubkey in 'p' tag: %s", tag[1])
			}
			hasPTag = true
			break
		}
	}

	if !hasPTag {
		return fmt.Errorf("NIP-46 response must have 'p' tag with client pubkey")
	}

	// Content must be NIP-44 encrypted
	if evt.Content == "" {
		return fmt.Errorf("NIP-46 response must have encrypted content")
	}

	// Validate NIP-44 format
	if !IsNIP44Payload(evt.Content) {
		return fmt.Errorf("invalid NIP-44 content in NIP-46 response")
	}

	return nil
}

// NIP46RequestWithTimeout wraps a NIP-46 request with timeout handling
// SPEC UPDATE (last month): Avoid silent timeouts
// Per the spec update, remote-signers SHOULD always respond to requests,
// even if the response is an error. Silent timeouts (where the remote-signer
// simply doesn't respond) should be avoided. This function enforces a timeout
// on the client side to detect such cases.
func NIP46RequestWithTimeout(ctx context.Context, timeout time.Duration, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- fn(ctx)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// Per spec update: timeout indicates a silent timeout from remote-signer
		return fmt.Errorf("NIP-46 silent timeout after %v (remote-signer did not respond)", timeout)
	}
}

// NIP46PingInterval returns the recommended ping interval
func NIP46PingInterval() time.Duration {
	return NIP46PingIntervalConst
}

// NIP46SessionTimeout returns the maximum session duration
func NIP46SessionTimeout() time.Duration {
	return NIP46SessionTimeoutConst
}

// IsNIP46Request checks if an event is a NIP-46 request
func IsNIP46Request(evt *nostr.Event) bool {
	return evt.Kind == 24133
}

// IsNIP46AuthChallenge checks if an event is a NIP-46 auth challenge
func IsNIP46AuthChallenge(evt *nostr.Event) bool {
	return evt.Kind == 24242
}

