package relay

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Shugur-Network/relay/internal/logger"
	nostr "github.com/nbd-wtf/go-nostr"
	"go.uber.org/zap"
)

// NIP-86: Relay Management API
// JSON-RPC over HTTP with NIP-98 Authorization

// managementRequest represents a NIP-86 JSON-RPC request body.
type managementRequest struct {
	Method string   `json:"method"`
	Params []string `json:"params"`
}

// managementResponse represents a NIP-86 JSON-RPC response body.
type managementResponse struct {
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// mgmtCap is the maximum number of entries the in-memory ban maps will
// hold. Past the cap, the oldest entries are evicted on insert. This stops
// a single compromised admin from OOM-ing the relay by calling banevent /
// banpubkey / blockip repeatedly (issue #63).
const mgmtCap = 100_000

// managementState holds in-memory state for NIP-86 management operations
// that don't map to existing relay infrastructure.
type managementState struct {
	mu           sync.RWMutex
	bannedEvents map[string]bool // event ID -> banned
	blockedIPs   map[string]bool // IP -> blocked (permanent via management)
	// insertionOrder tracks insertion time per key so we can evict the
	// oldest entry when the map exceeds mgmtCap. Only used for evict().
	insertOrder []string
}

var mgmtState = &managementState{
	bannedEvents: make(map[string]bool),
	blockedIPs:   make(map[string]bool),
}

// evictOldestLocked removes the oldest entry from the combined ban map.
// Caller must hold mgmtState.mu (write lock).
func (s *managementState) evictOldestLocked() {
	if len(s.insertOrder) == 0 {
		return
	}
	key := s.insertOrder[0]
	s.insertOrder = s.insertOrder[1:]
	delete(s.bannedEvents, key)
	delete(s.blockedIPs, key)
}

// recordInsert tracks a new key for later eviction. Caller must hold
// mgmtState.mu (write lock).
func (s *managementState) recordInsert(key string) {
	s.insertOrder = append(s.insertOrder, key)
	if (len(s.bannedEvents) + len(s.blockedIPs)) > mgmtCap {
		s.evictOldestLocked()
	}
}

// nip86SupportedMethods lists all implemented NIP-86 methods.
var nip86SupportedMethods = []string{
	"supportedmethods",
	"banpubkey",
	"listbannedpubkeys",
	"allowpubkey",
	"listallowedpubkeys",
	"banevent",
	"listbannedevents",
	"allowevent",
	"changerelayname",
	"changerelaydescription",
	"changerelayicon",
	"allowkind",
	"disallowkind",
	"listallowedkinds",
	"blockip",
	"unblockip",
	"listblockedips",
	"addrelayrole",
	"removerelayrole",
	"listrelayroles",
	// SPEC UPDATE (2 months ago): Added relay roles event management methods
	"createrole",
	"editrole",
	"deleterole",
	"assignrole",
	"unassignrole",
}

// handleManagementAPI handles NIP-86 JSON-RPC management requests.
func (s *Server) handleManagementAPI(w http.ResponseWriter, r *http.Request) {
	log := logger.New("nip86")

	// Handle CORS preflight
	if r.Method == http.MethodOptions {
		setManagementCORSHeaders(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Only POST allowed
	if r.Method != http.MethodPost {
		writeManagementError(w, http.StatusMethodNotAllowed, "only POST method is allowed")
		return
	}

	// Read body (64KB limit)
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		writeManagementError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	// Verify NIP-98 Authorization
	pubkey, authErr := verifyNIP98Auth(r, body, s.cfg.PublicURL)
	if authErr != "" {
		log.Warn("NIP-86 auth failure",
			zap.String("error", authErr),
			zap.String("client_ip", r.RemoteAddr))
		writeManagementError(w, http.StatusUnauthorized, authErr)
		return
	}

	// Check if pubkey is authorized as admin
	if !s.isAdmin(pubkey) {
		log.Warn("NIP-86 unauthorized admin attempt",
			zap.String("pubkey", pubkey[:16]+"..."),
			zap.String("client_ip", r.RemoteAddr))
		writeManagementError(w, http.StatusForbidden, "pubkey is not authorized as relay admin")
		return
	}

	// Parse JSON-RPC request
	var req managementRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeManagementError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}

	log.Info("NIP-86 management request",
		zap.String("method", req.Method),
		zap.String("admin", pubkey[:16]+"..."))

	// Dispatch method
	result, methodErr := s.dispatchManagementMethod(req.Method, req.Params)
	if methodErr != "" {
		// Audit every accepted method call, including failures, for forensic
		// visibility (issue #59).
		log.Warn("NIP-86 management method rejected",
			zap.String("method", req.Method),
			zap.String("admin", pubkey),
			zap.Strings("params", req.Params),
			zap.String("client_ip", r.RemoteAddr),
			zap.String("error", methodErr))
		writeManagementResponse(w, managementResponse{Error: methodErr})
		return
	}

	log.Info("NIP-86 management method accepted",
		zap.String("method", req.Method),
		zap.String("admin", pubkey),
		zap.Strings("params", req.Params),
		zap.String("client_ip", r.RemoteAddr))

	writeManagementResponse(w, managementResponse{Result: result})
}

// verifyNIP98Auth validates the NIP-98 Authorization header (kind 27235).
// Returns the authenticated pubkey and an error string (empty on success).
func verifyNIP98Auth(r *http.Request, body []byte, relayURL string) (string, string) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", "missing Authorization header"
	}

	if !strings.HasPrefix(authHeader, "Nostr ") {
		return "", "Authorization header must start with 'Nostr '"
	}

	// Decode base64 event
	encoded := strings.TrimPrefix(authHeader, "Nostr ")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", "invalid base64 in Authorization header"
	}

	// Parse event
	var evt nostr.Event
	if err := json.Unmarshal(decoded, &evt); err != nil {
		return "", "invalid event in Authorization header"
	}

	// Verify kind 27235
	if evt.Kind != 27235 {
		return "", "auth event must be kind 27235"
	}

	// Verify signature
	ok, err := evt.CheckSignature()
	if err != nil || !ok {
		return "", "invalid event signature"
	}

	// Check created_at is within 60 seconds
	now := time.Now().Unix()
	diff := now - int64(evt.CreatedAt)
	if diff < 0 {
		diff = -diff
	}
	if diff > 60 {
		return "", "auth event timestamp too old or too far in future"
	}

	// Verify u tag matches relay URL
	uTag := evt.Tags.GetFirst([]string{"u", ""})
	if uTag == nil || len(*uTag) < 2 {
		return "", "auth event missing 'u' tag"
	}
	eventURL := strings.TrimRight((*uTag)[1], "/")
	expectedURL := strings.TrimRight(relayURL, "/")
	if eventURL != expectedURL {
		return "", fmt.Sprintf("auth event 'u' tag mismatch: got %s, expected %s", eventURL, expectedURL)
	}

	// Verify method tag is POST
	methodTag := evt.Tags.GetFirst([]string{"method", ""})
	if methodTag == nil || len(*methodTag) < 2 {
		return "", "auth event missing 'method' tag"
	}
	if strings.ToUpper((*methodTag)[1]) != "POST" {
		return "", "auth event method must be POST"
	}

	// Verify payload tag (SHA256 of request body)
	payloadTag := evt.Tags.GetFirst([]string{"payload", ""})
	if payloadTag == nil || len(*payloadTag) < 2 {
		return "", "auth event missing 'payload' tag"
	}
	bodyHash := sha256.Sum256(body)
	expectedPayload := hex.EncodeToString(bodyHash[:])
	if (*payloadTag)[1] != expectedPayload {
		return "", "auth event payload hash does not match request body"
	}

	return evt.PubKey, ""
}

