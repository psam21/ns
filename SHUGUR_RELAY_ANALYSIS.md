# Shugur Relay — Deep Code Analysis & Implementation Plan

**Date:** 2026-02-10  
**Repo:** `https://github.com/Shugur-Network/relay`  
**Version:** 1.3.5  
**Language:** Go 1.24.4+ · ~20,600 lines of Go  

---

## 🧾 1. Repo Summary

### What This Repo Is

Shugur Relay is a **production-grade Nostr relay server** written in Go. [Nostr](https://nostr.com) (Notes and Other Stuff Transmitted by Relays) is an open protocol for censorship-resistant social networking. Relays are the backbone infrastructure: they accept, store, and forward cryptographically signed events between clients.

### What Problem It Solves

It provides a **horizontally-scalable, CockroachDB-backed** Nostr relay that can:

- Accept WebSocket connections from Nostr clients (e.g., Damus, Amethyst, Primal)
- Validate and persist events using cryptographic signature verification (BIP-340 Schnorr)
- Serve historical and real-time events back to subscribers via NIP-01 subscription filters
- Support **35+ NIPs** (Nostr Improvement Proposals) including DMs, Lightning Zaps, Cashu Wallets, Moderated Communities, Wikis, Calendars, Live Activities, Badges, and custom "Time Capsules"
- Operate in **standalone** (single node + single CockroachDB) or **distributed** (multi-node cluster with CockroachDB replication + Caddy reverse proxy) mode
- Provide a built-in **web dashboard**, health checks, Prometheus metrics, and rate limiting

### Important Files and Modules

| Path | Purpose |
|------|---------|
| `cmd/main.go` | Entry point — signal handling, context setup |
| `cmd/root.go` | Cobra CLI — defines `relay start`, flag parsing, config loading, app bootstrap |
| `internal/application/node.go` | `Node` struct — ties together DB, workers, validators, connections |
| `internal/application/node_builder.go` | Builder pattern for constructing `Node` (DB, validators, processor, rate limiter) |
| `internal/config/config.go` | Config loading: embedded defaults.yaml → user file → env vars (Viper) |
| `internal/config/defaults.yaml` | All default configuration values |
| `internal/relay/server.go` | HTTP/WebSocket server — routes, NIP-11, dashboard, health, metrics |
| `internal/relay/connection.go` | `WsConnection` — per-client WebSocket lifecycle, message loop, rate limiting |
| `internal/relay/subscription.go` | REQ/CLOSE handling, subscription management, event delivery |
| `internal/relay/plugin_validator.go` | Event validation: ID, pubkey, signature, kind, tags, timestamps |
| `internal/relay/nips/` | 36 NIP implementation files (~9,950 lines) — validation logic per NIP |
| `internal/storage/db.go` | CockroachDB connection pooling, Bloom filter, retry logic |
| `internal/storage/schema.sql` | DDL — `events` table with optimized indexes and constraints |
| `internal/storage/queries.go` | CRUD operations — insert, batch insert, replaceable/addressable upserts |
| `internal/storage/filter.go` | `CompiledFilter` — translates Nostr filters to SQL queries |
| `internal/storage/event_processor.go` | Async event processing with worker goroutines |
| `internal/storage/changefeed.go` | `EventDispatcher` — real-time event broadcasting + cross-node polling |
| `internal/web/handler.go` | Web dashboard, stats API, metrics API, cluster API |
| `internal/web/middleware.go` | Security headers, input validation, path sanitization |
| `internal/health/health.go` | Health check endpoint (DB, memory, connections, goroutines) |
| `internal/metrics/relay.go` | Prometheus metrics + sliding-window rate calculations |
| `internal/identity/relay_id.go` | Ed25519 keypair generation/persistence for relay identity |
| `internal/limiter/limiter.go` | Rate limiter with burst support and progressive banning |
| `internal/workers/workerpool.go` | Generic worker pool with job buffering |
| `docker/Dockerfile` | Multi-stage build (builder → production alpine) |
| `docker/compose/docker-compose.standalone.yml` | Standalone: CockroachDB + Relay + Caddy |
| `docker/compose/docker-compose.distributed.yml` | Distributed: 3-node cluster template |
| `docker/caddy/Caddyfile` | Caddy reverse proxy with HTTPS and security headers |
| `scripts/install.standalone.sh` | One-command installer (851 lines): Docker, CockroachDB, Relay, Caddy |
| `Makefile` | 396 lines: build, test, lint, docker, db, cross-compile, release |

---

## 🔍 2. Code Walkthrough (Deep Dive)

### Module: Entry Point

```
Module: cmd/main.go + cmd/root.go + cmd/version.go
Path: cmd/
Purpose: CLI bootstrap and application lifecycle management
Details:
  - main.go creates a top-level cancellable context, registers SIGTERM/SIGINT handlers
  - Uses Cobra CLI framework with `relay start` as the primary subcommand
  - root.go PersistentPreRunE loads config via Viper (defaults → file → env vars)
  - CLI flags override config: --relay-name, --db-host, --db-port, --log-level, --metrics-port
  - On `relay start`:
    1. Prints ASCII art banner
    2. Registers Prometheus metrics
    3. Calls application.New(ctx, cfg, nil) to build the Node
    4. Wires graceful shutdown via context cancellation → app.Shutdown()
    5. Calls app.Start(ctx) which launches the WebSocket server
  - main.go blocks on <-ctx.Done() for the `start` command only
Dependencies: Cobra, Viper, application, config, logger, metrics
Open questions / notes:
  - version.go just formats version string from ldflags
  - Build injects version/commit/date via `-X main.version=...`
```

### Module: Configuration

```
Module: internal/config
Path: internal/config/config.go, relay.go, database.go, logging.go, metrics.go, policy.go, capsules.go, general.go, defaults.yaml
Purpose: Centralized, validated, layered configuration system
Details:
  - Uses Go embed to bake defaults.yaml into the binary
  - Viper merges: embedded defaults → user YAML file → SHUGUR_* env vars
  - Config struct hierarchy:
    • GeneralConfig (reserved/empty)
    • LoggingConfig: level, file, format, rotation settings
    • MetricsConfig: enabled flag, Prometheus port
    • RelayConfig: name, description, contact, WS addr, timeouts, throttling
    • ThrottlingConfig: rate limits, max connections, ban rules
    • RelayPolicyConfig: pubkey blacklist/whitelist
    • DatabaseConfig: server host and port
    • CapsulesConfig: time capsules feature toggle + max witnesses
  - Custom validators registered via go-playground/validator:
    • wsaddr: validates ":8080" or "host:port" format
    • pubkey: validates 64-char hex string
    • reasonable_duration: 1s to 24h
    • buffer_size: power of 2, 1KB–1MB
    • host: IP or valid hostname
  - Cross-field validation:
    • ban_threshold vs rate limit correlation
    • event_cache should be >= 1/10th max_connections
    • DB port ≠ metrics port
    • PublicURL must use ws:// or wss:// scheme
  - Defaults: WS on :8080, metrics on :2112, DB on localhost:26257
    50 events/sec rate limit, 1000 max connections, 2048 byte content limit
Dependencies: Viper, go-playground/validator, zap logger
Open questions / notes:
  - No database credentials in config — connects as "root" user
  - Min/max connection pool sizes not directly configurable (derived from maxWSConnections)
```

### Module: Application (Node)

```
Module: internal/application
Path: internal/application/node.go, node_builder.go, node_utils.go
Purpose: Central orchestrator — assembles all components via Builder pattern
Details:
  - NodeBuilder.BuildDB():
    • Detects secure vs. insecure mode by checking ./certs/ca.crt existence
    • Constructs postgres:// connection URIs accordingly
    • Connects to "defaultdb" first to CREATE DATABASE shugur IF NOT EXISTS
    • Then connects to "shugur" database
    • Initializes schema (schema.sql), verifies it
    • Seeds Prometheus EventsStored gauge with actual row count
    • Rebuilds Bloom filter (10M entries, 1% FP rate) by scanning all event IDs
    • Creates EventDispatcher for real-time notifications
  - BuildWorkers(): runtime.NumCPU() * 2 goroutines, buffer = NumCPU * 300
  - BuildValidators(): PluginValidator + EventValidator
  - BuildProcessor(): 100,000-event buffered channel
  - BuildRateLimiter(): from config throttling settings
  - BuildLists(): blacklist/whitelist pubkeys from config
  - Build(): validates all components exist, assembles Node struct, starts expired-events cleaner (hourly)
  - Node.Start(): launches EventDispatcher.Start() + relay.NewServer().ListenAndServe()
  - Node.Shutdown(): 30s timeout, gracefully closes WS connections → stops dispatcher → stops processor → waits on worker pool → closes DB (with 3 retries)
Dependencies: storage, relay, workers, limiter, config, domain interfaces
Open questions / notes:
  - Private key (ed25519) is always nil in current code — relay identity uses separate identity package
  - Bloom filter is rebuilt from scratch on every startup (reads all event IDs from DB)
```

### Module: Relay Server

```
Module: internal/relay/server.go
Path: internal/relay/server.go
Purpose: HTTP server that handles WebSocket upgrades and HTTP API routes
Details:
  - gorilla/websocket upgrader with 1MB read/write buffers, compression, any-origin CORS
  - Route handling (single http.HandleFunc("/")):
    • WebSocket upgrade → handleWebSocketConnection()
    • GET / (browser) → web dashboard (HTML template)
    • Accept: application/nostr+json → NIP-11 relay metadata
    • /static/* → static files (JS, CSS, images)
    • /api/info → NIP-11 JSON
    • /api/stats → relay statistics JSON
    • /api/metrics → real-time metrics JSON
    • /api/cluster → CockroachDB cluster info JSON
    • /health → health check endpoint
  - HTTP server config: 15s read/write timeout, 60s idle
  - Graceful shutdown via context cancellation → httpSrv.Shutdown(30s)
Dependencies: gorilla/websocket, web, health, nips, metrics, domain, config
Open questions / notes:
  - All routes share a single handler function — no router middleware chain (e.g., chi or mux)
  - CORS is wide open (CheckOrigin always returns true) — standard for Nostr relays
```

### Module: WebSocket Connection Lifecycle

```
Module: internal/relay/connection.go
Path: internal/relay/connection.go (941 lines)
Purpose: Per-client WebSocket connection management, message loop, rate limiting, banning
Details:
  - handleWebSocketConnection():
    1. Extracts real client IP from X-Real-IP / X-Forwarded-For headers (Caddy proxy)
    2. Checks ban list (in-memory map with expiry timestamps)
    3. Checks global connection limit via metrics counter
    4. Upgrades HTTP → WebSocket
    5. Creates WsConnection struct with: rate limiter, event dispatcher channel, subscriptions map
    6. Registers connection with Node, starts HandleMessages goroutine
  
  - WsConnection struct fields:
    • gorilla/websocket.Conn, rate.Limiter (golang.org/x/time)
    • subscriptions map[string][]nostr.Filter (guarded by sync.RWMutex)
    • backpressureChan (100-slot buffer — backpressure mechanism)
    • eventChan from EventDispatcher for real-time event streaming
    • idle timeout (300s default), max lifetime (24h hard cap)
    • ping ticker (15s intervals)
  
  - HandleMessages() loop:
    1. Sets read deadline (60s), pong handler
    2. Main loop: ReadMessage → JSON unmarshal → switch on command type
    3. EVENT: rate limit check → if exceeded N times → ban client → handleEvent()
    4. REQ: handleRequest() (subscription.go)
    5. COUNT: handleCountRequest()
    6. CLOSE: handleClose()
    7. Metrics tracked per command: CommandsReceived, CommandProcessingDuration
  
  - handleEvent():
    1. Marshal/unmarshal the event JSON
    2. Call node.GetValidator().ValidateAndProcessEvent() — full NIP validation + BIP-340
    3. Queue event via EventProcessor.QueueEvent()
    4. Send OK response
  
  - processDispatcherEvents() (goroutine per connection):
    • Listens on eventChan for new events from EventDispatcher
    • For each event, checks all client subscriptions for filter match
    • Sends matching events as ["EVENT", subID, event]
  
  - eventMatchesFilter(): in-memory filter matching (IDs, authors, kinds, since, until, tags)
  
  - monitorConnection() (goroutine per connection):
    • Every 15s: sends WebSocket ping
    • Every 1min: checks idle timeout, max lifetime, backpressure
    • Closes connection if any threshold exceeded
  
  - Close(): sync.Once — cancels event context, unregisters from dispatcher, clears subscriptions, decrements metrics, sends close frame, closes socket
  
  - Ban mechanism: in-memory map[string]time.Time, periodic cleanup every 10 minutes
Dependencies: gorilla/websocket, golang.org/x/time/rate, domain, metrics, errors, config
Open questions / notes:
  - Ban list is in-memory only — does not survive relay restart
  - Per-connection goroutines: HandleMessages + processDispatcherEvents + monitorConnection = 3 goroutines per client
  - Max read limit: 2x max_content_length, minimum 1MB, maximum 32MB
```

### Module: Subscription Handling

```
Module: internal/relay/subscription.go
Path: internal/relay/subscription.go (425 lines)
Purpose: REQ/CLOSE command processing, subscription lifecycle
Details:
  - handleRequest():
    1. Validates array structure, subscription ID (max 64 chars)
    2. If sub already exists, removes old one first
    3. Parses filter from raw JSON (supports #tag syntax)
    4. Caps limit at 500 events
    5. Validates filter via PluginValidator.ValidateFilter()
    6. Special validation for NIP-65 relay lists, NIP-50 search
    7. Stores subscription in connection's subscriptions map
    8. Spawns goroutine for processSubscription()
  
  - processSubscription():
    1. Queries DB via QueryEvents() with 30s timeout
    2. For DM events (kinds 4, 14, 15), checks authorization via isAuthorizedForDM()
    3. Sends matching events via SendEvent()
    4. Sends EOSE (End of Stored Events) marker
  
  - handleClose(): removes subscription, decrements ActiveSubscriptions metric
Dependencies: nips, nostr, metrics, storage
Open questions / notes:
  - No subscription limit enforcement per connection (constant MaxSubscriptions = 100 exists but isn't checked here)
```

### Module: Event Validation

```
Module: internal/relay/plugin_validator.go
Path: internal/relay/plugin_validator.go (659 lines)
Purpose: Comprehensive Nostr event validation for all supported NIPs
Details:
  - ValidateEvent():
    1. ID format: 64-char hex
    2. PubKey format: 64-char hex
    3. Signature format: 128-char hex
    4. Kind allowed (extensive whitelist: 100+ event kinds)
    5. Ephemeral events (20000-29999) always allowed but not stored
    6. Blacklist check
    7. ID integrity: recomputes event ID and compares
    8. Timestamp bounds: max 5min future, oldest Jan 1 2021, max 2 days in past
    9. NIP-40 expiration check
    10. Content length check
    11. Tags validation: max elements per tag, total tag size
    12. Required tags per kind (e.g., kind 5 must have "e" tag, kind 7 must have "e" and "p")
    13. BIP-340 Schnorr signature verification via go-nostr
  
  - ValidateAndProcessEvent(): calls ValidateEvent() then NIP-specific validation
  - ValidateFilter(): validates limit, query params
  
  - Kind-specific required tags are meticulously defined for:
    • NIP-09 (deletion), NIP-25 (reactions), NIP-28 (public chat)
    • NIP-33 (addressable), NIP-51 (lists), NIP-52 (calendar)
    • NIP-53 (live activities), NIP-54 (wiki), NIP-57 (zaps)
    • NIP-58 (badges), NIP-60 (cashu wallets), NIP-72 (communities)
    • NIP-XX (time capsules), NIP-YY (nostr web pages)
Dependencies: go-nostr, config, nips, storage
Open questions / notes:
  - AllowedKinds is a static map — not configurable at runtime
  - Signature verification delegates to go-nostr.Event.CheckSignature() (BIP-340)
```

### Module: Storage Layer

```
Module: internal/storage
Path: internal/storage/db.go, schema.go, schema.sql, queries.go, filter.go, event_processor.go, changefeed.go, cluster.go
Purpose: CockroachDB interface — connection management, schema, CRUD, event processing, real-time sync
Details:
  DB (db.go):
    - Connection pool via pgx/v5 pgxpool
    - Pool sizing based on WS connection limits:
      • ≤200 WS: 8 max, 2 min DB conns ("small")
      • ≤2000 WS: 25 max, 5 min ("medium")
      • >2000 WS: 50 max, 10 min ("large")
    - Bloom filter: 10M entries, 1% false positive rate (willf/bloom)
    - Retry logic: 5 attempts with exponential backoff (2s, 4s, 8s...)
    - State machine: Initial → Connecting → Connected → Disconnecting → Closed
  
  Schema (schema.sql):
    - Single `events` table with columns: id, pubkey, created_at, kind, tags (JSONB), content, sig
    - Primary key: id (CHAR(64))
    - Covering indexes with STORING clauses:
      • created_at DESC STORING (pubkey, kind, tags, content, sig)
      • kind ASC, created_at ASC STORING (pubkey, tags, content, sig)
      • pubkey ASC, created_at ASC STORING (kind, tags, content, sig)
    - Inverted JSONB indexes on tags, pubkey+tags, kind+tags
    - UNIQUE constraints for replaceable events (kinds 0,3,41,10000-19999) and addressable events (30000-39999)
    - CHECK constraints: valid hex IDs, valid hex pubkeys, valid hex sigs, kind range 0-65535
  
  Queries (queries.go):
    - GetEvents(): uses CompiledFilter → SQL query → row scan → sort by created_at ASC
    - InsertEvent(): INSERT ON CONFLICT DO NOTHING (idempotent)
    - InsertReplaceableEvent(): CockroachDB UPSERT semantics
    - InsertAddressableEvent(): UPSERT keyed on pubkey+kind+d-tag
    - DeleteExpiredEvents(): JSONB query for "expiration" tag
    - BatchInsertEvents(): batches of 50 in transactions
    - persistDeletion(): deletes referenced events then stores kind-5 event
  
  Filter (filter.go):
    - CompiledFilter: pre-compiles IDs, authors, kinds into maps for O(1) lookups
    - GetBestIndex(): heuristic — ID wins, then pubkey+kind, then kind, then created_at
    - BuildQuery(): constructs parameterized SQL from compiled filter
    - Default limit: 500 events if unspecified
  
  EventProcessor (event_processor.go):
    - NumCPU * 2 worker goroutines, 100K buffered event channel
    - Bloom filter check before queuing (deduplication)
    - Processing per event kind:
      • Ephemeral (20000-29999): broadcast only, not stored
      • Deletion (kind 5): persistDeletion()
      • Replaceable: InsertReplaceableEvent()
      • Addressable: InsertAddressableEvent()
      • Regular: InsertEvent()
    - 3 retry attempts with exponential backoff (50ms, 100ms, 200ms)
    - After successful insert: add to Bloom filter, increment EventsStored metric, broadcast to local EventDispatcher
  
  EventDispatcher (changefeed.go):
    - Client registry: map[string]chan *nostr.Event (100-event buffered channels)
    - Standalone mode: only local broadcasting
    - Cluster mode: polls DB every 2s for new events by created_at > lastSeen
    - processEvents(): fans out events from eventBuffer to all registered clients (non-blocking send)
    - No actual CockroachDB changefeed used — falls back to polling pattern
  
  Cluster (cluster.go):
    - Queries crdb_internal.gossip_nodes for cluster topology
    - Identifies current node via crdb_internal.node_id()
    - Provides cluster health status: healthy / degraded / critical
Dependencies: pgx/v5, willf/bloom, go-nostr
Open questions / notes:
  - Only one table (events) — no users, sessions, or auxiliary tables
  - Changefeed support is verified but actual CockroachDB CDC is not used in practice — polling is the fallback
  - Bloom filter provides probabilistic dedup but is rebuilt on startup from full DB scan
  - StartExpiredEventsCleaner runs hourly, calling DeleteExpiredEvents
```

### Module: Web Dashboard & APIs

```
Module: internal/web
Path: internal/web/handler.go (538 lines), internal/web/middleware.go (470 lines)
Purpose: HTML dashboard, REST APIs for stats/metrics/cluster, security middleware
Details:
  - HandleDashboard(): loads web/templates/index.html, populates DashboardData struct
  - HandleStatsAPI(): returns JSON with active connections, messages, events, error rates
  - HandleMetricsAPI(): comprehensive real-time metrics including memory, uptime, cluster
  - HandleClusterAPI(): CockroachDB cluster info
  - HandleStatic(): serves static files with path traversal prevention
  - SecurityHeaders: CSP, HSTS, X-Frame-Options (most delegated to Caddy proxy)
  - InputValidation middleware: path length limits, query param whitelists, regex path patterns, header injection detection
Dependencies: config, constants, identity, metrics, storage, errors
Open questions / notes:
  - Dashboard is server-side rendered (Go templates) with client-side JS for live updates
  - Static assets include SVG logos, CSS, JS for the dashboard
```

### Module: NIP Implementations

```
Module: internal/relay/nips
Path: internal/relay/nips/*.go (36 files, ~9,950 lines)
Purpose: Per-NIP validation logic for all supported Nostr Improvement Proposals
Details:
  - nip01.go: BIP-340 signature verification via go-nostr
  - nip02.go: Contact list validation
  - nip03.go: OpenTimestamps attestation validation
  - nip04.go: Encrypted DM validation  
  - nip09.go: Event deletion (kind 5) — validates "e" tags and ownership
  - nip11.go: Relay info document serialization (JSON)
  - nip15.go: Nostr Marketplace (400 lines — stall, product, auction, bid validation)
  - nip16.go: Event treatment — ephemeral/replaceable/regular detection
  - nip17.go: Private DM validation (kinds 14, 15, 1059)
  - nip20.go: Command results (OK/NOTICE response formatting)
  - nip22.go: Comment event validation
  - nip25.go: Reaction event validation (requires "e" and "p" tags)
  - nip28.go: Public chat (390 lines — channel create, message, hide, mute)
  - nip33.go: Addressable event detection/validation
  - nip40.go: Expiration timestamp parsing and validation
  - nip44.go: NIP-44 encrypted payload validation
  - nip45.go: COUNT request handling
  - nip50.go: Search capability — validates search filter length and options
  - nip51.go: Lists (829 lines — mute, pin, bookmark, community, emoji sets)
  - nip52.go: Calendar events (1040 lines — date, time, calendar, RSVP)
  - nip53.go: Live activities (1453 lines — streams, chat, meeting spaces)
  - nip54.go: Wiki (681 lines — articles, merge requests, redirects)
  - nip56.go: Reporting (387 lines — abuse, illegal, spam)
  - nip57.go: Lightning Zaps (483 lines — zap request/receipt validation)
  - nip58.go: Badges (573 lines — definition, award, profile badges)
  - nip59.go: Gift wrap validation
  - nip60.go: Cashu Wallets (438 lines — wallet, token, spending history)
  - nip61.go: Nutzaps (348 lines — P2PK Cashu tokens)
  - nip65.go: Relay list metadata validation
  - nip72.go: Moderated communities (453 lines)
  - nip78.go: Application-specific data
  - nip_nostr_web.go: Nostr Web Pages (306 lines — assets, manifests, site indexes)
  - nip_time_capsules.go: Time capsules (168 lines — drand timelock validation)
  - common/validator.go: Shared validation framework
  - common/tags.go, common/utils.go, common/errors.go: Shared utilities
Dependencies: go-nostr, config, storage, constants
Open questions / notes:
  - All NIP validators are called during event ingestion — they perform structural validation
  - No NIP-42 (authentication) implementation — relay is open by default
  - Time capsules (NIP-XX) use drand chain hashes for timelock verification
```

### Module: Supporting Services

```
Module: internal/health, internal/identity, internal/limiter, internal/logger, internal/metrics, internal/workers, internal/errors
Path: (various)
Purpose: Infrastructure services
Details:
  - Health (health.go, 404 lines):
    • Checks: database ping, connection pool utilization, memory (warning 500MB, critical 1GB),
      active connection utilization, goroutine count (warning 1000, critical 5000)
    • Returns JSON: overall status + per-component status + summary
  
  - Identity (relay_id.go):
    • Generates Ed25519 keypair for relay identity
    • Persists private key to ~/.shugur/relay_id.key (permissions 0600)
    • Can use configured public key instead of generating
  
  - Limiter (limiter.go, 219 lines):
    • Composite rate limiter: per-key counters with window-based reset
    • Burst support, progressive ban with threshold
    • Auto-cleanup of idle counters (24h expiry)
    • Note: This module is initialized but the actual per-event rate limiting in connection.go uses golang.org/x/time/rate
  
  - Logger (logger.go, 269 lines):
    • Uber zap with atomic level switching
    • Output: console or JSON format
    • File output with rotation via lumberjack (configurable size, backups, age)
    • Context-aware logging with request/trace IDs
  
  - Metrics (relay.go, 368 lines):
    • Prometheus counters/gauges/histograms via promauto
    • Sliding window rate calculations (events/sec, connections/sec)
    • Atomic counters for dashboard display (bypasses Prometheus read limitations)
    • Key metrics: ActiveConnections, ActiveSubscriptions, MessagesReceived/Sent,
      EventsProcessed (by kind), EventsStored, DBConnections/Errors/Operations,
      CommandsReceived/ProcessingDuration, MessageSizeBytes, HTTPRequests/Duration
  
  - Workers (workerpool.go, 77 lines):
    • Simple channel-based worker pool with WaitGroup
    • Drops jobs if queue full (non-blocking AddJob)
    • 10ms sleep between jobs to prevent CPU busy-waiting
  
  - Errors (4 files):
    • Structured error types: Validation, Network, Database, Timeout, RateLimit, etc.
    • Severity levels: Low, Medium, High, Critical
    • Error handlers for HTTP and WebSocket contexts
    • Recoverability classification for retry logic
Dependencies: zap, prometheus, lumberjack
```

---

## 📌 3. Key Architecture and Flow

### Data Flow

```
Client (Nostr app)
    │
    │ WebSocket (wss://)
    ▼
┌──────────────────────────────┐
│       Caddy Reverse Proxy    │  ← TLS termination, security headers
│       (port 80/443)          │
└──────────┬───────────────────┘
           │ Proxy to :8080
           ▼
┌──────────────────────────────┐
│      relay.Server            │  ← HTTP handler: WS upgrade or REST API
│      (port 8080)             │
├──────────────────────────────┤
│   WsConnection.HandleMessages│  ← Per-client goroutine
│     ├─ EVENT → Validate      │
│     │    └─ PluginValidator   │  ← BIP-340 sig + NIP-specific rules
│     │    └─ EventProcessor    │  ← Async queue (100K buffer)
│     │         └─ storage.DB   │  ← CockroachDB (InsertEvent / Upsert)
│     │              └─ Bloom   │  ← Dedup check
│     │              └─ Dispatch│  ← EventDispatcher → all clients
│     ├─ REQ → Query DB        │
│     │    └─ CompiledFilter    │  ← SQL query builder
│     │    └─ Send events + EOSE│
│     ├─ COUNT → GetEventCount  │
│     └─ CLOSE → Remove sub    │
├──────────────────────────────┤
│   EventDispatcher            │  ← Real-time: eventBuffer → client channels
│     ├─ Local broadcast       │  ← Same-node instant delivery
│     └─ Cross-node polling    │  ← Cluster: poll DB every 2s for new events
├──────────────────────────────┤
│   Metrics server (:8181)     │  ← Prometheus /metrics endpoint
└──────────────────────────────┘
           │
           ▼
┌──────────────────────────────┐
│      CockroachDB             │  ← events table (JSONB tags, covering indexes)
│      (port 26257)            │
│      Admin UI (port 9090)    │
└──────────────────────────────┘
```

### Protocols Used

| Protocol | Where | Purpose |
|----------|-------|---------|
| WebSocket (RFC 6455) | Client ↔ Relay | Bidirectional Nostr event streaming |
| HTTP/1.1 | Browser → Dashboard, NIP-11 | REST APIs, static files |
| TLS/HTTPS | Caddy → Client | Encrypted transport (auto Let's Encrypt) |
| PostgreSQL wire protocol | Relay → CockroachDB | Database queries via pgx |
| BIP-340 (Schnorr) | Event validation | Cryptographic signature verification |
| NIP-44 (XChaCha20) | Encrypted payloads | DM encryption validation |

### Entry Points and Lifecycles

1. **Process Start**: `main()` → `Execute(ctx)` → Cobra dispatches to `start` command
2. **Application Init**: `application.New()` → Builder pattern → DB → Schema → Workers → Validators → Processor → RateLimiter → Lists → Node
3. **Server Start**: `Node.Start()` → EventDispatcher.Start() + relay.NewServer().ListenAndServe()
4. **Client Connection**: HTTP request → WebSocket upgrade → WsConnection.HandleMessages() loop
5. **Event Ingestion**: EVENT command → PluginValidator → EventProcessor.QueueEvent() → DB insert → EventDispatcher broadcast
6. **Subscription**: REQ command → CompiledFilter → DB query → stream events + EOSE → listen for real-time events
7. **Shutdown**: SIGTERM → context cancel → close WebSocket connections → stop dispatcher → stop processor → drain worker pool → close DB

---

## 📦 4. Implementation Plan (High-Level)

### Step 1: Provision Infrastructure

**Effort:** 1–2 hours | **Complexity:** Low

- **Get a VPS** with ≥2 GB RAM, 2 vCPU, ≥20 GB SSD (see hosting validation below)
- **Install Docker + Docker Compose** on the VPS
- **Point a domain** (e.g., `relay.yourdomain.com`) to the VPS IP via DNS A record
- **Open firewall ports:** 80 (HTTP), 443 (HTTPS), 22 (SSH)
  - Ports 8080 (relay), 26257 (CockroachDB), 8181 (metrics), 9090 (CRDB admin) should NOT be exposed externally — Caddy proxies traffic

### Step 2: Standalone Deployment (Recommended Start)

**Effort:** 30 minutes | **Complexity:** Low

Option A — One-command install:
```bash
curl -fsSL https://raw.githubusercontent.com/Shugur-Network/relay/main/scripts/install.standalone.sh | sudo bash
```
This will:
- Install Docker if missing
- Pull CockroachDB, Relay, and Caddy images
- Generate docker-compose.standalone.yml, Caddyfile, config.yaml
- Start all three services
- Auto-provision the database and schema

Option B — Manual Docker Compose:
```bash
git clone https://github.com/Shugur-Network/relay.git
cd relay
# Edit docker/compose/docker-compose.standalone.yml with your domain
# Edit docker/caddy/Caddyfile with your domain
docker compose -f docker/compose/docker-compose.standalone.yml up -d
```

### Step 3: Configure Your Relay

**Effort:** 30 minutes | **Complexity:** Low

Create/edit `config.yaml`:
```yaml
RELAY:
  NAME: "your-relay-name"
  DESCRIPTION: "Your relay description"
  CONTACT: "you@example.com"
  PUBLIC_URL: "wss://relay.yourdomain.com"
  WS_ADDR: ":8080"
  EVENT_CACHE_SIZE: 10000
  THROTTLING:
    MAX_CONNECTIONS: 1000
    MAX_CONTENT_LENGTH: 65536
    RATE_LIMIT:
      ENABLED: true
      MAX_EVENTS_PER_SECOND: 50
DATABASE:
  SERVER: "cockroachdb"  # Docker service name
  PORT: 26257
```

Edit `Caddyfile`:
```
relay.yourdomain.com {
    reverse_proxy relay:8080 {
        header_up Host {host}
        header_up X-Real-IP {remote}
        header_up X-Forwarded-For {remote}
    }
    encode gzip zstd
}
```

### Step 4: Verify Deployment

**Effort:** 15 minutes | **Complexity:** Low

```bash
# Check services are running
docker compose ps

# Test NIP-11 info document
curl -H "Accept: application/nostr+json" https://relay.yourdomain.com

# Test WebSocket connection
echo '["REQ","test",{"limit":1}]' | websocat wss://relay.yourdomain.com

# Check health
curl https://relay.yourdomain.com/health

# Check metrics
docker exec relay curl http://localhost:8181/metrics

# Dashboard
# Open https://relay.yourdomain.com in browser
```

### Step 5: Register on Relay Directories

**Effort:** 15 minutes | **Complexity:** Low

- Add your relay to https://nostr.watch
- Publish a kind-10002 relay list event from your Nostr account
- Share `wss://relay.yourdomain.com` with clients

### Step 6: Build from Source (Alternative)

**Effort:** 1 hour | **Complexity:** Medium

```bash
# Prerequisites: Go 1.24.4+, CockroachDB running
git clone https://github.com/Shugur-Network/relay.git
cd relay
go mod download
make build  # or: go build -o bin/relay ./cmd
./bin/relay start --config config.yaml
```

### Step 7: Monitoring & Maintenance

**Effort:** Ongoing | **Complexity:** Medium

- Set up Prometheus scraping from `:8181/metrics`
- Set up Grafana dashboards for key metrics
- Monitor disk usage (CockroachDB data grows with events)
- Set up log rotation (configured in config.yaml)
- Periodic backups: `cockroach sql --execute="BACKUP TO ..."`

---

### Requirements

| Category | Requirement |
|----------|-------------|
| **Runtime** | Docker 24+ with Docker Compose v2 |
| **Build (optional)** | Go 1.24.4+ |
| **Database** | CockroachDB (latest, auto-deployed via Docker) |
| **Reverse Proxy** | Caddy (auto-deployed, handles TLS via Let's Encrypt) |
| **DNS** | A record pointing your domain to the VPS IP |
| **OS** | Linux (Ubuntu 20.04+ recommended), also works on macOS |
| **Network** | Ports 80, 443 open; 8080, 26257, 8181 internal only |

### Risks / Unknowns

| Risk | Severity | Mitigation |
|------|----------|------------|
| CockroachDB memory usage on small VPS | High | CockroachDB uses 25% of RAM for cache by default; on 2GB VPS this is fine (~512MB). Monitor with `docker stats` |
| Disk growth from event storage | Medium | No built-in retention policy beyond NIP-40 expiration cleanup. Consider adding periodic `DELETE` for old events |
| Dockerfile uses Go 1.25rc3 (release candidate) | Low | Pin to `golang:1.24-alpine` for production stability |
| No authentication (NIP-42) | Medium | Relay is fully open by default. Use blacklist/whitelist pubkeys in config for moderation |
| Single-node CockroachDB = no HA | Low | Acceptable for personal/small relays. Distributed mode exists for scaling |
| Bloom filter rebuild on startup | Low | May take minutes with millions of events. Acceptable for initial deployment |
| No database migration system | Low | Schema is CREATE IF NOT EXISTS — safe for updates. Breaking schema changes would need manual migration |

### Testing Plan

1. **Smoke Test**: `curl` NIP-11, `websocat` send EVENT + REQ
2. **NIP Compliance**: Run shell tests from `tests/nips/test_nip*.sh` (39 test scripts included)
3. **Load Test**: Use `artillery` or similar to test concurrent WebSocket connections
4. **Monitoring**: Verify Prometheus metrics appear at `:8181/metrics`
5. **Failover**: Stop CockroachDB container, verify relay handles gracefully and reconnects

---

## 🖥️ 5. Hosting Validation

### Resource Requirements (Estimated)

Based on code analysis:

| Resource | Minimum (Low Traffic) | Recommended (Medium) | High Traffic |
|----------|-----------------------|----------------------|-------------|
| **CPU** | 1 vCPU | 2 vCPU | 4+ vCPU |
| **RAM** | 2 GB | 4 GB | 8 GB |
| **Storage** | 20 GB SSD | 40 GB SSD | 100+ GB SSD |
| **Bandwidth** | 1 TB/mo | 2 TB/mo | 5+ TB/mo |
| **Connections** | ≤100 concurrent | ≤1000 concurrent | ≤5000 concurrent |

**RAM breakdown** (2GB minimum):
- CockroachDB: ~500MB (25% cache default on single-node `--insecure`)
- Go relay binary: ~100–200MB typical heap
- Caddy: ~30–50MB
- OS: ~200–300MB
- Buffer: ~700MB remaining

**Note**: The relay code configures DB pool sizes automatically based on `MAX_CONNECTIONS` config. At 1000 WS connections, it uses 25 max DB connections (medium tier).

---

### Provider Assessment

```
Provider: Hetzner VPS (CX22 or CX32)
Expected Suitability: ✅ Yes — Best Option
Reasoning:
  - CX22 (2 vCPU, 4 GB RAM, 40 GB SSD): €4.15/mo (~₹400)
  - CX32 (4 vCPU, 8 GB RAM, 80 GB SSD): €7.45/mo (~₹720) for growth
  - 20 TB traffic included (far exceeds Nostr relay needs)
  - Excellent European data centers (Falkenstein, Nuremberg, Helsinki, Ashburn)
  - Docker pre-installed on cloud images
  - Snapshots for backup
Resource assumptions: 2 vCPU / 4 GB RAM handles 1000+ concurrent connections easily
Cost notes: ₹400–450/mo for CX22; CX32 at ₹720 scales to thousands of users
```

```
Provider: Vultr / Linode
Expected Suitability: ✅ Yes — Excellent
Reasoning:
  - Vultr Cloud Compute: $6/mo (1 vCPU, 2 GB RAM, 50 GB SSD) — tight but workable
  - Vultr High Performance: $12/mo (2 vCPU, 4 GB RAM, 100 GB NVMe) — comfortable
  - Linode Shared 2GB: $12/mo, 4GB: $24/mo
  - Both include generous bandwidth (2-4 TB)
  - Global data center presence
  - Linode now under Akamai — good CDN integration potential
Resource assumptions: 2 GB RAM minimum; 4 GB recommended for production
Cost notes: ~₹420–500/mo for 2 GB tier; ₹1000 for 4 GB tier
```

```
Provider: DigitalOcean
Expected Suitability: ✅ Yes — Fine
Reasoning:
  - Basic Droplet: $6/mo (1 vCPU, 1 GB RAM, 25 GB SSD) — too small
  - Regular Droplet: $12/mo (2 vCPU, 2 GB RAM, 50 GB SSD) — workable
  - $24/mo (2 vCPU, 4 GB RAM, 80 GB SSD) — comfortable
  - Mature platform, good monitoring, managed databases available (but CockroachDB is self-hosted)
  - 2-4 TB bandwidth included
Resource assumptions: 2 GB RAM minimum workable; 4 GB recommended
Cost notes: ~₹500/mo for usable tier; ₹1000 for comfortable tier
```

```
Provider: Fly.io
Expected Suitability: ⚠️ Conditional — Not Ideal
Reasoning:
  - Great for stateless apps but problematic for CockroachDB:
    • Fly Machines are ephemeral — CockroachDB needs persistent volumes
    • Volume lifecycle tied to machine region (can't migrate easily)
    • Need to separately provision and manage Fly Volumes
  - WebSocket support is very good (global anycast)
  - Pricing: shared-cpu-1x (256MB) free, dedicated starts at $31/mo for 2 vCPU/4GB
  - You'd need: 1 machine for relay + 1 machine for CockroachDB + volumes
  - Better suited if you use Fly's Postgres addon (not CockroachDB)
Resource assumptions: Relay could use shared-cpu-2x (1GB) $11/mo, but CockroachDB needs dedicated
Cost notes: ₹450–700 is optimistic; realistic with CockroachDB: ₹1500+
Verdict: Overkill for standalone, could work for globally distributed relay
```

```
Provider: Railway
Expected Suitability: ❌ No
Reasoning:
  - Usage-based pricing with no fixed cost predictability
  - $5/mo base + per-second compute metering
  - WebSocket connections are long-lived → expensive on usage-based pricing
  - CockroachDB would need to run as a Railway service (complex setup)
  - No persistent volumes by default
  - Better suited for short-lived request/response APIs, not WebSocket relays
Resource assumptions: A relay with 100 concurrent WS connections running 24/7 = high CPU hours
Cost notes: Estimated ₹2000-5000+/mo for 24/7 WebSocket relay — variable and high
```

```
Provider: Render
Expected Suitability: ❌ No
Reasoning:
  - Background workers: $7/mo per service (starting tier, 512 MB RAM)
  - WebSocket support exists but limited on starter plans
  - CockroachDB would need a private service (complex)
  - Persistent disks available but limited (20 GB max on starter)
  - $25/mo+ for a setup that works (relay + CockroachDB + adequate RAM)
  - Not cost-competitive with VPS providers for this use case
Resource assumptions: Need at least 2 services (relay + DB) with persistent storage
Cost notes: Minimum ₹700/mo and likely ₹2000+ for a workable config
```

---

## 📊 6. Final Recommendation

### Best Host Overall

**🏆 Hetzner CX22** — €4.15/mo (~₹400)

- 2 vCPU, 4 GB RAM, 40 GB SSD, 20 TB traffic
- Perfect match for the relay's resource profile
- CockroachDB + Relay + Caddy all fit comfortably in 4 GB
- European data centers with excellent connectivity
- Simple Docker setup — no PaaS complexity

**Runner-up: Vultr High Performance $12/mo** — if you need US/Asia data centers

### Minimum Viable Setup

| Component | Specification |
|-----------|---------------|
| VPS | 2 vCPU, 2 GB RAM, 20 GB SSD (absolute minimum) |
| VPS (recommended) | 2 vCPU, 4 GB RAM, 40 GB SSD |
| OS | Ubuntu 22.04/24.04 LTS |
| Docker | Docker CE 24+ with Compose v2 |
| Database | CockroachDB (latest, Docker, single-node `--insecure`) |
| Reverse Proxy | Caddy (Docker, auto-TLS) |
| DNS | 1 A record for your relay domain |
| Deployment Time | ~30 minutes with the install script |

### Performance Considerations

1. **CockroachDB tuning**: The relay auto-configures `--cache=25% --max-sql-memory=25%` for distributed setups. For standalone, the `start-single-node --insecure` command uses defaults which are generally fine for small relays.

2. **Connection limits**: Start with `MAX_CONNECTIONS: 1000`. Each WebSocket connection uses 3 goroutines. At 1000 connections = 3000 goroutines + worker pool goroutines — well within Go's capabilities.

3. **Event processing**: The 100K-event buffer and NumCPU*2 workers can handle sustained 5000+ events/sec on a 2-core VPS per the project's own benchmarks.

4. **Bloom filter**: Initialized at 10M entries with 1% false positive rate. Uses ~12MB memory. Excellent for deduplication up to moderate scale.

5. **SQLite vs CockroachDB**: This relay **only** supports CockroachDB — no SQLite fallback. This is a deliberate design choice for distributed capabilities. For a single-instance personal relay, CockroachDB is slightly heavier than SQLite-based relays (strfry, nostr-rs-relay) but provides built-in scaling path.

### Scalability Recommendations

| Scale | Setup | Expected Capacity |
|-------|-------|-------------------|
| **Personal** | 1 VPS, standalone, 2 GB RAM | 100–500 concurrent connections |
| **Community** | 1 VPS, standalone, 4–8 GB RAM | 1000–5000 concurrent connections |
| **Regional** | 3 VPS, distributed CockroachDB cluster, Caddy load balancer | 10,000–50,000 concurrent connections |
| **Global** | Multi-region CockroachDB, multiple relay nodes behind global LB | 50,000+ concurrent connections |

To scale from standalone to distributed:
1. Provision 2 more VPS nodes
2. Set up CockroachDB cluster with TLS certificates (`install.distributed.sh`)
3. Deploy relay instances on each node (stateless — can run N instances)
4. Configure Caddy with upstream load balancing
5. Enable cross-node event synchronization (auto-detected by the code when cluster mode is detected)

---

*Analysis complete. Every Go source file, configuration file, Docker config, and install script has been read and correlated to produce this document.*
