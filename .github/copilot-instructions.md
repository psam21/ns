# Copilot Instructions — nostr.ltd Relay and Blossom

**This is the single documentation file for this repository.** Everything that
was previously spread across `docs/` — the operations runbook, NIP tracking,
the production inventory, the rollback policy, migration completion criteria,
the reference-class table, and the legacy-reference audit — is consolidated
here. Do not create new Markdown files; extend this one.

## Project overview

This repository contains two independently maintained services deployed together:

- **Nostr relay** (`relay/`) — a Go relay backed by PostgreSQL, with WebSocket handling, event validation, storage, NIP-11 metadata, and the public operations dashboard.
- **Blossom media service** (`blossom/`) — a TypeScript/Node.js service for Nostr-authenticated, content-addressable media storage.
- **Deployment configuration** (`deploy/`) — systemd units, Caddy reverse-proxy configuration, a production relay configuration template, and a relay smoke-test script.

The public brand is **nostr.ltd**. The codebase originated from upstream open-source relay and Blossom projects but has diverged substantially. Preserve technical compatibility identifiers unless a deliberate migration is requested.

## Repository and environment

| Item | Value |
|---|---|
| GitHub repository | `https://github.com/psam21/ns.git` |
| Primary branch | `main` |
| Relay Go version | Go 1.25 or newer, as declared in `relay/go.mod` |
| Go module path | `github.com/Shugur-Network/relay` — **compatibility-sensitive, do not mass-replace** |
| Relay service path | `/opt/relay/relay-arm64` |
| Relay configuration | `/opt/relay/config.yaml` |
| Relay web templates | `/opt/relay/web/templates/` |
| Relay static assets | `/opt/relay/web/static/` |
| Relay systemd service | `relay` |
| Relay internal WebSocket port | `8080` |
| Metrics / pprof port | `2112`, bound to `127.0.0.1` only |
| Blossom service path | `/opt/blossom/` |
| Blossom systemd service | `blossom` |
| Blossom internal port | `3000` |
| Relay log file | `/var/log/relay/relay.log` — **not the journal** |
| Database | PostgreSQL 16, normally local to the production host |
| Advertised NIP registry | 77 identifiers in `relay/internal/constants/relay_metadata.go` |

Ports 8080, 2112 and 3000 are firewalled from the internet; only Caddy-published
443/80 is reachable, so reach the internals over SSH. Never commit host IP
addresses, private SSH key paths, database passwords, or complete connection
URLs — use shell variables, an ignored environment file, or the deployment
operator's local configuration.

### Service endpoints

| Service | URL | Path |
|---|---|---|
| Relay WebSocket | `wss://nostr.ltd` | 443 (Caddy) → 8080 |
| Relay HTTP | `https://nostr.ltd` | 443 (Caddy) → 8080 |
| Blossom API | `https://blossom.nostr.ltd` | 443 (Caddy) → 3000 |
| Blossom Admin | `https://blossom.nostr.ltd/admin` | 443 (Caddy) → 3000 |
| Relay metrics | `http://localhost:2112/metrics` | SSH-only |

### Environment variables

`NOSTR_*` is canonical; `SHUGUR_*` remains an intentional, documented
compatibility fallback. Precedence is `NOSTR_*` over `SHUGUR_*`, and a
deprecation warning is emitted without exposing values. Removal is gated on
issue #111 and must not happen without a successful canonical-only deploy and
an announced deprecation window.

| Canonical | Fallback | Purpose | Source |
|---|---|---|---|
| `NOSTR_RELAY_NAME` | `SHUGUR_RELAY_NAME` | Relay display name | `/opt/relay/config.yaml` |
| `NOSTR_RELAY_DESCRIPTION` | `SHUGUR_RELAY_DESCRIPTION` | Relay description | `/opt/relay/config.yaml` |
| `NOSTR_RELAY_CONTACT` | `SHUGUR_RELAY_CONTACT` | Operator contact | `/opt/relay/config.yaml` |
| `NOSTR_DATABASE_URL` | `SHUGUR_DATABASE_URL` | PostgreSQL connection | `/opt/relay/.env` |
| `NOSTR_LISTEN_ADDR` | `SHUGUR_LISTEN_ADDR` | WebSocket listen address | `/opt/relay/config.yaml` |
| `NOSTR_METRICS_ADDR` | `SHUGUR_METRICS_ADDR` | Metrics listen address | `/opt/relay/config.yaml` |

Blossom: `BLOSSOM_ADMIN_PASSWORD`, `S3_ACCESS_KEY`, `S3_SECRET_KEY` live in
`/opt/blossom/.env`; `BLOSSOM_ALLOW_GENERATED_PASSWORD` and
`BLOSSOM_EXTRA_CORS_ORIGINS` are set in the systemd unit. All deploy scripts,
units, CI jobs and local configs use the canonical `NOSTR_*` names only.

## Build and validate the relay

Run relay commands from `relay/`:

```bash
cd relay
go mod download
go test ./...
go test -race ./...
go vet ./...
go build ./...
gofmt -l ./internal/ ./cmd/          # must print nothing
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/relay-arm64 ./cmd
node --check web/static/script.js
```

The `gofmt` check is a CI gate (`fmt` job). 25 Go files had drifted out of
compliance invisibly, which means a real formatting error in a new change
cannot be told apart from existing noise.

### Blossom

```bash
cd blossom
pnpm install
npx tsc                # must precede the build and the tests
npx vite build
```

`build/` is gitignored, so a stale build can make a test pass. `rm -rf build`
before verifying anything build-dependent — "verified locally" is meaningless
otherwise.

