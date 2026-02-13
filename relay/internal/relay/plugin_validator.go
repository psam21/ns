package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Shugur-Network/relay/internal/config"
	"github.com/Shugur-Network/relay/internal/domain"
	"github.com/Shugur-Network/relay/internal/logger"
	"github.com/Shugur-Network/relay/internal/metrics"
	"github.com/Shugur-Network/relay/internal/relay/nips"
	"github.com/Shugur-Network/relay/internal/storage"
	nostr "github.com/nbd-wtf/go-nostr"
	"go.uber.org/zap"
)

// ValidationLimits defines your limit fields
type ValidationLimits struct {
	MaxContentLength  int
	MaxTagsLength     int
	MaxTagsPerEvent   int
	MaxTagElements    int
	MaxFutureSeconds  int
	OldestEventTime   int64
	RelayStartupTime  time.Time
	MaxMetadataLength int
	AllowedKinds      map[int]bool
	RequiredTags      map[int][]string
	MaxCreatedAt      int64
	MinCreatedAt      int64
}

// PluginValidator implements EventValidator
type PluginValidator struct {
	config    *config.Config
	blacklist map[string]bool
	mu        sync.RWMutex // protects blacklist and limits.AllowedKinds
	limits    ValidationLimits

	verifiedPubkeys map[string]time.Time
	db              *storage.DB
}

// Ensure PluginValidator implements domain.EventValidator
var _ domain.EventValidator = (*PluginValidator)(nil)

