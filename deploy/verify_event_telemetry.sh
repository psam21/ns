#!/usr/bin/env bash
# Read-only post-deployment verification for the relay dashboard telemetry.
# Usage: HTTP_URL=https://www.nostr.ltd ./verify_event_telemetry.sh

set -Eeuo pipefail

HTTP_URL="${HTTP_URL:-http://localhost:8080}"
CURL_MAX_TIME="${CURL_MAX_TIME:-10}"

fail() {
    printf 'FAIL: %s\n' "$1" >&2
    exit 1
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

require_command curl
require_command jq

fetch_json() {
    local endpoint="$1"
    curl --fail --silent --show-error --max-time "$CURL_MAX_TIME" \
        -H 'Accept: application/json' "${HTTP_URL%/}${endpoint}"
}

printf 'Checking %s\n' "${HTTP_URL%/}"

nip11="$(curl --fail --silent --show-error --max-time "$CURL_MAX_TIME" \
    -H 'Accept: application/nostr+json' "${HTTP_URL%/}/")" \
    || fail 'NIP-11 endpoint did not return successfully'
nip_count="$(printf '%s' "$nip11" | jq -r '.supported_nips | length')"
[[ "$nip_count" == "77" ]] || fail "NIP-11 advertised $nip_count supported NIPs; expected 77"
printf 'PASS: NIP-11 advertises 77 supported NIPs\n'

stats="$(fetch_json /api/stats)" || fail '/api/stats did not return successfully'
stats_ready="$(printf '%s' "$stats" | jq -r '.stats.events_stored_ready // false')"
stats_count="$(printf '%s' "$stats" | jq -r '.stats.events_stored // 0')"
stats_status="$(printf '%s' "$stats" | jq -r '.stats.events_stored_status // "unknown"')"
[[ "$stats_ready" == "true" ]] || fail "/api/stats has no confirmed stored-event count (status=$stats_status)"
printf 'PASS: /api/stats stored-event count=%s status=%s\n' "$stats_count" "$stats_status"

events="$(fetch_json /api/events)" || fail '/api/events did not return successfully'
events_ready="$(printf '%s' "$events" | jq -r '.stored_events_ready // false')"
events_count="$(printf '%s' "$events" | jq -r '.stored_events // 0')"
events_status="$(printf '%s' "$events" | jq -r '.stored_events_status // "unknown"')"
[[ "$events_ready" == "true" ]] || fail "/api/events has no confirmed stored-event count (status=$events_status)"
printf 'PASS: /api/events stored-event count=%s status=%s\n' "$events_count" "$events_status"

archive_status="$(printf '%s' "$events" | jq -r '.status // "unknown"')"
printf 'INFO: grouped archive status=%s (it may warm independently)\n' "$archive_status"
printf '%s\n' 'Result: PASS'
