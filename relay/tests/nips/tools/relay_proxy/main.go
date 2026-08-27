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
		upstream, err := dialUpstream(ctx, *upstreamURL)
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
		if err := b.preflightAuth(ctx); err != nil {
			_ = downstream.Close(websocket.StatusInternalError, "upstream authentication preflight failed")
			_ = upstream.Close(websocket.StatusInternalError, "authentication preflight failed")
			return
		}

		errs := make(chan error, 2)
		go func() { errs <- b.forwardDownstream(ctx) }()
		go func() { errs <- b.forwardUpstream(ctx) }()
		if err := <-errs; err != nil {
			status := websocket.CloseStatus(err)
			if status != websocket.StatusNormalClosure && status != websocket.StatusGoingAway {
				log.Printf("bridge connection ended: %v", err)
			}
		}
		cancel()
	})

	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("NIP test websocket bridge listening on %s -> %s", *listen, *upstreamURL)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func dialUpstream(ctx context.Context, upstreamURL string) (*websocket.Conn, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		conn, _, err := websocket.Dial(dialCtx, upstreamURL, nil)
		cancel()
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
		log.Printf("upstream websocket dial attempt %d/3 failed: %v", attempt, err)
		if attempt < 3 {
			select {
			case <-ctx.Done():
				break
			case <-time.After(2 * time.Second):
			}
		}
	}
	return nil, lastErr
}

func (b *bridge) preflightAuth(ctx context.Context) error {
	probeID := fmt.Sprintf("bridge-auth-%d", time.Now().UnixNano())
	probe, err := json.Marshal([]interface{}{"REQ", probeID, map[string]interface{}{"kinds": []int{1059}, "limit": 1}})
	if err != nil {
		return fmt.Errorf("encode authentication probe: %w", err)
	}
	b.writeMu.Lock()
	writeErr := b.upstream.Write(ctx, websocket.MessageText, probe)
	b.writeMu.Unlock()
	if writeErr != nil {
		return fmt.Errorf("send authentication probe: %w", writeErr)
	}

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		messageType, data, err := b.upstream.Read(probeCtx)
		if err != nil {
			if probeCtx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read authentication probe: %w", err)
		}
		if messageType != websocket.MessageText {
			continue
		}
		if challenge, ok := authChallenge(data); ok {
			if err := b.answerAuth(ctx, challenge); err != nil {
				return err
			}
			if err := b.waitForAuthAcknowledgement(probeCtx); err != nil {
				return err
			}
			// Keep this upstream session open. Closing the probe immediately after
			// AUTH can cause some relays to close the whole connection before the
			// downstream test request is forwarded.
			return nil
		}
		if command, ok := frameCommand(data); ok && (command == "EOSE" || command == "NOTICE") {
			return b.closeProbe(ctx, probeID)
		}
	}
}

func (b *bridge) waitForAuthAcknowledgement(ctx context.Context) error {
	for {
		messageType, data, err := b.upstream.Read(ctx)
		if err != nil {
			return fmt.Errorf("read AUTH acknowledgement: %w", err)
		}
		if messageType != websocket.MessageText {
			continue
		}
		if b.isAuthAcknowledgement(data) {
			var frame []json.RawMessage
			if err := json.Unmarshal(data, &frame); err != nil || len(frame) < 3 {
				return fmt.Errorf("invalid AUTH acknowledgement")
			}
			var accepted bool
			if err := json.Unmarshal(frame[2], &accepted); err != nil || !accepted {
				return fmt.Errorf("upstream rejected AUTH event")
			}
			return nil
		}
		if challenge, ok := authChallenge(data); ok {
			if err := b.answerAuth(ctx, challenge); err != nil {
				return err
			}
		}
	}
}

func (b *bridge) closeProbe(ctx context.Context, probeID string) error {
	closeMessage, err := json.Marshal([]interface{}{"CLOSE", probeID})
	if err != nil {
		return fmt.Errorf("encode authentication probe close: %w", err)
	}
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	if err := b.upstream.Write(ctx, websocket.MessageText, closeMessage); err != nil {
		return fmt.Errorf("close authentication probe: %w", err)
	}
	return nil
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