### Local development and smoke test

For local work the suite defaults to `ws://localhost:8080` /
`http://localhost:8080`; override both explicitly when pointing elsewhere.
`nak` here has no `--count` flag, and NIP-45 uses a `COUNT` message rather than
`REQ` — a probe using `REQ` gets an `EOSE` back in ~100 ms and will look fast
when nothing was counted.

```bash
RELAY_URL=ws://localhost:8080 HTTP_URL=http://localhost:8080 \
  ./deploy/test_relay.sh
```

The runner scripts also honour `RELAY_URL`, `HTTP_URL`, `TEST_TIMEOUT`,
`NIP_AUTH_BRIDGE` and `NIP_AUTH_RELAY_URL`; see `relay/tests/nips/README.md`.

## Deployment workflow

**Standing rule: every commit gets deployed.** After a commit is verified and
pushed, run the deploy script before considering the work done. Do not
hand-copy binaries — that bypasses the health gate, the preflight and the
rollback. The reason the 2026-10-02 outage survived five days is that nothing
checked whether the relay was actually serving, and that check now lives in
the script.

```bash
cd /home/jack/Documents/ns
AWS_HOST=<operator-authorized-host> AWS_KEY=<operator-authorized-key> \
  ./deploy/ns-deploy-full.sh
```

`AWS_HOST` and `AWS_KEY` must be exported — the script defaults both to empty
and there is no fallback. It requires a clean worktree and does
`git pull --ff-only`, so commit before deploying. Do not hard-code the host or
key into this file.

A successful run ends with `DEPLOY_EXIT=0` and:

```
1. Relay process:        ACTIVE
2. Blossom process:      ACTIVE
3. Direct event totals:  READY
4. Grouped telemetry:    READY
5. Upload functionality: PASS
```

**Verify independently afterwards.** The script checking its own work is not
proof:

```bash
curl -s -o /dev/null -w '%{http_code}\n' --max-time 15 https://nostr.ltd/
curl -s -o /dev/null -w '%{http_code}\n' --max-time 15 https://nostr.ltd/api/events
curl -s -o /dev/null -w '%{http_code}\n' --max-time 15 https://blossom.nostr.ltd/
ssh <host> cat /opt/relay/.last-commit    # must equal the commit you pushed
```

Useful switches: `RELAY_PREFLIGHT_STRICT=1` (abort if the host is unhealthy),
`RELAY_READY_TIMEOUT=120` (seconds to wait for the relay to serve),
`EVENTS_MAX_ATTEMPTS=120` (dashboard telemetry warm-up).

The deploy script stages, transfers and installs five monitoring assets from a
single filename list: `relay-memory-watch.sh`, `relay-alert.sh`,
`relay-watch.service`, `relay-watch.timer`, `relay-recover.service`. It also
`enable --now`s the timer. **Put anything you maintain in the deploy script** —
assets that are not deployed are not deployed, and a hand-installed copy
silently drifts.

## Rollback policy

| Change type | Rollback mechanism |
|---|---|
| Cosmetic / documentation | `git revert <sha>` |
| Configuration | Restore the previous **precedence contract**, not just remove the new keys |
| Deployment | Restore previous artifacts and units while preserving `.env`, database files and blob data |
| Go module-path migration | Coordinated release only — partial import-path changes are prohibited |

Constraints: `.env`, database files and blob data must never be touched by a
rollback. The deploy script creates backups (`relay-arm64.bak`,
`index.html.bak`, `style.css.bak`, `script.js.bak`) before swapping, and
restores Blossom from backup if the relay fails to restart after the Blossom
swap. A rollback test passed on 2026-09-03 (`scripts/rollback-test.sh`):
corrupt the binary, confirm the service fails, restore from backup, confirm
`active` and HTTP 202.

Outage fixes already in place, for context when reading old commits: `ddf136a`
(dashboard aggregation, startup, shutdown), `3254d0f` (deploy verifies the relay
serves; preflight; `GOMEMLIMIT`), `75065d3` (5-minute health timer + recovery
unit), `e64e181` (node-gyp `PATH` fix so the Blossom native addon builds).

## Operations runbook

### First check, always

```bash
/usr/local/bin/relay-memory-watch.sh
```

Exit `0` healthy, `1` degraded, `2` critical. This is the same signal the
5-minute timer uses. If it says `ok`, the relay is serving and there is no
incident.

### The failure signature

The 2026-10-02 outage had **no** symptom in systemd — `systemctl status` said
`active (running)` for five days. The combination is the tell:

| Signal | Healthy | Wedged |
|---|---|---|
| HTTP on :8080 | `200` | `000` (ttfb `0.000`) |
| `ss -sntl` Recv-Q | `0` | non-zero, e.g. `2624` |
| `/proc/pressure/memory` full avg10 | `0.00` | `> 0.10`, sustained |
| Swap used | small | exhausted |
| `vmstat` `wa` | low | `> 90%` |
| Threads | `Ssl` | some in `D` state |

`Recv-Q` is the fastest single check. Non-zero means the accept loop is not
running, whatever systemd says.

### Recovery

```bash
sudo systemctl kill -s SIGKILL relay
sudo systemctl start relay
sleep 3
curl -s -o /dev/null -w '%{http_code}\n' --max-time 8 http://localhost:8080/
```

**SIGKILL first, unconditionally.** `systemctl restart` is what failed during
the outage: the process was blocked in uninterruptible sleep on swap and could
not drain connections, so `stop` hung in `deactivating (stop-sigterm)` until it
was killed outright. `relay-recover.service` automates exactly this sequence;
the watcher calls it only when `WATCH_RECOVER=1`.

