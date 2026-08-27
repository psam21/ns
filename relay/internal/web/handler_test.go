package web

import (
	"encoding/json"
	"errors"
	"html/template"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Shugur-Network/relay/internal/constants"
	"github.com/Shugur-Network/relay/internal/metrics"
	"github.com/Shugur-Network/relay/internal/storage"
)

func TestBuildEventBreakdownData(t *testing.T) {
	counts := []storage.EventCountByKindMonth{
		{Year: 2025, Month: 12, Kind: 1, Count: 100, KindName: "Basic Protocol"},
		{Year: 2026, Month: 1, Kind: 1, Count: 2, KindName: "Basic Protocol"},
		{Year: 2026, Month: 2, Kind: 1, Count: 3, KindName: "Basic Protocol"},
		{Year: 2026, Month: 1, Kind: 5, Count: 4, KindName: "DNS Identifiers"},
		{Year: 2027, Month: 1, Kind: 1, Count: 6, KindName: "Basic Protocol"},
	}

	data := buildEventBreakdownData(counts)
	if len(data.Years) != 2 {
		t.Fatalf("expected two years from 2026 onward, got %d", len(data.Years))
	}
	if data.Years[0].Year != 2026 || data.Years[1].Year != 2027 {
		t.Fatalf("years are not sorted or filtered correctly: %#v", data.Years)
	}
	if data.Years[0].GrandTotal != 9 {
		t.Fatalf("expected 2026 grand total 9, got %d", data.Years[0].GrandTotal)
	}
	if data.Years[0].ColumnTotals[0] != 6 || data.Years[0].ColumnTotals[1] != 3 {
		t.Fatalf("unexpected 2026 column totals: %#v", data.Years[0].ColumnTotals)
	}
	if len(data.Years[0].Rows) != 2 {
		t.Fatalf("expected two 2026 rows, got %d", len(data.Years[0].Rows))
	}
	if data.Years[0].Rows[0].Kind != 1 || data.Years[0].Rows[0].RowTotal != 5 {
		t.Fatalf("unexpected first row: %#v", data.Years[0].Rows[0])
	}
	if data.Years[1].GrandTotal != 6 {
		t.Fatalf("expected 2027 grand total 6, got %d", data.Years[1].GrandTotal)
	}
}

func TestSupportedNIPRegistryCount(t *testing.T) {
	if got := len(constants.DefaultSupportedNIPs); got != 77 {
		t.Fatalf("expected 77 supported NIPs, got %d", got)
	}
}

func TestDashboardTemplateParses(t *testing.T) {
	path := filepath.Join("..", "..", "web", "templates", "index.html")
	_, err := template.New("index.html").Funcs(template.FuncMap{
		"formatNIP":      func(value interface{}) string { return "01" },
		"nipDescription": func(value interface{}) string { return "Protocol" },
	}).ParseFiles(path)
	if err != nil {
		t.Fatalf("dashboard template should parse: %v", err)
	}
}

func TestGetTopEventKinds(t *testing.T) {
	h := &Handler{eventsCache: EventBreakdownData{Years: []YearEventData{{
		Year: 2026,
		Rows: []NIPRowData{
			{Kind: 1, KindName: "Basic Protocol", RowTotal: 75},
			{Kind: 7, KindName: "Browser Extension", RowTotal: 25},
			{Kind: 42, KindName: "Authentication", RowTotal: 10},
		},
	}}}}

	items := h.getTopEventKinds(2)
	if len(items) != 2 {
		t.Fatalf("expected two top kinds, got %d", len(items))
	}
	if items[0].Kind != 1 || items[0].Count != 75 {
		t.Fatalf("unexpected first top kind: %#v", items[0])
	}
	if items[1].Kind != 7 || items[1].Count != 25 {
		t.Fatalf("unexpected second top kind: %#v", items[1])
	}
	if math.Abs(items[0].Share-68.18181818181819) > 0.000001 || math.Abs(items[1].Share-22.727272727272727) > 0.000001 {
		t.Fatalf("unexpected shares: %#v", items)
	}
}

