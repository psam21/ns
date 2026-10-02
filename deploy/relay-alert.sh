#!/usr/bin/env bash
#
# relay-alert.sh -- notify on relay health transitions.
#
# The watcher (relay-memory-watch.sh) already detects every condition worth
# knowing about and signals it through its exit code: 0 ok, 1 degraded, 2
# critical. That exit code went nowhere, so a wedge could sit undetected
# indefinitely. This consumes it.
#
# The problem this has to solve: relay-watch.timer runs every 5 minutes, so a
# condition that persists for an hour is the same finding 12 times. An
# operator who is paged 288 times a day stops reading the alerts, which makes
# the alerting worse than none at all.
#
# So alerts are *edge-triggered* on status transitions, not level-triggered:
#
#   ok -> degraded      one alert
#   degraded -> ok      one recovery alert
#   degraded -> critical one escalation alert
#   critical -> critical  suppressed
#
# The same condition, repeated, stays silent. Recovery is always announced,
# because "the page was real and it is over" is the message an operator most
# needs and the one least likely to arrive on its own.
#
# Delivery is a webhook (Discord/Slack-compatible JSON). It is best-effort by
# design: a failed notification must never affect the host, and must never
# cause the watcher to report a different health status than it measured.
#
# Exit codes: always 0. A notifier that fails is not a health finding, and
# wiring it into the watcher's status would let a bad webhook turn a healthy
# relay into an alerting one.
#
# Environment:
#   ALERT_WEBHOOK        required to send anything; unset = log only, exit 0
#   ALERT_STATE          state file (default /var/lib/relay-watch/alert-state)
#   ALERT_COOLDOWN       min seconds between alerts of the same kind (300)
#   ALERT_LABEL          prefix identifying this host in the message
#
set -uo pipefail

# Never let a notification attempt take the host down or hang a timer.
# connect-timeout caps the DNS+TCP phase, max-time caps the whole request
# including a slow response body, so a hung endpoint cannot outlive the timer.
CURL_OPTS=(--silent --show-error --fail
           --connect-timeout 5 --max-time 15
           --retry 1 --retry-delay 2)

STATE="${ALERT_STATE:-/var/lib/relay-watch/alert-state}"
COOLDOWN="${ALERT_COOLDOWN:-300}"
LABEL="${ALERT_LABEL:-nostr.ltd}"

# --- input -----------------------------------------------------------------

# The status is taken as an argument, not inferred, so this script cannot
# disagree with the watcher about what it measured.
status="${1:-}"
case "$status" in
    ok|degraded|critical) ;;
    *)
        echo "usage: $0 {ok|degraded|critical} [detail]" >&2
        exit 0   # not 2: a usage error is a caller bug, not a health alarm
        ;;
esac
detail="${2:-}"

# --- state -----------------------------------------------------------------

prev=""
if [ -r "$STATE" ]; then
    read -r prev < "$STATE" || prev=""
fi

now=$(date +%s)

# The state file is written unconditionally, including when the alert is
# suppressed. Forgetting to update it is how a change-detection mechanism
# silently becomes a no-op after one run.
mkdir -p "$(dirname "$STATE")" 2>/dev/null || true
if printf '%s\n' "$status" > "$STATE" 2>/dev/null; then
    :
else
    echo "alert: cannot write state file $STATE; alerts will repeat every run" >&2
fi

# --- should we alert? ------------------------------------------------------

kind=""
if [ "$prev" = "$status" ]; then
    # No transition. This is the common case, and staying quiet here is the
    # entire point of the design.
    exit 0
fi

case "$status" in
    critical)
        if [ "$prev" = "degraded" ]; then
            kind="ESCALATION"
        else
            kind="CRITICAL"
        fi
        ;;
    degraded)
        # Do not re-announce degraded while already critical. Recovering from
        # critical straight to ok is a recovery; critical -> degraded is still
        # a problem worth naming.
        if [ "$prev" = "critical" ]; then
            kind="still-degraded"
        else
            kind="DEGRADED"
        fi
        ;;
    ok)
        # Always announce recovery, but only if we were previously alerting.
        # An ok -> ok run is silence.
        if [ "$prev" = "critical" ] || [ "$prev" = "degraded" ]; then
            kind="RECOVERY"
        else
            exit 0
        fi
        ;;
