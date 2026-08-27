#!/usr/bin/env bash
# NIP-45 COUNT command integration tests.

set -u

RELAY="${RELAY:-${RELAY_URL:-ws://localhost:8080}}"
COUNT_TIMEOUT="${NIP45_COUNT_TIMEOUT:-15}"
COUNT_WINDOW_SECONDS="${NIP45_COUNT_WINDOW_SECONDS:-86400}"
COUNT_SINCE=$(( $(date +%s) - COUNT_WINDOW_SECONDS ))
passed=0
failed=0

check_count() {
    local label="$1"
    shift
    local output status
    if output=$(timeout --foreground "$COUNT_TIMEOUT" nak count "$RELAY" "$@" 2>&1); then
        if printf '%s\n' "$output" | tail -1 | grep -Eq '(^|: )[0-9]+$|"count"[[:space:]]*:[[:space:]]*[0-9]+'; then
            printf 'PASS: %s (%s)\n' "$label" "$(printf '%s' "$output" | tail -1)"
            passed=$((passed + 1))
        else
            printf 'FAIL: %s (unexpected COUNT response)\n%s\n' "$label" "$output" >&2
            failed=$((failed + 1))
        fi
    else
        status=$?
        if [[ "$status" -eq 124 ]]; then
            printf 'FAIL: %s (COUNT client timeout after %ss; relay did not answer)\n%s\n' "$label" "$COUNT_TIMEOUT" "$output" >&2
        else
            printf 'FAIL: %s (nak exit %s)\n%s\n' "$label" "$status" "$output" >&2
        fi
        failed=$((failed + 1))
    fi
}

printf '=== NIP-45 COUNT Command Tests ===\n'
printf 'Relay: %s\n' "$RELAY"
printf 'COUNT window: last %ss\n' "$COUNT_WINDOW_SECONDS"
printf 'Client timeout: %ss\n\n' "$COUNT_TIMEOUT"

check_count 'bounded COUNT request for kind 1 events' -k 1 --since "$COUNT_SINCE"
check_count 'COUNT with author filter' -k 1 -a 79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798
check_count 'COUNT for kind 10002 relay lists' -k 10002
check_count 'bounded COUNT with multiple kinds' -k 1 -k 10002 --since "$COUNT_SINCE"
check_count 'COUNT with time range' -k 1 --since "$COUNT_SINCE"

printf '\nSummary: passed=%d failed=%d\n' "$passed" "$failed"
((failed == 0))
