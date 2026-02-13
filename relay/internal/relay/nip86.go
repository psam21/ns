package relay

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// managementState holds in-memory state for NIP-86 management operations
// that don't map to existing relay infrastructure.
type managementState struct {
	mu           sync.RWMutex
	bannedEvents map[string]bool // event ID -> banned
	blockedIPs   map[string]bool // IP -> blocked (permanent via management)
}

var mgmtState = &managementState{
	bannedEvents: make(map[string]bool),
	blockedIPs:   make(map[string]bool),
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
		writeManagementResponse(w, managementResponse{Error: methodErr})
		return
	}

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
// The relay owner pubkey (PUBLIC_KEY) is always an admin.
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

	// Track in management state
	mgmtState.mu.Lock()
	mgmtState.blockedIPs[ip] = true
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
