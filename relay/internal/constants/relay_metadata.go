package constants

import (
	"fmt"
	"time"
	
	"github.com/Shugur-Network/relay/internal/config"
	"github.com/Shugur-Network/relay/internal/identity"
	nip11 "github.com/nbd-wtf/go-nostr/nip11"
)

// Database constants
const (
	DatabaseName = "shugur"
)

// Default relay metadata constants
const (
	DefaultRelayDescription = "Open public Nostr relay infrastructure for portable identity, signed events, and resilient communication."
	DefaultRelayContact     = "epochshield@proton.me"
	DefaultRelaySoftware    = "nostr.ltd"
	DefaultRelayVersion     = "2.0.0"
	DefaultRelayIcon        = "https://nostr.ltd/favicon.ico"
)

// DefaultSupportedNIPs lists the NIPs supported by the relay
var DefaultSupportedNIPs = []interface{}{
	1,  // NIP-01: Basic protocol flow description
	2,  // NIP-02: Follow List
	5,  // NIP-05: Mapping Nostr keys to DNS-based internet identifiers
	7,  // NIP-07: window.nostr capability for web browsers
	9,  // NIP-09: Event Deletion Request
	10, // NIP-10: Text Notes and Threads
	11, // NIP-11: Relay Information Document
	13, // NIP-13: Proof of Work
	17, // NIP-17: Private Direct Messages
	18, // NIP-18: Reposts
	19, // NIP-19: bech32-encoded entities (npub, nsec, note, nprofile, nevent, naddr)
	21, // NIP-21: nostr: URI scheme
	22, // NIP-22: Comment
	23, // NIP-23: Long-form Content
	24, // NIP-24: Extra metadata fields and tags
	25, // NIP-25: Reactions
	27, // NIP-27: Text Note References
	29, // NIP-29: Relay-based Groups
	30, // NIP-30: Custom Emoji
	32, // NIP-32: Labeling
	34, // NIP-34: Git Stuff
	35, // NIP-35: Torrents
	36, // NIP-36: Sensitive Content / Content Warning
	37, // NIP-37: Draft Wraps
	38, // NIP-38: User Statuses
	39, // NIP-39: External Identities in Profiles
	40, // NIP-40: Expiration Timestamp
	42, // NIP-42: Authentication of clients to relays
	43, // NIP-43: Relay Access Metadata and Requests
	44, // NIP-44: Encrypted Payloads (Versioned)
	45, // NIP-45: Counting Events
	46, // NIP-46: Nostr Remote Signing
	47, // NIP-47: Nostr Wallet Connect
	48, // NIP-48: Bridged Events (NWC)
	49, // NIP-49: Private Key Encryption (ncryptsec)
	50, // NIP-50: Search Capability
	51, // NIP-51: Lists
	52, // NIP-52: Calendar Events
	53, // NIP-53: Live Activities
	"5A", // NIP-5A: Static Websites (nsites)
	54, // NIP-54: Wiki
	56, // NIP-56: Reporting
	57, // NIP-57: Lightning Zaps
	58, // NIP-58: Badges
	59, // NIP-59: Gift Wrap
	60, // NIP-60: Cashu Wallets
	61, // NIP-61: Nutzaps
	62, // NIP-62: Request to Vanish
	64, // NIP-64: Chess (PGN)
	65, // NIP-65: Relay List Metadata
	66, // NIP-66: Relay Discovery and Liveness Monitoring
	67, // NIP-67: EOSE Completeness Hint
	69, // NIP-69: Peer-to-peer Order Events
	70, // NIP-70: Protected Events
	71, // NIP-71: Video Events
	75, // NIP-75: Zap Goals
	77, // NIP-77: Negentropy Syncing
	78, // NIP-78: Application-specific data
	84, // NIP-84: Highlights
	85, // NIP-85: Trusted Assertions
	86, // NIP-86: Relay Management API
	87, // NIP-87: Cashu and Fedimint Discoverability
	88, // NIP-88: Polls
	89, // NIP-89: Recommended Application Handlers
	92, // NIP-92: Media Attachments (imeta)
	94, // NIP-94: File Metadata
	98, // NIP-98: HTTP Auth
	99, // NIP-99: Classified Listings
	"7D", // NIP-7D: Threads
	"A0", // NIP-A0: Voice Messages
	"A4", // NIP-A4: Public Messages
	"B0", // NIP-B0: Web Bookmarking
	"C0", // NIP-C0: Code Snippets
	"F4", // NIP-F4: Podcasts
	"CC", // NIP-CC: Geocaching Events
	"C7", // NIP-C7: Chats
	"B7", // NIP-B7: Blossom Server List
}

