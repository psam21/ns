package relay

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Shugur-Network/relay/internal/logger"
	nostr "github.com/nbd-wtf/go-nostr"
	"go.uber.org/zap"
)

// NIP-43: Relay Access Metadata and Requests
// https://github.com/nostr-protocol/nips/blob/master/43.md
//
// Event kinds:
//   13534 — Membership list (relay-signed, replaceable)
//    8000 — Add user (relay-signed)
//    8001 — Remove user (relay-signed)
//   28934 — Join request (user-sent, with claim/invite code)
//   28935 — Invite request (ephemeral, relay-generated on the fly)
//   28936 — Leave request (user-sent)
//   10010 — Relay membership list (user-signed, replaceable)

// MembershipStore manages NIP-43 relay membership and invite codes.
type MembershipStore struct {
	mu          sync.RWMutex
	members     map[string]time.Time // pubkey -> joined at
	inviteCodes map[string]*InviteCode
}

// InviteCode represents a relay invite claim string.
type InviteCode struct {
	Code      string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedBy    string // pubkey that redeemed it, empty if unused
	CreatedBy string // admin pubkey that generated it (empty if relay-auto)
}

var (
	membershipStoreInstance *MembershipStore
	membershipOnce         sync.Once
)

// GetMembershipStore returns the singleton membership store.
func GetMembershipStore() *MembershipStore {
	membershipOnce.Do(func() {
		membershipStoreInstance = &MembershipStore{
			members:     make(map[string]time.Time),
			inviteCodes: make(map[string]*InviteCode),
		}
		logger.New("nip43").Info("NIP-43 membership store initialized")
	})
	return membershipStoreInstance
}

// --- Membership Management ---

// IsMember checks if a pubkey is a member of the relay.
func (ms *MembershipStore) IsMember(pubkey string) bool {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	_, ok := ms.members[strings.ToLower(pubkey)]
	return ok
}

// AddMember adds a pubkey to the membership list.
func (ms *MembershipStore) AddMember(pubkey string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.members[strings.ToLower(pubkey)] = time.Now()
}

// RemoveMember removes a pubkey from the membership list.
func (ms *MembershipStore) RemoveMember(pubkey string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	delete(ms.members, strings.ToLower(pubkey))
}

// GetMembers returns all current member pubkeys.
func (ms *MembershipStore) GetMembers() []string {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	members := make([]string, 0, len(ms.members))
	for pk := range ms.members {
		members = append(members, pk)
	}
	return members
}

// MemberCount returns the number of members.
func (ms *MembershipStore) MemberCount() int {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return len(ms.members)
}

// --- Invite Code Management ---

// GenerateInviteCode creates a new invite code with an expiry duration.
func (ms *MembershipStore) GenerateInviteCode(createdBy string, ttl time.Duration) *InviteCode {
	code := generateRandomCode(16)
	now := time.Now()

	invite := &InviteCode{
		Code:      code,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
		CreatedBy: createdBy,
	}

	ms.mu.Lock()
	ms.inviteCodes[code] = invite
	ms.mu.Unlock()

	logger.New("nip43").Info("Invite code generated",
		zap.String("code_prefix", code[:8]+"..."),
		zap.Duration("ttl", ttl),
		zap.String("created_by", createdBy))

	return invite
}

// ValidateInviteCode checks if an invite code is valid and unused.
func (ms *MembershipStore) ValidateInviteCode(code string) error {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	invite, ok := ms.inviteCodes[code]
	if !ok {
		return fmt.Errorf("restricted: that is an invalid invite code")
	}
	if invite.UsedBy != "" {
		return fmt.Errorf("restricted: that invite code has already been used")
	}
	if time.Now().After(invite.ExpiresAt) {
		return fmt.Errorf("restricted: that invite code is expired")
	}
	return nil
}

// RedeemInviteCode marks an invite code as used by a pubkey.
func (ms *MembershipStore) RedeemInviteCode(code, pubkey string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	invite, ok := ms.inviteCodes[code]
	if !ok {
		return fmt.Errorf("restricted: that is an invalid invite code")
	}
	if invite.UsedBy != "" {
		return fmt.Errorf("restricted: that invite code has already been used")
	}
	if time.Now().After(invite.ExpiresAt) {
		return fmt.Errorf("restricted: that invite code is expired")
	}
	invite.UsedBy = strings.ToLower(pubkey)
	return nil
}

