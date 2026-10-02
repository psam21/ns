package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestServeHandlerExposesPrometheusAndPprof pins the contract of the metrics
// server, which previously did not exist at all.
//
// METRICS_ENABLED and METRICS_PORT were parsed, validated and defaulted in
// every config file (deploy/config.yaml sets ENABLED: true, PORT: 2112), but
// nothing in the tree ever read them to start a server. Observed on the
// production host 2026-10-02: port 2112 was closed, and
// `curl localhost:2112/metrics` returned nothing. So `metrics.go`'s whole
// Prometheus surface — the gauges the dashboard reads — was unreachable in
// production.
//
// The pprof handlers matter just as much. Without net/http/pprof there is no
// way to answer "what is holding 700MB of heap" on a running server: no heap
// profile, no goroutine dump, no block profile. That is precisely the
// question that could not be answered during the 2026-10-02 outage, and the
// reason the memory growth there is still unexplained.
func TestServeHandlerExposesPrometheusAndPprof(t *testing.T) {
	srv := newMetricsServer("127.0.0.1", 0)

	for _, tc := range []struct {
		path string
		// wantContentType is checked only where a binary body is expected.
		// pprof's default (non-debug) responses are gzipped protobuf, so
		// asserting on body text fails even when the handler is correct.
		wantContentType string
		wantBodyHas     string
		desc            string
	}{
		{path: "/metrics", wantBodyHas: "go_goroutines", desc: "Prometheus scrape endpoint"},
		{path: "/debug/pprof/", wantBodyHas: "goroutine", desc: "pprof index"},
		{path: "/debug/pprof/heap", wantContentType: "application/octet-stream", desc: "heap profile (binary)"},
		{path: "/debug/pprof/allocs", wantContentType: "application/octet-stream", desc: "allocs profile (binary)"},
		{path: "/debug/pprof/block", wantContentType: "application/octet-stream", desc: "block profile (binary)"},
		{path: "/debug/pprof/mutex", wantContentType: "application/octet-stream", desc: "mutex profile (binary)"},
		{path: "/debug/pprof/goroutine?debug=1", wantBodyHas: "goroutine", desc: "goroutine dump (text)"},
		{path: "/debug/pprof/heap?debug=1", wantBodyHas: "", desc: "heap profile (text)"},
		{path: "/debug/pprof/cmdline", desc: "cmdline"},
		{path: "/debug/pprof/symbol", desc: "symbol"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200", tc.path, rec.Code)
			}
			if rec.Body.Len() == 0 {
				t.Errorf("%s: empty body", tc.path)
			}
			if tc.wantContentType != "" {
				if got := rec.Header().Get("Content-Type"); !strings.Contains(got, tc.wantContentType) {
					t.Errorf("%s: Content-Type = %q, want it to contain %q",
						tc.path, got, tc.wantContentType)
				}
			}
			if tc.wantBodyHas != "" && !strings.Contains(rec.Body.String(), tc.wantBodyHas) {
				t.Errorf("%s: body does not contain %q (got %d bytes)",
					tc.path, tc.wantBodyHas, rec.Body.Len())
			}
		})
	}
}

// TestMetricsServerDoesNotBlockStartup verifies the server is started in the
// background. A metrics listener that can hang startup would reintroduce the
// exact failure mode the relay had on 2026-10-02, where a long synchronous
// start-up task (the bloom rebuild) delayed the listener by 56 seconds.
func TestMetricsServerDoesNotBlockStartup(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		StartMetricsServer("127.0.0.1", 0)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("StartMetricsServer blocked; it must return immediately")
	}
}
