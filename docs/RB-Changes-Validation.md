# RB-Changes.md Validation — 2026-08-20

Document created: 2026-08-20 (same day as clone — fresh, not stale).
Repo: `https://github.com/psam21/ns` (cloned to `/home/jack/Documents/ns`)
Branch: `main` (HEAD: `3212ad5`)

---

## Method
Each of the 30 findings was verified against actual source files in the cloned repo (`relay/`, `blossom/src/`). Status: `REAL` (confirmed), `FIXED` (not found in code — would indicate resolved), or `PARTIAL`.

---

## Phase 1 — Security (5/5 REAL)

| # | Finding | File Evidence | Status | Severity |
|---|---------|--------------|--------|----------|
| 1 | SSRF in Blossom `/mirror` and `/fetch` | `mirror.ts`: `new URL(ctx.request.body.url)` with zero host/IP validation. `transport/http.ts`: `new URL(pointer.url)` — no private-range blocking, no timeout (only `.onion` check) | **REAL** | Critical |
| 2 | No subscription limit per connection | `subscription.go`: `handleRequest()` — checks subID length (≤64), filter validity, auth-required DMs. No `MaxSubscriptions` (100) check. `constants/relay_metadata.go`: `MaxSubscriptions = 100` defined but never enforced | **REAL** | High |
| 3 | Deletion auth bypass | `plugin_validator.go`: calls `nips.ValidateDeletionAuth()`. `nip09.go`: `ValidateEventDeletion()` checks `"e"` tags exist but `ValidateDeletionAuth()` only errors if target IS found with different author (`!ok` → passes). Attacker can delete non-existent event | **REAL** | High |
| 4 | No rate limiting on Blossom | `index.ts`: `cors({ origin: "*" })`, no `koa-rate-limit` middleware, no rate-limit code anywhere | **REAL** | High |
| 5 | Blossom CORS wide open | `index.ts`: `origin: "*"`, `allowMethods: "*"`, `allowHeaders: "Authorization,*"`, `exposeHeaders: "*"` | **REAL** | Medium |

---

## Phase 2 — Correctness (6/6 REAL)

| # | Finding | File Evidence | Status | Severity |
|---|---------|--------------|--------|----------|
| 6 | Stream corruption in `/fetch` cache save | `fetch.ts`: `response.pipe(pass)` (line 104) AND `uploadModule.saveFromResponse(response)` (line 129). Streams consumed once — cache save fails silently | **REAL** | High |
| 7 | NIP-09 missing "a" tag support | `nip09.go`: `ValidateEventDeletion()` checks only `"e"` tag (`helper.ValidateRequiredTag(event, "e")`). No `"a"` branch | **REAL** | High |
| 8 | NIP-78 incorrectly requires "p" tag | `nip78.go`: `if !hasPTag { return fmt.Errorf(...) }`. Only `"d"` tag should be required per NIP-78 spec | **REAL** | Medium |
| 9 | NIP-45 COUNT stubbed | `nip45.go`: `HandleCountRequest()` returns `&CountResponse{Count: 0}` always. Filter validation works but query never runs | **REAL** | Medium |
| 10 | Media auth binding mismatch | `media.ts`: checks auth hash against `upload.sha256` (original), stores optimized blob (`optimizedUpload.sha256` — different hash from optimization). Response returns optimized descriptor. BUD-06 broken | **REAL** | Medium |
| 11 | NIP-50 search full table scan | `filter.go` (line 171): `content ILIKE $n`. `nip50.go` (line 74): `content ILIKE $%d`. Leading wildcard prevents index usage. No GIN trigram index in `schema.sql` | **REAL** | High |

---

## Phase 3 — Reliability (6/6 REAL)

| # | Finding | File Evidence | Status | Severity |
|---|---------|--------------|--------|----------|
| 12 | In-memory state without persistence | `nip29.go`: `groupStoreInstance` (`map[string]*Group`). `nip43.go`: `membershipStoreInstance` (map). `nip86.go`: `managementState` (line 37 comment: "holds in-memory state"). No DB tables for these | **REAL** | High |
| 13 | No schema migration system | `storage/schema.go`: `InitializeSchema()` skips all DDL if `events` table exists (`tableExists` check). No `schema_version` table, no numbered migration files | **REAL** | High |
| 14 | Silent event dropping in 3 places | `event_processor.go`: `QueueEvent()` (line 60), `QueueDeletion()` (line 33), `QueueVanish()` (line 46) — all have non-blocking `select` with `default:` that logs a warning but provides no metric or client signal. No `DroppedEvents` Prometheus counter | **REAL** | Medium |
| 15 | Dual metrics truth | `metrics/relay.go`: both `prometheus.NewGaugeVec` metrics (`ActiveConnections`, etc.) AND separate `atomic.AddInt64` counters (`activeConnectionsCount`, `messagesProcessedCount`, etc.). `IncrementMessagesProcessed()` updates both. Drift possible | **REAL** | Medium |
| 16 | Zero Go unit tests | `find relay -name "*_test.go"` → 0 results | **REAL** | High |
| 17 | Blossom zero tests | `find blossom/src -name "*.test.*" -o -name "*.spec.*"` → 0 results | **REAL** | High |

---

## Phase 4 — Architecture (5/5 REAL)