// isAdmin checks if the pubkey is authorized as a relay admin.
// The relay owner pubkey (PUBLIC_KEY) is always an admin. Pubkeys in
// Relay.AdminPubkeys are also admins. Pubkeys that have been assigned
// the "admin" role via the NIP-86 role-management methods are also
// treated as admins (issue #58). This wires the previously-orphaned
// role assignments into the actual authorization decision.
func (s *Server) isAdmin(pubkey string) bool {
	pubkey = strings.ToLower(pubkey)

	// Relay owner pubkey is always admin
	if s.cfg.PublicKey != "" && strings.ToLower(s.cfg.PublicKey) == pubkey {
		return true
	}

	// Check admin pubkeys list
	for _, admin := range s.fullCfg.Relay.AdminPubkeys {
		if strings.ToLower(admin) == pubkey {
			return true
		}
	}

	// Check role assignments: any pubkey holding the "admin" role is
	// considered a relay admin. Other role names (e.g. "moderator",
	// "readonly") are not yet honored; the audit ends with the role
	// mechanism at least consulted for the admin role.
	if s.fullCfg.Relay.UserRoles != nil {
		for _, r := range s.fullCfg.Relay.UserRoles[pubkey] {
			if strings.EqualFold(r, "admin") {
				return true
			}
		}
	}

	return false
}

