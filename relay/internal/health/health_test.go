package health

import (
	"testing"
	"time"
)

func TestDetermineOverallStatus(t *testing.T) {
	h := &HealthChecker{}
	if got := h.determineOverallStatus([]*ComponentStatus{{Status: StatusHealthy}}); got != StatusHealthy {
		t.Fatalf("healthy status = %s", got)
	}
	if got := h.determineOverallStatus([]*ComponentStatus{{Status: StatusHealthy}, {Status: StatusDegraded}}); got != StatusDegraded {
		t.Fatalf("degraded status = %s", got)
	}
	if got := h.determineOverallStatus([]*ComponentStatus{{Status: StatusDegraded}, {Status: StatusUnhealthy}}); got != StatusUnhealthy {
		t.Fatalf("unhealthy status = %s", got)
	}
}

func TestCountComponentsByStatus(t *testing.T) {
	h := &HealthChecker{}
	components := []*ComponentStatus{{Status: StatusHealthy}, {Status: StatusHealthy}, {Status: StatusDegraded}}
	if got := h.countComponentsByStatus(components, StatusHealthy); got != 2 {
		t.Fatalf("healthy count = %d, want 2", got)
	}
	if got := h.countComponentsByStatus(components, StatusUnhealthy); got != 0 {
		t.Fatalf("unhealthy count = %d, want 0", got)
	}
}

func TestFormatUptime(t *testing.T) {
	h := &HealthChecker{}
	cases := []struct {
		input time.Duration
		want  string
	}{
		{5 * time.Second, "5s"},
		{2*time.Minute + 3*time.Second, "2m 3s"},
		{time.Hour + 2*time.Minute + 3*time.Second, "1h 2m 3s"},
		{time.Hour*25 + 2*time.Minute + 3*time.Second, "1d 1h 2m 3s"},
	}
	for _, tc := range cases {
		if got := h.formatUptime(tc.input); got != tc.want {
			t.Errorf("formatUptime(%s) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