// CleanExpired removes expired and used invite codes.
func (ms *MembershipStore) CleanExpired() int {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	count := 0
	now := time.Now()
	for code, invite := range ms.inviteCodes {
		if now.After(invite.ExpiresAt) || invite.UsedBy != "" {
			delete(ms.inviteCodes, code)
			count++
		}
	}
	return count
}

// --- NIP-43 Event Handling ---

// IsNIP43Event returns true if the event kind is a NIP-43 kind.
func IsNIP43Event(evt *nostr.Event) bool {
	switch evt.Kind {
	case 13534, 8000, 8001, 28934, 28935, 28936, 10010:
		return true
	}
	return false
}

// HandleNIP43Event processes a NIP-43 event. Returns (accepted bool, message string, relayEvents []*nostr.Event).
// relayEvents are events the relay should sign and store (membership list updates, add/remove events).
func (ms *MembershipStore) HandleNIP43Event(evt *nostr.Event) (bool, string, []*nostr.Event) {
	gs := GetGroupStore()
	if gs == nil {
		return false, "error: relay key not initialized", nil
	}

	switch evt.Kind {
	case 28934:
		return ms.handleJoinRequest(evt, gs)
	case 28936:
		return ms.handleLeaveRequest(evt, gs)
	case 10010:
		// User's relay membership list — just accept and store
		return true, "", nil
	case 13534, 8000, 8001:
		// Relay-signed events — only accept from relay's own pubkey
		if strings.ToLower(evt.PubKey) != strings.ToLower(gs.GetRelayPubkey()) {
			return false, "restricted: only relay can publish kind " + fmt.Sprintf("%d", evt.Kind), nil
		}
		return true, "", nil
	case 28935:
		// Invite requests are generated by the relay, not stored
		return false, "restricted: kind 28935 is relay-generated", nil
	}

	return false, "error: unhandled NIP-43 kind", nil
}

// handleJoinRequest processes a kind 28934 join request with an invite code.
func (ms *MembershipStore) handleJoinRequest(evt *nostr.Event, gs *GroupStore) (bool, string, []*nostr.Event) {
	// Validate timestamp (must be within 5 minutes of now)
	evtTime := time.Unix(int64(evt.CreatedAt), 0)
	if math.Abs(time.Since(evtTime).Minutes()) > 5 {
		return false, "restricted: event timestamp is too far from current time", nil
	}

	// Must have "-" tag (NIP-70 protected)
	if !hasProtectedTag(evt) {
		return false, "restricted: join request must include '-' tag", nil
	}

	// Extract claim code
	claimCode := ""
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "claim" {
			claimCode = tag[1]
			break
		}
	}
	if claimCode == "" {
		return false, "restricted: join request must include 'claim' tag with invite code", nil
	}

	pubkey := strings.ToLower(evt.PubKey)

	// Check if already a member
	if ms.IsMember(pubkey) {
		return true, "duplicate: you are already a member of this relay", nil
	}

	// Validate and redeem the invite code
	if err := ms.RedeemInviteCode(claimCode, pubkey); err != nil {
		return false, err.Error(), nil
	}

	// Add to membership
	ms.AddMember(pubkey)

	logger.New("nip43").Info("New member joined via invite code",
		zap.String("pubkey", pubkey))

	// Generate relay-signed events: kind 8000 (add user) + updated kind 13534 (membership list)
	var relayEvents []*nostr.Event

	addEvt := ms.createAddUserEvent(pubkey, gs)
	if addEvt != nil {
		relayEvents = append(relayEvents, addEvt)
	}

	memberListEvt := ms.createMembershipListEvent(gs)
	if memberListEvt != nil {
		relayEvents = append(relayEvents, memberListEvt)
	}

	return true, "info: welcome!", relayEvents
}