// NewPluginValidator returns a PluginValidator with default settings
func NewPluginValidator(cfg *config.Config, database *storage.DB) *PluginValidator {
	// Use configuration values for content length limits
	maxContentLength := cfg.Relay.ThrottlingConfig.MaxContentLen
	if maxContentLength == 0 {
		maxContentLength = 64000 // fallback default
	}

	defaultLimits := ValidationLimits{
		MaxContentLength:  maxContentLength, // Use configured value
		MaxTagsLength:     10000,
		MaxTagsPerEvent:   256,
		MaxTagElements:    16,
		MaxFutureSeconds:  300,
		OldestEventTime:   1609459200, // Jan 1, 2021
		RelayStartupTime:  time.Now(),
		MaxMetadataLength: 10000,
		AllowedKinds: map[int]bool{
			0: true, 1: true, 2: true, 3: true, 4: true, 5: true,
			6: true, 7: true, 9: true, 11: true, 16: true, 20: true, 21: true, 24: true,
			40: true, 41: true, 42: true, 43: true, 44: true, 62: true,
			14: true, 15: true, 1059: true, 10050: true,
			1984: true, 1985: true, 9734: true, 9735: true, 10002: true,
			30023: true, 30024: true, // NIP-23: Long-form Content + Drafts
			31989: true, 31990: true, // NIP-89: App Handlers
			1111: true, // NIP-22: Comment
			// NIP-C7 Chats (kind 9), NIP-7D Threads (kind 11), NIP-A4 Public Messages (kind 24)
			// NIP-68 Picture-first (kind 20), NIP-71 Video (kind 21), NIP-62 Vanish (kind 62)
			// NIP-32 Labeling (kind 1985)
			// NIP-20 Command Results
			24133: true,
			// NIP-16 Ephemeral Events (20000-29999) — range-allowed in validator
			// NIP-33 Addressable Events
			30000: true, 30001: true, 30002: true, 30003: true,
			// NIP-51 Lists - Standard Lists
			10000: true, // Mute list
			10001: true, // Pinned notes
			10003: true, // Bookmarks
			10004: true, // Communities
			10005: true, // Public chats
			10006: true, // Blocked relays
			10007: true, // Search relays
			10009: true, // Simple groups
			10012: true, // Relay feeds
			10015: true, // Interests
			10020: true, // Media follows
			10030: true, // Emojis
			10101: true, // Good wiki authors
			10102: true, // Good wiki relays
			// NIP-51 Lists - Sets
			30004: true, // Curation sets (articles/notes)
			30005: true, // Curation sets (videos)
			30007: true, // Kind mute sets
			30015: true, // Interest sets
			30030: true, // Emoji sets
			30063: true, // Release artifact sets
			30267: true, // App curation sets
			39089: true, // Starter packs
			39092: true, // Media starter packs
			// NIP-15 Marketplace
			30017: true, // Stall
			30018: true, // Product
			30019: true, // Marketplace UI/UX
			30020: true, // Auction Product
			1021:  true, // Bid
			1022:  true, // Bid Confirmation
			// Other NIPs
			8:     true, // NIP-58: Badge Award
			1040:  true, // NIP-03 OpenTimestamps attestation
			1041:  true, // NIP-XX Time-Lock Encrypted Messages
			1063:  true, // NIP-94: File Metadata
			1222:  true, // NIP-A0: Voice Messages
			1244:  true, // NIP-A0: Voice Reply
			1337:  true, // NIP-C0: Code Snippets
			2003:  true, // NIP-35: Torrents
			1018:  true, // NIP-88: Polls (response)
			1068:  true, // NIP-88: Polls (poll event)
			9041:  true, // NIP-75: Zap Goals
			9802:  true, // NIP-84: Highlights
			13194: true, // NIP-47: Wallet Connect info
			30008: true, // NIP-58: Profile Badges
			30009: true, // NIP-58: Badge Definition
			30078: true, // NIP-78 Application-specific Data
			30315: true, // NIP-38: User Statuses
			30382: true, // NIP-85: Trusted Assertions
			10040: true, // NIP-85: Trusted Assertion Delegation
			30402: true, // NIP-99: Classified Listings
			30403: true, // NIP-99: Draft Classified Listing
			// NIP-52 Calendar Events
			31922: true, // Date-based Calendar Event
			31923: true, // Time-based Calendar Event  
			31924: true, // Calendar
			31925: true, // Calendar Event RSVP
			// NIP-53 Live Activities
			30311: true, // Live Streaming Event
			1311:  true, // Live Chat Message
			30312: true, // Meeting Space
			30313: true, // Meeting Room Event
			10312: true, // Room Presence
			// NIP-54 Wiki
			30818: true, // Wiki Article
			818:   true, // Merge Request
			30819: true, // Wiki Redirect
			// NIP-60 Cashu Wallets
			17375: true, // Wallet Event
			7375:  true, // Token Event
			7376:  true, // Spending History Event
			7374:  true, // Quote Event
			// NIP-61 Nutzaps
			9321:  true, // Nutzap event  
			10019: true, // Nutzap info event
			// NIP-34 Git Stuff
			1617:  true, // Patches
			1618:  true, // Pull Requests
			1619:  true, // Issues
			1621:  true, // Comments on Git
			1630:  true, 1631: true, 1632: true, 1633: true, // Patch status
			10317: true, // Repository state
			30617: true, // Repository
			30618: true, // Repository announcements
			// NIP-37 Draft Wraps
			31234: true, // Draft event
			10013: true, // Draft list
			// NIP-71 Video Events
			34235: true, // Video event
			34236: true, // Short-form vertical video
			// NIP-87 Ecash Mint Discoverability
			38000: true, // Mint recommendation
			38172: true, // Mint trust
			38173: true, // Mint trust revocation
			// NIP-69 P2P Order Events
			38383: true, // P2P Order
			// NIP-B0 Web Bookmarking
			39701: true, // Web Bookmark
			// NIP-B7 Blossom Server List
			10063: true, // User Blossom Server List
			// NIP-72 Moderated Communities
			34550: true, // Community Definition
			4550:  true, // Moderation Approval
			// NIP-EE MLS E2EE Messaging
			443:   true, // MLS KeyPackage Event
			444:   true, // MLS Welcome Event (inner, arrives via gift wrap)
			445:   true, // MLS Group Event (encrypted group messages)
			10051: true, // KeyPackage Relays List
			// NIP-YY Nostr Web Pages
			1125:  true, // Asset (HTML, CSS, JavaScript, fonts, etc.)
			1126:  true, // Page Manifest
			31126: true, // Site Index
			11126: true, // Entrypoint
			// NIP-43 Relay Access Metadata
			13534: true, // Membership list (relay-signed)
			8000:  true, // Add user (relay-signed)
			8001:  true, // Remove user (relay-signed)
			28934: true, // Join request (user-sent)
			28935: true, // Invite request (ephemeral, relay-generated)
			28936: true, // Leave request (user-sent)
			10010: true, // User relay membership list
			// NIP-66 Relay Discovery and Liveness Monitoring
			30166: true, // Relay Discovery (addressable)
			10166: true, // Relay Monitor Announcement (replaceable)
			// NIP-39 External Identities in Profiles
			10011: true, // External Identity List (replaceable)
			// NIP-64 Chess (PGN)
			64: true, // Chess game in PGN format
		},
		RequiredTags: map[int][]string{
			5:     {"e"},      // Deletion events must have an "e" tag
			7:     {"e", "p"}, // Reaction events require "e" and "p" tags
			8:     {"a", "p"}, // NIP-58: Badge Award requires "a" and "p" tags
			41:    {"e"},      // NIP-28: Channel Metadata requires "e" tag
			42:    {"e"},      // NIP-28: Channel Message requires "e" tag
			43:    {"e"},      // NIP-28: Hide Message requires "e" tag
			44:    {"p"},      // NIP-28: Mute User requires "p" tag
			1059:  {"p"},      // Gift wrap events must have a "p" tag
			30000: {"d"},      // NIP-33: Addressable Events require "d" tag
			30001: {"d"},      // NIP-33: Addressable Events require "d" tag
			30002: {"d"},      // NIP-33: Addressable Events require "d" tag
			30003: {"d"},      // NIP-33: Addressable Events require "d" tag
			30004: {"d"},      // NIP-51: Curation sets require "d" tag
			30005: {"d"},      // NIP-51: Video curation sets require "d" tag
			30007: {"d"},      // NIP-51: Kind mute sets require "d" tag
			30008: {"d"},      // NIP-58: Profile Badges require "d" tag
			30009: {"d"},      // NIP-58: Badge Definition require "d" tag
			30015: {"d"},      // NIP-51: Interest sets require "d" tag
			30030: {"d"},      // NIP-51: Emoji sets require "d" tag
			30063: {"d"},      // NIP-51: Release artifact sets require "d" tag
			30267: {"d"},      // NIP-51: App curation sets require "d" tag
			39089: {"d"},      // NIP-51: Starter packs require "d" tag
			39092: {"d"},      // NIP-51: Media starter packs require "d" tag
			30017: {"d"},      // Stall events require "d" tag
			30018: {"d", "t"}, // Product events require "d" and at least one "t" tag
			1021:  {"e"},      // Bid events require "e" tag
			1022:  {"e"},      // Bid confirmation events require "e" tag
			1040:  {"e"},      // OpenTimestamps attestation requires "e" tag
			1041:  {"tlock"},  // NIP-XX Time capsule requires "tlock" tag
			30078: {"d"},      // NIP-78: Application-specific Data requires "d" tag
			// NIP-52 Calendar Events
			31922: {"d", "title", "start"}, // Date-based Calendar Event requires "d", "title", and "start" tags
			31923: {"d", "title", "start"}, // Time-based Calendar Event requires "d", "title", and "start" tags
			31924: {"d", "title"},          // Calendar requires "d" and "title" tags
			31925: {"d", "a", "status"},    // Calendar Event RSVP requires "d", "a", and "status" tags
			// NIP-53 Live Activities
			30311: {"d"},                    // Live Streaming Event requires "d" tag
			1311:  {"a"},                    // Live Chat Message requires "a" tag
			30312: {"d", "room", "status", "service"}, // Meeting Space requires "d", "room", "status", and "service" tags
			30313: {"d", "a", "title", "starts", "status"}, // Meeting Room Event requires "d", "a", "title", "starts", and "status" tags
			10312: {"a"},                    // Room Presence requires "a" tag
			// NIP-54 Wiki
			30818: {"d"},                    // Wiki Article requires "d" tag
			818:   {"a", "p"},               // Merge Request requires "a" and "p" tags
			30819: {"d", "redirect"},        // Wiki Redirect requires "d" and "redirect" tags
			// NIP-60 Cashu Wallets - Note: Most tags are encrypted in content, minimal required public tags
			7374:  {"expiration", "mint"},   // Quote Event requires "expiration" and "mint" tags
			// NIP-72 Moderated Communities
			34550: {"d"},           // Community Definition requires "d" tag
			4550:  {"a", "p", "k"}, // Moderation Approval requires community, author, and kind tags (e tag only for non-replaceable events)
			// NIP-EE MLS E2EE Messaging
			443:   {"mls_protocol_version", "mls_ciphersuite"}, // KeyPackage requires protocol version and ciphersuite
			445:   {"h"},            // Group Event requires "h" tag (group ID)
			10051: {"relay"},        // KeyPackage Relays List requires at least one "relay" tag
			// NIP-YY Nostr Web Pages
			1125:  {"m", "x"},  // Asset requires "m" (MIME type) and "x" (SHA-256 hash) tags
			1126:  {"e"},       // Page Manifest requires "e" (asset references) tags
			31126: {"d", "x"},  // Site Index requires "d" (truncated hash) and "x" (full SHA-256 hash) tags
			11126: {"a"},       // Entrypoint requires "a" (address to site index) tag
			// NIP-43 Relay Access Metadata
			28934: {"claim"},   // Join request requires "claim" tag with invite code
			// NIP-66 Relay Discovery
			30166: {"d"},       // Relay Discovery requires "d" tag (relay URL)
		},
		MaxCreatedAt: time.Now().Unix() + 300,    // 5 minutes in future
		MinCreatedAt: time.Now().Unix() - 172800, // 2 days in past
	}

	return &PluginValidator{
		config:          cfg,
		blacklist:       make(map[string]bool),
		limits:          defaultLimits,
		verifiedPubkeys: make(map[string]time.Time),
		db:              database,
	}
}