// CustomNIP represents a custom NIP implementation
type CustomNIP struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Link        string `json:"link"`
}

// DefaultCustomNIPs lists custom NIPs implemented by this relay
var DefaultCustomNIPs = []CustomNIP{
	{
		ID:          "XX",
		Name:        "Time Capsules",
		Description: "Time-locked message delivery with threshold witness mode and VDF support",
		Link:        "https://github.com/psam21/ns/tree/main/relay/internal/relay/nips",
	},
	{
		ID:          "YY",
		Name:        "Nostr Web Pages",
		Description: "Censorship-resistant static websites on Nostr",
		Link:        "https://github.com/psam21/ns/tree/main/relay/internal/relay/nips",
	},
}

// Relay limitations and settings
const (
	MaxMessageLength = 2048
	MaxSubscriptions = 100
	MaxFilters       = 100
	MaxLimit         = 100
	MaxSubIDLength   = 100
	MaxEventTags     = 100
	MaxContentLength = 2048
	MinPowDifficulty = 0
	AuthRequired     = false
	PaymentRequired  = false
	RestrictedWrites = false
)

// Database operation constants
const (
	DefaultQueryPrealloc = 500           // Default query result preallocation size
	MaxDBRetries         = 3             // Maximum database connection retry attempts
	DBRetryDelay         = 1             // Database retry delay in seconds
	
	// Database connection pool constants (production-optimized)
	// Pool sizes are calculated based on expected load patterns:
	// Small scale: Up to 200 WebSocket connections
	// Medium scale: 200-2000 WebSocket connections  
	// Large scale: 2000+ WebSocket connections
	DBPoolSmallMaxConns     = 8   // For small deployments (up to 200 WS connections)
	DBPoolSmallMinConns     = 2   // Minimum idle connections for small deployments
	DBPoolMediumMaxConns    = 25  // For medium deployments (200-2000 WS connections) 
	DBPoolMediumMinConns    = 5   // Minimum idle connections for medium deployments
	DBPoolLargeMaxConns     = 50  // For large deployments (2000+ WS connections)
	DBPoolLargeMinConns     = 10  // Minimum idle connections for large deployments
)

// Duration constants
const (
	DBConnMaxLifetime    = 60 * time.Minute  // Connection max lifetime (1 hour)
	DBConnMaxIdleTime    = 15 * time.Minute  // Max idle time (15 minutes)
	DBConnAcquireTimeout = 10 * time.Second  // Timeout for acquiring connection
)

// Timeout constants (in seconds)
const (
	HealthCheckTimeout = 5 // Timeout for health check operations
)

// Dashboard cache bounds. These are guardrails against unbounded growth
// of in-memory caches used by the public dashboard. The actual data is
// bounded by the number of distinct (year, kind) combinations in the
// events table, but these constants provide a hard ceiling so a
// misconfigured query or schema change cannot exhaust memory.
const (
	// EventBreakdownMaxRows caps the number of (year, kind, month) rows
	// returned by the grouped breakdown query. With ~100 kinds and a
	// few years, the practical maximum is ~6000 rows; 10000 leaves
	// headroom while still preventing unbounded scans.
	EventBreakdownMaxRows = 10000
)

