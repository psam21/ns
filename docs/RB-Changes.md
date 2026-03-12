# NostrRelayBlossom — Codebase Review

> Line-by-line review of every file in both codebases. Architecture, critique, tech debt, and recommendations.

---

### Phase 1 — Security (Immediate)

| # | Finding | Impact | Scope |
|---|---------|--------|-------|
| 1 | SSRF in Blossom `/mirror` and `/fetch` — downloads from any user-provided URL with no host validation | **CRITICAL.** Attacker can hit AWS metadata endpoint (`169.254.169.254`) to steal IAM credentials, scan internal network, or proxy requests | Block private IP ranges (10/8, 172.16/12, 192.168/16, 127/8, 169.254/16) in `mirror.ts` and `transport/http.ts`. DNS resolution check before connecting. Add 30s request timeout |
| 2 | No subscription limit per connection — unlimited REQ commands, each stores filters + spawns goroutine | Single malicious client can exhaust relay memory and goroutines | Enforce `MaxSubscriptions = 100` (constant exists in `constants/relay_metadata.go` but is never checked) in `handleRequest()`. Respond with CLOSED |
| 3 | Deletion authorization bypass — kind 5 validation allows deletion when target event not in DB | Attacker submits deletion before target arrives; unauthorized event removal | Reject deletion if target not found. Store deletion event regardless (NIP-09 specified behavior) in `plugin_validator.go` |
| 4 | No rate limiting on Blossom — zero throttling on any endpoint | Upload, mirror, and media endpoints can be hammered without consequence | Add Koa rate-limit middleware to `index.ts` with per-IP limits on upload/mirror/media routes |
| 5 | Blossom CORS wide open — `Access-Control-Allow-Origin: *` | Any origin can make authenticated requests to Blossom API | Restrict to known origins or use allowlist in `index.ts` CORS config |

### Phase 2 — Correctness (Short-Term)

| # | Finding | Impact | Scope |
|---|---------|--------|-------|
| 6 | Stream corruption in `/fetch` cache save — response piped to client AND `saveFromResponse()` simultaneously | Node.js streams consumed once; cache save silently fails. Every fetch re-downloads from origin | Use `SplitStream` (already defined in `helpers/stream.ts`) to fork response in `fetch.ts` |
| 7 | NIP-09 missing "a" tag support — validator only handles "e" tags for deletion | Addressable events (kind 30000-39999) cannot be deleted by `kind:pubkey:d-tag` reference | Add "a" tag branch to deletion validator in `nip09.go` with format validation |
| 8 | NIP-78 incorrectly requires "p" tag — spec says only "d" tag is required | Valid NIP-78 application-specific data events rejected if missing "p" tag | Remove "p" tag requirement in `nip78.go`, keep only "d" tag |
| 9 | NIP-45 `HandleCountRequest()` is stubbed — always returns 0 | COUNT requests advertised but non-functional | Implement actual count query in `nip45.go` using existing filter→SQL pipeline |
| 10 | Media auth binding mismatch — client signs original SHA-256, server stores optimized blob with different hash | BUD-06 cryptographic binding broken for `/media` uploads | Return both original and optimized hash in response, or skip BUD-06 validation for media endpoint (document as intentional) in `media.ts` |
| 11 | NIP-50 search is a full table scan — `content ILIKE '%' || $n || '%'` with leading wildcard | Queries on millions of events guaranteed slow, no index usable | Add PostgreSQL GIN trigram index (`gin_trgm_ops`) on `content` column, or use `tsvector`/`tsquery` in `schema.sql` + `filter.go` |

### Phase 3 — Reliability (Medium-Term)

| # | Finding | Impact | Scope |
|---|---------|--------|-------|
| 12 | In-memory state without persistence — NIP-29 groups, NIP-43 membership, NIP-86 bans all memory-only | Relay restart wipes all groups, memberships, admin bans. Features advertised but unreliable | Add `groups`, `group_members`, `membership`, `invite_codes` PostgreSQL tables. Load on startup, write-through on mutation in `nip29.go`, `nip43.go`, `nip86.go` |
| 13 | No schema migration system — `schema.go` fast-paths all DDL if `events` table exists | Adding columns, indexes, or tables requires manual SQL on production. No `ALTER TABLE` capability | Add `schema_version` table. Numbered migration files (001_initial.sql, 002_add_groups.sql, etc.). Run pending on startup. Keep fast-path for common no-migration case |
| 14 | Silent event dropping in 3 places — `QueueEvent()` buffer full, `EventDispatcher` buffer full, per-client channels full | Under load, relay loses events with no observable signal. Operators can't detect saturation | Add `DroppedEvents` Prometheus counter in `event_processor.go` and `changefeed.go`. Return appropriate error codes to clients |
| 15 | Dual metrics truth — Prometheus gauges AND separate atomic int64 counters for same data | Two sources of truth drift apart. Dashboard and Prometheus show different numbers. `SyncActiveConnectionsCount()` exists because they desync | Use `prometheus.NewGaugeFunc()` backed by atomic counters, or remove Prometheus gauges and add custom collector reading from atomics in `metrics/relay.go` |
| 16 | Zero unit tests for Go business logic — 35 shell integration tests but no `_test.go` files | Breaking changes to validators, event processor, connection handler, filter compiler not caught until deployment | Add table-driven `_test.go` per NIP validator file. Cover valid events, invalid events, edge cases |
| 17 | Blossom has zero tests — no unit, integration, or e2e tests | Any change risks silent regression | Add test suite for API endpoints (upload, fetch, mirror, delete) with mocked storage |

