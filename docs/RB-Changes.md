# NostrRelayBlossom — Deep Codebase Review

> Full architecture breakdown, critique, and recommendations.
> Generated 2025-03-12 from a line-by-line read of every file in both codebases.

---

## I. Architecture Overview

### System Topology

```
Internet
  │
  ├─ wss://nostr.ltd ──► Caddy ──► :8080 ──► Shugur Relay (Go)
  │                                              │
  │                                              ├─ WebSocket connections
  │                                              ├─ NIP-11 metadata (HTTP)
  │                                              ├─ NIP-86 management API
  │                                              ├─ Dashboard (HTML/CSS/JS)
  │                                              └─ PostgreSQL 16 (local)
  │
  └─ https://blossom.nostr.ltd ──► Caddy ──► :3000 ──► Blossom (Node.js/Koa)
                                                          │
                                                          ├─ Upload/Media/Mirror APIs
                                                          ├─ Admin dashboard (SPA)
                                                          ├─ SQLite (metadata)
                                                          └─ AWS S3 (blob storage)
```

### Relay Architecture (Go — ~15,000 LOC)

| Layer | Packages | Responsibility |
|-------|----------|---------------|
| **Entry** | `cmd/` | CLI (Cobra), config loading (Viper), signal handling |
| **Application** | `application/` | Node lifecycle, builder pattern, subsystem orchestration |
| **Protocol** | `relay/`, `relay/nips/` | WebSocket handling, NIP implementations, validation |
| **Storage** | `storage/` | PostgreSQL queries, event processing pipeline, Bloom filter dedup |
| **Domain** | `domain/` | Interfaces (`WebSocketConnection`, `EventHandler`, `EventValidator`, `NodeInterface`) |
| **Infrastructure** | `config/`, `logger/`, `metrics/`, `health/`, `identity/`, `limiter/`, `workers/`, `constants/`, `errors/`, `web/` | Cross-cutting concerns |

**Data flow for an EVENT command:**
```
Client WS frame → HandleMessages() → handleEvent()
  → PluginValidator.ValidateAndProcessEvent()
    → Size check → Duplicate check (Bloom + DB) → ID verify → Sig verify
    → ValidateEvent() (kind, timestamp, tags, pow, expiration, dedicated NIP validators)
  → NIP-70 protected event check → NIP-29 group check → NIP-43 membership check
  → EventProcessor.QueueEvent() (async, buffered channel)
    → Worker routes by kind:
       Ephemeral → broadcast only
       Replaceable → DELETE old + INSERT new
       Addressable → DELETE old + INSERT new (by d-tag)
       Deletion → persistDeletion (e-tags + a-tags)
       Vanish → persistVanish (wipe all events from pubkey)
       Default → INSERT (ON CONFLICT DO NOTHING)
  → EventDispatcher broadcasts to matching subscriptions
  → OK response to client
```

### Blossom Architecture (TypeScript — ~3,000 LOC)

| Layer | Files | Responsibility |
|-------|-------|---------------|
| **App** | `index.ts` | Koa setup, CORS, error handler, lifecycle |
| **API** | `api/*.ts` | Upload, fetch, mirror, delete, list, has, media |
| **Auth** | `api/router.ts` | NIP-98 style auth (kind 24242), BUD-06 sha256 binding |
| **Storage** | `storage/` | Local filesystem or S3 abstraction |
| **DB** | `db/` | SQLite (better-sqlite3) for blob metadata |
| **Discovery** | `discover/` | Nostr event-based + upstream CDN blob discovery |
| **Optimize** | `optimize/` | Sharp (images) + ffmpeg (video) transcoding |
| **Rules** | `rules/` | MIME type + pubkey matching for upload policy |
| **Admin** | `admin-api/` | Dashboard CRUD for blobs, users, rules |

---

## II. What Works Well

### 1. Configuration System — Exemplary

Three-layer merge (embedded `defaults.yaml` → config file → `SHUGUR_*` env vars) with Viper. 10 custom validators (`wsaddr`, `pubkey`, `reasonable_duration`, `buffer_size`, etc.) with cross-field validation (ban ratio, cache-per-connection, port conflicts). `UnmarshalExact` with human-friendly error formatting. Most Go projects skip this level of polish.