// ValidateEvent checks an event thoroughly
func (pv *PluginValidator) ValidateEvent(ctx context.Context, event nostr.Event) (bool, string) {

	// Check context cancellation at strategic points
	if ctx.Err() != nil {
		return false, "operation canceled"
	}

	// 1. Basic structure checks
	if len(event.ID) != 64 || !isHexString(event.ID) {
		return false, "invalid event ID format"
	}

	if len(event.PubKey) != 64 || !isHexString(event.PubKey) {
		return false, "invalid pubkey format"
	}

	if len(event.Sig) != 128 || !isHexString(event.Sig) {
		return false, "invalid signature format"
	}

	// 2. Check if kind is allowed
	if !pv.limits.AllowedKinds[event.Kind] {
		// Check if it's an ephemeral event (20000-29999) - these should be allowed per NIP-16
		if event.Kind >= 20000 && event.Kind < 30000 {
			// Ephemeral events are allowed but not stored
		} else if event.Kind >= 5000 && event.Kind <= 5999 {
			// NIP-90 DVM job requests
		} else if event.Kind >= 6000 && event.Kind <= 6999 {
			// NIP-90 DVM job results
		} else if event.Kind == 7000 {
			// NIP-90 DVM job feedback
		} else if event.Kind >= 9000 && event.Kind <= 9030 {
			// NIP-29 Relay-based Groups moderation events
		} else if event.Kind == 9021 || event.Kind == 9022 {
			// NIP-29 join/leave requests
		} else if event.Kind >= 39000 && event.Kind <= 39003 {
			// NIP-29 group metadata events
		} else {
			return false, fmt.Sprintf("unsupported event kind: %d", event.Kind)
		}
	}

	// 3. Check blacklist (case-insensitive)
	pv.mu.RLock()
	banned := pv.blacklist[strings.ToLower(event.PubKey)]
	pv.mu.RUnlock()
	if banned {
		return false, "pubkey is blacklisted"
	}

	// 4. Verify event ID matches content
	computedID := event.GetID()
	if computedID != event.ID {
		return false, "event ID does not match content"
	}

	// 5. Check timestamps
	now := time.Now().Unix()
	maxFutureTime := now + int64(pv.limits.MaxFutureSeconds)

	if event.CreatedAt.Time().Unix() > maxFutureTime {
		return false, fmt.Sprintf("event timestamp is too far in the future (max %d seconds)", pv.limits.MaxFutureSeconds)
	}

	if event.CreatedAt.Time().Unix() < pv.limits.OldestEventTime {
		return false, "event timestamp is too old"
	}

	// 6. NIP-40: Check expiration timestamp
	if expTime, hasExpiration := nips.GetExpirationTime(event); hasExpiration {
		if time.Now().After(expTime) {
			return false, "event has expired"
		}
		// Validate expiration tag format
		if err := nips.ValidateExpirationTag(event); err != nil {
			return false, fmt.Sprintf("invalid expiration tag: %v", err)
		}
	}

	// 6b. NIP-13: Proof of Work validation
	if err := nips.ValidatePoW(event, pv.config.Relay.MinPowDifficulty); err != nil {
		return false, err.Error()
	}

	// 6. Content length check
	if len(event.Content) > pv.limits.MaxContentLength {
		return false, fmt.Sprintf("content exceeds maximum length of %d bytes", pv.limits.MaxContentLength)
	}

	// 7. Tags validation
	tagsSize := 0
	for _, tag := range event.Tags {
		if len(tag) > pv.limits.MaxTagElements {
			return false, "tag has too many elements"
		}
		for _, elem := range tag {
			tagsSize += len(elem)
		}
	}

	if tagsSize > pv.limits.MaxTagsLength {
		return false, "tags exceed maximum total size"
	}

	if len(event.Tags) > pv.limits.MaxTagsPerEvent {
		return false, "too many tags"
	}

	// 8. Kind-specific required tags
	if requiredTags, hasRequirements := pv.limits.RequiredTags[event.Kind]; hasRequirements {
		for _, requiredTag := range requiredTags {
			found := false
			for _, tag := range event.Tags {
				if len(tag) > 0 && tag[0] == requiredTag {
					found = true
					break
				}
			}
			if !found {
				if event.Kind == 30018 && requiredTag == "t" {
					return false, "product must have at least one category tag"
				}
				return false, fmt.Sprintf("missing required '%s' tag", requiredTag)
			}
		}
	}

	// Special handling for deletion events (kind 5)
	if event.Kind == 5 {
		// Validate deletion authorization
		for _, tag := range event.Tags {
			if len(tag) >= 2 && tag[0] == "e" {
				targetEvent, err := pv.db.GetEventByID(context.Background(), tag[1])
				if err == nil && targetEvent.ID != "" && targetEvent.PubKey != event.PubKey {
					logger.Warn("Unauthorized deletion attempt blocked",
						zap.String("deletion_event_id", event.ID),
						zap.String("deleter_pubkey", event.PubKey),
						zap.String("target_event_id", tag[1]),
						zap.String("target_event_pubkey", targetEvent.PubKey))
					return false, "unauthorized: only the event author can delete their events"
				}
			}
		}
	}

	// NIP-specific validation using dedicated validators
	if err := pv.validateWithDedicatedNIPs(&event); err != nil {
		return false, fmt.Sprintf("NIP validation failed: %v", err)
	}

	return true, ""
}

