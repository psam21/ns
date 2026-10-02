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
*down* from 64 MB to 56 MB. **There is no per-connection leak**; the buffer
fix in `4da537e` did its job.

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
   exits 2 on a critical reading and that exit code currently goes
   nowhere. Needs a notifier (email or webhook) to be useful.

2. **Autorecovery is installed but off.** `relay-recover.service` performs
   the SIGKILL-then-start sequence that recovered the relay on 2026-10-02,
   and the watcher will trigger it when `WATCH_RECOVER=1`. Left off by
   default because restarting every 5 minutes over a genuinely broken relay
   hides the real fault.

3. **`MaxRequestsPerSecond`, `MaxBanDuration` and `ProgressiveBan` are
   unimplemented.** They are accepted and validated, set in
   `deploy/config.yaml`, and never read — see `knownUnused` in
   `relay/internal/config/dead_config_test.go`. An operator who tunes them
   gets no behaviour change and no warning. Either implement or remove.

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