// dispatchManagementMethod routes a NIP-86 method call to the appropriate handler.
func (s *Server) dispatchManagementMethod(method string, params []string) (interface{}, string) {
	switch method {
	case "supportedmethods":
		return nip86SupportedMethods, ""
	case "banpubkey":
		return s.mgmtBanPubkey(params)
	case "listbannedpubkeys":
		return s.mgmtListBannedPubkeys()
	case "allowpubkey":
		return s.mgmtAllowPubkey(params)
	case "listallowedpubkeys":
		return s.mgmtListAllowedPubkeys()
	case "banevent":
		return s.mgmtBanEvent(params)
	case "listbannedevents":
		return s.mgmtListBannedEvents()
	case "allowevent":
		return s.mgmtAllowEvent(params)
	case "changerelayname":
		return s.mgmtChangeRelayName(params)
	case "changerelaydescription":
		return s.mgmtChangeRelayDescription(params)
	case "changerelayicon":
		return s.mgmtChangeRelayIcon(params)
	case "allowkind":
		return s.mgmtAllowKind(params)
	case "disallowkind":
		return s.mgmtDisallowKind(params)
	case "listallowedkinds":
		return s.mgmtListAllowedKinds()
	case "blockip":
		return s.mgmtBlockIP(params)
	case "unblockip":
		return s.mgmtUnblockIP(params)
	case "listblockedips":
		return s.mgmtListBlockedIPs()
	case "addrelayrole":
		return s.mgmtAddRelayRole(params)
	case "removerelayrole":
		return s.mgmtRemoveRelayRole(params)
	case "listrelayroles":
		return s.mgmtListRelayRoles()
	// SPEC UPDATE (2 months ago): Added relay roles event management methods
	case "createrole":
		return s.mgmtCreateRole(params)
	case "editrole":
		return s.mgmtEditRole(params)
	case "deleterole":
		return s.mgmtDeleteRole(params)
	case "assignrole":
		return s.mgmtAssignRole(params)
	case "unassignrole":
		return s.mgmtUnassignRole(params)
	default:
		return nil, fmt.Sprintf("unknown method: %s", method)
	}
}

// --- Pubkey Ban/Allow ---

func (s *Server) mgmtBanPubkey(params []string) (interface{}, string) {
	if len(params) < 1 {
		return nil, "missing pubkey parameter"
	}
	pubkey := strings.ToLower(params[0])
	if len(pubkey) != 64 {
		return nil, "invalid pubkey: must be 64 hex characters"
	}

	pv, ok := s.node.GetValidator().(*PluginValidator)
	if !ok {
		return nil, "internal error: validator type mismatch"
	}
	pv.AddBlacklistedPubkey(pubkey)

	logger.New("nip86").Info("Pubkey banned via management API",
		zap.String("pubkey", pubkey[:16]+"..."))

	return true, ""
}

func (s *Server) mgmtListBannedPubkeys() (interface{}, string) {
	pv, ok := s.node.GetValidator().(*PluginValidator)
	if !ok {
		return nil, "internal error: validator type mismatch"
	}
	pubkeys := pv.GetBlacklistedPubkeys()
	sort.Strings(pubkeys)
	return pubkeys, ""
}