// DefaultRelayMetadata returns the default relay metadata document
func DefaultRelayMetadata(cfg *config.Config) nip11.RelayInformationDocument {
	// Get or create relay identity, using configured public key if provided
	relayIdentity, err := identity.GetOrCreateRelayIdentityWithConfig(cfg.Relay.PublicKey)
	if err != nil {
		// Fallback to default if identity system fails
		relayIdentity = &identity.RelayIdentity{
			RelayID:   "relay-unknown",
			PublicKey: "unknown",
		}
	}

	// Use relay name from config, fallback to nostr.ltd if empty
	relayName := cfg.Relay.Name
	if relayName == "" {
		relayName = "nostr.ltd"
	}

	// Use relay description from config, fallback to default if empty
	relayDescription := cfg.Relay.Description
	if relayDescription == "" {
		relayDescription = DefaultRelayDescription
	}

	// Use relay contact from config, fallback to default if empty
	relayContact := cfg.Relay.Contact
	if relayContact == "" {
		relayContact = DefaultRelayContact
	}

	// Use relay icon from config, fallback to default if empty
	relayIcon := cfg.Relay.Icon
	if relayIcon == "" {
		relayIcon = DefaultRelayIcon
	}

	// Use relay banner from config if provided
	relayBanner := cfg.Relay.Banner

	// Use relay posting policy from config if provided
	relayPostingPolicy := cfg.Relay.PostingPolicy

	// Use relay countries from config if provided
	relayCountries := cfg.Relay.RelayCountries

	// Use actual configuration values for limitations where available, fallback to constants
	maxContentLength := cfg.Relay.ThrottlingConfig.MaxContentLen
	if maxContentLength == 0 {
		maxContentLength = MaxContentLength // fallback to default constant
	}

	return nip11.RelayInformationDocument{
		Name:          relayName,
		Description:   relayDescription,
		Contact:       relayContact,
		PubKey:        relayIdentity.PublicKey,
		SupportedNIPs: DefaultSupportedNIPs,
		Software:      DefaultRelaySoftware,
		Version:       config.Version,
		Icon:          relayIcon,
		Banner:         relayBanner,
		PostingPolicy:  relayPostingPolicy,
		RelayCountries: relayCountries,
		Limitation: &nip11.RelayLimitationDocument{
			MaxMessageLength: maxContentLength, // Use actual configured content length
			MaxSubscriptions: MaxSubscriptions, // Use constant (configurable via config if needed)
			MaxLimit:         MaxLimit,         // Use constant (configurable via config if needed)
			MaxSubidLength:   MaxSubIDLength,   // Use constant (configurable via config if needed)
			MaxEventTags:     MaxEventTags,     // Use constant (configurable via config if needed)
			MaxContentLength: maxContentLength, // Use actual configured content length
			MinPowDifficulty: cfg.Relay.MinPowDifficulty, // Use configured PoW difficulty (NIP-13)
			AuthRequired:     AuthRequired,     // Use constant (configurable via config if needed)
			PaymentRequired:  PaymentRequired,  // Use constant (configurable via config if needed)
			RestrictedWrites: RestrictedWrites, // Use constant (configurable via config if needed)
		},
	}
}

