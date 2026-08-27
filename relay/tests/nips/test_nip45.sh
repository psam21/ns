#!/usr/bin/env bash
# NIP-45 COUNT command integration tests.

set -u

RELAY="${RELAY:-${RELAY_URL:-ws://localhost:8080}}"
passed=0
failed=0

check_count() {
    local label="$1"
    shift
    local output
    if output=$(nak count "$RELAY" "$@" 2>&1); then
        if printf '%s\n' "$output" | tail -1 | grep -Eq '(^|: )[0-9]+$|"count"[[:space:]]*:[[:space:]]*[0-9]+'; then
            printf 'PASS: %s (%s)\n' "$label" "$(printf '%s' "$output" | tail -1)"
            passed=$((passed + 1))
        else
            printf 'FAIL: %s (unexpected COUNT response)\n%s\n' "$label" "$output" >&2
            failed=$((failed + 1))
        fi
    else
        printf 'FAIL: %s\n%s\n' "$label" "$output" >&2
        failed=$((failed + 1))
    fi
}

printf '=== NIP-45 COUNT Command Tests ===\n'
printf 'Relay: %s\n\n' "$RELAY"

check_count 'basic COUNT request for kind 1 events' -k 1
check_count 'COUNT with author filter' -k 1 -a 79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798
check_count 'COUNT for kind 10002 relay lists' -k 10002
check_count 'COUNT with multiple kinds' -k 1 -k 10002
check_count 'COUNT with time range' -k 1 --since "$(date -d '24 hours ago' +%s)"

printf '\nSummary: passed=%d failed=%d\n' "$passed" "$failed"
((failed == 0))
