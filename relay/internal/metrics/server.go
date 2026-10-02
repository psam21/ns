package metrics

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/pprof"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/Shugur-Network/relay/internal/logger"
)

var log = logger.New("metrics")

// metricsServerTimeout bounds header reads and writes. A profiling endpoint
// that can be held open indefinitely is itself a small DoS surface, so the
// whole server gets a WriteTimeout.
//
// This is deliberately NOT zero: a zero timeout in Go means no deadline at
// all, which is the failure mode that wedged the relay on 2026-10-02.
const metricsServerTimeout = 15 * time.Second

// newMetricsServer builds the handler serving both the Prometheus scrape
// endpoint and the net/http/pprof profiling endpoints.
//
// pprof is included deliberately. On 2026-10-02 the relay consumed 780MB RSS
// plus 951MB swap and the cause could not be identified, because the running
// process exposed no way to take a heap profile — only aggregate MemStats
// counters. With /debug/pprof/heap the same question is answerable in one
// request against a live server, without a redeploy.
//
// The listener is separate from the relay's own port on purpose: pprof exposes
// goroutine stacks and heap contents, which is sensitive. Keeping it on a
// distinct port that is not published by Caddy means it is reachable only
// from the host itself.
func newMetricsServer(host string, port int) *http.Server {
	mux := http.NewServeMux()

	mux.Handle("/metrics", promhttp.Handler())

	// Registered explicitly rather than via net/http/pprof's init side effect,
	// so the routes are visible in this file and cannot be lost by dropping
	// the blank import.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	mux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	mux.Handle("/debug/pprof/block", pprof.Handler("block"))
	mux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
	mux.Handle("/debug/pprof/allocs", pprof.Handler("allocs"))
	mux.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))

	return &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(port)),
		Handler:           mux,
		ReadHeaderTimeout: metricsServerTimeout,
		WriteTimeout:      metricsServerTimeout,
		IdleTimeout:       30 * time.Second,
	}
}

// StartMetricsServer starts the metrics and profiling listener in the
// background and returns immediately.
//
// It never blocks and never returns an error: a failure to bind the metrics
// port must not prevent the relay from serving traffic. The relay's own
// listener on :8080 is what matters, and the 2026-10-02 outage showed what
// happens when a secondary task is allowed to delay it.
//
// Bind errors are logged rather than propagated for the same reason. A relay
// without metrics is degraded; a relay that does not start is an outage.
func StartMetricsServer(host string, port int) {
	if host == "" {
		host = "127.0.0.1"
	}
	srv := newMetricsServer(host, port)

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		log.Warn("Metrics server could not bind; relay will continue without it",
			zap.String("addr", srv.Addr),
			zap.Error(err))
		return
	}

	metricsMu.Lock()
	metricsSrv = srv
	metricsMu.Unlock()

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Warn("Metrics server stopped", zap.Error(err))
		}
	}()

	log.Info("Metrics and pprof server listening",
		zap.String("addr", srv.Addr),
		zap.String("metrics_path", "/metrics"),
		zap.String("pprof_path", "/debug/pprof/"))
}

// StopMetricsServer shuts the metrics listener down gracefully. Exposed for
// tests and for a future clean shutdown path.
func StopMetricsServer(ctx context.Context) error {
	metricsMu.Lock()
	srv := metricsSrv
	metricsMu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// metricsSrv is retained so StopMetricsServer has something to stop.
var (
	metricsMu  sync.Mutex
	metricsSrv *http.Server
)
