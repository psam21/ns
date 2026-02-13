package relay

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Shugur-Network/relay/internal/config"
	"github.com/Shugur-Network/relay/internal/logger"
	nostr "github.com/nbd-wtf/go-nostr"
	"go.uber.org/zap"
)

// NIP-29: Relay-based Groups
// Implements relay-managed groups with membership enforcement,
// moderation events, and relay-signed metadata.

// Group represents a NIP-29 relay-managed group.
type Group struct {
	ID         string            // random group identifier (a-z0-9-_)
	Name       string            // display name
	Picture    string            // group picture URL
	About      string            // group description
	Members    map[string]bool   // pubkey -> is member
	Admins     map[string][]string // pubkey -> list of roles
	Roles      map[string]string // role name -> description
	Private    bool              // only members can read
	Restricted bool              // only members can write (previously called "closed" for writing)
	Hidden     bool              // hide metadata from non-members
	Closed     bool              // join requests not honored
	InviteCodes map[string]bool  // valid invite codes
	CreatedAt  time.Time
}

// GroupStore manages all NIP-29 groups in memory.
type GroupStore struct {
	mu     sync.RWMutex
	groups map[string]*Group // group ID -> Group
	relayPrivateKey string  // hex-encoded secp256k1 private key for signing
	relayPubkey     string  // hex-encoded public key
	cfg             *config.Config
}

// groupStoreInstance is the package-level NIP-29 group store singleton.
var groupStoreInstance *GroupStore

// GetGroupStore returns the package-level NIP-29 group store.
func GetGroupStore() *GroupStore {
	return groupStoreInstance
}

// InitGroupStore initializes the package-level group store. Called from NewServer.
func InitGroupStore(cfg *config.Config) *GroupStore {
	groupStoreInstance = NewGroupStore(cfg)
	return groupStoreInstance
}

// NewGroupStore creates a new group store, initializing or generating the relay keypair.
func NewGroupStore(cfg *config.Config) *GroupStore {
	gs := &GroupStore{
		groups: make(map[string]*Group),
		cfg:    cfg,
	}

	// Initialize relay keypair for signing group metadata events
	if cfg.Relay.PrivateKey != "" {
		gs.relayPrivateKey = cfg.Relay.PrivateKey
		// Derive public key from private key
		pub, err := nostr.GetPublicKey(cfg.Relay.PrivateKey)
		if err != nil {
			logger.New("nip29").Error("Invalid relay private key, generating new one",
				zap.Error(err))
			gs.generateKeypair()
		} else {
			gs.relayPubkey = pub
		}
	} else if cfg.Relay.PublicKey != "" {
		// Public key set but no private key — cannot sign events
		gs.relayPubkey = cfg.Relay.PublicKey
		logger.New("nip29").Warn("Relay public key set but no private key — NIP-29 metadata signing disabled")
	} else {
		// Auto-generate keypair
		gs.generateKeypair()
	}

	// Update config with derived/generated public key
	if gs.relayPubkey != "" && cfg.Relay.PublicKey == "" {
		cfg.Relay.PublicKey = gs.relayPubkey
	}

	logger.New("nip29").Info("NIP-29 group store initialized",
		zap.String("relay_pubkey", gs.relayPubkey))

	return gs
}

func (gs *GroupStore) generateKeypair() {
	sk := nostr.GeneratePrivateKey()
	pub, err := nostr.GetPublicKey(sk)
	if err != nil {
		logger.New("nip29").Error("Failed to generate relay keypair", zap.Error(err))
		return
	}
	gs.relayPrivateKey = sk
	gs.relayPubkey = pub
	logger.New("nip29").Info("Generated new relay keypair for NIP-29",
		zap.String("pubkey", pub))
}

// GetRelayPubkey returns the relay's public key for NIP-11 "self" field.
func (gs *GroupStore) GetRelayPubkey() string {
	return gs.relayPubkey
}

// --- Group Operations ---

// GetGroup returns a group by ID (nil if not found).
func (gs *GroupStore) GetGroup(groupID string) *Group {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.groups[groupID]
}

