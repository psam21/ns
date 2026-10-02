#!/usr/bin/env bash
#
# relay-memory-watch.sh — detect the conditions that preceded the 2026-10-02
# nostr.ltd outage, before they become an outage.
#
# WHY THIS EXISTS
#
# On 2026-10-02 the relay reported `active (running)` for five days while
# serving zero bytes on every request. Nothing in systemd, and nothing the
# deploy script checked, would have flagged it:
#
#   Memory:    768.4M (high: 768.0M  available: 0B)   <- pinned at MemoryHigh
#   Swap:      928.8M of 1023M                        <- fully consumed
#   vmstat:    wa=97-98%                              <- IO wait, not CPU
#   Threads:   3 in D state (folio_wait_bit_common)   <- blocked paging in
#   Listener:  Recv-Q 2624 on :8080                   <- accept loop starved
#   HTTP:      000 on every request, ttfb=0.000
#
# The signature is the combination, not any single value. A healthy relay on
# this host sits at ~47MB RSS with 0.00 memory pressure.
#
# WHAT IT CHECKS
#
#   1. HTTP liveness   — the only check that reflects real user impact.
#   2. Recv-Q backlog  — non-zero means the accept loop is not running.
#   3. Memory pressure — PSI `full` avg10; sustained >40% means the cgroup is
#                         stalled in direct reclaim and goroutines will enter
#                         D state.
#   4. Swap            — exhausted swap is what turns pressure into a hang.
#   5. RSS trend       — growth across invocations, so slow leaks are visible
#                         before they hit the ceiling.
#
# USAGE
#
#   ./relay-memory-watch.sh            # one-shot check, exit 0/1/2
#   ./relay-memory-watch.sh --json     # machine-readable
#   WATCH_STATE=/var/tmp/relay-watch.state ./relay-memory-watch.sh
#
#   # cron every 5 minutes:
#   */5 * * * * /opt/relay/relay-memory-watch.sh --json >>/var/log/relay-watch.jsonl
#
#   # alert on non-zero:
#   */5 * * * * /opt/relay/relay-memory-watch.sh || logger -t relay-watch "relay degraded"
#
# EXIT CODES
#
#   0  healthy
#   1  degraded  — one warning threshold crossed
#   2  critical  — relay not serving, or backlogged, or swap exhausted
#
# Run it on the host as a user that can read /proc/pressure/memory and run
# `ss`. No root is required; `sudo` is not used.

set -uo pipefail

RELAY_URL="${RELAY_URL:-http://localhost:8080/}"
STATE_FILE="${WATCH_STATE:-/var/tmp/relay-memory-watch.state}"
CURL_MAX_TIME="${CURL_MAX_TIME:-8}"
JSON_OUTPUT=0

for arg in "$@"; do
    case "$arg" in
        --json) JSON_OUTPUT=1 ;;
        -h|--help) sed -n '2,50p' "$0"; exit 0 ;;
        *) echo "unknown argument: $arg" >&2; exit 0 ;;
    esac
done

# --- collect -----------------------------------------------------------------

# HTTP code. 000 means curl gave up: the connection never delivered a byte,
# which is the wedged case, not a slow-but-working one.
#
# curl exits non-zero AND prints 000 on failure, so `|| echo 000` would append
# a second 000 and yield "000000". Take curl's output when it is non-empty and
# fall back to 000 only when it printed nothing at all.
http_code=$(curl -s -o /dev/null -w '%{http_code}' \
    --max-time "$CURL_MAX_TIME" "$RELAY_URL" 2>/dev/null)
http_code="${http_code:-000}"
case "$http_code" in
    ''|*[!0-9]*) http_code=000 ;;
esac

# PSI. `full avg10=` is field 3; field 2 is the literal "avg10=".
pressure_full=$(awk '/^full/{split($3,a,"="); print a[2]; exit}' /proc/pressure/memory 2>/dev/null)
pressure_some=$(awk '/^some/{split($2,a,"="); print a[2]; exit}' /proc/pressure/memory 2>/dev/null)

swap_free_kb=$(awk '/^SwapFree/{print $2; exit}' /proc/meminfo 2>/dev/null)
swap_total_kb=$(awk '/^SwapTotal/{print $2; exit}' /proc/meminfo 2>/dev/null)
mem_available_kb=$(awk '/^MemAvailable/{print $2; exit}' /proc/meminfo 2>/dev/null)

# `ss -H` emits a stats preamble before the socket table, so match the LISTEN
# row explicitly. Recv-Q is its first field.
recv_q=$(ss -sntlH "sport = :8080" 2>/dev/null | awk '$1=="LISTEN"{print $2; exit}')
[ -z "$recv_q" ] && recv_q="unknown"