// validateWithDedicatedNIPs validates events using dedicated NIP validation functions
func (pv *PluginValidator) validateWithDedicatedNIPs(event *nostr.Event) error {
	switch event.Kind {
	case 3:
		return nips.ValidateFollowList(event)
	case 4:
		return nips.ValidateEncryptedDirectMessage(event)
	case 5:
		return nips.ValidateEventDeletion(event)
	case 7:
		return nips.ValidateReaction(event)
	case 8:
		return nips.ValidateBadgeAward(event)
	case 14, 15, 10050:
		return nips.ValidatePrivateDirectMessage(event)
	case 40, 41, 42, 43, 44:
		return nips.ValidatePublicChat(event)
	case 1040:
		return nips.ValidateOpenTimestampsAttestation(event)
	case 1984:
		return nips.ValidateReport(event)
	case 9734:
		return nips.ValidateZapRequest(event)
	case 9735:
		return nips.ValidateZapReceipt(event)
	case 24133:
		return nips.ValidateCommandResult(event)
	case 30008:
		return nips.ValidateProfileBadges(event)
	case 30009:
		return nips.ValidateBadgeDefinition(event)
	case 30017, 30018, 30019, 30020, 1021, 1022:
		return nips.ValidateMarketplaceEvent(event)
	case 30023:
		return nips.ValidateLongFormContent(event)
	case 30078:
		return nips.ValidateApplicationSpecificData(event)
	case 13194:
		return nips.ValidateGiftWrapEvent(event)
	case 10002:
		return nips.ValidateKind10002(*event)
	case 1041:
		return nips.ValidateTimeCapsuleEvent(event)
	case 1059:
		return nips.ValidateGiftWrapEvent(event)
	// NIP-51 Lists validation
	case 10000, 10001, 10003, 10004, 10005, 10006, 10007, 10009, 10012, 10015, 10020, 10030, 10101, 10102:
		return nips.ValidateList(event) // Standard lists
	case 30000, 30001, 30004, 30005, 30007, 30015, 30030, 30063, 30267, 39089, 39092:
		return nips.ValidateList(event) // Sets
	// NIP-52 Calendar Events validation
	case 31922:
		return nips.ValidateDateBasedCalendarEvent(event)
	case 31923:
		return nips.ValidateTimeBasedCalendarEvent(event)
	case 31924:
		return nips.ValidateCalendar(event)
	case 31925:
		return nips.ValidateCalendarEventRSVP(event)
	// NIP-53 Live Activities validation
	case 30311:
		return nips.ValidateLiveStreamingEvent(event)
	case 1311:
		return nips.ValidateLiveChatMessage(event)
	case 30312:
		return nips.ValidateMeetingSpace(event)
	case 30313:
		return nips.ValidateMeetingRoomEvent(event)
	case 10312:
		return nips.ValidateRoomPresence(event)
	// NIP-54 Wiki validation
	case 30818:
		return nips.ValidateWikiArticle(event)
	case 818:
		return nips.ValidateMergeRequest(event)
	case 30819:
		return nips.ValidateWikiRedirect(event)
	// NIP-60 Cashu Wallets validation
	case 17375:
		return nips.ValidateWalletEvent(event)
	case 7375:
		return nips.ValidateTokenEvent(event)
	case 7376:
		return nips.ValidateSpendingHistoryEvent(event)
	case 7374:
		return nips.ValidateQuoteEvent(event)
	// NIP-61 Nutzaps validation
	case 9321:
		return nips.ValidateNutzapEvent(event)
	case 10019:
		return nips.ValidateNutzapInfoEvent(event)
	// NIP-72 Moderated Communities validation
	case 34550:
		return nips.ValidateCommunityDefinition(event)
	case 1111:
		// Check if this is a community post (has community A tag) or regular comment
		for _, tag := range event.Tags {
			if len(tag) >= 2 && tag[0] == "A" && strings.HasPrefix(tag[1], "34550:") {
				return nips.ValidateCommunityPost(event)
			}
		}
		// Fallback to regular comment validation
		return nips.ValidateComment(event)
	case 4550:
		return nips.ValidateApprovalEvent(event)
	// NIP-EE MLS E2EE Messaging validation
	case 443:
		return nips.ValidateKeyPackageEvent(event)
	case 444:
		return nips.ValidateWelcomeEvent(event)
	case 445:
		return nips.ValidateGroupEvent(event)
	case 10051:
		return nips.ValidateKeyPackageRelaysList(event)
	// NIP-YY Nostr Web Pages validation
	case 1125:
		return nips.ValidateAsset(event)
	case 1126:
		return nips.ValidatePageManifest(event)
	case 31126:
		return nips.ValidateSiteIndex(event)
	case 11126:
		return nips.ValidateEntrypoint(event)
	default:
		// Check for NIP-16 ephemeral events
		if event.Kind >= 20000 && event.Kind < 30000 {
			return nips.ValidateEventTreatment(event)
		}
		// Check if it's a addressable event
		if nips.IsParameterizedReplaceableKind(event.Kind) {
			return nips.ValidateParameterizedReplaceableEvent(event)
		}
		// Check for NIP-24 extra metadata
		if nips.HasExtraMetadata(event) {
			return nips.ValidateExtraMetadata(event)
		}
	}

	return nil
}