// GetAllGroups returns all group IDs.
func (gs *GroupStore) GetAllGroups() []string {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	ids := make([]string, 0, len(gs.groups))
	for id := range gs.groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// IsMember checks if a pubkey is a member of a group.
func (gs *GroupStore) IsMember(groupID, pubkey string) bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	g := gs.groups[groupID]
	if g == nil {
		return false
	}
	return g.Members[pubkey]
}

// IsAdmin checks if a pubkey is an admin of a group.
func (gs *GroupStore) IsAdmin(groupID, pubkey string) bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	g := gs.groups[groupID]
	if g == nil {
		return false
	}
	_, isAdmin := g.Admins[pubkey]
	return isAdmin
}

// --- Event Processing ---

// ValidateGroupEvent checks if an event targeting a NIP-29 group is valid.
// Returns (allowed, reason). Called from the event validator for events with `h` tags.
func (gs *GroupStore) ValidateGroupEvent(evt *nostr.Event) (bool, string) {
	// Extract group ID from h tag
	groupID := getHTag(evt)
	if groupID == "" {
		return false, "missing 'h' tag for group event"
	}

	// Validate group ID format
	if !isValidGroupID(groupID) {
		return false, "invalid group ID format (must be a-z0-9-_)"
	}

	gs.mu.RLock()
	group := gs.groups[groupID]
	gs.mu.RUnlock()

	// For moderation events (9000-9009), check admin permissions
	if evt.Kind >= 9000 && evt.Kind <= 9009 {
		return gs.validateModerationEvent(evt, group, groupID)
	}

	// For join requests (9021), always allow
	if evt.Kind == 9021 {
		return true, ""
	}

	// For leave requests (9022), must be a member
	if evt.Kind == 9022 {
		if group == nil {
			return false, "group not found"
		}
		if !group.Members[evt.PubKey] {
			return false, "not a member of this group"
		}
		return true, ""
	}

	// For regular events in managed groups, check membership
	if group != nil && group.Restricted {
		if !group.Members[evt.PubKey] {
			return false, "restricted: only members can post to this group"
		}
	}

	return true, ""
}

func (gs *GroupStore) validateModerationEvent(evt *nostr.Event, group *Group, groupID string) (bool, string) {
	// kind 9007 (create-group) is special — no existing group needed
	if evt.Kind == 9007 {
		if group != nil {
			return false, "group already exists"
		}
		// Only relay admin or any authed user can create groups
		return true, ""
	}

	if group == nil {
		return false, "group not found"
	}

	// Check if sender is an admin
	isRelayOwner := strings.ToLower(evt.PubKey) == strings.ToLower(gs.relayPubkey)
	_, senderIsAdmin := group.Admins[evt.PubKey]

	if !isRelayOwner && !senderIsAdmin {
		return false, "not authorized: must be group admin"
	}

	return true, ""
}

// ProcessGroupEvent processes a validated NIP-29 event and updates group state.
// Returns relay-generated events to broadcast (metadata updates).
func (gs *GroupStore) ProcessGroupEvent(evt *nostr.Event) []*nostr.Event {
	groupID := getHTag(evt)
	if groupID == "" {
		return nil
	}

	log := logger.New("nip29")
	var relayEvents []*nostr.Event

	switch evt.Kind {
	case 9007: // create-group
		relayEvents = gs.handleCreateGroup(evt, groupID, log)
	case 9000: // put-user
		relayEvents = gs.handlePutUser(evt, groupID, log)
	case 9001: // remove-user
		relayEvents = gs.handleRemoveUser(evt, groupID, log)
	case 9002: // edit-metadata
		relayEvents = gs.handleEditMetadata(evt, groupID, log)
	case 9005: // delete-event
		log.Info("Delete-event request in group",
			zap.String("group", groupID),
			zap.String("admin", evt.PubKey[:16]+"..."))
		// Event deletion is handled by the existing NIP-09 pipeline
	case 9008: // delete-group
		relayEvents = gs.handleDeleteGroup(evt, groupID, log)
	case 9009: // create-invite
		gs.handleCreateInvite(evt, groupID, log)
	case 9021: // join request
		relayEvents = gs.handleJoinRequest(evt, groupID, log)
	case 9022: // leave request
		relayEvents = gs.handleLeaveRequest(evt, groupID, log)
	}

	return relayEvents
}