| # | Finding | File Evidence | Status | Severity |
|---|---------|--------------|--------|----------|
| 18 | `NodeInterface` god object | `domain/node.go`: 9+ methods (`DB()`, `Config()`, `RegisterConn()`, `UnregisterConn()`, `GetActiveConnectionCount()`, `GetConnectionCount()`, `GetStartTime()`, `GetValidator()`, `GetEventProcessor()`, `GetEventDispatcher()`) | **REAL** | Medium |
| 19 | Error handling inconsistency | `errors/` package (`AppError`, `ErrorMiddleware`, `WebSocketHandler`) exists. `plugin_validator.go`: validators return `fmt.Errorf` (not `AppError`). `errors/handlers.go`: complex middleware but not wired to validators | **REAL** | Medium |
| 20 | NIP validator code duplication | Each NIP file (`nip_ee.go`, `nip62.go`, `nip09.go`, `nip78.go`, `nip50.go`) reimplements tag loops (`for _, tag := range evt.Tags`), format checks (`len(tag) >= 2`), kind checks independently | **REAL** | Low |
| 21 | `events` table not partitioned | `schema.sql`: single `CREATE TABLE events (...)`. No `CREATE TABLE ... PARTITION BY` or partition management. No month/year partitioning | **REAL** | Medium |
| 22 | `cross-validate()` commented out | `config/config.go`: line 298 `// if err := crossValidate(&cfg); err != nil {`. Line 411 `// func crossValidate(cfg *Config) error {`. Function body commented out/deleted | **REAL** | Low |

---

## Phase 5 — Cleanup / Tech Debt (8/8 REAL)

| # | Finding | File Evidence | Status | Severity |
|---|---------|--------------|--------|----------|
| 23 | `capsules.go.old` dead file | `relay/internal/models/capsules.go.old` exists on disk | **REAL** | Low |
| 24 | `ValidationResult` type unused | `domain/handler.go`: type defined (line 25), 0 usages outside definition | **REAL** | Low |
| 25 | `cleanupInactiveCounters()` goroutine leak | `event_validator.go`: `go limiter.cleanupInactiveCounters()` (line 47). Ticker created but never stored/stopped (`ticker.Stop()` never called) | **REAL** | Medium |
| 26 | Blossom `user-profiles.ts` cache | `user-profiles.ts`: `profiles` Map — no TTL, no eviction. Concurrent fetches: `fetchProfile()` called multiple times for same pubkey before first completes. Returns `user.profile` (undefined before promise resolves) | **REAL** | High |
| 27 | Admin `JSON.parse` without error handling | `admin-api/helpers.ts`: lines 12-14 — `JSON.parse(queryStrings.filter/sort/range)` with no `try/catch` | **REAL** | Medium |
| 28 | Blossom SQL LIKE wildcard injection | `helpers/sql.ts`: `buildConditionsFromFilter()` — `params.push(...searchFields.map(() => `%${value}%`))`. No escaping of `%` or `_` from user `value` input | **REAL** | Medium |
| 29 | NDK connects at import time | `ndk.ts`: `ndk.connect()` called at line 11 (module import), regardless of whether `config.discovery.nostr.enabled` is true | **REAL** | Medium |
| 30 | `script.js` memory leak + stale selector | `script.js`: line 92 — `setInterval()` inside `forEach` over `.stat-value`, no reference stored, never cleared. `.stat-card` class: exists in `preview.html`/`test-dashboard.html` but NOT in `templates/index.html` — stale selector for actual dashboard | **REAL** | Medium |

---

## Risk Areas (R1–R5) — All still apply

| # | Risk | Evidence |
|---|------|----------|
| R1 | Scale ceiling (t4g.small, no partitioning, full table scans, in-memory state) | Confirmed — all findings 11, 12, 21, 2 contribute |
| R2 | NIP spec churn (67 NIPs, no automated drift detection) | Confirmed — `DefaultSupportedNIPs` exists (`constants/relay_metadata.go`), `temp/nips/` directory exists (per instructions) |
| R3 | Single point of failure (both services on one EC2) | Confirmed — deployment docs (`deploy/`) reference single instance |
| R4 | Blossom security posture (SSRF, CORS open, rate limit missing, admin password logged) | Confirmed — findings 1, 4, 5, 29, plus `index.ts` line 110: `logger(...)` logs admin password |
| R5 | Observability gaps (no alerting, log aggregation, distributed tracing) | Confirmed — Prometheus metrics exist but no alert rules; Blossom uses `console.log`/`logger()` with no structured aggregation |

---

## Strengths (S1–S8) — All still valid

Verified by reading the source files mentioned in each strength. All 8 strengths remain accurate descriptions of the code.

---

## Final Tally

- **Total findings:** 30
- **REAL (unfixed, open):** 30
- **FIXED:** 0
- **PARTIAL:** 0

---

## Conclusion

**None of the 30 findings have been resolved since the document was written (2026-08-20, same day as repo clone).** The document is accurate and current. Every security finding (1–5), correctness finding (6–11), reliability finding (12–17), architecture finding (18–22), and cleanup finding (23–30) is confirmed by direct source inspection.

No code changes were detected between the document's assertions and the current `main` branch (`3212ad5`). The repo is the latest version with all 30 issues intact.