// ValidateFilter ensures a filter is within safe limits
func (pv *PluginValidator) ValidateFilter(f nostr.Filter) error {
	// Apply limit cap
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 500
	}

	// Validate time range
	if f.Since != nil && f.Until != nil && f.Since.Time().Unix() > f.Until.Time().Unix() {
		return fmt.Errorf("'since' timestamp is after 'until' timestamp")
	}

	// Don't allow queries too far in the future
	now := time.Now().Unix()
	maxFutureTime := now + int64(pv.limits.MaxFutureSeconds)
	if f.Until != nil && f.Until.Time().Unix() > maxFutureTime {
		return fmt.Errorf("'until' timestamp is too far in the future")
	}

	// Check IDs format
	for _, id := range f.IDs {
		if len(id) != 64 || !isHexString(id) {
			return fmt.Errorf("invalid event ID: %s", id)
		}
	}

	// Check authors format
	for _, author := range f.Authors {
		if len(author) != 64 || !isHexString(author) {
			return fmt.Errorf("invalid pubkey in authors: %s", author)
		}
	}

	// Prevent excessive tag filters
	if len(f.Tags) > 10 {
		return fmt.Errorf("too many tag filters (max 10)")
	}

	// Check tag values
	for _, values := range f.Tags {
		if len(values) > 20 {
			return fmt.Errorf("too many values in tag filter (max 20)")
		}
	}

	return nil
}

