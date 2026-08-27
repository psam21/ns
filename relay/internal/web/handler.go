package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Shugur-Network/relay/internal/config"
	"github.com/Shugur-Network/relay/internal/constants"
	"github.com/Shugur-Network/relay/internal/errors"
	"github.com/Shugur-Network/relay/internal/identity"
	"github.com/Shugur-Network/relay/internal/metrics"
	"github.com/Shugur-Network/relay/internal/storage"
	"go.uber.org/zap"
)

// DashboardData represents the data passed to the dashboard template
type DashboardData struct {
	Name                string                `json:"name"`
	Description         string                `json:"description"`
	Software            string                `json:"software"`
	Version             string                `json:"version"`
	Contact             string                `json:"contact"`
	Icon                string                `json:"icon"`
	Host                string                `json:"host"`
	Pubkey              string                `json:"pubkey"`
	RelayID             string                `json:"relay_id"`
	SupportedNIPs       []interface{}         `json:"supported_nips"`
	CustomNIPs          []constants.CustomNIP `json:"custom_nips"`
	Limitation          *LimitationData       `json:"limitation"`
	Stats               *StatsData            `json:"stats"`
	TopKinds            []EventKindSummary    `json:"top_kinds"`
	EventKinds          []EventKindSummary    `json:"event_kinds"`
	EventCacheStatus    string                `json:"event_cache_status"`
	EventCacheUpdatedAt string                `json:"event_cache_updated_at,omitempty"`
	EventCacheMessage   string                `json:"event_cache_message,omitempty"`
	LiveSince           string                `json:"live_since"`
	Cluster             *storage.DatabaseInfo `json:"cluster"`
}

// LimitationData represents relay limitations
type LimitationData struct {
	MaxMessageLength int  `json:"max_message_length"`
	MaxSubscriptions int  `json:"max_subscriptions"`
	MaxFilters       int  `json:"max_filters"`
	MaxEventTags     int  `json:"max_event_tags"`
	MaxConnections   int  `json:"max_connections"`
	AuthRequired     bool `json:"auth_required"`
	PaymentRequired  bool `json:"payment_required"`
}

// StatsData represents relay statistics
type StatsData struct {
	ActiveConnections     int64            `json:"active_connections"`
	TotalConnections      int64            `json:"total_connections"`
	MessagesProcessed     int64            `json:"messages_processed"`
	EventsStored          int64            `json:"events_stored"`
	EventsStoredReady     bool             `json:"events_stored_ready"`
	EventsStoredStatus    string           `json:"events_stored_status"`
	EventsStoredUpdatedAt string           `json:"events_stored_updated_at,omitempty"`
	EventsStoredMessage   string           `json:"events_stored_message,omitempty"`
	ActiveSubscriptions   int64            `json:"active_subscriptions"`
	MessagesSent          int64            `json:"messages_sent"`
	EventsPerSecond       float64          `json:"events_per_second"`
	ConnectionsPerSecond  float64          `json:"connections_per_second"`
	AverageResponseTime   float64          `json:"average_response_time_ms"`
	ErrorRate             float64          `json:"error_rate"`
	MemoryUsage           map[string]int64 `json:"memory_usage"`
	LoadPercentage        float64          `json:"load_percentage"`
}

// EventKindSummary represents an aggregate count for a Nostr event kind.
type EventKindSummary struct {
	Kind     int     `json:"kind"`
	KindName string  `json:"kind_name"`
	Count    int64   `json:"count"`
	Share    float64 `json:"share"`
}

// Handler provides HTTP handlers for the web dashboard
type Handler struct {
	config    *config.Config
	logger    *zap.Logger
	startTime time.Time
	liveSince time.Time
	db        interface {
		GetTotalEventCount(ctx context.Context) (int64, error)
		GetTotalEventCount2026Plus(ctx context.Context) (int64, error)
		GetDatabaseInfo(ctx context.Context) (*storage.DatabaseInfo, error)
		GetClusterHealth(ctx context.Context) (map[string]interface{}, error)
		GetYearsWithEvents(ctx context.Context) ([]int, error)
		GetEventCountsByKindMonth(ctx context.Context, year int) ([]storage.EventCountByKindMonth, error)
		GetEventCountsByKindMonthFromYear(ctx context.Context, startYear int) ([]storage.EventCountByKindMonth, error)
	} // Database interface
	eventsCacheMu            sync.RWMutex
	eventsCache              EventBreakdownData
	eventsCacheUpdatedAt     time.Time
	eventsCacheRefreshing    bool
	eventsCacheLastAttemptAt time.Time
	eventsCacheLastErr       error
	eventsTotal              int64
	eventsTotalUpdatedAt     time.Time
	eventsTotalRefreshing    bool
	eventsTotalLastAttemptAt time.Time
	eventsTotalLastErr       error
	storedCountRefreshing    bool
	storedCountLastAttemptAt time.Time
	storedCountLastErr       error
}

const (
	eventBreakdownCacheTTL     = 30 * time.Second
	eventRefreshRetryDelay     = 15 * time.Second
	eventQueryTimeout          = 20 * time.Second
	storedCountRefreshInterval = 5 * time.Minute
)