### Monitoring and alerting

```bash
systemctl list-timers relay-watch.timer
tail -f /var/log/relay/watch.jsonl          # 5-minute JSONL history
sudo systemctl start relay-watch.service    # force one run
```

`deploy/relay-alert.sh` consumes the watcher's exit code and posts to a
webhook. Alerts are **edge-triggered on status transitions**, not
level-triggered: the timer runs every 5 minutes, so a condition persisting an
hour would otherwise be the same finding 12 times, and an operator paged 288
times a day stops reading — which makes alerting worse than none. So
`ok → degraded` alerts once, `degraded → degraded` is silent,
`degraded → critical` escalates, and recovery is always announced.

To enable, on the host:

```bash
sudo systemctl edit relay-watch.service
# [Service]
# Environment=ALERT_WEBHOOK=https://discord.com/api/webhooks/...
# Environment=WATCH_RECOVER=1
```

Both are deliberately not managed by the deploy: `ALERT_WEBHOOK` is a secret
belonging in a drop-in, and overwriting it each deploy would clobber operator
configuration. With `ALERT_WEBHOOK` unset the notifier logs the would-be alert
to stderr and sends nothing, so shipping it is inert until the drop-in exists.

`WATCH_RECOVER` is off by default because the threshold it acts on is memory,
not liveness: a relay that is responding but leaking gets killed and restarted
every 5 minutes forever, converting a diagnosable slow leak into an unexplained
restart loop. If you enable it, watch `systemctl show relay -p NRestarts`.

Three properties the notifier guarantees, each because the opposite is worse:

- It **always exits 0**. A webhook outage must not change the health status the watcher reports, nor fail the timer. Wiring it into the status would let a bad webhook turn a healthy relay into an alerting one.
- Alerts are **not transactional** with the relay. The watcher observes the relay; it is not the relay. A failure to install it warns rather than rolling back a good release.
- The payload is built with **`jq`**, not string interpolation. The first version used `printf` plus a hand-rolled escape and emitted **invalid JSON**, so `curl` reported HTTP 200 from a receiver that never parsed it.

Corrupt state files, non-numeric cooldown stamps and unwritable state
directories are all handled — arithmetic on a non-numeric value would abort the
script before it alerts, so the stamp is validated first.

### Where the logs actually are

**The relay does not log to the journal.** `LOGGING.FILE` in
`/opt/relay/config.yaml` is `/var/log/relay/relay.log`; the systemd unit
captures only the startup banner. `journalctl -u relay` shows service
start/stop lines and nothing else. Reading only the journal is why a
client-side broken pipe appeared to have no server-side explanation — it cost
real time during the `fc4b994` rate-limiter work, where three diagnostic
attempts came back empty because the answer was in a file nobody had opened.

```bash
sudo tail -n 50 /var/log/relay/relay.log           # the real log
sudo grep '"level":"error"' /var/log/relay/relay.log | tail -20
```

`sudo` is required; the log is not world-readable. Format is zap JSON, one
object per line, so it parses directly:

```bash
sudo grep 'WebSocket connection closed' /var/log/relay/relay.log \
  | python3 -c 'import sys,json
for l in sys.stdin:
    d=json.loads(l)
    print(d["reason"], d["real_client_ip"], round(d["connection_duration"]))'
```

The `ts` field is a **float epoch**, not a string. To scope a query to the
current process, compare numerically against
`systemctl show relay -p ActiveEnterTimestamp` — string-comparing a JSON line
against an ISO timestamp silently inverts the filter, because `{` sorts above
`2` and every line matches.

The `reason` field exists as of `e07eded`, with a caveat worth recording. That
commit raised the close log from Debug to Info so close reasons would be
visible, and in the same edit dropped the `zap.String("reason", ...)` argument
from the call. The comment claimed the reason was logged; the deployed binary
emitted a line saying a connection closed and nothing about why. On the live
host: 12 closes, 0 attributable. `Close()` now always logs
`reason: "unspecified"` rather than staying silent, and
`TestCloseReasonIsAlwaysLoggable` covers it.

### Metrics and pprof

Bound to `127.0.0.1:2112` only — pprof exposes heap contents, so it must never
be proxied. Reach it over SSH:

```bash
curl -s --max-time 20 http://localhost:2112/metrics | grep '^nostr_relay_'
curl -s --max-time 20 'http://localhost:2112/debug/pprof/goroutine?debug=1' | head -1
```

`nostr_relay_rate_limited_total{type="REQ"}` is the signal that
`MAX_REQUESTS_PER_SECOND` is engaging. Before `fc4b994` that metric did not
exist, so there was no way to tell "nobody is exceeding the limit" from "the
limit does not exist".

Taking a heap profile — `go` is not installed on the host, so copy it out:

```bash
ssh <host> 'curl -s --max-time 30 -o /tmp/heap.pb.gz http://localhost:2112/debug/pprof/heap'
scp <host>:/tmp/heap.pb.gz .
go tool pprof -top -inuse_space relay/bin/relay-arm64 /tmp/heap.pb.gz
```

Other useful profiles: `/debug/pprof/goroutine?debug=1`, `/debug/pprof/block`,
`/debug/pprof/mutex`, `/debug/pprof/allocs`.

### Memory: what is actually allocated

Answered with a live heap profile on 2026-10-02. Total Go heap is **~27 MB**
against a `MemoryHigh` of 768 MB; RSS sits at 50–65 MB. Top allocations, all
fixed-size and allocated once at startup:

