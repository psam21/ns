# Operations Runbook — nostr.ltd

Written 2026-10-02, immediately after the outage that took the relay down for
five days. Everything here was verified against the live host; anything
speculative is labelled as such.

## Host access

```bash
ssh -i ~/.ssh/nostr-relay-key.pem ubuntu@13.201.250.44
```

`13.201.250.44` is in `~/.ssh/known_hosts`. `RELAY_HOST` / `SSH_KEY` env vars
are unset and are not required. Ports 8080/2112/3000 are firewalled from the
internet — test the relay through SSH:

```bash
curl -s -o /dev/null -w '%{http_code}\n' --max-time 8 http://localhost:8080/
```

## First check, always

```bash
/usr/local/bin/relay-memory-watch.sh
```

Exit `0` healthy, `1` degraded, `2` critical. This is the same signal the
5-minute timer uses. If it says `ok`, the relay is serving and there is no
incident.

## The failure signature

The 2026-10-02 outage had **no** symptom in systemd. `systemctl status` said
`active (running)` for the entire five days. The combination is the tell:

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

## Recovery

```bash
sudo systemctl kill -s SIGKILL relay
sudo systemctl start relay
sleep 3
curl -s -o /dev/null -w '%{http_code}\n' --max-time 8 http://localhost:8080/
```

**SIGKILL first, unconditionally.** `systemctl restart` is what failed during
the outage: the process was blocked in uninterruptible sleep on swap and could
not drain connections, so `stop` hung in `deactivating (stop-sigterm)` until
it was killed outright.

`relay-recover.service` automates exactly this sequence. It is installed but
**not enabled** — the watcher only calls it when `WATCH_RECOVER=1`.

## What is deployed

| Commit | Change |
|---|---|
| `ddf136a` | dashboard aggregation, startup, shutdown (the outage fix) |
| `3254d0f` | deploy verifies the relay serves; preflight; GOMEMLIMIT |
| `75065d3` | 5-minute health timer + recovery unit |
| `e64e181` | node-gyp PATH fix so Blossom native addon builds |

## Deploy

**Standing rule: every commit gets deployed.** After a commit is verified and
pushed, run the deploy script before considering the work done. Do not hand-copy
binaries — that bypasses the health gate, the preflight and the rollback.

```bash
cd /home/jack/Documents/ns
AWS_HOST=ubuntu@13.201.250.44 AWS_KEY=~/.ssh/nostr-relay-key.pem \
  ./deploy/ns-deploy-full.sh
```

`AWS_HOST` and `AWS_KEY` must be exported — the script defaults both to empty
and there is no fallback. The script requires a clean worktree and does
`git pull --ff-only`, so commit before deploying.

A successful run ends with `DEPLOY_EXIT=0` and this block:

```
1. Relay process:        ACTIVE
2. Blossom process:      ACTIVE
3. Direct event totals:  READY
4. Grouped telemetry:    READY
5. Upload functionality: PASS
```

**Verify independently afterwards.** The script checking its own work is not
proof, so confirm from outside:

```bash
curl -s -o /dev/null -w '%{http_code}\n' --max-time 15 https://nostr.ltd/
curl -s -o /dev/null -w '%{http_code}\n' --max-time 15 https://nostr.ltd/api/events
curl -s -o /dev/null -w '%{http_code}\n' --max-time 15 https://blossom.nostr.ltd/
ssh -i ~/.ssh/nostr-relay-key.pem ubuntu@13.201.250.44 cat /opt/relay/.last-commit
```

The last one must equal the commit you just pushed.

Useful switches:

- `RELAY_PREFLIGHT_STRICT=1` — abort if the host is unhealthy before restarting.
- `RELAY_READY_TIMEOUT=120` — seconds to wait for the relay to serve (default).
- `EVENTS_MAX_ATTEMPTS=120` — how long to wait for dashboard telemetry to warm.

The script already does transactional rollback, release directories, backups
and Blossom verification. Do not hand-roll a deploy; the reason the outage
survived five days is that nothing checked whether the relay was actually
serving, and that check now lives in the script.

## Monitoring

```bash
systemctl list-timers relay-watch.timer
tail -f /var/log/relay/watch.jsonl          # 5-minute JSONL history
sudo systemctl start relay-watch.service    # force one run
```

To enable unattended recovery:

```bash
sudo systemctl edit relay-watch.service
# [Service]
# Environment=WATCH_RECOVER=1
```

Leaving it off is the safer default. If the relay is genuinely broken (bad
deploy, disk full, corrupt database), restarting every 5 minutes hides the
real fault and destroys the evidence.

## Where the logs actually are

