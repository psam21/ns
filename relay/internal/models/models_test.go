package models

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	nostr "github.com/nbd-wtf/go-nostr"
)

func TestRelayInfoJSONRoundTrip(t *testing.T) {
	want := RelayInfo{
		Address:   "wss://relay.example",
		PeerID:    "peer-1",
		PublicKey: "pubkey",
		IsActive:  true,
		IsSynced:  true,
		LastSeen:  time.Unix(123, 0).UTC(),
	}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got RelayInfo
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestSyncMessageJSONRoundTrip(t *testing.T) {
	want := SyncMessage{Cmd: "SyncChunk", Since: 10, Limit: 2, Kinds: []int{1, 7}, Events: []nostr.Event{{Kind: 1, Content: "hello"}}}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got SyncMessage
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if got.Cmd != want.Cmd || got.Since != want.Since || got.Limit != want.Limit || !reflect.DeepEqual(got.Kinds, want.Kinds) || len(got.Events) != 1 || got.Events[0].Content != "hello" {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}