| Site | Size | What it is |
|---|---|---|
| `willf/bitset.New` | 12.9 MB | the duplicate-event Bloom filter |
| `storage.NewEventProcessor` | 10.2 MB | `make(chan nostr.Event, 100000)` — 104 B × 100,000 |
| `secp256k1.init` | 1.1 MB | NIP-29 group keypair generation |

`NewEventProcessor`'s 100,000-slot channel is 10.2 MB of the 27 MB total. The
queue **drops on full** rather than blocking, so the buffer only decides how
quickly events are dropped under burst, not whether they are accepted.
Reducing it would free ~10 MB but would also raise `EventsDropped` during
bursts. Not changed: 10 MB against a 768 MB ceiling is not the outage's cause,
and shrinking it risks dropping events that are currently accepted.

## NIP support and coverage

### How to read the registry

`DefaultSupportedNIPs` in `relay/internal/constants/relay_metadata.go` is the
canonical advertised registry: **77 identifiers** (67 decimal plus 10 quoted
hex — `5A`, `7D`, `A0`, `A4`, `B0`, `B7`, `C0`, `C7`, `CC`, `F4`). The public
dashboard renders the complete list in a searchable, internally scrollable
panel. `relay/internal/web/handler_test.go` asserts the exact count.

An advertised identifier is **not** a claim that every sentence of the
specification is enforced by a dedicated validator. Relay support is
distributed across connection handling, filters, event validation,
persistence, the web/API layer, and Blossom. Track conformance against concrete
behaviour and tests, not only against the NIP-11 list.

Any change to the registry must update `DefaultSupportedNIPs`, the NIP-11
output, the dashboard, `relay/tests/nips/coverage.tsv`, and the exact-count
regression test **together**. Drift between them is the failure mode the
coverage gate exists to catch.

### Coverage matrix

`relay/tests/nips/coverage.tsv` has one row per advertised identifier — 77
rows, currently classed as:

| Evidence class | Rows | Meaning |
|---|---|---|
| `contract` | 47 | registry-contract coverage |
| `integration` | 26 | executed by `run_all.sh` |
| `manual` | 3 | requires human or client review |
| `external` | 1 | depends on a service outside the relay |

`run_coverage.sh` verifies the matrix exactly matches `DefaultSupportedNIPs`,
validates script syntax, runs the dashboard JS check and the Go test suite, and
can optionally perform live checks.

```bash
cd relay
./tests/nips/run_coverage.sh --static        # safe, run in every change
make test-nip-coverage
```

A successful static run means every identifier has an explicit disposition and
all automated evidence is structurally runnable. It does **not** turn
client-only or external-service NIPs into relay conformance tests.

### Integration suite

36 shell scripts live in `relay/tests/nips/` (34 numbered NIP scripts plus
`test_nip_nostr_web.sh` and `test_nip_time_capsules.sh`). They publish events
and sometimes delete them.

```bash
cd relay
RELAY_URL=wss://nostr.ltd HTTP_URL=https://nostr.ltd \
  NIP_AUTH_RELAY_URL=wss://nostr.ltd ./tests/nips/run_all.sh
```

**Use the public URL, not an SSH tunnel.** NIP-42 AUTH requires the `relay` tag
to match the URL the relay advertises. Connecting over a tunnel at
`ws://localhost:18080` sends a `relay` tag the relay correctly rejects, and
nip17/nip42/nip59 fail for that reason alone. That is correct behaviour by the
relay, not a bug — but it is indistinguishable from one by looking at the
failure.

`run_all.sh` selects scripts by `$3 == "integration"`, so it runs **26 of the
36**. The other ten (`nip03`, `nip04`, `nip15`, `nip16`, `nip20`, `nip28`,
`nip33`, `nip72`, `nostr_web`, `time_capsules`) have no matrix row. The runner
now **names every skipped script** and prints a `Skipped:` line, because a
runner that hides its own selection is indistinguishable from one that is
complete. The skip is currently correct — those NIPs are not advertised — but
"correct" and "visible" are different properties.

Safety: the scripts publish to production and every kind-5 deletion targets an
event the same script created moments earlier, so no third-party data is at
risk. They add roughly 150–200 events per run. Take a baseline first with
`curl -s --max-time 20 https://nostr.ltd/api/stats | jq '.stats.events_stored'`.

### What the live suite found

The suite had **never been run against a live relay** before 2026-10-02. The
first execution found production bugs that unit tests, the dashboard, and
every manual check had missed.

| Commit | Defect | Symptom |
|---|---|---|
| `9bb9b68` | Author-only REQ built `WHERE true` — the author predicate was never emitted | Client received other people's events |
| `249efd5` | `= ANY(ARRAY[...])` is not indexable; author REQ scanned 1.2M rows for **92 seconds** | Exceeded the 5s timeout → empty response |
| `b2c6f4c` | NIP-45 `COUNT` had the same `= ANY` bug in a third copy of the builder | `count operation timed out` at 5s |

All three presented as "an author filter returns nothing". The first dropped the
predicate; the second had the predicate and could not run it.

The measured difference, on 1,198,763 rows with `events_pubkey_created_at` on
`(pubkey, created_at)`:

```
pubkey = $1                       Index Scan   ...      2.5 ms
pubkey IN ($1)                    Index Scan   ...      2.6 ms
pubkey = ANY(ARRAY[$1]::text[])   different index,
                                  1,198,963 rows filtered
                                             ... 92,101 ms
```

`= ANY(ARRAY[...])` is set membership against an expression; PostgreSQL cannot
drive a btree index from it. `IN (...)` expands to scalar equalities, which the
index serves. **The planner does not rewrite one into the other**, and
**binding the array as a parameter does not help either** — `= ANY($1)` is still
a membership test (measured 5,528 ms via `PREPARE`).