### 2. Storage Schema — Elegant

`relay/internal/storage/schema.sql` encodes Nostr protocol semantics at the database level. Partial unique indexes for replaceable events (`WHERE kind IN (0,3,41,10000-19999)`) and addressable events (`WHERE kind IN (30000-39999)`) mean the database enforces replacement rules — not the application. The `nostr_d_tag()` immutable function extracts d-tag values from JSONB for the addressable index. Correct by construction.

### 3. Event Processing Pipeline — Well-Designed

Kind-based routing in `processEvents()`: ephemeral events bypass storage (NIP-16 compliant), vanish events cascade-delete all author events, replaceable/addressable use DELETE+INSERT. Worker pool (2×CPU goroutines) with backpressure via buffered channels.

### 4. Bloom Filter for Dedup — Smart Tradeoff

10M-entry filter with 1% false positive rate gives O(1) duplicate detection before hitting the database. False positives mean occasionally skipping a valid new event (acceptable — event will be stored on other relays). False negatives are impossible (Bloom filter guarantee). Rebuilt from DB on startup.

### 5. NIP Coverage — Ambitious

67 supported NIPs with dedicated validator file per NIP and a common validation framework. Custom NIPs (XX: Time Capsules with tlock/drand, YY: Nostr Web Pages with SHA-256 content verification) show genuine innovation.

### 6. Security Hardening in Deployment

systemd services use `NoNewPrivileges=true`, `ProtectSystem=strict`, `ProtectHome=true`, explicit `ReadWritePaths`. Relay runs as dedicated `relay` user. Better than most deployments.

### 7. Dashboard — Polished

Dark-themed with live stats polling, NIP badge grid, JetBrains Mono typography. `formatNIP` template function handles mixed `int`/`string` NIP identifiers (zero-pads integers, passes strings for custom NIPs like "EE" and "C7").

### 8. Blossom Upload Size Enforcement — Defense in Depth

Three-layer enforcement: Content-Length header check (413 early rejection), streaming byte counter (hard kill mid-upload), post-write file stat verification.

---

## III. Critique — What Doesn't Work

### A. Relay: Structural Issues

#### 1. The `NodeInterface` God Object

`relay/internal/domain/node.go` defines `NodeInterface` with 9+ methods spanning database access, configuration, connection management, validation, event processing, and dispatching. Service locator anti-pattern masquerading as dependency injection. Every component that needs *anything* takes a `NodeInterface`, creating invisible coupling.

- **Impact:** Testing any component requires mocking the entire Node. Changes to one concern ripple through the interface.
- **Fix:** Split into focused interfaces: `ConnectionRegistry`, `EventStore`, `ConfigProvider`, `ValidatorProvider`. Inject only what each component needs.

#### 2. In-Memory State Without Persistence (NIP-29, NIP-43, NIP-86)

Groups (`nip29.go`), membership (`nip43.go`), banned events, and blocked IPs (`nip86.go`) all live in memory. A relay restart wipes all groups, all memberships, all management state.

- **Impact:** Features advertised as supported are functionally unreliable. Users who create groups lose them on restart. Admin bans disappear.
- **Fix:** Persist to PostgreSQL. `groups`, `group_members`, `membership`, `invite_codes` tables. Load on startup, write-through on mutation.

#### 3. No Schema Migration System

`schema.go` fast-paths all DDL if the `events` table exists. No migration tracking, no version table, no `ALTER TABLE` capability.

- **Impact:** Adding a column, index, or table requires manual SQL on production. The performance optimization becomes a correctness trap.
- **Fix:** Add a `schema_version` table. Numbered migration files. Run pending on startup. Keep fast-path for the common no-migration case.

#### 4. Silent Event Dropping

Events are dropped silently in at least three places:
- `EventProcessor.QueueEvent()` returns false if buffer full — no metric, no log
- `EventDispatcher.eventBuffer` drops when full — warning logged but event lost
- Per-client channels drop when full — event lost, no notification

- **Impact:** Under load, relay silently loses events. Operators can't detect saturation.
- **Fix:** Add `DroppedEvents` Prometheus counter. Return error codes to clients on backpressure.