// AddBlacklistedPubkey adds a pubkey to the blacklist
func (pv *PluginValidator) AddBlacklistedPubkey(pubkey string) {
	pv.mu.Lock()
	defer pv.mu.Unlock()
	pv.blacklist[strings.ToLower(pubkey)] = true
}

// RemoveBlacklistedPubkey removes a pubkey from the blacklist
func (pv *PluginValidator) RemoveBlacklistedPubkey(pubkey string) {
	pv.mu.Lock()
	defer pv.mu.Unlock()
	delete(pv.blacklist, strings.ToLower(pubkey))
}

// GetBlacklistedPubkeys returns a copy of all blacklisted pubkeys
func (pv *PluginValidator) GetBlacklistedPubkeys() []string {
	pv.mu.RLock()
	defer pv.mu.RUnlock()
	pubkeys := make([]string, 0, len(pv.blacklist))
	for k := range pv.blacklist {
		pubkeys = append(pubkeys, k)
	}
	return pubkeys
}

// GetAllowedKinds returns a sorted list of all allowed event kinds
func (pv *PluginValidator) GetAllowedKinds() []int {
	pv.mu.RLock()
	defer pv.mu.RUnlock()
	kinds := make([]int, 0, len(pv.limits.AllowedKinds))
	for k := range pv.limits.AllowedKinds {
		kinds = append(kinds, k)
	}
	return kinds
}