**The relay does not log to the journal.** `LOGGING.FILE` in
`/opt/relay/config.yaml` is `/var/log/relay/relay.log`, and the systemd unit
captures only the startup banner.

```bash
sudo tail -n 50 /var/log/relay/relay.log          # <-- the real log
sudo tail -f /var/log/relay/relay.log
sudo grep '"level":"error"' /var/log/relay/relay.log | tail -20
sudo grep 'WebSocket connection closed' /var/log/relay/relay.log | tail -20
```

`journalctl -u relay` shows the service start/stop lines and nothing else.
Reading only the journal is why a client-side broken pipe appeared to have no
server-side explanation at all — this cost real time during the rate-limiter
work in `fc4b994`, where three attempts to diagnose a dropped connection came
back empty because the answer was in a file nobody had opened.

`sudo` is required: the log is not world-readable.

Log format is zap JSON, one object per line, so it parses directly:

```bash
sudo grep 'WebSocket connection closed' /var/log/relay/relay.log \
  | python3 -c 'import sys,json
for l in sys.stdin:
    d=json.loads(l)
    print(d["reason"], d["real_client_ip"], round(d["connection_duration"]))'
```

The `reason` field exists as of `e07eded` — with a caveat worth recording.
That commit raised the close log from Debug to Info specifically so close
reasons would be visible, and in the same edit dropped the
`zap.String("reason", ...)` argument from the call. The comment claimed the
reason was logged; the deployed binary emitted a line saying a connection
closed and nothing about why. On the live host: 12 closes, 0 attributable.

The lesson generalises. A change whose entire purpose is "make X observable"
must assert that X is actually *emitted*. Asserting the log level is not
enough, because the field carrying the information is a separate argument that
an edit can silently remove.
`TestCloseReasonIsAlwaysLoggable` now covers it, and `Close()` logs
`reason: "unspecified"` rather than staying silent when no path set one.

### Metrics and pprof

Bound to `127.0.0.1:2112` only — pprof exposes heap contents, so it must not be
public. Reach it over SSH:

```bash
curl -s --max-time 20 http://localhost:2112/metrics | grep '^nostr_relay_'
curl -s --max-time 20 'http://localhost:2112/debug/pprof/goroutine?debug=1' \
  | head -1
```

`nostr_relay_rate_limited_total{type="REQ"}` is the signal that
`MAX_REQUESTS_PER_SECOND` is engaging. Before `fc4b994` that metric did not
exist, so there was no way to tell "nobody is exceeding the limit" from "the
limit does not exist".

## Memory: what is actually allocated

Answered with a live heap profile on 2026-10-02, after the metrics/pprof
endpoint was enabled. Total Go heap is **~27 MB** against a `MemoryHigh` of
768 MB, and RSS sits at 50–65 MB.

Top allocations, and all three are fixed-size structures allocated once at
startup:

| Site | Size | What it is |
|---|---|---|
| `willf/bitset.New` | 12.9 MB | the duplicate-event Bloom filter |
| `storage.NewEventProcessor` | 10.2 MB | `make(chan nostr.Event, 100000)` — 104 bytes × 100,000 |
| `secp256k1.init` | 1.1 MB | NIP-29 group keypair generation |

Under a 400-connection load test, heap moved 26.7 MB → 27.7 MB and RSS went
*down* from 64 MB to 56 MB, which appeared to rule out a per-connection leak.
**That conclusion was wrong**, and the load test is why: it ran for minutes,
and the leak it missed is proportional to *connections over time*, not
concurrent connections.

### The real leak: per-connection goroutines

`watch.jsonl` on the production host then showed RSS climbing 64 MB → 124 MB
over ~3.6 hours in a single process, with the Go heap flat at 26.6 MB. Flat
heap plus rising RSS is the signature of something the Go heap profiler cannot
see — goroutine stacks and what they pin.

The goroutine profile said it directly:

```
55 @ ... (*WsConnection).monitorConnection connection.go:944
55 @ ... startNegSweeper.func1                nip77.go:112
```

`NewWsConnection` handed both background goroutines the **server-wide**
context threaded down from `ListenAndServe`. That context is canceled only at
process shutdown, so `Close()` stopped neither goroutine. Each abandoned
goroutine also pins its `WsConnection` — the websocket, its read/write buffers
and its subscription map — so every closed connection stayed resident
permanently. `startNegSweeper` had carried a comment claiming it "exits when
the WebSocket connection terminates"; that was false, and the comment is what
made the bug hard to see.