// NewHandler creates a new web handler
func NewHandler(cfg *config.Config, logger *zap.Logger, node interface{}) *Handler {
	h := &Handler{
		config:    cfg,
		logger:    logger,
		startTime: time.Now(),
		liveSince: loadFirstBootTime(),
	}

	// Set database interface if node provides it
	if nodeWithDB, ok := node.(interface {
		DB() *storage.DB
	}); ok {
		h.db = nodeWithDB.DB()
	}

	return h
}

// HandleDashboard serves the main dashboard page
func (h *Handler) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	// Apply security headers for dashboard
	dashboardHeaders := DefaultSecurityHeaders()
	dashboardHeaders.Apply(w)

	// Load template with custom functions
	tmplPath := filepath.Join("web", "templates", "index.html")
	funcMap := template.FuncMap{
		"formatNIP": func(v interface{}) string {
			switch val := v.(type) {
			case int:
				return fmt.Sprintf("%02d", val)
			case string:
				return val
			default:
				return fmt.Sprintf("%v", val)
			}
		},
		"nipDescription": func(v interface{}) string {
			nip := ""
			switch val := v.(type) {
			case int:
				nip = fmt.Sprintf("%02d", val)
			case string:
				nip = val
			default:
				nip = fmt.Sprintf("%v", val)
			}
			descriptions := map[string]string{
				"01": "Basic Protocol",
				"02": "Follow List",
				"03": "OpenTimestamps",
				"05": "DNS Identifiers",
				"07": "window.nostr",
				"09": "Event Deletion",
				"10": "Text Notes & Threads",
				"11": "Relay Info",
				"13": "Proof of Work",
				"15": "Marketplace",
				"17": "Private DMs",
				"18": "Reposts",
				"19": "bech32 Entities",
				"21": "nostr: URI Scheme",
				"22": "Comment",
				"23": "Long-form Content",
				"24": "Extra Metadata",
				"25": "Reactions",
				"27": "Text Note References",
				"29": "Relay Groups",
				"30": "Custom Emoji",
				"32": "Labeling",
				"34": "Git Stuff",
				"35": "Torrents",
				"36": "Content Warning",
				"37": "Draft Wraps",
				"38": "User Statuses",
				"39": "External Identities",
				"40": "Expiration",
				"42": "Authentication",
				"43": "Relay Access Metadata",
				"44": "Encrypted Payloads",
				"45": "Event Counts",
				"46": "Remote Signing",
				"47": "Wallet Connect",
				"48": "Bridged Events",
				"49": "ncryptsec",
				"50": "Search",
				"51": "Lists",
				"52": "Calendar Events",
				"53": "Live Activities",
				"5A": "Static Websites",
				"54": "Wiki",
				"56": "Reporting",
				"57": "Lightning Zaps",
				"58": "Badges",
				"59": "Gift Wrap",
				"60": "Cashu Wallets",
				"61": "Nutzaps",
				"62": "Request to Vanish",
				"64": "Chess (PGN)",
				"65": "Relay List Metadata",
				"66": "Relay Discovery",
				"67": "EOSE Completeness Hint",
				"69": "P2P Orders",
				"70": "Protected Events",
				"71": "Video Events",
				"72": "Communities",
				"75": "Zap Goals",
				"77": "Negentropy Sync",
				"78": "App-specific Data",
				"84": "Highlights",
				"85": "Trusted Assertions",
				"86": "Management API",
				"87": "Ecash Mint Discovery",
				"88": "Polls",
				"89": "App Handlers",
				"90": "Data Vending Machine",
				"92": "Media Attachments",
				"94": "File Metadata",
				"98": "HTTP Auth",
				"99": "Classified Listings",
				"7D": "Threads",
				"A0": "Voice Messages",
				"A4": "Public Messages",
				"B0": "Web Bookmarking",
				"C0": "Code Snippets",
				"C7": "Chats",
				"CC": "Geocaching",
				"F4": "Podcasts",
				"B7": "Blossom Server List",
			}
			if desc, ok := descriptions[nip]; ok {
				return desc
			}
			return ""
		},
	}
	tmpl, err := template.New("index.html").Funcs(funcMap).ParseFiles(tmplPath)
	if err != nil {
		h.logger.Error("Failed to parse dashboard template", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Prepare dashboard data
	data := h.getDashboardData(r.Host)

	// Execute template
	if err := tmpl.Execute(w, data); err != nil {
		h.logger.Error("Failed to execute dashboard template", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// HandleStatic serves static files
func (h *Handler) HandleStatic(w http.ResponseWriter, r *http.Request) {
	// Apply security headers for static files
	staticHeaders := DefaultSecurityHeaders()
	staticHeaders.Apply(w)

	// Serve static files safely, preventing path traversal
	root := filepath.Join("web", "static")

	// Extract and validate the requested path
	requestedPath := strings.TrimPrefix(r.URL.Path, "/static/")

	// Use our new sanitization function
	sanitizedPath, err := SanitizePath(requestedPath)
	if err != nil {
		h.logger.Warn("Static file path validation failed",
			zap.Error(err),
			zap.String("requested_path", requestedPath),
			zap.String("client_ip", r.RemoteAddr))
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Join and ensure the resolved path remains within the static root
	fullPath := filepath.Join(root, sanitizedPath)
	if rel, err := filepath.Rel(root, fullPath); err != nil || strings.HasPrefix(rel, "..") {
		h.logger.Warn("Path traversal attempt detected",
			zap.String("requested_path", requestedPath),
			zap.String("sanitized_path", sanitizedPath),
			zap.String("full_path", fullPath),
			zap.String("client_ip", r.RemoteAddr))
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Security headers and caching for static assets
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")

	http.ServeFile(w, r, fullPath)
}

// HandleStatsAPI serves the stats API endpoint
func (h *Handler) HandleStatsAPI(w http.ResponseWriter, r *http.Request) {
	// Apply security headers for API endpoints
	apiHeaders := APISecurityHeaders()
	apiHeaders.Apply(w)

	// Set headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	// Handle preflight requests
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Only allow GET requests
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get current stats
	stats := h.getStatsData()
	liveSince := h.liveSince.Format("Jan 2, 2006")

	// Create response structure
	response := struct {
		Stats     *StatsData `json:"stats"`
		LiveSince string     `json:"live_since"`
	}{
		Stats:     stats,
		LiveSince: liveSince,
	}

	// Encode and send response
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("Failed to encode stats response", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// HandleMetricsAPI serves real-time metrics for dashboard
func (h *Handler) HandleMetricsAPI(w http.ResponseWriter, r *http.Request) {
	// Apply security headers for API endpoints
	apiHeaders := APISecurityHeaders()
	apiHeaders.Apply(w)

	// Set headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	// Handle preflight requests
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Only allow GET requests
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get relay identity
	relayIdentity, err := identity.GetOrCreateRelayIdentity()
	relayID := "unknown"
	if err == nil {
		relayID = relayIdentity.RelayID
	}

	// Determine status based on health
	status := "online"
	activeConns := metrics.GetActiveConnectionsCount()
	if activeConns == 0 {
		status = "idle"
	}

	// Get current stats
	stats := h.getStatsData()
	uptime := time.Since(h.startTime)

	// Get cluster information
	clusterInfo := h.getClusterData()

	// Create comprehensive metrics response
	response := map[string]interface{}{
		"relay_id":               relayID,
		"name":                   fmt.Sprintf("SHU%s", relayID[len(relayID)-2:]), // Extract last 2 chars for name
		"status":                 status,
		"uptime_seconds":         int64(uptime.Seconds()),
		"uptime_human":           h.formatUptime(uptime),
		"active_connections":     stats.ActiveConnections,
		"total_connections":      stats.TotalConnections,
		"messages_processed":     stats.MessagesProcessed,
		"events_stored":          stats.EventsStored,
		"active_subscriptions":   stats.ActiveSubscriptions,
		"messages_sent":          stats.MessagesSent,
		"events_per_second":      stats.EventsPerSecond,
		"connections_per_second": stats.ConnectionsPerSecond,
		"average_response_time":  stats.AverageResponseTime,
		"error_rate":             stats.ErrorRate,
		"load_percentage":        stats.LoadPercentage,
		"memory_usage":           stats.MemoryUsage,
		"cluster":                clusterInfo,
		"timestamp":              time.Now().Unix(),
	}

	// Encode and send response
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("Failed to encode metrics response", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// getDashboardData prepares data for the dashboard template
func (h *Handler) getDashboardData(host string) *DashboardData {
	metadata := constants.DefaultRelayMetadata(h.config)

	// Get relay identity for the relay ID
	relayIdentity, err := identity.GetOrCreateRelayIdentity()
	relayID := "unknown"
	if err == nil {
		relayID = relayIdentity.RelayID
	}

	// Clean host (remove port if present)
	if strings.Contains(host, ":") {
		host = strings.Split(host, ":")[0]
	}

	// Get cluster information
	clusterInfo := h.getClusterData()
	eventSnapshot := h.eventCacheSnapshot()
	if eventSnapshot.updatedAt.IsZero() || time.Since(eventSnapshot.updatedAt) >= eventBreakdownCacheTTL {
		h.requestEventBreakdownRefresh()
		eventSnapshot = h.eventCacheSnapshot()
	}
	eventStatus, eventMessage := eventCacheState(eventSnapshot.updatedAt, eventSnapshot.refreshing, eventSnapshot.lastErr)

	return &DashboardData{
		Name:          metadata.Name,
		Description:   metadata.Description,
		Software:      metadata.Software,
		Version:       metadata.Version,
		Contact:       metadata.Contact,
		Icon:          metadata.Icon,
		Host:          host,
		Pubkey:        metadata.PubKey,
		RelayID:       relayID,
		SupportedNIPs: metadata.SupportedNIPs,
		CustomNIPs:    constants.DefaultCustomNIPs,
		Limitation: &LimitationData{
			MaxMessageLength: metadata.Limitation.MaxMessageLength,
			MaxSubscriptions: metadata.Limitation.MaxSubscriptions,
			MaxEventTags:     metadata.Limitation.MaxEventTags,
			MaxConnections:   h.config.Relay.ThrottlingConfig.MaxConnections,
			AuthRequired:     metadata.Limitation.AuthRequired,
			PaymentRequired:  metadata.Limitation.PaymentRequired,
		},
		Stats:               h.getStatsData(),
		TopKinds:            h.getTopEventKinds(6),
		EventKinds:          h.getTopEventKinds(65536),
		EventCacheStatus:    eventStatus,
		EventCacheUpdatedAt: formatEventCacheTime(eventSnapshot.updatedAt),
		EventCacheMessage:   eventMessage,
		LiveSince:           h.liveSince.Format("Jan 2, 2006"),
		Cluster:             clusterInfo,
	}
}

// getStatsData retrieves current statistics
func (h *Handler) getStatsData() *StatsData {
	eventsStored, eventsStoredReady := h.getStoredEventCount()
	h.eventsCacheMu.RLock()
	storedRefreshing := h.storedCountRefreshing
	storedLastErr := h.storedCountLastErr
	h.eventsCacheMu.RUnlock()
	storedUpdatedAt := metrics.GetStoredEventsCountUpdatedAt()
	storedStatus, storedMessage := storedEventState(storedUpdatedAt, storedRefreshing, storedLastErr)

	// Get memory usage
	memUsage := getMemoryUsage()

	// Calculate load percentage (based on active connections vs max)
	maxConnections := int64(1000) // Fallback default if not configured
	if h.config != nil && h.config.Relay.ThrottlingConfig.MaxConnections > 0 {
		maxConnections = int64(h.config.Relay.ThrottlingConfig.MaxConnections)
	}

	activeConns := metrics.GetActiveConnectionsCount()
	loadPercentage := float64(activeConns) / float64(maxConnections) * 100
	if loadPercentage > 100 {
		loadPercentage = 100
	}

	// Get other metrics - using our tracking functions
	stats := &StatsData{
		ActiveConnections:     activeConns,
		TotalConnections:      metrics.GetTotalConnectionsCount(),
		MessagesProcessed:     metrics.GetMessagesProcessedCount(),
		EventsStored:          eventsStored,
		EventsStoredReady:     eventsStoredReady,
		EventsStoredStatus:    storedStatus,
		EventsStoredUpdatedAt: formatEventCacheTime(storedUpdatedAt),
		EventsStoredMessage:   storedMessage,
		ActiveSubscriptions:   metrics.GetActiveSubscriptionsCount(),
		MessagesSent:          metrics.GetMessagesSentCount(),
		EventsPerSecond:       metrics.GetEventsPerSecond(),
		ConnectionsPerSecond:  metrics.GetConnectionsPerSecond(),
		AverageResponseTime:   metrics.GetAverageResponseTime(),
		ErrorRate:             metrics.GetErrorRate(),
		MemoryUsage:           memUsage,
		LoadPercentage:        loadPercentage,
	}

	return stats
}

// getTopEventKinds returns the most common event kinds from the cached breakdown.
func (h *Handler) getTopEventKinds(limit int) []EventKindSummary {
	if limit <= 0 {
		return nil
	}

	h.eventsCacheMu.RLock()
	data := h.eventsCache
	h.eventsCacheMu.RUnlock()

	counts := make(map[int]EventKindSummary)
	var total int64
	for _, year := range data.Years {
		for _, row := range year.Rows {
			item := counts[row.Kind]
			item.Kind = row.Kind
			item.KindName = row.KindName
			item.Count += row.RowTotal
			counts[row.Kind] = item
			total += row.RowTotal
		}
	}
	if total == 0 {
		return nil
	}

	items := make([]EventKindSummary, 0, len(counts))
	for _, item := range counts {
		item.Share = float64(item.Count) / float64(total) * 100
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Kind < items[j].Kind
		}
		return items[i].Count > items[j].Count
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

// getClusterData retrieves database information
func (h *Handler) getClusterData() *storage.DatabaseInfo {
	if h.db == nil {
		return &storage.DatabaseInfo{
			IsHealthy: false,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), constants.HealthCheckTimeout*time.Second)
	defer cancel()

	dbInfo, err := h.db.GetDatabaseInfo(ctx)
	if err != nil {
		h.logger.Warn("Failed to get database information", zap.Error(err))
		return &storage.DatabaseInfo{
			IsHealthy: false,
		}
	}

	return dbInfo
}

// HandleClusterAPI serves the cluster API endpoint
func (h *Handler) HandleClusterAPI(w http.ResponseWriter, r *http.Request) {
	// Apply security headers for API endpoints
	apiHeaders := APISecurityHeaders()
	apiHeaders.Apply(w)

	// Set headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	// Handle preflight requests
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Only allow GET requests
	if r.Method != "GET" {
		// Use new error handling system
		methodErr := errors.ValidationError("METHOD_NOT_ALLOWED",
			"Only GET requests are allowed for this endpoint").
			WithUserMessage("Method not allowed.")
		errors.HandleHTTPError(w, r, methodErr)
		return
	}

	if h.db == nil {
		// Use new error handling system
		dbErr := errors.InternalError("Database not available", nil).
			WithSeverity(errors.SeverityCritical).
			WithUserMessage("Database service is temporarily unavailable.")
		errors.HandleHTTPError(w, r, dbErr)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), constants.HealthCheckTimeout*time.Second)
	defer cancel()

	// Check if requesting health or full cluster info - validate query parameter
	requestType := r.URL.Query().Get("type")
	if requestType != "" {
		requestType = SanitizeQueryParam(requestType)
		// Only allow specific values
		if requestType != "health" && requestType != "info" {
			// Use new error handling system
			validationErr := errors.ValidationError("INVALID_TYPE_PARAMETER",
				"Type parameter must be 'health' or 'info'").
				WithUserMessage("Invalid type parameter. Use 'health' or 'info'.")
			errors.HandleHTTPError(w, r, validationErr)
			return
		}
	}

	if requestType == "health" {
		health, err := h.db.GetClusterHealth(ctx)
		if err != nil {
			// Use new error handling system
			dbErr := errors.HandleDatabaseError("cluster health check", err)
			errors.HandleHTTPError(w, r, dbErr)
			return
		}

		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(health); err != nil {
			// Use new error handling system
			encodeErr := errors.InternalError("Failed to encode cluster health response", err)
			errors.HandleHTTPError(w, r, encodeErr)
			return
		}
	} else {
		clusterInfo, err := h.db.GetDatabaseInfo(ctx)
		if err != nil {
			// Use new error handling system
			dbErr := errors.HandleDatabaseError("cluster info retrieval", err)
			errors.HandleHTTPError(w, r, dbErr)
			return
		}

		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(clusterInfo); err != nil {
			h.logger.Error("Failed to encode cluster info response", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}
}

// EventBreakdownData represents the data for the events breakdown page
type EventBreakdownData struct {
	Years []YearEventData `json:"years"`
}

// YearEventData represents event data for a single year
type YearEventData struct {
	Year         int          `json:"year"`
	Months       []string     `json:"months"`
	Rows         []NIPRowData `json:"rows"`
	ColumnTotals []int64      `json:"column_totals"`
	GrandTotal   int64        `json:"grand_total"`
}

// NIPRowData represents a single NIP/kind row in the table
type NIPRowData struct {
	Kind        int     `json:"kind"`
	KindName    string  `json:"kind_name"`
	MonthCounts []int64 `json:"month_counts"`
	RowTotal    int64   `json:"row_total"`
}

type eventCacheSnapshot struct {
	data       EventBreakdownData
	updatedAt  time.Time
	refreshing bool
	lastErr    error
}

func (h *Handler) eventCacheSnapshot() eventCacheSnapshot {
	h.eventsCacheMu.RLock()
	defer h.eventsCacheMu.RUnlock()
	return eventCacheSnapshot{
		data:       h.eventsCache,
		updatedAt:  h.eventsCacheUpdatedAt,
		refreshing: h.eventsCacheRefreshing,
		lastErr:    h.eventsCacheLastErr,
	}
}

func eventCacheState(updatedAt time.Time, refreshing bool, lastErr error) (string, string) {
	switch {
	case !updatedAt.IsZero() && refreshing:
		return "refreshing", "Refreshing archive telemetry in the background."
	case !updatedAt.IsZero() && lastErr != nil:
		return "stale", "Showing the last complete snapshot; refresh is retrying."
	case !updatedAt.IsZero():
		return "ready", "Archive telemetry is current."
	case refreshing:
		return "warming", "Preparing archive telemetry without blocking live relay metrics."
	case lastErr != nil:
		return "unavailable", "Archive telemetry is temporarily unavailable; retrying."
	default:
		return "pending", "Archive telemetry has not completed its first refresh."
	}
}

func eventTotalState(updatedAt time.Time, refreshing bool, lastErr error) (string, string) {
	switch {
	case !updatedAt.IsZero() && refreshing:
		return "refreshing", "Refreshing the 2026+ total in the background."
	case !updatedAt.IsZero() && lastErr != nil:
		return "stale", "Showing the last known total; refresh is retrying."
	case !updatedAt.IsZero():
		return "ready", "2026+ total is current."
	case refreshing:
		return "warming", "Reading the 2026+ total without blocking relay metrics."
	case lastErr != nil:
		return "unavailable", "Event total temporarily unavailable; retrying."
	default:
		return "pending", "Event total has not completed its first refresh."
	}
}

func formatEventCacheTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

// storedEventState describes the primary all-events count used by the dashboard.
func storedEventState(updatedAt time.Time, refreshing bool, lastErr error) (string, string) {
	switch {
	case !updatedAt.IsZero() && refreshing:
		return "refreshing", "Refreshing the database-backed total in the background."
	case !updatedAt.IsZero() && lastErr != nil:
		return "stale", "Showing the last confirmed total; an exact refresh is retrying."
	case !updatedAt.IsZero():
		return "ready", "Database-backed total · live inserts included."
	case refreshing:
		return "warming", "Confirming the stored-event total without blocking relay traffic."
	case lastErr != nil:
		return "unavailable", "The database total could not be confirmed; retrying."
	default:
		return "pending", "The stored-event total has not been confirmed yet."
	}
}

// getStoredEventCount returns the all-events total seeded during startup and maintained on successful inserts.
// Exact refreshes run asynchronously so dashboard requests never wait on COUNT(*).
func (h *Handler) getStoredEventCount() (int64, bool) {
	count := metrics.GetStoredEventsCount()
	updatedAt := metrics.GetStoredEventsCountUpdatedAt()
	ready := metrics.StoredEventsCountReady()

	h.eventsCacheMu.RLock()
	refreshing := h.storedCountRefreshing
	lastAttemptAt := h.storedCountLastAttemptAt
	h.eventsCacheMu.RUnlock()

	if (!ready || updatedAt.IsZero() || time.Since(updatedAt) >= storedCountRefreshInterval) &&
		(!refreshing && (lastAttemptAt.IsZero() || time.Since(lastAttemptAt) >= eventRefreshRetryDelay)) {
		h.requestStoredEventCountRefresh()
	}
	return count, ready
}

func (h *Handler) requestStoredEventCountRefresh() {
	if h.db == nil {
		return
	}

	h.eventsCacheMu.Lock()
	if h.storedCountRefreshing || (!h.storedCountLastAttemptAt.IsZero() && time.Since(h.storedCountLastAttemptAt) < eventRefreshRetryDelay) {
		h.eventsCacheMu.Unlock()
		return
	}
	h.storedCountRefreshing = true
	h.storedCountLastAttemptAt = time.Now()
	h.eventsCacheMu.Unlock()

	go h.refreshStoredEventCount()
}

func (h *Handler) refreshStoredEventCount() {
	ctx, cancel := context.WithTimeout(context.Background(), eventQueryTimeout)
	defer cancel()

	count, err := h.db.GetTotalEventCount(ctx)
	h.eventsCacheMu.Lock()
	h.storedCountRefreshing = false
	if err != nil {
		h.storedCountLastErr = err
		h.eventsCacheMu.Unlock()
		h.logger.Warn("Failed to refresh stored event total", zap.Error(err))
		return
	}
	h.storedCountLastErr = nil
	h.eventsCacheMu.Unlock()
	metrics.SetStoredEventsCount(count)
}

// getCachedEventCount returns a fast 2026+ total without waiting for the grouped archive query.
func (h *Handler) getCachedEventCount() (int64, bool) {
	h.eventsCacheMu.RLock()
	count := h.eventsTotal
	ready := !h.eventsTotalUpdatedAt.IsZero()
	updatedAt := h.eventsTotalUpdatedAt
	refreshing := h.eventsTotalRefreshing
	lastAttemptAt := h.eventsTotalLastAttemptAt
	h.eventsCacheMu.RUnlock()

	if (!ready || time.Since(updatedAt) >= eventBreakdownCacheTTL) &&
		(!refreshing && (lastAttemptAt.IsZero() || time.Since(lastAttemptAt) >= eventRefreshRetryDelay)) {
		h.requestEventTotalRefresh()
	}
	return count, ready
}

func (h *Handler) eventsCacheGrandTotalLocked() int64 {
	var total int64
	for _, year := range h.eventsCache.Years {
		total += year.GrandTotal
	}
	return total
}

// requestEventTotalRefresh starts the cheap direct-count refresh independently from the grouped archive query.
func (h *Handler) requestEventTotalRefresh() {
	if h.db == nil {
		return
	}

	h.eventsCacheMu.Lock()
	if h.eventsTotalRefreshing || (!h.eventsTotalLastAttemptAt.IsZero() && time.Since(h.eventsTotalLastAttemptAt) < eventRefreshRetryDelay) {
		h.eventsCacheMu.Unlock()
		return
	}
	h.eventsTotalRefreshing = true
	h.eventsTotalLastAttemptAt = time.Now()
	h.eventsCacheMu.Unlock()

	go h.refreshEventTotal()
}

func (h *Handler) refreshEventTotal() {
	ctx, cancel := context.WithTimeout(context.Background(), eventQueryTimeout)
	defer cancel()

	count, err := h.db.GetTotalEventCount2026Plus(ctx)
	h.eventsCacheMu.Lock()
	h.eventsTotalRefreshing = false
	if err != nil {
		h.eventsTotalLastErr = err
		h.eventsCacheMu.Unlock()
		h.logger.Warn("Failed to refresh direct event total", zap.Error(err))
		return
	}
	h.eventsTotal = count
	h.eventsTotalUpdatedAt = time.Now()
	h.eventsTotalLastErr = nil
	h.eventsCacheMu.Unlock()
}

// requestEventBreakdownRefresh starts at most one background refresh at a time.
func (h *Handler) requestEventBreakdownRefresh() {
	if h.db == nil {
		return
	}

	h.eventsCacheMu.Lock()
	if h.eventsCacheRefreshing || (!h.eventsCacheLastAttemptAt.IsZero() && time.Since(h.eventsCacheLastAttemptAt) < eventRefreshRetryDelay) {
		h.eventsCacheMu.Unlock()
		return
	}
	h.eventsCacheRefreshing = true
	h.eventsCacheLastAttemptAt = time.Now()
	h.eventsCacheMu.Unlock()

	go h.refreshEventBreakdown()
}

func (h *Handler) refreshEventBreakdown() {
	ctx, cancel := context.WithTimeout(context.Background(), eventQueryTimeout)
	defer cancel()

	counts, err := h.db.GetEventCountsByKindMonthFromYear(ctx, 2026)
	if err != nil {
		h.logger.Warn("Failed to refresh event breakdown cache", zap.Error(err))
		h.eventsCacheMu.Lock()
		h.eventsCacheRefreshing = false
		h.eventsCacheLastErr = err
		h.eventsCacheMu.Unlock()
		return
	}

	data := buildEventBreakdownData(counts)
	h.eventsCacheMu.Lock()
	h.eventsCache = data
	h.eventsCacheUpdatedAt = time.Now()
	h.eventsCacheRefreshing = false
	h.eventsCacheLastErr = nil
	h.eventsCacheMu.Unlock()

}

func eventBreakdownGrandTotal(data EventBreakdownData) int64 {
	var total int64
	for _, year := range data.Years {
		total += year.GrandTotal
	}
	return total
}

func buildEventBreakdownData(counts []storage.EventCountByKindMonth) EventBreakdownData {
	months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	byYear := make(map[int]map[int]*NIPRowData)

	for _, count := range counts {
		if count.Year < 2026 || count.Month < 1 || count.Month > 12 {
			continue
		}
		if _, ok := byYear[count.Year]; !ok {
			byYear[count.Year] = make(map[int]*NIPRowData)
		}
		if _, ok := byYear[count.Year][count.Kind]; !ok {
			byYear[count.Year][count.Kind] = &NIPRowData{
				Kind:        count.Kind,
				KindName:    count.KindName,
				MonthCounts: make([]int64, 12),
			}
		}
		byYear[count.Year][count.Kind].MonthCounts[count.Month-1] = count.Count
	}

	years := make([]int, 0, len(byYear))
	for year := range byYear {
		years = append(years, year)
	}
	sort.Ints(years)

	data := EventBreakdownData{Years: make([]YearEventData, 0, len(years))}
	for _, year := range years {
		rows := make([]NIPRowData, 0, len(byYear[year]))
		for _, row := range byYear[year] {
			for _, count := range row.MonthCounts {
				row.RowTotal += count
			}
			rows = append(rows, *row)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Kind < rows[j].Kind })

		columnTotals := make([]int64, 12)
		var grandTotal int64
		for _, row := range rows {
			for month := range row.MonthCounts {
				columnTotals[month] += row.MonthCounts[month]
			}
			grandTotal += row.RowTotal
		}
		data.Years = append(data.Years, YearEventData{
			Year:         year,
			Months:       months,
			Rows:         rows,
			ColumnTotals: columnTotals,
			GrandTotal:   grandTotal,
		})
	}
	return data
}

// HandleEvents serves the events breakdown page.
func (h *Handler) HandleEvents(w http.ResponseWriter, r *http.Request) {
	dashboardHeaders := DefaultSecurityHeaders()
	dashboardHeaders.Apply(w)

	if h.db == nil {
		http.Error(w, "Database not available", http.StatusServiceUnavailable)
		return
	}

	storedCount, storedReady := h.getStoredEventCount()
	storedUpdatedAt := metrics.GetStoredEventsCountUpdatedAt()
	h.eventsCacheMu.RLock()
	storedRefreshing := h.storedCountRefreshing
	storedLastErr := h.storedCountLastErr
	h.eventsCacheMu.RUnlock()
	storedStatus, storedMessage := storedEventState(storedUpdatedAt, storedRefreshing, storedLastErr)

	_, _ = h.getCachedEventCount()
	snapshot := h.eventCacheSnapshot()
	if snapshot.updatedAt.IsZero() || time.Since(snapshot.updatedAt) >= eventBreakdownCacheTTL {
		h.requestEventBreakdownRefresh()
		snapshot = h.eventCacheSnapshot()
	}
	eventStatus, eventMessage := eventCacheState(snapshot.updatedAt, snapshot.refreshing, snapshot.lastErr)

	h.eventsCacheMu.RLock()
	total := h.eventsTotal
	totalReady := !h.eventsTotalUpdatedAt.IsZero()
	totalUpdatedAt := h.eventsTotalUpdatedAt
	totalRefreshing := h.eventsTotalRefreshing
	totalLastErr := h.eventsTotalLastErr
	h.eventsCacheMu.RUnlock()
	totalStatus, totalMessage := eventTotalState(totalUpdatedAt, totalRefreshing, totalLastErr)

	tmplPath := filepath.Join("web", "templates", "events.html")
	funcMap := template.FuncMap{
		"formatNIP": func(v interface{}) string {
			switch val := v.(type) {
			case int:
				return fmt.Sprintf("%02d", val)
			case string:
				return val
			default:
				return fmt.Sprintf("%v", val)
			}
		},
	}

	tmpl, err := template.New("events.html").Funcs(funcMap).ParseFiles(tmplPath)
	if err != nil {
		h.logger.Error("Failed to parse events template", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	lastUpdated := formatEventCacheTime(snapshot.updatedAt)
	pageData := struct {
		EventBreakdownData
		Loading            bool
		LastUpdated        string
		CacheStatus        string
		CacheMessage       string
		EventTotal         int64
		EventTotalReady    bool
		EventTotalStatus   string
		EventTotalMessage  string
		StoredEventCount   int64
		StoredEventReady   bool
		StoredEventStatus  string
		StoredEventMessage string
	}{
		EventBreakdownData: snapshot.data,
		Loading:            snapshot.updatedAt.IsZero(),
		LastUpdated:        lastUpdated,
		CacheStatus:        eventStatus,
		CacheMessage:       eventMessage,
		EventTotal:         total,
		EventTotalReady:    totalReady,
		EventTotalStatus:   totalStatus,
		EventTotalMessage:  totalMessage,
		StoredEventCount:   storedCount,
		StoredEventReady:   storedReady,
		StoredEventStatus:  storedStatus,
		StoredEventMessage: storedMessage,
	}
	if err := tmpl.Execute(w, pageData); err != nil {
		h.logger.Error("Failed to execute events template", zap.Error(err))
	}
}

// HandleEventsAPI returns cached event telemetry and starts refreshes without blocking requests.
func (h *Handler) HandleEventsAPI(w http.ResponseWriter, r *http.Request) {
	apiHeaders := APISecurityHeaders()
	apiHeaders.Apply(w)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.db == nil {
		http.Error(w, "Database not available", http.StatusServiceUnavailable)
		return
	}

	storedCount, storedReady := h.getStoredEventCount()
	storedUpdatedAt := metrics.GetStoredEventsCountUpdatedAt()
	h.eventsCacheMu.RLock()
	storedRefreshing := h.storedCountRefreshing
	storedLastErr := h.storedCountLastErr
	h.eventsCacheMu.RUnlock()
	storedStatus, storedMessage := storedEventState(storedUpdatedAt, storedRefreshing, storedLastErr)

	_, _ = h.getCachedEventCount()
	snapshot := h.eventCacheSnapshot()
	if snapshot.updatedAt.IsZero() || time.Since(snapshot.updatedAt) >= eventBreakdownCacheTTL {
		h.requestEventBreakdownRefresh()
		snapshot = h.eventCacheSnapshot()
	}

	h.eventsCacheMu.RLock()
	totalEvents := h.eventsTotal
	totalReady := !h.eventsTotalUpdatedAt.IsZero()
	totalUpdatedAt := h.eventsTotalUpdatedAt
	totalRefreshing := h.eventsTotalRefreshing
	totalLastErr := h.eventsTotalLastErr
	h.eventsCacheMu.RUnlock()

	status, message := eventCacheState(snapshot.updatedAt, snapshot.refreshing, snapshot.lastErr)
	totalStatus, totalMessage := eventTotalState(totalUpdatedAt, totalRefreshing, totalLastErr)
	response := struct {
		Status              string             `json:"status"`
		Refreshing          bool               `json:"refreshing"`
		UpdatedAt           string             `json:"updated_at,omitempty"`
		Message             string             `json:"message,omitempty"`
		Error               string             `json:"error,omitempty"`
		TotalEvents         int64              `json:"total_events"`
		TotalReady          bool               `json:"total_ready"`
		TotalStatus         string             `json:"total_status"`
		TotalUpdatedAt      string             `json:"total_updated_at,omitempty"`
		TotalMessage        string             `json:"total_message,omitempty"`
		StoredEvents        int64              `json:"stored_events"`
		StoredEventsReady   bool               `json:"stored_events_ready"`
		StoredEventsStatus  string             `json:"stored_events_status"`
		StoredEventsUpdated string             `json:"stored_events_updated_at,omitempty"`
		StoredEventsMessage string             `json:"stored_events_message,omitempty"`
		Data                EventBreakdownData `json:"data"`
		TopKinds            []EventKindSummary `json:"top_kinds"`
		EventKinds          []EventKindSummary `json:"event_kinds"`
	}{
		Status:              status,
		Refreshing:          snapshot.refreshing,
		UpdatedAt:           formatEventCacheTime(snapshot.updatedAt),
		Message:             message,
		TotalEvents:         totalEvents,
		TotalReady:          totalReady,
		TotalStatus:         totalStatus,
		TotalUpdatedAt:      formatEventCacheTime(totalUpdatedAt),
		TotalMessage:        totalMessage,
		StoredEvents:        storedCount,
		StoredEventsReady:   storedReady,
		StoredEventsStatus:  storedStatus,
		StoredEventsUpdated: formatEventCacheTime(storedUpdatedAt),
		StoredEventsMessage: storedMessage,
		Data:                snapshot.data,
		TopKinds:            h.getTopEventKinds(6),
		EventKinds:          h.getTopEventKinds(65536),
	}
	if snapshot.lastErr != nil {
		response.Error = "The grouped archive refresh has not completed."
	}
	if snapshot.updatedAt.IsZero() {
		w.WriteHeader(http.StatusAccepted)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("Failed to encode events response", zap.Error(err))
	}
}

// formatUptime formats duration as a human-readable string
func (h *Handler) formatUptime(duration time.Duration) string {
	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	} else {
		return fmt.Sprintf("%dm", minutes)
	}
}

// loadFirstBootTime reads or creates the .first_boot timestamp file
func loadFirstBootTime() time.Time {
	const path = ".first_boot"
	data, err := os.ReadFile(path)
	if err == nil {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data))); err == nil {
			return t
		}
	}
	now := time.Now().UTC()
	_ = os.WriteFile(path, []byte(now.Format(time.RFC3339)), 0644)
	return now
}

// getMemoryUsage returns current memory usage statistics
func getMemoryUsage() map[string]int64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Safe conversion function to prevent integer overflow
	safeUint64ToInt64 := func(val uint64) int64 {
		if val > 9223372036854775807 { // math.MaxInt64
			return 9223372036854775807
		}
		return int64(val)
	}

	return map[string]int64{
		"alloc":           safeUint64ToInt64(m.Alloc),       // Currently allocated bytes
		"total_alloc":     safeUint64ToInt64(m.TotalAlloc),  // Total allocated bytes (cumulative)
		"sys":             safeUint64ToInt64(m.Sys),         // System memory obtained from OS
		"heap_alloc":      safeUint64ToInt64(m.HeapAlloc),   // Heap allocated bytes
		"heap_sys":        safeUint64ToInt64(m.HeapSys),     // Heap system bytes
		"heap_idle":       safeUint64ToInt64(m.HeapIdle),    // Heap idle bytes
		"heap_inuse":      safeUint64ToInt64(m.HeapInuse),   // Heap in-use bytes
		"heap_objects":    safeUint64ToInt64(m.HeapObjects), // Number of allocated heap objects
		"stack_inuse":     safeUint64ToInt64(m.StackInuse),  // Stack in-use bytes
		"stack_sys":       safeUint64ToInt64(m.StackSys),    // Stack system bytes
		"num_gc":          int64(m.NumGC),                   // Number of GC cycles (uint32 -> int64 is safe)
		"gc_cpu_fraction": int64(m.GCCPUFraction * 1000000), // GC CPU fraction (scaled)
	}
}