// handleLeaveRequest processes a kind 28936 leave request.
func (ms *MembershipStore) handleLeaveRequest(evt *nostr.Event, gs *GroupStore) (bool, string, []*nostr.Event) {
	// Validate timestamp
	evtTime := time.Unix(int64(evt.CreatedAt), 0)
	if math.Abs(time.Since(evtTime).Minutes()) > 5 {
		return false, "restricted: event timestamp is too far from current time", nil
	}

	// Must have "-" tag
	if !hasProtectedTag(evt) {
		return false, "restricted: leave request must include '-' tag", nil
	}

	pubkey := strings.ToLower(evt.PubKey)

	if !ms.IsMember(pubkey) {
		return false, "restricted: you are not a member of this relay", nil
	}

	ms.RemoveMember(pubkey)

	logger.New("nip43").Info("Member left the relay",
		zap.String("pubkey", pubkey))

	var relayEvents []*nostr.Event

	removeEvt := ms.createRemoveUserEvent(pubkey, gs)
	if removeEvt != nil {
		relayEvents = append(relayEvents, removeEvt)
	}

	memberListEvt := ms.createMembershipListEvent(gs)
	if memberListEvt != nil {
		relayEvents = append(relayEvents, memberListEvt)
	}

	return true, "", relayEvents
}

// GenerateInviteEvent creates a kind 28935 ephemeral invite event for a requesting user.
func (ms *MembershipStore) GenerateInviteEvent(gs *GroupStore, ttl time.Duration) *nostr.Event {
	if gs == nil || gs.relayPrivateKey == "" {
		return nil
	}

	invite := ms.GenerateInviteCode("relay", ttl)

	evt := &nostr.Event{
		Kind:      28935,
		PubKey:    gs.relayPubkey,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"claim", invite.Code},
			{"-"},
		},
		Content: "",
	}

	if err := evt.Sign(gs.relayPrivateKey); err != nil {
		logger.New("nip43").Error("Failed to sign invite event", zap.Error(err))
		return nil
	}

	return evt
}

// --- Relay-signed event creation ---

// createAddUserEvent creates a kind 8000 relay-signed event for adding a member.
func (ms *MembershipStore) createAddUserEvent(pubkey string, gs *GroupStore) *nostr.Event {
	if gs.relayPrivateKey == "" {
		return nil
	}

	evt := &nostr.Event{
		Kind:      8000,
		PubKey:    gs.relayPubkey,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"-"},
			{"p", pubkey},
		},
		Content: "",
	}

	if err := evt.Sign(gs.relayPrivateKey); err != nil {
		logger.New("nip43").Error("Failed to sign add-user event", zap.Error(err))
		return nil
	}

	return evt
}

// createRemoveUserEvent creates a kind 8001 relay-signed event for removing a member.
func (ms *MembershipStore) createRemoveUserEvent(pubkey string, gs *GroupStore) *nostr.Event {
	if gs.relayPrivateKey == "" {
		return nil
	}

	evt := &nostr.Event{
		Kind:      8001,
		PubKey:    gs.relayPubkey,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"-"},
			{"p", pubkey},
		},
		Content: "",
	}

	if err := evt.Sign(gs.relayPrivateKey); err != nil {
		logger.New("nip43").Error("Failed to sign remove-user event", zap.Error(err))
		return nil
	}

	return evt
}

// createMembershipListEvent creates a kind 13534 relay-signed membership list event.
func (ms *MembershipStore) createMembershipListEvent(gs *GroupStore) *nostr.Event {
	if gs.relayPrivateKey == "" {
		return nil
	}

	members := ms.GetMembers()

	tags := nostr.Tags{{"-"}}
	for _, pk := range members {
		tags = append(tags, nostr.Tag{"member", pk})
	}

	evt := &nostr.Event{
		Kind:      13534,
		PubKey:    gs.relayPubkey,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags:      tags,
		Content:   "",
	}

	if err := evt.Sign(gs.relayPrivateKey); err != nil {
		logger.New("nip43").Error("Failed to sign membership list event", zap.Error(err))
		return nil
	}

	return evt
}

// hasProtectedTag checks if an event has the NIP-70 "-" tag.
func hasProtectedTag(evt *nostr.Event) bool {
	for _, tag := range evt.Tags {
		if len(tag) >= 1 && tag[0] == "-" {
			return true
		}
	}
	return false
}

// generateRandomCode generates a hex-encoded random code of the given byte length.
func generateRandomCode(byteLen int) string {
	b := make([]byte, byteLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