// NIPKindNames maps event kinds to their NIP names
var NIPKindNames = map[int]string{
	0:      "NIP-01: Metadata",
	1:      "NIP-01: Text Note",
	2:      "NIP-02: Follow List",
	3:      "NIP-03: OpenTimestamps",
	4:      "NIP-17: Encrypted DM",
	5:      "NIP-05: DNS Identifier",
	6:      "NIP-06: Repost (deprecated)",
	7:      "NIP-07: window.nostr",
	8:      "NIP-58: Badge Award",
	9:      "NIP-C7: Chat",
	10:     "NIP-03: OpenTimestamps",
	11:     "NIP-11: Relay Info",
	13:     "NIP-13: Proof of Work",
	14:     "NIP-17: Gift Wrap",
	15:     "NIP-15: Marketplace (deprecated)",
	16:     "NIP-16: Ephemeral",
	17:     "NIP-17: Private DM",
	18:     "NIP-18: Repost",
	19:     "NIP-19: bech32 Entities",
	20:     "NIP-68: Picture-first",
	21:     "NIP-71: Video",
	22:     "NIP-22: Comment",
	23:     "NIP-23: Long-form Content",
	24:     "NIP-A4: Public Message",
	25:     "NIP-25: Reaction",
	27:     "NIP-27: Text Note References",
	28:     "NIP-28: Public Chat (deprecated)",
	29:     "NIP-29: Relay Groups",
	30:     "NIP-30: Custom Emoji",
	32:     "NIP-32: Labeling",
	34:     "NIP-34: Git",
	35:     "NIP-35: Torrents",
	36:     "NIP-36: Content Warning",
	37:     "NIP-37: Draft Wraps",
	38:     "NIP-38: User Status",
	39:     "NIP-39: External Identities",
	40:     "NIP-40: Expiration",
	41:     "NIP-41: (reserved)",
	42:     "NIP-42: Auth",
	43:     "NIP-43: Relay Access",
	44:     "NIP-44: Encrypted Payloads",
	45:     "NIP-45: Event Counts",
	46:     "NIP-46: Remote Signing",
	47:     "NIP-47: Wallet Connect",
	48:     "NIP-48: Bridged Events",
	49:     "NIP-49: ncryptsec",
	50:     "NIP-50: Search",
	51:     "NIP-51: Lists",
	52:     "NIP-52: Calendar",
	53:     "NIP-53: Live Activities",
	54:     "NIP-54: Wiki",
	56:     "NIP-56: Reporting",
	57:     "NIP-57: Zaps",
	58:     "NIP-58: Badges",
	59:     "NIP-59: Gift Wrap",
	60:     "NIP-60: Cashu Wallets",
	61:     "NIP-61: Nutzaps",
	62:     "NIP-62: Vanish",
	64:     "NIP-64: Chess",
	65:     "NIP-65: Relay List",
	66:     "NIP-66: Relay Discovery",
	67:     "NIP-67: EOSE Hint",
	69:     "NIP-69: P2P Orders",
	70:     "NIP-70: Protected Events",
	71:     "NIP-71: Video",
	72:     "NIP-72: Communities (deprecated)",
	75:     "NIP-75: Zap Goals",
	77:     "NIP-77: Negentropy",
	78:     "NIP-78: App Data",
	84:     "NIP-84: Highlights",
	85:     "NIP-85: Trusted Assertions",
	86:     "NIP-86: Management API",
	87:     "NIP-87: Ecash Discovery",
	88:     "NIP-88: Polls",
	89:     "NIP-89: App Handlers",
	90:     "NIP-90: Data Vending",
	92:     "NIP-92: Media Attachments",
	94:     "NIP-94: File Metadata",
	98:     "NIP-98: HTTP Auth",
	99:     "NIP-99: Classifieds",
	10000:  "NIP-51: Mute List",
	10001:  "NIP-51: Pinned Notes",
	10002:  "NIP-51: Relay List",
	10003:  "NIP-51: Bookmarks",
	10004:  "NIP-51: Communities",
	10005:  "NIP-51: Public Chats",
	10006:  "NIP-51: Blocked Relays",
	10007:  "NIP-51: Search Relays",
	10009:  "NIP-51: Simple Groups",
	10012:  "NIP-51: Relay Feeds",
	10015:  "NIP-51: Interests",
	10020:  "NIP-51: Media Follows",
	10030:  "NIP-51: Emojis",
	10040:  "NIP-85: Assertion Delegation",
	10050:  "NIP-51: Release Artifacts",
	10051:  "NIP-EE: KeyPackage Relays",
	10054:  "NIP-F4: Favorite Podcasts",
	10063:  "NIP-B7: Blossom Servers",
	10064:  "NIP-F4: Authored Podcasts",
	1018:   "NIP-88: Poll Response",
	1021:   "NIP-15: Bid",
	1022:   "NIP-15: Bid Confirmation",
	10312:  "NIP-53: Room Presence",
	1040:   "NIP-03: OpenTimestamps",
	1041:   "NIP-XX: Time-Lock",
	1063:   "NIP-94: File Metadata",
	1068:   "NIP-88: Poll",
	1111:   "NIP-22: Comment",
	1125:   "NIP-YY: Web Page Asset",
	1222:   "NIP-A0: Voice Message",
	1244:   "NIP-A0: Voice Reply",
	1311:   "NIP-53: Live Chat",
	1337:   "NIP-C0: Code Snippet",
	1984:   "NIP-35: Torrent",
	1985:   "NIP-32: Label",
	2003:   "NIP-35: Torrent",
	24133:  "NIP-20: Command Result",
	30000:  "NIP-51: Curation Set",
	30001:  "NIP-51: Curation Set",
	30002:  "NIP-51: Curation Set",
	30003:  "NIP-51: Curation Set",
	30004:  "NIP-51: Curation Set",
	30005:  "NIP-51: Curation Set",
	30007:  "NIP-51: Kind Mute Set",
	30008:  "NIP-58: Profile Badges",
	30009:  "NIP-58: Badge Definition",
	30015:  "NIP-51: Interest Set",
	30017:  "NIP-15: Stall",
	30018:  "NIP-15: Product",
	30019:  "NIP-15: Marketplace UI",
	30020:  "NIP-15: Auction Product",
	30023:  "NIP-23: Long-form Content",
	30024:  "NIP-23: Draft",
	30030:  "NIP-51: Emoji Set",
	30063:  "NIP-51: Release Artifacts",
	30078:  "NIP-78: App Data",
	30267:  "NIP-51: App Curation",
	30311:  "NIP-53: Live Stream",
	30312:  "NIP-53: Meeting Space",
	30313:  "NIP-53: Meeting Room",
	30315:  "NIP-38: User Status",
	30382:  "NIP-85: Trusted Assertion",
	30402:  "NIP-99: Classified",
	30403:  "NIP-99: Draft Classified",
	30818:  "NIP-54: Wiki Article",
	30819:  "NIP-54: Wiki Redirect",
	31922:  "NIP-52: Calendar Event",
	31923:  "NIP-52: Calendar Event",
	31924:  "NIP-52: Calendar",
	31925:  "NIP-52: RSVP",
	31989:  "NIP-89: App Handler",
	31990:  "NIP-89: App Handler",
	34550:  "NIP-72: Community",
	37516:  "NIP-CC: Geocache",
	37517:  "NIP-CC: Geocache List",
	38000:  "NIP-87: Mint Recommendation",
	38172:  "NIP-87: Cashu Mint",
	38173:  "NIP-87: Fedimint",
	38383:  "NIP-69: P2P Order",
	39089:  "NIP-51: Starter Pack",
	39092:  "NIP-51: Media Starter Pack",
	39701:  "NIP-B0: Web Bookmark",
	443:    "NIP-EE: MLS KeyPackage",
	444:    "NIP-EE: MLS Welcome",
	445:    "NIP-EE: MLS Group",
	7374:   "NIP-60: Quote",
	7375:   "NIP-60: Token",
	7376:   "NIP-60: Spending History",
	9041:   "NIP-75: Zap Goal",
	9321:   "NIP-61: Nutzap",
	9734:   "NIP-68: Picture-first",
	9735:   "NIP-68: Picture-first",
	9802:   "NIP-84: Highlight",
	10154:  "NIP-F4: Podcast Metadata",
	10164:  "NIP-F4: Authored Podcasts",
	15128:  "NIP-5A: Root Site",
	35128:  "NIP-5A: Named Site",
	5128:   "NIP-5A: Site Snapshot",
	17375:  "NIP-60: Wallet Event",
}

// GetNIPName returns the NIP name for a given event kind
func GetNIPName(kind int) string {
	if name, ok := NIPKindNames[kind]; ok {
		return name
	}
	return fmt.Sprintf("Kind %d", kind)
}