// --- Moderation Event Handlers ---

func (gs *GroupStore) handleCreateGroup(evt *nostr.Event, groupID string, log *zap.Logger) []*nostr.Event {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	if gs.groups[groupID] != nil {
		return nil // group already exists
	}

	group := &Group{
		ID:          groupID,
		Name:        groupID,
		Members:     map[string]bool{evt.PubKey: true},
		Admins:      map[string][]string{evt.PubKey: {"admin"}},
		Roles:       map[string]string{"admin": "Full group control", "moderator": "Can delete messages and remove users"},
		InviteCodes: make(map[string]bool),
		CreatedAt:   time.Now(),
	}

	// Parse flags from tags
	for _, tag := range evt.Tags {
		switch {
		case len(tag) >= 1 && tag[0] == "private":
			group.Private = true
		case len(tag) >= 1 && tag[0] == "restricted":
			group.Restricted = true
		case len(tag) >= 1 && tag[0] == "closed":
			group.Closed = true
		case len(tag) >= 2 && tag[0] == "name":
			group.Name = tag[1]
		case len(tag) >= 2 && tag[0] == "about":
			group.About = tag[1]
		case len(tag) >= 2 && tag[0] == "picture":
			group.Picture = tag[1]
		}
	}

	gs.groups[groupID] = group

	log.Info("Group created",
		zap.String("group", groupID),
		zap.String("creator", evt.PubKey[:16]+"..."))

	// Generate relay metadata events
	return gs.generateGroupMetadataLocked(group)
}

func (gs *GroupStore) handlePutUser(evt *nostr.Event, groupID string, log *zap.Logger) []*nostr.Event {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	group := gs.groups[groupID]
	if group == nil {
		return nil
	}

	// Extract target pubkeys and roles from p tags
	for _, tag := range evt.Tags {
		if tag[0] != "p" || len(tag) < 2 {
			continue
		}
		targetPubkey := tag[1]
		group.Members[targetPubkey] = true

		// Extract roles (tag elements after the pubkey)
		if len(tag) > 2 {
			roles := make([]string, 0, len(tag)-2)
			for _, r := range tag[2:] {
				if r != "" {
					roles = append(roles, r)
				}
			}
			if len(roles) > 0 {
				group.Admins[targetPubkey] = roles
			}
		}

		log.Info("User added to group",
			zap.String("group", groupID),
			zap.String("user", targetPubkey[:16]+"..."))
	}

	return gs.generateGroupMetadataLocked(group)
}

func (gs *GroupStore) handleRemoveUser(evt *nostr.Event, groupID string, log *zap.Logger) []*nostr.Event {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	group := gs.groups[groupID]
	if group == nil {
		return nil
	}

	for _, tag := range evt.Tags {
		if tag[0] != "p" || len(tag) < 2 {
			continue
		}
		targetPubkey := tag[1]
		delete(group.Members, targetPubkey)
		delete(group.Admins, targetPubkey)

		log.Info("User removed from group",
			zap.String("group", groupID),
			zap.String("user", targetPubkey[:16]+"..."))
	}

	return gs.generateGroupMetadataLocked(group)
}

func (gs *GroupStore) handleEditMetadata(evt *nostr.Event, groupID string, log *zap.Logger) []*nostr.Event {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	group := gs.groups[groupID]
	if group == nil {
		return nil
	}

	for _, tag := range evt.Tags {
		if len(tag) < 1 {
			continue
		}
		switch tag[0] {
		case "name":
			if len(tag) >= 2 {
				group.Name = tag[1]
			}
		case "about":
			if len(tag) >= 2 {
				group.About = tag[1]
			}
		case "picture":
			if len(tag) >= 2 {
				group.Picture = tag[1]
			}
		case "private":
			group.Private = true
		case "restricted":
			group.Restricted = true
		case "hidden":
			group.Hidden = true
		case "closed":
			group.Closed = true
		case "open":
			group.Closed = false
		case "public":
			group.Private = false
		case "visible":
			group.Hidden = false
		case "unrestricted":
			group.Restricted = false
		}
	}

	log.Info("Group metadata edited",
		zap.String("group", groupID))

	return gs.generateGroupMetadataLocked(group)
}