func (s *Server) mgmtAllowPubkey(params []string) (interface{}, string) {
	if len(params) < 1 {
		return nil, "missing pubkey parameter"
	}
	pubkey := strings.ToLower(params[0])

	pv, ok := s.node.GetValidator().(*PluginValidator)
	if !ok {
		return nil, "internal error: validator type mismatch"
	}
	pv.RemoveBlacklistedPubkey(pubkey)

	logger.New("nip86").Info("Pubkey unbanned via management API",
		zap.String("pubkey", pubkey[:16]+"..."))

	return true, ""
}

func (s *Server) mgmtListAllowedPubkeys() (interface{}, string) {
	whitelist := s.fullCfg.RelayPolicy.Whitelist.PubKeys
	if whitelist == nil {
		return []string{}, ""
	}
	return whitelist, ""
}

// --- Event Ban/Allow ---

func (s *Server) mgmtBanEvent(params []string) (interface{}, string) {
	if len(params) < 1 {
		return nil, "missing event_id parameter"
	}
	eventID := strings.ToLower(params[0])
	if len(eventID) != 64 {
		return nil, "invalid event_id: must be 64 hex characters"
	}

	mgmtState.mu.Lock()
	mgmtState.bannedEvents[eventID] = true
	mgmtState.recordInsert(eventID)
	mgmtState.mu.Unlock()

	logger.New("nip86").Info("Event banned via management API",
		zap.String("event_id", eventID[:16]+"..."))

	return true, ""
}

func (s *Server) mgmtListBannedEvents() (interface{}, string) {
	mgmtState.mu.RLock()
	defer mgmtState.mu.RUnlock()

	events := make([]string, 0, len(mgmtState.bannedEvents))
	for id := range mgmtState.bannedEvents {
		events = append(events, id)
	}
	sort.Strings(events)
	return events, ""
}

func (s *Server) mgmtAllowEvent(params []string) (interface{}, string) {
	if len(params) < 1 {
		return nil, "missing event_id parameter"
	}
	eventID := strings.ToLower(params[0])

	mgmtState.mu.Lock()
	delete(mgmtState.bannedEvents, eventID)
	mgmtState.mu.Unlock()

	logger.New("nip86").Info("Event unbanned via management API",
		zap.String("event_id", eventID[:16]+"..."))

	return true, ""
}

// --- Relay Info Changes ---

func (s *Server) mgmtChangeRelayName(params []string) (interface{}, string) {
	if len(params) < 1 {
		return nil, "missing name parameter"
	}
	name := params[0]
	if len(name) > 30 {
		return nil, "relay name too long (max 30 characters)"
	}
	s.fullCfg.Relay.Name = name
	s.cfg.Name = name

	logger.New("nip86").Info("Relay name changed via management API",
		zap.String("name", name))

	return true, ""
}

func (s *Server) mgmtChangeRelayDescription(params []string) (interface{}, string) {
	if len(params) < 1 {
		return nil, "missing description parameter"
	}
	desc := params[0]
	if len(desc) > 200 {
		return nil, "description too long (max 200 characters)"
	}
	s.fullCfg.Relay.Description = desc
	s.cfg.Description = desc

	logger.New("nip86").Info("Relay description changed via management API",
		zap.String("description", desc))

	return true, ""
}

func (s *Server) mgmtChangeRelayIcon(params []string) (interface{}, string) {
	if len(params) < 1 {
		return nil, "missing icon URL parameter"
	}
	icon := params[0]
	// Cap length and restrict scheme to http(s) to prevent XSS sinks in
	// NIP-11 consumers (issue #60).
	if len(icon) > 2048 {
		return nil, "icon URL too long (max 2048 characters)"
	}
	parsed, err := url.Parse(icon)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, "icon URL must be a valid http(s) URL"
	}
	s.fullCfg.Relay.Icon = icon
	s.cfg.Icon = icon

	logger.New("nip86").Info("Relay icon changed via management API",
		zap.String("icon", icon))

	return true, ""
}