Fixed by passing `conn.eventCtx`, the per-connection context that `Close()`
already cancels. Regression test: `TestConnectionGoroutinesExitOnClose`, with
`TestConnectionGoroutinesLeakWithServerContext` as the negative control that
deliberately reproduces the old call pattern and asserts the goroutines
survive.

**Lesson:** a load test bounds memory only if it runs long enough to cover the
slowest growth. Minutes-long tests cannot see a leak that takes hours, and a
flat heap is evidence about the *heap*, not about the process.

### Related defects found in the same pass

Auditing every other `go` spawn in `internal/relay` for the same
server-context mistake turned up three more:

| Site | Defect |
|---|---|
| `processSubscription` | built a 30s timeout context, assigned it to `_`, canceled it via `defer`, then queried with the **unbounded** ctx. The bound applied to nothing, so a slow query pinned a DB connection and a goroutine forever. |
| REQ handler | passed the server ctx to `processSubscription`, so a REQ arriving just before disconnect kept querying. |
| REQ handler | read `len(c.subscriptions)` without `subMu`, racing `Close()`'s whole-map replacement. |
| REQ handler | incremented `ActiveSubscriptions` on every REQ including a duplicate `sub_id`. NIP-01 says a second REQ with the same id *replaces* the first, so the gauge grew without bound while `Close()` decremented by the map size — it never returned to zero. |

All four are the same shape as the original: a limit that is written but not
threaded through to the operation it is meant to bound.

### Taking a heap profile

```bash
ssh -i ~/.ssh/nostr-relay-key.pem ubuntu@13.201.250.44 \
  'curl -s --max-time 30 -o /tmp/heap.pb.gz http://localhost:2112/debug/pprof/heap'
scp -i ~/.ssh/nostr-relay-key.pem ubuntu@13.201.250.44:/tmp/heap.pb.gz .
go tool pprof -top -inuse_space relay/bin/relay-arm64 /tmp/heap.pb.gz
```

`go` is not installed on the host, so copy the profile out and analyse it
locally.

Other useful profiles on the same port: `/debug/pprof/goroutine?debug=1`,
`/debug/pprof/block`, `/debug/pprof/mutex`, `/debug/pprof/allocs`.

The listener is bound to `127.0.0.1` and Caddy publishes only 443/80, so
pprof is host-local only. Do not proxy it — it exposes heap contents and
goroutine stacks.

### Candidate reductions, not applied

`NewEventProcessor`'s 100,000-slot channel is 10.2 MB of the 27 MB total.
The queue **drops on full** rather than blocking, so the buffer only decides
how quickly events are dropped under burst, not whether they are accepted.
Reducing it would free ~10 MB, but it would also raise `EventsDropped`
during bursts. Not changed: 10 MB against a 768 MB ceiling is not the
outage's cause, and shrinking it risks dropping events that are currently
accepted.

## Open items

1. **No alerting on the watcher.** `relay-watch.timer` records every 5
   minutes to `/var/log/relay/watch.jsonl`, but nothing notifies. The script
   exits 2 on a critical reading and that exit code currently goes nowhere.
   Needs a notifier (email or webhook) to be useful.

2. **Autorecovery is installed but off.** `relay-recover.service` performs
   the SIGKILL-then-start sequence that recovered the relay on 2026-10-02,
   and the watcher will trigger it when `WATCH_RECOVER=1`. Left off by
   default because restarting every 5 minutes over a genuinely broken relay
   hides the real fault.

3. ~~**`MaxRequestsPerSecond`, `MaxBanDuration` and `ProgressiveBan` are
   unimplemented.**~~ **Fixed.** All three were configured in
   `deploy/config.yaml` and read nowhere in the codebase. See "Limits that were
   written but never applied" below.

4. **`golang.org/x/crypto` still carries GO-2026-5932**, which has no fix
   upstream: the `x/crypto/openpgp` package is unmaintained and unsafe by
   design. The relay has **no direct `x/crypto` import** — it is pulled in
   only as an indirect dependency of `go-playground/validator/v10` for
   `sha3`. Upgrading v0.52.0 → v0.56.0 cleared the other three advisories;
   this one cannot be cleared without dropping the validator dependency.
   `govulncheck` reports the relay's own code as unaffected.

## Limits that were written but never applied

Three settings in `deploy/config.yaml` were validated at startup and read
nowhere in the codebase. An operator tuning them got no behaviour change and
no warning, because a setting that validates is indistinguishable from one that
works.

Found by `relay/internal/config/dead_config_test.go`, which reflects over
`RelayConfig` and fails on any field referenced nowhere outside
`internal/config`. The two entries still exempted are genuinely
unimplementable (`WriteTimeout` cannot apply to a hijacked WebSocket;
`EventCacheSize` sizes no runtime structure).