This is invisible without `EXPLAIN`: the query is correct, the results are
correct, it just takes 92 seconds, and no aggregate on the dashboard moves.
Author filters are the most common REQ shape after kind.

Not every `ANY` is a bug. The `id = ANY($1)` in the NIP-09 deletion path is
deliberately left alone: `pubkey = $2` is a scalar equality that drives the
index and the id list is a cheap recheck over the small set it returns (Bitmap
Index Scan, 1.3 ms). A comment in the code says so, so nobody "fixes" it on
the strength of a grep.

### Implementation areas

| Area | Primary locations | Verification |
|---|---|---|
| Protocol, subscriptions, filters, lifecycle | `relay/internal/relay/connection.go`, `subscription.go`, `filter.go`; `storage/event_processor.go` | Go tests + NIP scripts |
| Relay metadata and registry | `relay/internal/constants/relay_metadata.go`, `internal/web/handler.go` | NIP-11 + exact 77-entry test |
| Authentication and protected events | `relay/internal/relay/connection.go`, `nips/nip42.go` | Go tests + integration |
| Deletion and vanish | `relay/internal/storage/queries.go`, `event_processor.go` | Go storage tests + NIP-09 |
| COUNT, search, negentropy | `relay/internal/relay/nips/nip45.go`, `filter.go`, `nip77.go` | Go tests + targeted scripts |
| Relay groups and access metadata | `relay/internal/relay/nip29.go`, `nip43.go` | Go tests; expand integration |
| Wallet Connect and encrypted payloads | `relay/internal/relay/nips/nip47.go`, `nip44.go` | Targeted scripts + validators |
| Media event metadata | `relay/internal/relay/nips/nip92.go`, `nip94.go`; `blossom/` | Partial; add negative cases |
| Blossom storage and auth | `blossom/src/`, `deploy/blossom.service` | Service tests + smoke tests |
| Dashboard telemetry and event cache | `relay/internal/web/handler.go`, `relay/web/` | Handler tests, JS check, preview |
| Project extensions | `relay/internal/relay/nips/`, `test_nip_time_capsules.sh`, `test_nip_nostr_web.sh` | Specialized; may need external deps |

## Dashboard behavior

The public home page is a compact, pilot-style operations console rather than a
marketing landing page. Light theme by default with a persistent dark-mode
switch, designed to fit a single desktop viewport; long protocol registries are
searchable and internally scrollable.

- `relay/web/templates/index.html` — cockpit layout, supported-NIP registry, observed event-kind panel, quick-connect area, limits, service links.
- `relay/web/static/style.css` — responsive light/dark visual system, compact panel layout.
- `relay/web/static/script.js` — hydrates `/api/stats`, refreshes cached `/api/events` telemetry, renders event-kind summaries, filters NIPs and observed kinds, maintains theme preference, drives copy/toast interactions.
- `relay/internal/web/handler.go` — dashboard data models, concurrency-safe event cache, `/api/stats`, `/api/events`, top-kind summaries, template helpers.

**Do not fabricate live dashboard metrics in production code.** Preview
renderers may use clearly labelled representative values, but the live
dashboard must use relay/cache data.

## Limits that were written but never applied

Three settings in `deploy/config.yaml` were validated at startup and read
nowhere in the codebase. An operator tuning them got no behaviour change and no
warning, because **a setting that validates is indistinguishable from one that
works**.

Found by `relay/internal/config/dead_config_test.go`, which reflects over
`RelayConfig` and fails on any field referenced nowhere outside
`internal/config`. Two entries remain exempted and are genuinely
unimplementable: `WriteTimeout` (cannot apply to a hijacked WebSocket) and
`EventCacheSize` (sizes no runtime structure).

### `MAX_REQUESTS_PER_SECOND` — a single-connection DoS

The inbound limiter guarded **only** `EVENT`. `REQ` was unmetered, and each
`REQ` spawns `processSubscription` — a goroutine running a database query.
`MaxSubscriptions` caps *concurrently registered* subscriptions, not the rate of
new ones, and does nothing about goroutines already querying. One socket could
saturate both the relay and PostgreSQL.

Fixed with a **second** token bucket, `requestLimiter`, charged against
`MAX_REQUESTS_PER_SECOND`. Deliberately not the same bucket as the event
limiter: sharing one means a client legitimately publishing 30 events/second
has no budget left to read with, so its own writes throttle its reads. Two
buckets also stop a `REQ` flood from starving `EVENT` delivery.

`requestLimiterApplies` encodes policy by **cost, not verb** — `REQ`, `COUNT`,
`NEG-OPEN` and `NEG-MSG` are limited; `CLOSE`, `AUTH` and `NEG-CLOSE` are not,
because rejecting those would strand a subscription, leave a client
permanently unable to authenticate, or leak a negentropy session. Each is a
way to turn a rate limiter into an outage.

Rejection drops the frame and lets the bucket refill rather than closing the
socket. NIP-01 defines no "too fast" notice, and a client syncing a large
backlog will legitimately burst past a per-second limit. The new
`nostr_relay_rate_limited_total{type}` metric makes it visible when the limit is
engaging.

### `PROGRESSIVE_BAN` / `MAX_BAN_DURATION` — an unusable ladder