// --- Kind Allow/Disallow ---

func (s *Server) mgmtAllowKind(params []string) (interface{}, string) {
	if len(params) < 1 {
		return nil, "missing kind parameter"
	}
	kind, err := strconv.Atoi(params[0])
	if err != nil {
		return nil, "invalid kind: must be a number"
	}
	if kind < 0 || kind > 65535 {
		return nil, "invalid kind: must be 0-65535"
	}

	pv, ok := s.node.GetValidator().(*PluginValidator)
	if !ok {
		return nil, "internal error: validator type mismatch"
	}
	pv.AddAllowedKind(kind)

	logger.New("nip86").Info("Kind allowed via management API",
		zap.Int("kind", kind))

	return true, ""
}

func (s *Server) mgmtDisallowKind(params []string) (interface{}, string) {
	if len(params) < 1 {
		return nil, "missing kind parameter"
	}
	kind, err := strconv.Atoi(params[0])
	if err != nil {
		return nil, "invalid kind: must be a number"
	}

	pv, ok := s.node.GetValidator().(*PluginValidator)
	if !ok {
		return nil, "internal error: validator type mismatch"
	}
	pv.RemoveAllowedKind(kind)

	logger.New("nip86").Info("Kind disallowed via management API",
		zap.Int("kind", kind))

	return true, ""
}

func (s *Server) mgmtListAllowedKinds() (interface{}, string) {
	pv, ok := s.node.GetValidator().(*PluginValidator)
	if !ok {
		return nil, "internal error: validator type mismatch"
	}
	kinds := pv.GetAllowedKinds()
	sort.Ints(kinds)
	return kinds, ""
}

// --- IP Block/Unblock ---

func (s *Server) mgmtBlockIP(params []string) (interface{}, string) {
	if len(params) < 1 {
		return nil, "missing IP parameter"
	}
	ip := params[0]
	if ip == "" {
		return nil, "IP address cannot be empty"
	}

	// Validate that the input parses as an IP (not a CIDR, not junk).
	// Previously any string could be stored and looked up, which let a
	// malicious admin corrupt the ban map (issue #52).
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return nil, "invalid IP address"
	}

	// Track in management state
	mgmtState.mu.Lock()
	mgmtState.blockedIPs[ip] = true
	mgmtState.recordInsert(ip)
	mgmtState.mu.Unlock()

	// Also add to the relay's client ban list with permanent expiry
	banListMutex.Lock()
	clientBanList[ip] = time.Now().Add(100 * 365 * 24 * time.Hour) // ~100 years = permanent
	banListMutex.Unlock()

	logger.New("nip86").Info("IP blocked via management API",
		zap.String("ip", ip))

	return true, ""
}

func (s *Server) mgmtUnblockIP(params []string) (interface{}, string) {
	if len(params) < 1 {
		return nil, "missing IP parameter"
	}
	ip := params[0]

	// Remove from management state
	mgmtState.mu.Lock()
	delete(mgmtState.blockedIPs, ip)
	mgmtState.mu.Unlock()

	// Also remove from relay's client ban list
	banListMutex.Lock()
	delete(clientBanList, ip)
	banListMutex.Unlock()

	logger.New("nip86").Info("IP unblocked via management API",
		zap.String("ip", ip))

	return true, ""
}