### `MAX_REQUESTS_PER_SECOND` — a single-connection DoS

The inbound limiter guarded **only** `EVENT`. `REQ` was unmetered, and each
`REQ` spawns `processSubscription` — a goroutine running a database query.
`MaxSubscriptions` caps *concurrently registered* subscriptions, not the rate
of new ones, and does nothing about goroutines already querying. One socket
could therefore saturate both the relay and PostgreSQL.

Fixed with a **second** token bucket, `requestLimiter`, charged against
`MAX_REQUESTS_PER_SECOND`. It is deliberately not the same bucket as the event
limiter: sharing one would mean a client legitimately publishing 30 events/second
has no budget left to read with, so its own writes would throttle its reads.
Two buckets also stop a `REQ` flood from starving `EVENT` delivery.

`requestLimiterApplies` encodes the policy by **cost, not verb** — `REQ`,
`COUNT`, `NEG-OPEN` and `NEG-MSG` are limited; `CLOSE`, `AUTH` and `NEG-CLOSE`
are not, because rejecting those would strand a subscription, leave a client
permanently unable to authenticate, or leak a negentropy session. Each of those
is a way to turn a rate limiter into an outage.

Rejection drops the frame and lets the bucket refill rather than closing the
socket. NIP-01 defines no "too fast" notice, and a client syncing a large
backlog will legitimately burst past a per-second limit. A new metric,
`nostr_relay_rate_limited_total{type}`, makes it visible when the limit is
actually engaging — previously there was no way to distinguish "nobody is
exceeding the limit" from "the limit does not exist".

### `PROGRESSIVE_BAN` / `MAX_BAN_DURATION` — an unusable ladder

Bans used a single fixed `ThrottlingConfig.BanDuration`. Worse, the violation
count was **deleted on every accepted connection** ("Reset exceeded count on new
allowed connection"), so a client could trip the limiter, disconnect,
reconnect, and be treated as a first offender forever. No code path could
produce a second-rung ban, so "progressive" was not merely unimplemented — the
counting it would have depended on was actively reset.

Fixed by `relay/internal/ban.go`. A `banRegistry` owns the ban map and the
violation history, and violations now **survive reconnects**. `banDurationFor`
escalates by doubling, capped at `MAX_BAN_DURATION`:

```
1st -> 5m   2nd -> 10m   3rd -> 20m   ...  capped at MAX_BAN_DURATION
```

The cap is checked **before** each doubling, not after. `time.Duration` is
int64 nanoseconds, ~292 years; without the guard the ladder wraps negative, and
`time.Now().Add(negative)` is an already-expired ban. The negative control
confirms it: removing the guard makes `banDurationFor(26, ...)` return
**−2327892h**, meaning a chronic abuser would receive *no* ban at all after ~26
violations. That is the exact failure `MAX_BAN_DURATION` exists to prevent, and
it is invisible unless the tests are run against the unguarded version.

`PROGRESSIVE_BAN: false` reproduces the old flat behaviour exactly. NIP-86
management blocks bypass the ladder deliberately — an operator decision should
not scale with an automated violation count.

## Dependency security state

| Scope | Result |
|---|---|
| `gh api .../dependabot/alerts` open | 0 |
| `blossom` `pnpm audit` | No known vulnerabilities |
| `blossom/admin` `pnpm audit` | No known vulnerabilities |
| `govulncheck ./...` — relay's own code | 0 |
| `govulncheck ./...` — indirect modules | 1 (GO-2026-5932, no fix available, unused) |

Re-check with:

```bash
gh api repos/psam21/ns/dependabot/alerts --jq '[.[]|select(.state=="open")]|length'
(cd blossom && pnpm audit)
(cd relay && ~/go/bin/govulncheck ./...)
```

## Rules learned the hard way

- **Always bound network calls.** `curl` with no `--max-time` hangs forever if
  packets are dropped. Use `--connect-timeout`, `--max-time`, and wrap in
  `timeout`.
- **`000` means your client gave up**, not that the server returned an error.
- **Never swallow a `Scan` error.** A `return nil` on a failed scan made the
  `event_kind_stats` migration silently no-op in production with nothing in
  the logs.
- **`pg_class.relkind` is `"char"` (OID 18)** — needs `::text` for pgx.
- **Postgres index names are schema-scoped**, so an index on an old view
  collides with the same name on its replacement table.
- **Measure, don't assume.** A covering index "should" have fixed the slow
  refresh; measured, it was 2x *worse* (heap fetches). A `last_autovacuum`
  47 days stale looked like bloat; measured, dead tuples were under threshold.
