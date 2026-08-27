package web

import (
	"html/template"
	"math"
	"path/filepath"
	"testing"

	"github.com/Shugur-Network/relay/internal/constants"
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