// AddAllowedKind adds an event kind to the allowed kinds map
func (pv *PluginValidator) AddAllowedKind(kind int) {
	pv.mu.Lock()
	defer pv.mu.Unlock()
	pv.limits.AllowedKinds[kind] = true
}

// RemoveAllowedKind removes an event kind from the allowed kinds map
func (pv *PluginValidator) RemoveAllowedKind(kind int) {
	pv.mu.Lock()
	defer pv.mu.Unlock()
	delete(pv.limits.AllowedKinds, kind)
}

// ValidateAndProcessEvent performs validation and processing of incoming events
func (pv *PluginValidator) ValidateAndProcessEvent(ctx context.Context, event nostr.Event) (bool, string, error) {
	// Check event size using configured limit
	if len(event.Content) > pv.limits.MaxContentLength {
		return false, fmt.Sprintf("invalid: event content too large (max %d bytes)", pv.limits.MaxContentLength), nil
	}

	// Create a timeout context for database operations
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Direct database check for duplicates with retry
	var exists bool
	var err error
	for i := 0; i < 3; i++ {
		exists, err = pv.db.EventExists(dbCtx, event.ID)
		if err == nil {
			break
		}
		if i < 2 {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		return false, "error checking event existence", fmt.Errorf("database error after retries: %w", err)
	}

	if exists {
		metrics.DuplicateEvents.Inc()
		return true, "duplicate: event already exists", nil
	}

	// Verify event ID matches content (prevents ID spoofing)
	computedID := event.GetID()
	if computedID != event.ID {
		return false, "invalid: event ID does not match content", nil
	}

	// Verify signature (important for security)
	valid, err := event.CheckSignature()
	if err != nil || !valid {
		return false, "invalid: signature verification failed", nil
	}

	// Perform base validation
	valid, reason := pv.ValidateEvent(dbCtx, event)
	if !valid {
		return false, reason, nil
	}

	// Special handling for specific event kinds
	switch event.Kind {
	case 5: // deletion
		if err := nips.ValidateDeletionAuth(
			event.Tags,
			event.PubKey,
			func(id string) (nostr.Event, bool) {
				evt, err := pv.db.GetEventByID(dbCtx, id)
				if err != nil {
					logger.Error("Error fetching event for deletion validation",
						zap.String("event_id", id),
						zap.Error(err))
					return nostr.Event{}, false
				}
				return evt, true
			},
		); err != nil {
			return false, err.Error(), nil
		}
	case 0: // Metadata
		if err := pv.validateMetadataEvent(event); err != nil {
			return false, err.Error(), nil
		}

	case 1041: // NIP-XX Time capsule
		if err := nips.ValidateTimeCapsuleEvent(&event); err != nil {
			return false, fmt.Sprintf("invalid time capsule: %s", err.Error()), nil
		}
	case 1059: // NIP-59 Gift wrap (for private time capsules and MLS Welcome events)
		if err := nips.ValidateGiftWrapEvent(&event); err != nil {
			return false, fmt.Sprintf("invalid gift wrap: %s", err.Error()), nil
		}
	case 443: // NIP-EE MLS KeyPackage
		if err := nips.ValidateKeyPackageEvent(&event); err != nil {
			return false, fmt.Sprintf("invalid MLS KeyPackage: %s", err.Error()), nil
		}
	case 445: // NIP-EE MLS Group Event
		if err := nips.ValidateGroupEvent(&event); err != nil {
			return false, fmt.Sprintf("invalid MLS Group event: %s", err.Error()), nil
		}
	}

	// Check if delegation is being used (NIP-26)
	if delegationTag := nips.ExtractDelegationTag(event); delegationTag != nil {
		if err := nips.ValidateDelegation(&event, delegationTag); err != nil {
			return false, fmt.Sprintf("invalid delegation: %s", err.Error()), nil
		}
		logger.Debug("Event with valid delegation accepted",
			zap.String("event_id", event.ID),
			zap.String("delegator", delegationTag.MasterPubkey))
	}

	return true, "", nil
}

// validateMetadataEvent validates a metadata event (kind 0)
func (pv *PluginValidator) validateMetadataEvent(event nostr.Event) error {
	// Ensure content is valid JSON
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(event.Content), &metadata); err != nil {
		return fmt.Errorf("metadata must be valid JSON: %w", err)
	}

	// Validate common metadata fields
	if name, ok := metadata["name"].(string); ok && len(name) > 100 {
		return fmt.Errorf("name field too long (max 100 characters)")
	}

	if about, ok := metadata["about"].(string); ok && len(about) > 500 {
		return fmt.Errorf("about field too long (max 500 characters)")
	}

	return nil
}
