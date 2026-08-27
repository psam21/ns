package web

import (
	"testing"

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
