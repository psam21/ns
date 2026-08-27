package main

import (
	"testing"

	"github.com/coder/websocket"
)

func TestAuthChallenge(t *testing.T) {
	challenge, ok := authChallenge([]byte(`["AUTH","challenge-123"]`))
	if !ok || challenge != "challenge-123" {
		t.Fatalf("authChallenge() = %q, %v", challenge, ok)
	}
	if _, ok := authChallenge([]byte(`["EVENT",{}]`)); ok {
		t.Fatal("EVENT frame must not be treated as an AUTH challenge")
	}
	if _, ok := authChallenge([]byte(`["AUTH"]`)); ok {
		t.Fatal("incomplete AUTH frame must not be accepted")
	}
}

func TestAuthAcknowledgement(t *testing.T) {
	bridge := &bridge{authEventIDs: map[string]struct{}{"auth-event": {}}}
	if !bridge.isAuthAcknowledgement([]byte(`["OK","auth-event",true,"authenticated"]`)) {
		t.Fatal("expected matching AUTH acknowledgement")
	}
	if bridge.isAuthAcknowledgement([]byte(`["OK","auth-event",true,"authenticated"]`)) {
		t.Fatal("AUTH acknowledgement must be consumed once")
	}
	if bridge.isAuthAcknowledgement([]byte(`["OK","other-event",true,"saved"]`)) {
		t.Fatal("ordinary event acknowledgement must be forwarded")
	}
}

func TestBridgeUsesTextMessages(t *testing.T) {
	if websocket.MessageText != 1 {
		t.Fatalf("unexpected websocket text message constant: %d", websocket.MessageText)
	}
}