### Phase 4 — Architecture (Long-Term)

| # | Finding | Impact | Scope |
|---|---------|--------|-------|
| 18 | `NodeInterface` god object — 9+ methods spanning DB, config, connections, validators, processors, dispatchers | Testing any component requires mocking entire Node. Changes to one concern ripple through interface | Split into focused interfaces: `ConnectionRegistry`, `EventStore`, `ConfigProvider`, `ValidatorProvider` in `domain/node.go`. Inject only what each component needs |
| 19 | Error handling inconsistency — elaborate `errors/` package with `AppError`, severity, stack traces, but stub functions in `handlers.go` all return `false` | Error classification is dead code. NIP validators return plain `error` bypassing the system entirely | Either commit to `errors/` (fix stubs, make validators use it) or remove and use standard `error` returns consistently |
| 20 | NIP validator code duplication — each NIP file reimplements tag validation, kind checks, format parsing | Changes to common patterns (e.g., tag format) require updates in 10+ files | Expand `common/` validator framework usage. Centralize tag validation, pubkey format checks, timestamp validation |
| 21 | `events` table not partitioned — single table for all events, all time ranges | Queries over historical data slow as table grows. No way to archive old partitions | Partition by month/year on `created_at`. Add partition management (create future, drop/archive old) in `schema.sql` |
| 22 | `cross-validate()` config function commented out | Cross-field config validation (port conflicts, interdependent settings) not enforced at startup | Uncomment and complete in `config/` package |

### Phase 5 — Cleanup (Tech Debt)

| # | Finding | Impact | Scope |
|---|---------|--------|-------|
| 23 | `models/capsules.go.old` dead file | Confusing to future maintainers | Delete file |
| 24 | `domain/` `ValidationResult` type defined but never used | Dead code | Remove from `domain/` |
| 25 | `cleanupInactiveCounters()` goroutine leak — ticker never stopped | Goroutine accumulates on each connection, never cleaned up | Store ticker reference, call `ticker.Stop()` on connection close in `connection.go` |
| 26 | Blossom `user-profiles.ts` — Map cache with no TTL, race condition on concurrent fetches, returns `undefined` on first call | Profile cache grows unbounded; concurrent fetches for same pubkey duplicate work | Add TTL-based eviction, deduplicate concurrent fetches with a pending-promise map |
| 27 | Blossom admin `JSON.parse` without error handling in multiple admin-api files | Malformed JSON request body crashes the handler | Wrap in try/catch or use Koa body parser middleware |
| 28 | Blossom SQL LIKE wildcard injection in `helpers/` query builders | User input with `%` or `_` alters query semantics | Escape LIKE wildcards before interpolation in admin-api queries |
| 29 | Blossom NDK connects at import time even when discovery is disabled | Unnecessary network connection, startup delay, error if relay unreachable | Lazy-init NDK only when discovery features are actually used in `ndk.ts` |
| 30 | `script.js` memory leak — `setInterval` in `addStatsCardAnimations` never cleared; `.stat-card` selector references nonexistent class | Timer accumulates across page lifecycle; animation code targets stale selectors | Clear interval on page unload; fix CSS selector in `web/static/script.js` |

### Summary — Risk Areas

| # | Risk | Detail |
|---|------|--------|
| R1 | Scale ceiling | t4g.small (2 vCPU, 2 GB RAM), 500 max connections, no partitioning, full table scans for search, in-memory NIP-29/43 state. Supports personal/small-community relay. Scaling past ~1000 users needs partitioning, read replicas, NIP-50 indexing, connection pooler |
| R2 | NIP specification churn | 67 NIPs, each spec change requires manual validator update. No automated drift detection. `temp/nips/` clone could power a CI diff job |
| R3 | Single point of failure | Both services on one EC2 instance. No redundancy, load balancing, or failover |
| R4 | Blossom security posture | No rate limiting, CORS open, SSRF, admin password logged to stdout, no request logging. Fine for personal use, dangerous if exposed |
| R5 | Observability gaps | Relay has Prometheus + health checks but no alerting, log aggregation, or distributed tracing. Blossom uses `debug` (stderr, no structure) |

### Summary — What Works Well

| # | Strength | Detail |
|---|----------|--------|
| S1 | Configuration system | 3-layer merge (embedded defaults → config file → env vars), 10 custom validators, cross-field validation, `UnmarshalExact` with human-friendly errors |
| S2 | Storage schema | Partial unique indexes encode protocol semantics (replaceable + addressable events). `nostr_d_tag()` immutable function. Correct by construction |
| S3 | Event processing pipeline | Kind-based routing, 2×CPU worker pool, ephemeral bypass, vanish cascade delete, replaceable/addressable upsert |
| S4 | Bloom filter dedup | 10M entries, 1% FP rate, O(1) duplicate detection, rebuilt from DB on startup |
| S5 | NIP coverage | 67 NIPs + 2 custom (Time Capsules with tlock/drand, Web Pages with SHA-256 verification). Dedicated validator per NIP |
| S6 | Deployment hardening | systemd `NoNewPrivileges`, `ProtectSystem=strict`, dedicated relay user, explicit `ReadWritePaths` |
| S7 | Dashboard | Dark theme, live stats, NIP badge grid, mixed int/string NIP formatting |
| S8 | Blossom upload enforcement | 3-layer: Content-Length check → streaming byte counter → post-write stat verification |