#### 5. Dual Metrics Truth

`metrics/relay.go` maintains both Prometheus gauges AND separate atomic int64 counters for the same data. `SyncActiveConnectionsCount()` exists because they drift.

- **Fix:** Use `prometheus.NewGaugeFunc()` or maintain only atomic counters with a custom Prometheus collector.

### B. Relay: Security Concerns

#### 6. No Subscription Limit Per Connection

A client can open unlimited subscriptions via REQ. Each stores filters and spawns a goroutine.

- **Impact:** Single malicious client exhaust memory/goroutines.
- **Fix:** Enforce `MaxSubscriptions = 100` (constant exists in `constants/relay_metadata.go` but is never checked).

#### 7. Deletion Authorization Bypass

When validating kind 5 (deletion), if the target event isn't in the DB, the deletion is **allowed**. Attacker submits deletion before target arrives.

- **Fix:** Reject deletion if target not found. Store deletion event regardless (NIP-09 specified behavior).

#### 8. NIP-50 Search Full Table Scan

`content ILIKE '%' || $n || '%'` with leading wildcard cannot use any index.

- **Fix:** Add PostgreSQL GIN trigram index or use `tsvector`/`tsquery` for full-text search.

### C. Blossom: Critical Issues

#### 9. SSRF Vulnerability in `/mirror` and `/fetch`

`mirror.ts` downloads from any user-provided URL with no host validation. Attacker can scan internal network, hit AWS metadata endpoint (`169.254.169.254`), or proxy requests.

- **Impact:** **CRITICAL.** On EC2, metadata endpoint can expose IAM credentials.
- **Fix:** Block private IP ranges (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8, 169.254.0.0/16). DNS resolution check before connecting. Mandatory request timeout.

#### 10. Stream Corruption in `/fetch` Cache Save

In `fetch.ts`, response stream is piped to client AND passed to `saveFromResponse()`. Node.js streams can only be consumed once — cache save silently fails.

- **Impact:** Caching layer for discovered blobs is broken. Every fetch re-downloads from origin.
- **Fix:** Use `SplitStream` (already defined in `helpers/stream.ts`) to fork the response.

#### 11. Media Auth Binding Mismatch

Client signs auth binding to original file's SHA-256. Server optimizes (transcode/resize), producing different SHA-256. Stored blob hash doesn't match auth event.

- **Impact:** BUD-06 cryptographic binding is broken for media uploads.
- **Fix:** Return both original and optimized hash, or skip BUD-06 for media (document as intentional).

### D. Cross-Cutting Issues

#### 12. No Rate Limiting on Blossom

Unlike relay (per-connection rate limiting), Blossom has zero rate limiting. Upload, mirror, and media endpoints can be hammered.

#### 13. Error Handling Inconsistency

Relay has elaborate error framework (`errors/`) with `AppError`, severity levels, stack traces — but stub functions in `handlers.go` (`isConnectionError()`, `isTimeoutError()`) all return `false`, making classification dead code. NIP validators return plain `error` bypassing the system.

#### 14. NIP Spec Compliance

- **NIP-09:** Missing "a" tag support for addressable event deletion (only "e" tags handled)
- **NIP-78:** Incorrectly requires "p" tag (spec says "d" only)
- **NIP-45:** `HandleCountRequest()` is stubbed (always returns 0)

#### 15. Zero Unit Tests

Relay has 35 shell-based integration tests (impressive), but zero Go unit tests for business logic. Validators, event processor, connection handler, and filter compiler have no coverage. Blossom has no tests at all.

---

## IV. Technical Debt Register