esac

# Cooldown: a flapping condition must not become a page storm. Bypassed for
# RECOVERY, because withholding "it is fixed" is strictly worse than one extra
# message.
if [ "$kind" != "RECOVERY" ]; then
    stamp="${STATE}.${kind}"
    last=0
    if [ -r "$stamp" ]; then
        read -r last < "$stamp" || last=0
        # Defend against a corrupt or non-numeric stamp: arithmetic on a
        # non-numeric value is a shell error that would abort the script
        # before it alerts.
        case "$last" in
            ''|*[!0-9]*) last=0 ;;
        esac
    fi
    if [ $(( now - last )) -lt "$COOLDOWN" ]; then
        exit 0
    fi
    printf '%s\n' "$now" > "$stamp" 2>/dev/null || true
fi

# --- message ---------------------------------------------------------------

host=$(hostname -s 2>/dev/null || echo unknown)
ts=$(date -Is)

title="$LABEL relay $kind"
if [ -n "$detail" ]; then
    msg="$title

$detail

host: $host
previous status: ${prev:-none (first run)}
watcher ran: $ts"
else
    msg="$title

host: $host
previous status: ${prev:-none (first run)}
watcher ran: $ts"
fi

# Colour by severity. Deliberately not relying on an emoji for the signal:
# the severity is also in the text, so it survives a client that does not
# render them.
#
# Decimal, not hex. Discord/Slack want a JSON integer, and `0xE01B24` is not
# valid JSON -- passing it to `jq --argjson` makes jq reject the whole payload
# and the alert silently never send. That is the same class of bug as the
# malformed escaping this replaced: the failure is invisible unless something
# actually parses the result.
case "$kind" in
    CRITICAL|ESCALATION) colour=14687012 ;;  # red
    DEGRADED)            colour=16098851 ;;  # amber
    RECOVERY)            colour=3066993  ;;  # green
    *)                   colour=3447003  ;;  # blue
esac

# --- send ------------------------------------------------------------------

if [ -z "${ALERT_WEBHOOK:-}" ]; then
    # No webhook configured. Log loudly so the operator knows alerting is
    # inert rather than assuming it is armed.
    echo "ALERT (not sent, ALERT_WEBHOOK unset): $msg"
    exit 0
fi

# --- build the payload ------------------------------------------------------
#
# Built with jq, not by string interpolation.
#
# The first version of this assembled the JSON with printf and a hand-rolled
# sed escape. A test against a real receiver proved it emitted invalid JSON:
# only `msg` was escaped, so the *title* -- interpolated a second time, into
# "content" and into "embeds[].title" -- went in raw, and the newlines were
# double-escaped into a literal \n that rendered as "\n" on screen instead of
# a line break. It "sent successfully" because curl reported HTTP 200 from a
# receiver that never parsed the body.
#
# That is the failure mode worth naming: an endpoint that accepts malformed
# JSON makes a broken payload indistinguishable from a working one. The test
# has to *parse* what was sent, not observe that it was sent.
#
# jq is present on the production host (1.7). If it is somehow absent, fall
# back to a payload with no interpolated strings at all, so the alert still
# arrives as a real notification rather than being lost or malformed.
if command -v jq >/dev/null 2>&1; then
    payload=$(printf '%s' "$msg" | jq -Rs \
        --arg title "$title" \
        --arg username "$LABEL relay-watch" \
        --argjson colour "$colour" \
        '{content: $title, username: $username,
          embeds: [{title: $title, description: ., color: $colour}]}')
else
    echo "alert: jq unavailable; sending title-only payload" >&2
    payload=$(printf '{"content":"%s relay health alert (see journal for detail)","username":"relay-watch"}' \
        "$LABEL")
fi

if [ -z "$payload" ]; then
    echo "alert: payload construction produced nothing; not sending" >&2
    exit 0
fi

if out=$(curl "${CURL_OPTS[@]}" -X POST -H 'Content-Type: application/json' \
        -d "$payload" "$ALERT_WEBHOOK" 2>&1); then
    echo "alert sent: $kind"
else
    # Report to stderr and exit 0. A webhook outage must not change the
    # health status, must not fail the timer, and must not retry into a loop.
    echo "alert FAILED to send ($kind): $out" >&2
fi

exit 0
