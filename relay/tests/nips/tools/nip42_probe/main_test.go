package main

import (
	"strings"
	"testing"
)

func TestAuthChallenge(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{name: "valid", input: `["AUTH","challenge-123"]`, want: "challenge-123", ok: true},
		{name: "wrong command", input: `["NOTICE","challenge-123"]`, ok: false},
		{name: "empty challenge", input: `["AUTH",""]`, ok: false},
		{name: "malformed", input: `not-json`, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := authChallenge([]byte(tt.input))
			if ok != tt.ok || got != tt.want {
				t.Fatalf("authChallenge(%q) = (%q, %v), want (%q, %v)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestAcknowledgement(t *testing.T) {
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tests := []struct {
		name      string
		input     string
		wantOK    bool
		wantMatch bool
	}{
		{name: "accepted", input: `["OK","` + id + `",true,""]`, wantOK: true, wantMatch: true},
		{name: "rejected", input: `["OK","` + id + `",false,"auth-required: test"]`, wantOK: false, wantMatch: true},
		{name: "other event", input: `["OK","bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",true,""]`, wantMatch: false},
		{name: "malformed accepted flag", input: `["OK","` + id + `","yes",""]`, wantMatch: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOK, _, gotMatch := eventAcknowledgement([]byte(tt.input), id)
			if gotOK != tt.wantOK || gotMatch != tt.wantMatch {
				t.Fatalf("eventAcknowledgement(%q) = (%v, %v), want (%v, %v)", tt.input, gotOK, gotMatch, tt.wantOK, tt.wantMatch)
			}
		})
	}
}

func TestParseQueryResponse(t *testing.T) {
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const otherID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	eventFrame := `{"kind":1059,"id":"` + id + `","pubkey":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","created_at":1,"tags":[],"content":"ciphertext","sig":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}`

	event, done, err := parseQueryResponse([]byte(`["EVENT","sub",`+eventFrame+`]`), id)
	if err != nil || !done || event == nil || event.ID != id {
		t.Fatalf("valid EVENT parsed as event=%v done=%v err=%v", event, done, err)
	}

	event, done, err = parseQueryResponse([]byte(`["EVENT","sub",`+eventFrame+`]`), otherID)
	if err != nil || done || event != nil {
		t.Fatalf("nonmatching EVENT parsed as event=%v done=%v err=%v", event, done, err)
	}

	_, done, err = parseQueryResponse([]byte(`["EOSE","sub"]`), id)
	if !done || err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("EOSE = done=%v err=%v, want not-found terminal error", done, err)
	}

	_, done, err = parseQueryResponse([]byte(`["CLOSED","sub","auth-required: test"]`), id)
	if !done || err == nil || !strings.Contains(err.Error(), "auth-required") {
		t.Fatalf("CLOSED = done=%v err=%v, want rejection error", done, err)
	}
}