Bans used a single fixed `ThrottlingConfig.BanDuration`. Worse, the violation
count was **deleted on every accepted connection** ("Reset exceeded count on new
allowed connection"), so a client could trip the limiter, disconnect, reconnect,
and be treated as a first offender forever. No code path could produce a
second-rung ban, so "progressive" was not merely unimplemented — the counting it
would have depended on was actively reset.

Fixed by `relay/internal/ban.go`. A `banRegistry` owns the ban map and the
violation history, and violations now **survive reconnects**. `banDurationFor`
escalates by doubling, capped at `MAX_BAN_DURATION`:

```
1st -> 5m   2nd -> 10m   3rd -> 20m   ...  capped at MAX_BAN_DURATION
```

The cap is checked **before** each doubling, not after. `time.Duration` is int64
nanoseconds, ~292 years; without the guard the ladder wraps negative, and
`time.Now().Add(negative)` is an already-expired ban. The negative control
confirms it: removing the guard makes `banDurationFor(26, ...)` return
**−2327892h**, meaning a chronic abuser receives *no* ban at all after ~26
violations — the exact failure `MAX_BAN_DURATION` exists to prevent, and
invisible unless the tests run against the unguarded version.

`PROGRESSIVE_BAN: false` reproduces the old flat behaviour exactly. NIP-86
management blocks bypass the ladder deliberately — an operator decision should
not scale with an automated violation count.

## The goroutine and subscription leak

`NewWsConnection` handed both background goroutines the **server-wide** context
threaded down from `ListenAndServe`. That context is canceled only at process
shutdown, so `Close()` stopped neither goroutine. Each abandoned goroutine also
pins its `WsConnection` — the websocket, its read/write buffers and its
subscription map — so every closed connection stayed resident permanently.

`startNegSweeper` carried a comment claiming it "exits when the WebSocket
connection terminates"; that was false, and **the comment is what made the bug
hard to see**.

Evidence: RSS climbing 64 MB → 124 MB over ~3.6 hours in a single process with
the Go heap flat at 26.6 MB. Flat heap plus rising RSS is the signature of
something the Go heap profiler cannot see — goroutine stacks and what they pin.
The goroutine profile said it directly:

```
55 @ ... (*WsConnection).monitorConnection connection.go:944
55 @ ... startNegSweeper.func1                nip77.go:112
```

Fixed by passing `conn.eventCtx`, the per-connection context `Close()` already
cancels. Regression test `TestConnectionGoroutinesExitOnClose`, with
`TestConnectionGoroutinesLeakWithServerContext` as the negative control that
deliberately reproduces the old pattern and asserts the goroutines survive.

Auditing every other `go` spawn in `internal/relay` for the same mistake turned
up four more, all the same shape — a limit that is written but not threaded
through to the operation it is meant to bound:

| Site | Defect |
|---|---|
| `processSubscription` | built a 30s timeout context, assigned it to `_`, canceled it via `defer`, then queried with the **unbounded** ctx — the bound applied to nothing, so a slow query pinned a DB connection and a goroutine forever |
| REQ handler | passed the server ctx to `processSubscription`, so a REQ arriving just before disconnect kept querying |
| REQ handler | read `len(c.subscriptions)` without `subMu`, racing `Close()`'s whole-map replacement |
| REQ handler | incremented `ActiveSubscriptions` on every REQ including a duplicate `sub_id`. NIP-01 says a second REQ with the same id *replaces* the first, so the gauge grew without bound while `Close()` decremented by the map size — it never returned to zero |

## Open items

1. **`ALERT_WEBHOOK` unset** — alerting ships but is inert until the systemd
   drop-in exists. Deliberately unmanaged because it is a secret.
2. **`WATCH_RECOVER=1` off** — see the reasoning above; enabling it without
   watching `NRestarts` trades a diagnosable leak for a restart loop.
3. **`golang.org/x/crypto` carries GO-2026-5932**, which has no upstream fix —
   `x/crypto/openpgp` is unmaintained and unsafe by design. The relay has **no
   direct `x/crypto` import**; it is pulled in only as an indirect dependency of
   `go-playground/validator/v10` for `sha3`. Upgrading v0.52.0 → v0.56.0 cleared
   the other three advisories. `govulncheck` reports the relay's own code as
   unaffected. Not actionable; recorded so it is not re-investigated. Re-check
   only if `x/crypto` ships a maintained replacement or the validator dependency
   is dropped.
4. **A `COUNT` with no author still times out.** `{"kinds":[1]}` takes ~4.9 s
   and hits the 5 s query timeout. This is **not** the `= ANY` bug; it
   reproduces identically on the old binary and is inherent to the query:

   ```
   SELECT count(*) FROM events WHERE kind IN (1)
     ->  Parallel Seq Scan on events
           Filter: (kind = 1)
           Rows Removed by Filter: 296,756
           actual rows=102,960
                                                  4,857 ms
   ```

   The relay has 308,879 kind-1 events, so counting them means visiting a large
   fraction of 1.2M rows, and PostgreSQL chooses a sequential scan even though
   `idx_events_kind_created_at_covering` exists. Options, none taken
   unilaterally: `ALTER TABLE events SET (parallel_tuple_cost = 0)` with a lower
   `random_page_cost` (cheap to test, but changes plans for *every* query on the
   table); a per-kind count table updated on insert (exact, no scan, but must stay
   consistent with `CleanExpiredEvents`/`DeleteExpiredEvents`, which run outside
   the insert path); or reject authorless `COUNT` as a client error, which
   `IsHLLEligible` may already have been meant to gate. With a low-volume author
   the same query plans as a Bitmap Index Scan and returns in 3.1 ms with the
   correct count, so the index path is sound and the problem is row volume.
5. **Ten NIP scripts are stale.** `nip03`, `nip04`, `nip15`, `nip16`, `nip20`,
   `nip28`, `nip33`, `nip72`, `nostr_web`, `time_capsules` test NIPs the relay
   does not advertise. They should be brought up to date with current behaviour
   or deleted. Until then they are correctly skipped and correctly reported.
6. **Go module-path migration is deferred** to 2027-03-01 (issues #109/#118).
   If approved then, the safe sequence is: inventory → vanity metadata → imports
   → clean-clone test → compatibility note. Partial changes are prohibited.
7. **`SHUGUR_*` fallback removal is gated** on issue #111. Requires a successful
   canonical-only deploy, an executed rollback test, and an announced
   deprecation window. The fallbacks are a deliberate compatibility feature, not
   unfinished cleanup.

### Legacy reference policy

`github.com/Shugur-Network/relay` (120 import references) and the branding
guard in `relay/cmd/root_test.go` are **retained by policy**. Four references
were replaced in the 2026-09-03 audit: two workflow repo guards
(`Shugur-Network/relay` → `psam21/ns`), one stale-bot message, and one comment
in `internal/application/node.go`. Reference classes and their treatment:

| Class | Treatment |
|---|---|
| Public branding | Replace with `nostr.ltd` or neutral relay wording |
| Active defaults | Replace with `nostr.ltd`; validate NIP-11 output |
| Configuration keys | Add canonical `NOSTR_*`, retain documented `SHUGUR_*` aliases with defined precedence and a value-free deprecation warning |
| Database identifiers | Preserve current production names; new names only for fresh installs or a planned migration |
| Container/image references | Replace only when a verified `psam21/ns` image exists; otherwise make local-build explicit and pin digests |
| Go module/import path | Separate final phase; atomic, coordinated release |
| Tests and fixtures | Update assertions to canonical names while preserving legacy-alias compatibility tests |
| Documentation | Update after behaviour and aliases settle |

Principles: no blind global replacement — classify every occurrence first; no
secret or data migration in the same change as a branding change; aliases are
deliberate, documented and time-bounded; database identifiers and storage paths
are preserved absent a dedicated migration.

Completion status: public branding, active defaults, database identifiers,
container references, tests/fixtures and documentation are **complete**;
configuration keys await #111; module path deferred to 2027-03-01.

The four replaced references were: `docker-security-scan.yml` and
`merge-queue.yml` repo guards (`Shugur-Network/relay` → `psam21/ns`), the
`stale.yml` bot message (`Shugur Relay project` → `nostr.ltd Relay project`),
and a comment in `relay/internal/application/node.go` (`run the Shugur node` →
`run the nostr.ltd relay`). Verified clean at the time: Blossom source, Blossom
config and Docker files, the root README, and the deploy systemd units.

## Dependency security state

| Scope | Result |
|---|---|
| `gh api .../dependabot/alerts` open | 0 |
| `blossom` `pnpm audit` | No known vulnerabilities |
| `blossom/admin` `pnpm audit` | No known vulnerabilities |
| `govulncheck ./...` — relay's own code | 0 |
| `govulncheck ./...` — indirect modules | 1 (GO-2026-5932, no fix available, unused) |

```bash
gh api repos/psam21/ns/dependabot/alerts --jq '[.[]|select(.state=="open")]|length'
(cd blossom && pnpm audit)
(cd relay && ~/go/bin/govulncheck ./...)
```

## CI

`.github/workflows/ci.yml` has five jobs: `fmt` (gofmt gate), `relay`,
`blossom`, `dashboard` (JS syntax), `nip-coverage`.

The Blossom job order matters and was the fix for a permanently-red pipeline:
Checkout → pnpm setup (9.12.0) → Node 22 → install → **`npx tsc`** → unit
suites → `pnpm build`. Running `tsc` before the tests and build is what made
CI green for the first time in repo history.

All actions are SHA-pinned and declare `using: node24`: `actions/checkout`
v5.0.0, `actions/setup-go` v7.0.0, `actions/setup-node` v7.0.0,
`actions/upload-artifact` v7.0.1, `pnpm/action-setup` v6.1.0. Earlier pins
(`setup-go` v5.5.0, `setup-node` v4.4.0, `upload-artifact` v4.6.2,
`pnpm/action-setup` v4.0.0) declared `node20` and were being force-run on
Node 24. **Verify a pin's `action.yml` says `node24` before writing it down** —
a tag name is not evidence of what an action does.

## Key source files

| File | Purpose |
|---|---|
| `relay/cmd/main.go` | Relay entry point |
| `relay/cmd/root.go` | CLI commands and configuration loading |
| `relay/internal/constants/relay_metadata.go` | Relay metadata, 77 supported NIPs, custom NIPs, advertised limits |
| `relay/internal/relay/plugin_validator.go` | Central event validation and allowed-kind rules |
| `relay/internal/relay/connection.go` | WebSocket routing, AUTH, protected events, subscriptions, rate limits |
| `relay/internal/relay/subscription.go` | Subscription lifecycle and query dispatch |
| `relay/internal/relay/ban.go` | Ban registry and progressive-ban ladder |
| `relay/internal/relay/filter.go` | Filter validation and search support |
| `relay/internal/relay/nip77.go` | Negentropy synchronization |
| `relay/internal/relay/nip86.go` | Relay Management API |
| `relay/internal/relay/nips/nip45.go` | COUNT and HyperLogLog |
| `relay/internal/storage/filter.go` | Compiled-filter query builder (`IN (...)`, never `= ANY`) |
| `relay/internal/storage/queries.go` | PostgreSQL persistence, `buildFilterWhere`, event aggregation |
| `relay/internal/storage/filter_test.go` | Filter-builder regression tests |
| `relay/internal/storage/queries_filter_test.go` | COUNT-path builder regression tests |
| `relay/internal/config/dead_config_test.go` | Fails on config fields read nowhere |
| `relay/internal/web/handler.go` | HTTP handlers, event cache, dashboard APIs, template data |
| `relay/web/templates/index.html` | Public operations console markup |
| `relay/web/static/style.css` | Dashboard styles and theme system |
| `relay/web/static/script.js` | Dashboard hydration, filtering, refresh, interactions |
| `relay/tests/nips/` | 77-row coverage matrix, 36 scripts, suite runners |
| `deploy/ns-deploy-full.sh` | Transactional deploy with health gate and rollback |
| `deploy/relay-memory-watch.sh` | Health watcher (exit 0/1/2) |
| `deploy/relay-alert.sh` | Edge-triggered webhook notifier |

## Rules learned the hard way

- **Always bound network calls.** `curl` with no `--max-time` hangs forever if packets are dropped. Use `--connect-timeout`, `--max-time`, and wrap in `timeout`.
- **`000` means your client gave up**, not that the server returned an error.
- **Never swallow a `Scan` error.** A `return nil` on a failed scan made the `event_kind_stats` migration silently no-op in production with nothing in the logs.
- **`pg_class.relkind` is `"char"` (OID 18)** — needs `::text` for pgx.
- **Postgres index names are schema-scoped**, so an index on an old view collides with the same name on its replacement table.
- **Measure, don't assume.** A covering index "should" have fixed the slow refresh; measured, it was 2× *worse* (heap fetches). A `last_autovacuum` 47 days stale looked like bloat; measured, dead tuples were under threshold.
- **A limit that validates is not a limit that works.** Grep for the *shape*, not the instance: a `WithTimeout` assigned to `_`, a per-connection ctx swapped for a server-wide one, a config field validated but never read.
- **Negative controls are not optional.** Three times in the 2026-10-02 work a test passed against broken code: a race test whose stop-channel fired before the goroutines overlapped, a goroutine-count assertion that matched nothing, and a format gate verified in both directions before being trusted. If you have not seen the test fail, you have not tested it.
- **A change that makes X observable must assert X is emitted**, not just that logging was enabled. `e07eded` raised a log line to Info so close reasons would be visible and dropped the `zap.String("reason", ...)` argument in the same edit. The field carrying the information is a separate argument.
- **Check where logs go before concluding there are none.**
- **Distrust a hand-rolled protocol probe that a real client contradicts.** A raw-socket WebSocket probe reported broken pipes; `nak` did the same REQ fine. The probe was not reading its socket. Separately, a probe using `REQ` where NIP-45 expects `COUNT` got an `EOSE` back in 100 ms and would have reported "already fast" — check the response type, not just the latency.
- **Gate what you would otherwise only catch by chance.** 25 Go files had drifted out of `gofmt` compliance invisibly.
- **An endpoint that accepts malformed input makes a broken payload look working.** The first webhook notifier emitted invalid JSON and `curl` reported HTTP 200 because the receiver never parsed the body. `0xE01B24` is not a JSON integer, so `jq --argjson` rejected the whole payload and the alert silently never sent. A test that observes *that something was sent* is not a test; parse what arrived.
- **Put anything you maintain in the deploy script.** Assets that are not deployed are not deployed.
- **One symptom can have two causes, and fixing one looks like total failure.** "Author filter returns nothing" was a dropped predicate *and* a 92-second non-indexable scan. Fixing the first produced a change that was correct, reviewed, deployed, tested — and still failed. The conclusion "my diagnosis was wrong" is wrong; the diagnosis was incomplete. **Enumerate the full chain that must work before declaring the first break sufficient.**
- **`= ANY(ARRAY[...])` and `= ANY($1)` both cannot use a btree index.** `IN (...)` can, and the planner will not rewrite one into the other. Measured: 92,101 ms and 5,528 ms versus 2.6 ms and 1.3 ms. Never write `= ANY` for an equality filter.
- **"Not reached by the function I fixed" is not "not reached."** The NIP-45 path was recorded as dead because a grep in the file being edited found no callers; it was called from `nip45.go` on every COUNT. Grep the whole module, then read the call sites.
- **Two hand-maintained copies of anything will drift.** `GetEventCount` and `GetEventPubkeys` were near-duplicates; the second was written because the first wasn't reusable and it silently kept the bug. When you fix a bug in one copy, ask what made the second necessary — usually the fix belongs in a shared extraction.
- **Run the integration suite.** 36 mutating NIP scripts had never run against a live relay, and the first execution found bugs that unit tests, the dashboard, and every manual check had missed. The suite is the only thing that exercises the real query path.
- **A runner that hides its own selection is indistinguishable from one that is complete.** `run_all.sh` ran 26 of 36 scripts and said nothing about the other 10.

## Engineering conventions

Use descriptive commit messages that explain **what changed and why**,
including the reasoning that was wrong on the way there. Run `git diff --check`
before committing. Prefer direct commits to `main` for validated work unless a
pull request is explicitly requested. Review NIP specifications before changing
validators or the advertised registry, and add or update focused tests whenever
behaviour changes.

Never commit host IP addresses, private key paths, database passwords, or
complete connection URLs.

## References

- [Nostr NIP repository](https://github.com/nostr-protocol/nips)
- [Nostr protocol site](https://nostr.org/)
- [Blossom ecosystem](https://github.com/hzrd149/blossom-server)
- [Project repository](https://github.com/psam21/ns)