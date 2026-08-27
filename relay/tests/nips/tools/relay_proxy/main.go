package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip42"
)

type bridge struct {
	downstream   *websocket.Conn
	upstream     *websocket.Conn
	relayURL     string
	secretKey    string
	writeMu      sync.Mutex
	authMu       sync.Mutex
	authEventIDs map[string]struct{}
}

func main() {
	listen := flag.String("listen", "127.0.0.1:18080", "local listen address")
	upstreamURL := flag.String("upstream", "wss://www.nostr.ltd", "upstream relay websocket URL")
	relayURL := flag.String("relay-url", "wss://nostr.ltd", "relay URL placed in NIP-42 AUTH events")
	flag.Parse()

	secretKey := nostr.GeneratePrivateKey()
	if _, err := nostr.GetPublicKey(secretKey); err != nil {
		log.Fatalf("generate test signer: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		downstream, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			log.Printf("accept downstream websocket: %v", err)
			return
		}
		defer downstream.Close(websocket.StatusNormalClosure, "test bridge complete")

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		upstream, _, err := websocket.Dial(ctx, *upstreamURL, nil)
		if err != nil {
			log.Printf("dial upstream websocket: %v", err)
			return
		}
		defer upstream.Close(websocket.StatusNormalClosure, "test bridge complete")

		b := &bridge{
			downstream:   downstream,
			upstream:     upstream,
			relayURL:     *relayURL,
			secretKey:    secretKey,
			authEventIDs: make(map[string]struct{}),
		}

		errs := make(chan error, 2)
		go func() { errs <- b.forwardDownstream(ctx) }()
		go func() { errs <- b.forwardUpstream(ctx) }()
		<-errs
		cancel()
	})

	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("NIP test websocket bridge listening on %s -> %s", *listen, *upstreamURL)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func (b *bridge) forwardDownstream(ctx context.Context) error {
	for {
		messageType, data, err := b.downstream.Read(ctx)
		if err != nil {
			return err
		}
		b.writeMu.Lock()
		err = b.upstream.Write(ctx, messageType, data)
		b.writeMu.Unlock()
		if err != nil {
			return err
		}
	}
}

func (b *bridge) forwardUpstream(ctx context.Context) error {
	for {
		messageType, data, err := b.upstream.Read(ctx)
		if err != nil {
			return err
		}
		challenge, isAuth := authChallenge(data)
		if isAuth {
			if err := b.answerAuth(ctx, challenge); err != nil {
				return err
			}
			continue
		}
		if b.isAuthAcknowledgement(data) {
			continue
		}
		if err := b.downstream.Write(ctx, messageType, data); err != nil {
			return err
		}
	}
}

func authChallenge(data []byte) (string, bool) {
	var frame []json.RawMessage
	if err := json.Unmarshal(data, &frame); err != nil || len(frame) != 2 {
		return "", false
	}
	var command string
	if err := json.Unmarshal(frame[0], &command); err != nil || command != "AUTH" {
		return "", false
	}
	var challenge string
	if err := json.Unmarshal(frame[1], &challenge); err != nil || challenge == "" {
		return "", false
	}
	return challenge, true
}

func (b *bridge) isAuthAcknowledgement(data []byte) bool {
	var frame []json.RawMessage
	if err := json.Unmarshal(data, &frame); err != nil || len(frame) < 2 {
		return false
	}
	var command, eventID string
	if json.Unmarshal(frame[0], &command) != nil || command != "OK" || json.Unmarshal(frame[1], &eventID) != nil {
		return false
	}
	b.authMu.Lock()
	defer b.authMu.Unlock()
	if _, ok := b.authEventIDs[eventID]; ok {
		delete(b.authEventIDs, eventID)
		return true
	}
	return false
}

func (b *bridge) answerAuth(ctx context.Context, challenge string) error {
	pubkey, err := nostr.GetPublicKey(b.secretKey)
	if err != nil {
		return fmt.Errorf("derive test signer public key: %w", err)
	}
	event := nip42.CreateUnsignedAuthEvent(challenge, pubkey, b.relayURL)
	if err := event.Sign(b.secretKey); err != nil {
		return fmt.Errorf("sign NIP-42 AUTH event: %w", err)
	}
	message, err := json.Marshal([]interface{}{"AUTH", event})
	if err != nil {
		return fmt.Errorf("encode NIP-42 AUTH event: %w", err)
	}
	b.authMu.Lock()
	b.authEventIDs[event.ID] = struct{}{}
	b.authMu.Unlock()
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	return b.upstream.Write(ctx, websocket.MessageText, message)
}