func (s *Server) mgmtListBlockedIPs() (interface{}, string) {
	mgmtState.mu.RLock()
	defer mgmtState.mu.RUnlock()

	ips := make([]string, 0, len(mgmtState.blockedIPs))
	for ip := range mgmtState.blockedIPs {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	return ips, ""
}

// --- Relay Role Management ---

// mgmtAddRelayRole adds a new relay role.
func (s *Server) mgmtAddRelayRole(params []string) (interface{}, string) {
	if len(params) < 2 {
		return nil, "missing role name and description parameters"
	}
	roleName := params[0]
	roleDesc := params[1]

	if roleName == "" || roleDesc == "" {
		return nil, "role name and description cannot be empty"
	}

	gs := GetGroupStore()
	if gs == nil {
		return nil, "relay key not initialized"
	}

	// Add role to the group store's role definitions
	gs.mu.Lock()
	if gs.groups == nil {
		gs.groups = make(map[string]*Group)
	}
	// For now, store in a special config key
	if s.fullCfg.Relay.Roles == nil {
		s.fullCfg.Relay.Roles = make(map[string]string)
	}
	s.fullCfg.Relay.Roles[roleName] = roleDesc
	gs.mu.Unlock()

	logger.New("nip86").Info("Relay role added via management API",
		zap.String("role", roleName),
		zap.String("description", roleDesc))

	return true, ""
}

func (s *Server) mgmtRemoveRelayRole(params []string) (interface{}, string) {
	if len(params) < 1 {
		return nil, "missing role name parameter"
	}
	roleName := params[0]

	if s.fullCfg.Relay.Roles == nil {
		return nil, "role not found"
	}

	if _, exists := s.fullCfg.Relay.Roles[roleName]; !exists {
		return nil, "role not found"
	}

	delete(s.fullCfg.Relay.Roles, roleName)

	logger.New("nip86").Info("Relay role removed via management API",
		zap.String("role", roleName))

	return true, ""
}

func (s *Server) mgmtListRelayRoles() (interface{}, string) {
	if s.fullCfg.Relay.Roles == nil {
		return map[string]string{}, ""
	}

	// Return a copy to avoid race conditions
	roles := make(map[string]string)
	for k, v := range s.fullCfg.Relay.Roles {
		roles[k] = v
	}
	return roles, ""
}

// SPEC UPDATE (2 months ago): Added relay roles event management methods
// https://github.com/nostr-protocol/nips/blob/master/86.md
//
// The NIP-86 spec was updated to add granular role management methods:
//   - createrole: [id, label, description, color, order]
//   - editrole: [id, label, description, color, order]
//   - deleterole: [id]
//   - assignrole: [pubkey, role-id]
//   - unassignrole: [pubkey, role-id]

// mgmtCreateRole creates a new relay role with full metadata.
// params: [id, label, description, color, order]
func (s *Server) mgmtCreateRole(params []string) (interface{}, string) {
	if len(params) < 5 {
		return nil, "missing parameters: expected [id, label, description, color, order]"
	}
	id, label, description, color, orderStr := params[0], params[1], params[2], params[3], params[4]

	if id == "" || label == "" {
		return nil, "role id and label cannot be empty"
	}

	if _, exists := s.fullCfg.Relay.Roles[id]; exists {
		return nil, fmt.Sprintf("role '%s' already exists", id)
	}

	// Validate order is numeric
	if _, err := strconv.Atoi(orderStr); err != nil {
		return nil, fmt.Sprintf("invalid order value '%s': must be numeric", orderStr)
	}

	// Store as JSON-encoded metadata for full role info
	roleMeta := fmt.Sprintf(`{"label":%q,"description":%q,"color":%q,"order":%q}`, label, description, color, orderStr)
	s.fullCfg.Relay.Roles[id] = roleMeta

	logger.New("nip86").Info("Relay role created via management API",
		zap.String("role_id", id),
		zap.String("label", label))

	return true, ""
}

// mgmtEditRole edits an existing relay role.
// params: [id, label, description, color, order]
func (s *Server) mgmtEditRole(params []string) (interface{}, string) {
	if len(params) < 5 {
		return nil, "missing parameters: expected [id, label, description, color, order]"
	}
	id, label, description, color, orderStr := params[0], params[1], params[2], params[3], params[4]

	if id == "" || label == "" {
		return nil, "role id and label cannot be empty"
	}

	if _, exists := s.fullCfg.Relay.Roles[id]; !exists {
		return nil, fmt.Sprintf("role '%s' not found", id)
	}

	if _, err := strconv.Atoi(orderStr); err != nil {
		return nil, fmt.Sprintf("invalid order value '%s': must be numeric", orderStr)
	}

	roleMeta := fmt.Sprintf(`{"label":%q,"description":%q,"color":%q,"order":%q}`, label, description, color, orderStr)
	s.fullCfg.Relay.Roles[id] = roleMeta

	logger.New("nip86").Info("Relay role edited via management API",
		zap.String("role_id", id),
		zap.String("label", label))

	return true, ""
}

// mgmtDeleteRole deletes a relay role.
// params: [id]
func (s *Server) mgmtDeleteRole(params []string) (interface{}, string) {
	if len(params) < 1 {
		return nil, "missing role id parameter"
	}
	id := params[0]

	if id == "" {
		return nil, "role id cannot be empty"
	}

	if _, exists := s.fullCfg.Relay.Roles[id]; !exists {
		return nil, fmt.Sprintf("role '%s' not found", id)
	}

	delete(s.fullCfg.Relay.Roles, id)

	logger.New("nip86").Info("Relay role deleted via management API",
		zap.String("role_id", id))

	return true, ""
}

// mgmtAssignRole assigns a role to a pubkey.
// params: [pubkey, role-id]
func (s *Server) mgmtAssignRole(params []string) (interface{}, string) {
	if len(params) < 2 {
		return nil, "missing parameters: expected [pubkey, role-id]"
	}
	pubkey := strings.ToLower(params[0])
	roleID := params[1]

	if len(pubkey) != 64 {
		return nil, "invalid pubkey: must be 64 hex characters"
	}

	if _, exists := s.fullCfg.Relay.Roles[roleID]; !exists {
		return nil, fmt.Sprintf("role '%s' not found", roleID)
	}

	if s.fullCfg.Relay.UserRoles == nil {
		s.fullCfg.Relay.UserRoles = make(map[string][]string)
	}

	// Avoid duplicate assignments
	for _, r := range s.fullCfg.Relay.UserRoles[pubkey] {
		if r == roleID {
			return true, ""
		}
	}
	s.fullCfg.Relay.UserRoles[pubkey] = append(s.fullCfg.Relay.UserRoles[pubkey], roleID)

	logger.New("nip86").Info("Role assigned via management API",
		zap.String("pubkey", pubkey),
		zap.String("role_id", roleID))

	return true, ""
}

// mgmtUnassignRole removes a role from a pubkey.
// params: [pubkey, role-id]
func (s *Server) mgmtUnassignRole(params []string) (interface{}, string) {
	if len(params) < 2 {
		return nil, "missing parameters: expected [pubkey, role-id]"
	}
	pubkey := strings.ToLower(params[0])
	roleID := params[1]

	if len(pubkey) != 64 {
		return nil, "invalid pubkey: must be 64 hex characters"
	}

	roles, exists := s.fullCfg.Relay.UserRoles[pubkey]
	if !exists {
		return nil, fmt.Sprintf("no roles assigned to pubkey")
	}

	filtered := roles[:0]
	found := false
	for _, r := range roles {
		if r == roleID {
			found = true
			continue
		}
		filtered = append(filtered, r)
	}

	if !found {
		return nil, fmt.Sprintf("role '%s' not assigned to pubkey", roleID)
	}

	if len(filtered) == 0 {
		delete(s.fullCfg.Relay.UserRoles, pubkey)
	} else {
		s.fullCfg.Relay.UserRoles[pubkey] = filtered
	}

	logger.New("nip86").Info("Role unassigned via management API",
		zap.String("pubkey", pubkey),
		zap.String("role_id", roleID))

	return true, ""
}

// --- Response Helpers ---

func setManagementCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
}

func writeManagementResponse(w http.ResponseWriter, resp managementResponse) {
	setManagementCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func writeManagementError(w http.ResponseWriter, status int, message string) {
	setManagementCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(managementResponse{Error: message})
}

// IsBannedEvent checks if an event ID has been banned via NIP-86 management.
// Called from event processing to filter banned events.
func IsBannedEvent(eventID string) bool {
	mgmtState.mu.RLock()
	defer mgmtState.mu.RUnlock()
	return mgmtState.bannedEvents[strings.ToLower(eventID)]
}