func (gs *GroupStore) handleDeleteGroup(evt *nostr.Event, groupID string, log *zap.Logger) []*nostr.Event {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	delete(gs.groups, groupID)
	log.Info("Group deleted",
		zap.String("group", groupID))

	return nil
}

func (gs *GroupStore) handleCreateInvite(evt *nostr.Event, groupID string, log *zap.Logger) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	group := gs.groups[groupID]
	if group == nil {
		return
	}

	// Extract invite code from tags
	for _, tag := range evt.Tags {
		if tag[0] == "code" && len(tag) >= 2 {
			group.InviteCodes[tag[1]] = true
			log.Info("Invite code created for group",
				zap.String("group", groupID),
				zap.String("code", tag[1][:8]+"..."))
		}
	}
}

func (gs *GroupStore) handleJoinRequest(evt *nostr.Event, groupID string, log *zap.Logger) []*nostr.Event {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	group := gs.groups[groupID]

	// Unmanaged groups — auto-accept
	if group == nil {
		return nil
	}

	// Already a member
	if group.Members[evt.PubKey] {
		// NIP-29 says return "duplicate: " error prefix
		log.Debug("Duplicate join request",
			zap.String("group", groupID),
			zap.String("user", evt.PubKey[:16]+"..."))
		return nil
	}

	// Closed groups only accept invites
	if group.Closed {
		// Check for invite code
		codeTag := evt.Tags.GetFirst([]string{"code", ""})
		if codeTag == nil || len(*codeTag) < 2 || !group.InviteCodes[(*codeTag)[1]] {
			log.Debug("Join request rejected (closed group, no valid invite)",
				zap.String("group", groupID),
				zap.String("user", evt.PubKey[:16]+"..."))
			return nil
		}
		// Consume invite code
		delete(group.InviteCodes, (*codeTag)[1])
	}

	// Accept: add member
	group.Members[evt.PubKey] = true

	log.Info("User joined group",
		zap.String("group", groupID),
		zap.String("user", evt.PubKey[:16]+"..."))

	// Generate a kind 9000 (put-user) event from relay
	putUserEvt := gs.signRelayEventLocked(&nostr.Event{
		Kind:      9000,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"h", groupID},
			{"p", evt.PubKey},
		},
		Content: "auto-accepted join request",
	})

	result := gs.generateGroupMetadataLocked(group)
	if putUserEvt != nil {
		result = append(result, putUserEvt)
	}
	return result
}

func (gs *GroupStore) handleLeaveRequest(evt *nostr.Event, groupID string, log *zap.Logger) []*nostr.Event {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	group := gs.groups[groupID]
	if group == nil {
		return nil
	}

	delete(group.Members, evt.PubKey)
	delete(group.Admins, evt.PubKey)

	log.Info("User left group",
		zap.String("group", groupID),
		zap.String("user", evt.PubKey[:16]+"..."))

	// Generate a kind 9001 (remove-user) event from relay
	removeEvt := gs.signRelayEventLocked(&nostr.Event{
		Kind:      9001,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Tags: nostr.Tags{
			{"h", groupID},
			{"p", evt.PubKey},
		},
		Content: "user left the group",
	})

	result := gs.generateGroupMetadataLocked(group)
	if removeEvt != nil {
		result = append(result, removeEvt)
	}
	return result
}

// --- Relay-signed Metadata Generation ---

