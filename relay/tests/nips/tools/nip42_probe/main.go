package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"time"

	"github.com/coder/websocket"
	nostr "github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip42"
)

func main() {
	relayURL := flag.String("relay", "wss://www.nostr.ltd", "relay WebSocket URL")
	authRelayURL := flag.String("auth-relay", "wss://nostr.ltd", "relay URL included in the AUTH event")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, *relayURL, nil)
	if err != nil {
		fail("dial relay", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "NIP-42 probe complete")

	secretKey := nostr.GeneratePrivateKey()
	pubkey, err := nostr.GetPublicKey(secretKey)
	if err != nil {
		fail("derive probe public key", err)
	}

	for {
		messageType, data, err := conn.Read(ctx)
		if err != nil {
			fail("read relay challenge", err)
		}
		if messageType != websocket.MessageText {
			continue
		}
		challenge, ok := authChallenge(data)
		if !ok {
			continue
		}

		event := nip42.CreateUnsignedAuthEvent(challenge, pubkey, *authRelayURL)
		if err := event.Sign(secretKey); err != nil {
			fail("sign AUTH event", err)
		}
		message, err := json.Marshal([]interface{}{"AUTH", event})
		if err != nil {
			fail("encode AUTH event", err)
		}
		if err := conn.Write(ctx, websocket.MessageText, message); err != nil {
			fail("send AUTH event", err)
		}

		for {
			_, response, err := conn.Read(ctx)
			if err != nil {
				fail("read AUTH acknowledgement", err)
			}
			ok, reason, matches := authAcknowledgement(response, event.ID)
			if matches {
				if !ok {
					fail("AUTH rejected", fmt.Errorf("%s", reason))
				}
				fmt.Printf("NIP-42 AUTH accepted for probe pubkey %s\n", pubkey)
				return
			}
		}
	}
}

func authChallenge(data []byte) (string, bool) {
	var frame []json.RawMessage
	if err := json.Unmarshal(data, &frame); err != nil || len(frame) != 2 {
		return "", false
	}
	var command, challenge string
	if json.Unmarshal(frame[0], &command) != nil || command != "AUTH" || json.Unmarshal(frame[1], &challenge) != nil || challenge == "" {
		return "", false
	}
	return challenge, true
}

func authAcknowledgement(data []byte, eventID string) (bool, string, bool) {
	var frame []json.RawMessage
	if err := json.Unmarshal(data, &frame); err != nil || len(frame) < 3 {
		return false, "", false
	}
	var command, responseID string
	if json.Unmarshal(frame[0], &command) != nil || command != "OK" || json.Unmarshal(frame[1], &responseID) != nil || responseID != eventID {
		return false, "", false
	}
	var accepted bool
	if err := json.Unmarshal(frame[2], &accepted); err != nil {
		return false, "invalid acknowledgement", true
	}
	reason := ""
	if len(frame) >= 4 {
		_ = json.Unmarshal(frame[3], &reason)
	}
	return accepted, reason, true
}

func fail(operation string, err error) {
	panic(fmt.Errorf("%s: %w", operation, err))
}