rss_kb=$(awk '/^VmRSS/{print $2; exit}' "/proc/$(pgrep -f 'relay-arm64' | head -1)/status" 2>/dev/null)
[ -z "$rss_kb" ] && rss_kb="unknown"

# --- RSS trend ---------------------------------------------------------------

rss_trend="unknown"
if [ "$rss_kb" != "unknown" ] && [ -f "$STATE_FILE" ]; then
    prev_rss=$(cat "$STATE_FILE" 2>/dev/null || echo "")
    prev_ts=$(awk '{print $1}' "$STATE_FILE" 2>/dev/null || echo 0)
    now_ts=$(date +%s)
    if [ -n "$prev_rss" ] && [ "$now_ts" -gt "$prev_ts" ] && [ "$prev_ts" -gt 0 ]; then
        elapsed_h=$(( (now_ts - prev_ts) / 3600 ))
        if [ "$elapsed_h" -ge 1 ]; then
            delta=$(( rss_kb - prev_rss ))
            rss_trend=$(awk -v d="$delta" -v h="$elapsed_h" 'BEGIN{printf "%+.1f", d/h}')
        fi
    fi
fi
if [ "$rss_kb" != "unknown" ]; then
    printf '%s %s\n' "$(date +%s)" "$rss_kb" > "$STATE_FILE" 2>/dev/null || true
fi

# --- evaluate ----------------------------------------------------------------

warnings=()
critical=0

if [ "$http_code" != "200" ] && [ "$http_code" != "202" ]; then
    warnings+=("relay not serving HTTP (code=${http_code})")
    critical=1
fi

if [ "$recv_q" != "unknown" ] && [ "${recv_q:-0}" -gt 0 ] 2>/dev/null; then
    warnings+=("accept backlog not draining (Recv-Q=${recv_q})")
    critical=1
fi

if [ -n "${swap_total_kb:-0}" ] && [ "${swap_total_kb:-0}" -gt 0 ] \
   && [ "${swap_free_kb:-0}" -eq 0 ]; then
    warnings+=("swap fully exhausted")
    critical=1
fi

if [ -n "$pressure_full" ] && [ "$pressure_full" != "unknown" ]; then
    if awk "BEGIN{exit !($pressure_full > 40)}"; then
        warnings+=("heavy memory pressure (full avg10=${pressure_full})")
        [ "$critical" -eq 0 ] && critical=1
    elif awk "BEGIN{exit !($pressure_full > 10)}"; then
        warnings+=("elevated memory pressure (full avg10=${pressure_full})")
    fi
fi

if [ "${mem_available_kb:-0}" -lt 262144 ] 2>/dev/null; then
    warnings+=("low available memory ($(( mem_available_kb / 1024 ))MB)")
fi

# --- report ------------------------------------------------------------------

status="ok"
if [ "$critical" -ne 0 ]; then status="critical"; elif [ "${#warnings[@]}" -gt 0 ]; then status="degraded"; fi

if [ "$JSON_OUTPUT" -eq 1 ]; then
    printf '{"ts":"%s","status":"%s","http_code":"%s","recv_q":"%s","rss_kb":"%s","rss_mb_per_hour":"%s","pressure_full":"%s","pressure_some":"%s","swap_free_mb":%s,"swap_total_mb":%s,"mem_available_mb":%s,"warnings":[%s]}\n' \
        "$(date -Is)" "$status" "$http_code" "$recv_q" "$rss_kb" "$rss_trend" \
        "${pressure_full:-}" "${pressure_some:-}" \
        "$(( ${swap_free_kb:-0} / 1024 ))" "$(( ${swap_total_kb:-0} / 1024 ))" \
        "$(( ${mem_available_kb:-0} / 1024 ))" \
        "$(for w in "${warnings[@]:-}"; do [ -n "$w" ] && printf '"%s",' "$w"; done | sed 's/,$//')"
else
    echo "relay watch  $(date -Is)  [$status]"
    echo "  http          $http_code"
    echo "  recv-q        $recv_q"
    echo "  rss           ${rss_kb} kB   (trend ${rss_trend} MB/h)"
    echo "  pressure      full=${pressure_full} some=${pressure_some}"
    echo "  swap          $(( ${swap_free_kb:-0} / 1024 ))MB free of $(( ${swap_total_kb:-0} / 1024 ))MB"
    echo "  mem available $(( ${mem_available_kb:-0} / 1024 ))MB"
    for w in "${warnings[@]:-}"; do
        [ -n "$w" ] && echo "  WARN: $w"
    done
    if [ "$critical" -ne 0 ]; then
        echo
        echo "  Recovery if the relay is wedged:"
        echo "    sudo systemctl kill -s SIGKILL relay"
        echo "    sudo systemctl start relay"
    fi
fi

if [ "$critical" -ne 0 ]; then exit 2; fi
if [ "${#warnings[@]}" -gt 0 ]; then exit 1; fi
exit 0