| ID | Component | Description | Severity | Effort |
|----|-----------|-------------|----------|--------|
| TD-01 | Relay: NIP-29/43 | In-memory state, no persistence | High | Medium |
| TD-02 | Relay: Storage | No schema migration system | High | Medium |
| TD-03 | Relay: Errors | Error handler stubs (all return false) | Medium | Low |
| TD-04 | Relay: NIP validators | Code duplication across nip files | Medium | Medium |
| TD-05 | Relay: Connection | No subscription limit enforcement | High | Low |
| TD-06 | Relay: Metrics | Dual truth (Prometheus + atomic counters) | Medium | Medium |
| TD-07 | Relay: Config | `cross-validate()` function commented out | Low | Low |
| TD-08 | Relay: Models | `capsules.go.old` dead file | Low | Trivial |
| TD-09 | Relay: Domain | `ValidationResult` type unused | Low | Trivial |
| TD-10 | Relay: NIP-09 | Missing "a" tag support | Medium | Low |
| TD-11 | Relay: NIP-78 | Incorrectly requires "p" tag | Medium | Low |
| TD-12 | Relay: NIP-45 | `HandleCountRequest()` stubbed | Low | Low |
| TD-13 | Blossom: API | SSRF in mirror/fetch | Critical | Low |
| TD-14 | Blossom: Fetch | Stream corruption in cache save | High | Low |
| TD-15 | Blossom: Media | Auth binding mismatch | High | Medium |
| TD-16 | Blossom: List | No pagination | Medium | Low |
| TD-17 | Blossom: Admin | JSON.parse without error handling | Medium | Trivial |
| TD-18 | Blossom: SQL | LIKE wildcard injection | Medium | Trivial |
| TD-19 | Both | Zero unit tests | High | High |
| TD-20 | Blossom: NDK | Connects at import time even if unused | Low | Low |

---

## V. Risk Areas

### Risk 1: Scale Ceiling

t4g.small (2 vCPU, 2 GB RAM). `MAX_CONNECTIONS: 500`, no table partitioning, full table scans for NIP-50 search, single PostgreSQL instance, all NIP-29/43 state in memory. Architecture supports personal/small-community relay. Scaling beyond ~1000 concurrent users requires: table partitioning, read replicas, NIP-50 indexing, connection pooler.

### Risk 2: NIP Specification Churn

67 NIPs, each spec change requires manual validator update. No automated spec-drift detection. The `temp/nips/` local clone could power a CI diff job.

### Risk 3: Single Point of Failure

Both services on a single EC2 instance. No redundancy, no load balancing, no failover. `.first_boot` timestamp is instance-local.

### Risk 4: Blossom Security Posture

No rate limiting, CORS wide open, SSRF vulnerability, admin password can be auto-generated and logged to stdout, no request logging. Appropriate for personal use, dangerous if exposed to untrusted users.

### Risk 5: Operational Observability

Relay has Prometheus metrics and health checks but no alerting, no log aggregation, no distributed tracing. Blossom uses `debug` npm package (stderr, no structure).

---

## VI. Recommendations (Priority Ordered)

### Immediate (Security)

1. **Fix Blossom SSRF** — Add IP range validation in `mirror.ts` and `transport/http.ts`. Block private ranges. Add 30s request timeout.
2. **Enforce subscription limits** — Check `len(c.subscriptions) >= MaxSubscriptions` in `handleRequest()`. Respond with CLOSED.
3. **Fix deletion auth** — Reject deletion when target event not found in DB.

### Short-Term (Correctness)

4. **Fix stream corruption** — Use `SplitStream` to fork HTTP response for both client delivery and cache save.
5. **Add NIP-09 "a" tag support** — Validator only checks "e" tags.
6. **Fix NIP-78 validator** — Remove incorrect "p" tag requirement. Only "d" tag per spec.
7. **Add event drop metrics** — Prometheus counter for drops at EventProcessor queue, EventDispatcher buffer, per-client channels.

### Medium-Term (Reliability)

8. **Persist NIP-29/43 state** — `groups`, `group_members`, `membership`, `invite_codes` PostgreSQL tables. Load on startup, write-through.
9. **Schema migration system** — `schema_version` table, numbered migration files, run pending on startup.
10. **Add unit tests for validators** — Table-driven `_test.go` per NIP validator file.

### Long-Term (Architecture)

11. **Break up NodeInterface** — Extract `ConnectionRegistry`, `EventStore`, `ConfigProvider`. Inject focused dependencies.
12. **NIP-50 full-text search** — PostgreSQL trigram index or tsvector. Remove ILIKE.
13. **Table partitioning** — Partition `events` by time on `created_at`.
14. **Unified error framework** — Commit to `errors/` package (fix stubs, make validators use it) or remove and use standard `error`.

