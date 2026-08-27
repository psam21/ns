package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/coder/websocket"
	nostr "github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip42"
)

func main() {
	relayURL := flag.String("relay", "wss://www.nostr.ltd", "relay WebSocket URL")
	authRelayURL := flag.String("auth-relay", "wss://nostr.ltd", "relay URL included in the AUTH event")
	queryID := flag.String("query-id", "", "optional event ID to retrieve after authentication")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, *relayURL, nil)
	if err != nil {
		fail("dial relay", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "NIP-42 probe complete")

	secretKey := os.Getenv("NIP_TEST_SECRET_KEY")
	if secretKey == "" {
		secretKey = nostr.GeneratePrivateKey()
	}
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
			accepted, reason, matches := authAcknowledgement(response, event.ID)
			if !matches {
				continue
			}
			if !accepted {
				fail("AUTH rejected", fmt.Errorf("%s", reason))
			}
			if *queryID == "" {
				fmt.Printf("NIP-42 AUTH accepted for probe pubkey %s\n", pubkey)
				return
			}
			if err := queryEvent(ctx, conn, *queryID); err != nil {
				fail("authenticated event query", err)
			}
			return
		}
	}
}

func queryEvent(ctx context.Context, conn *websocket.Conn, eventID string) error {
	subscriptionID := fmt.Sprintf("nip-query-%d", time.Now().UnixNano())
	request, err := json.Marshal([]interface{}{"REQ", subscriptionID, map[string]interface{}{"ids": []string{eventID}, "limit": 1}})
	if err != nil {
		return fmt.Errorf("encode REQ: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, request); err != nil {
		return fmt.Errorf("send REQ: %w", err)
	}
	defer func() {
		closeMessage, _ := json.Marshal([]interface{}{"CLOSE", subscriptionID})
		_ = conn.Write(ctx, websocket.MessageText, closeMessage)
	}()

	for {
		messageType, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("read event response: %w", err)
		}
		if messageType != websocket.MessageText {
			continue
		}
		command, ok := frameCommand(data)
		if !ok {
			continue
		}
		switch command {
		case "EVENT":
			var frame []json.RawMessage
			if err := json.Unmarshal(data, &frame); err != nil || len(frame) < 3 {
				continue
			}
			var event nostr.Event
			if err := json.Unmarshal(frame[2], &event); err != nil || event.ID != eventID {
				continue
			}
			encoded, err := json.Marshal(event)
			if err != nil {
				return fmt.Errorf("encode event: %w", err)
			}
			fmt.Println(string(encoded))
			return nil
		case "EOSE":
			return fmt.Errorf("event %s not found", eventID)
		case "CLOSED", "NOTICE":
			return fmt.Errorf("relay rejected event query: %s", string(data))
		}
	}
}

func frameCommand(data []byte) (string, bool) {
	var frame []json.RawMessage
	if err := json.Unmarshal(data, &frame); err != nil || len(frame) == 0 {
		return "", false
	}
	var command string
	if err := json.Unmarshal(frame[0], &command); err != nil {
		return "", false
	}
	return command, true
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