func TestEventCacheState(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		updatedAt  time.Time
		refreshing bool
		lastErr    error
		wantStatus string
	}{
		{name: "pending", wantStatus: "pending"},
		{name: "warming", refreshing: true, wantStatus: "warming"},
		{name: "unavailable", lastErr: errors.New("database unavailable"), wantStatus: "unavailable"},
		{name: "ready", updatedAt: now, wantStatus: "ready"},
		{name: "refreshing", updatedAt: now, refreshing: true, wantStatus: "refreshing"},
		{name: "stale", updatedAt: now, lastErr: errors.New("query timeout"), wantStatus: "stale"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, message := eventCacheState(tt.updatedAt, tt.refreshing, tt.lastErr)
			if status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", status, tt.wantStatus)
			}
			if message == "" {
				t.Fatal("expected a non-empty operator message")
			}
		})
	}
}

func TestFormatEventCacheTime(t *testing.T) {
	if got := formatEventCacheTime(time.Time{}); got != "" {
		t.Fatalf("zero timestamp = %q, want empty string", got)
	}
	value := time.Date(2026, time.August, 27, 7, 0, 0, 0, time.FixedZone("IST", 19800))
	if got := formatEventCacheTime(value); got != "2026-08-27T01:30:00Z" {
		t.Fatalf("formatted timestamp = %q", got)
	}
}

func TestEventTotalState(t *testing.T) {
	status, message := eventTotalState(time.Time{}, false, errors.New("database unavailable"))
	if status != "unavailable" || message == "" {
		t.Fatalf("unexpected unavailable total state: %q %q", status, message)
	}
	status, _ = eventTotalState(time.Now(), true, nil)
	if status != "refreshing" {
		t.Fatalf("unexpected refreshing total state: %q", status)
	}
}

func TestStatsUsesStartupStoredEventMetric(t *testing.T) {
	metrics.SetStoredEventsCount(123)

	h := &Handler{}
	stats := h.getStatsData()
	if stats.EventsStored != 123 {
		t.Fatalf("events stored = %d, want startup-seeded count 123", stats.EventsStored)
	}
	if !stats.EventsStoredReady {
		t.Fatal("expected startup-seeded stored-event count to be ready")
	}
	if stats.EventsStoredStatus != "ready" {
		t.Fatalf("events stored status = %q, want ready", stats.EventsStoredStatus)
	}
}

func TestEventsAPIIncludesDirectTotalWhileBreakdownWarms(t *testing.T) {
	metrics.SetStoredEventsCount(42)
	now := time.Now()
	h := &Handler{
		db:                    &storage.DB{},
		eventsCacheRefreshing: true,
		eventsTotal:           42,
		eventsTotalUpdatedAt:  now,
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	h.HandleEventsAPI(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status code = %d, want %d while breakdown warms", recorder.Code, http.StatusAccepted)
	}
	var response struct {
		Status       string `json:"status"`
		TotalEvents  int64  `json:"total_events"`
		TotalReady   bool   `json:"total_ready"`
		StoredEvents int64  `json:"stored_events"`
		StoredReady  bool   `json:"stored_events_ready"`
		StoredStatus string `json:"stored_events_status"`
		Message      string `json:"message"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != "warming" {
		t.Fatalf("status = %q, want warming", response.Status)
	}
	if !response.TotalReady || response.TotalEvents != 42 {
		t.Fatalf("direct total = %d ready=%v, want 42 and ready", response.TotalEvents, response.TotalReady)
	}
	if !response.StoredReady || response.StoredEvents != 42 || response.StoredStatus != "ready" {
		t.Fatalf("stored total = %d ready=%v status=%q, want 42, ready, ready", response.StoredEvents, response.StoredReady, response.StoredStatus)
	}
	if response.Message == "" {
		t.Fatal("expected a non-empty warming message")
	}
}
