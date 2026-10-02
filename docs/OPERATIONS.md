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

## Open items

1. **RSS trend has no slope yet.** The watcher only reports a trend once two
   runs are ≥1 hour apart. It has been running since 09:02 on 2026-10-02;
   `jq 'select(.rss_mb_per_hour != "unknown")' /var/log/relay/watch.jsonl`
   will show the first data point. If it trends upward over days, the growth
   is not fully explained.

2. **The memory growth was never root-caused.** Ruled out by measurement:
   bloom filter (11.4MB), database bloat (4.6% dead tuples, below the
   autovacuum threshold), in-process maps (all have expiry sweeps).
   Per-connection cost measured at ~20KB after `4da537e`. Conclusion was
   load-driven growth amplified by the swap-thrash feedback loop.
   `GOMEMLIMIT=512MiB` is a guardrail against that loop, not a fix.

3. **One moderate GitHub vulnerability** on the default branch
   (dependabot alert 141). Not triaged.

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