// generateGroupMetadataLocked generates kinds 39000, 39001, 39002, 39003 for a group.
// Must be called with gs.mu held.
func (gs *GroupStore) generateGroupMetadataLocked(group *Group) []*nostr.Event {
	if gs.relayPrivateKey == "" {
		return nil
	}

	var events []*nostr.Event
	now := nostr.Timestamp(time.Now().Unix())

	// kind 39000 — group metadata
	metaTags := nostr.Tags{
		{"d", group.ID},
		{"name", group.Name},
	}
	if group.Picture != "" {
		metaTags = append(metaTags, nostr.Tag{"picture", group.Picture})
	}
	if group.About != "" {
		metaTags = append(metaTags, nostr.Tag{"about", group.About})
	}
	if group.Private {
		metaTags = append(metaTags, nostr.Tag{"private"})
	}
	if group.Restricted {
		metaTags = append(metaTags, nostr.Tag{"restricted"})
	}
	if group.Hidden {
		metaTags = append(metaTags, nostr.Tag{"hidden"})
	}
	if group.Closed {
		metaTags = append(metaTags, nostr.Tag{"closed"})
	}

	metaEvt := gs.signRelayEventLocked(&nostr.Event{
		Kind:      39000,
		CreatedAt: now,
		Tags:      metaTags,
		Content:   "",
	})
	if metaEvt != nil {
		events = append(events, metaEvt)
	}

	// kind 39001 — group admins
	adminTags := nostr.Tags{{"d", group.ID}}
	for pubkey, roles := range group.Admins {
		tag := nostr.Tag{"p", pubkey}
		tag = append(tag, roles...)
		adminTags = append(adminTags, tag)
	}
	adminsEvt := gs.signRelayEventLocked(&nostr.Event{
		Kind:      39001,
		CreatedAt: now,
		Tags:      adminTags,
		Content:   fmt.Sprintf("admins of group %s", group.ID),
	})
	if adminsEvt != nil {
		events = append(events, adminsEvt)
	}

	// kind 39002 — group members
	memberTags := nostr.Tags{{"d", group.ID}}
	for pubkey := range group.Members {
		memberTags = append(memberTags, nostr.Tag{"p", pubkey})
	}
	membersEvt := gs.signRelayEventLocked(&nostr.Event{
		Kind:      39002,
		CreatedAt: now,
		Tags:      memberTags,
		Content:   fmt.Sprintf("members of group %s", group.ID),
	})
	if membersEvt != nil {
		events = append(events, membersEvt)
	}

	// kind 39003 — group roles
	roleTags := nostr.Tags{{"d", group.ID}}
	for roleName, roleDesc := range group.Roles {
		roleTags = append(roleTags, nostr.Tag{"role", roleName, roleDesc})
	}
	rolesEvt := gs.signRelayEventLocked(&nostr.Event{
		Kind:      39003,
		CreatedAt: now,
		Tags:      roleTags,
		Content:   fmt.Sprintf("roles for group %s", group.ID),
	})
	if rolesEvt != nil {
		events = append(events, rolesEvt)
	}

	return events
}

// signRelayEventLocked signs an event with the relay's private key.
// Must be called with gs.mu held (or from a safe context).
func (gs *GroupStore) signRelayEventLocked(evt *nostr.Event) *nostr.Event {
	if gs.relayPrivateKey == "" {
		return nil
	}
	evt.PubKey = gs.relayPubkey
	if err := evt.Sign(gs.relayPrivateKey); err != nil {
		logger.New("nip29").Error("Failed to sign relay event",
			zap.Int("kind", evt.Kind),
			zap.Error(err))
		return nil
	}
	return evt
}

// --- Helpers ---

// getHTag extracts the group ID from an event's h tag.
func getHTag(evt *nostr.Event) string {
	hTag := evt.Tags.GetFirst([]string{"h", ""})
	if hTag == nil || len(*hTag) < 2 {
		return ""
	}
	return (*hTag)[1]
}

// isValidGroupID checks if a group ID only contains a-z0-9-_
func isValidGroupID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// generateGroupID creates a random group identifier.
func generateGroupID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("group-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// IsGroupEvent returns true if an event is a NIP-29 group event (has h tag or is group metadata).
func IsGroupEvent(evt *nostr.Event) bool {
	// Moderation events
	if evt.Kind >= 9000 && evt.Kind <= 9009 {
		return true
	}
	// Join/leave
	if evt.Kind == 9021 || evt.Kind == 9022 {
		return true
	}
	// Group metadata (relay-generated)
	if evt.Kind >= 39000 && evt.Kind <= 39003 {
		return true
	}
	// Any event with h tag
	return getHTag(evt) != ""
}
