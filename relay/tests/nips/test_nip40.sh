#!/usr/bin/env bash
# NIP-40: Expiration Timestamp integration tests.

set -u

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
RELAY_ROOT=$(cd -- "$SCRIPT_DIR/../.." && pwd)
RELAY="${RELAY:-${RELAY_URL:-ws://localhost:8080}}"
TEST_TIMEOUT="${TEST_TIMEOUT:-30}"
passed=0
failed=0

pass() {
    printf 'PASS: %s\n' "$1"
    passed=$((passed + 1))
}

fail() {
    printf 'FAIL: %s\n' "$1" >&2
    failed=$((failed + 1))
}

printf '=== NIP-40 Expiration Timestamp Tests ===\n'
printf 'Relay: %s\n\n' "$RELAY"

if grep -q 'func GetExpirationTime' "$RELAY_ROOT/internal/relay/nips/nip40.go" && \
   grep -q 'func IsExpired' "$RELAY_ROOT/internal/relay/nips/nip40.go" && \
   grep -q 'func ValidateExpirationTag' "$RELAY_ROOT/internal/relay/nips/nip40.go"; then
    pass 'NIP-40 helper implementation is present'
else
    fail 'NIP-40 helper implementation is missing'
fi

if grep -q 'GetExpirationTime' "$RELAY_ROOT/internal/relay/plugin_validator.go" && \
   grep -q 'event has expired' "$RELAY_ROOT/internal/relay/plugin_validator.go"; then
    pass 'NIP-40 validation is integrated into the relay validator'
else
    fail 'NIP-40 validation is not integrated into the relay validator'
fi

SECRET_KEY="$(nak key generate)"
FUTURE_EXPIRATION=$(( $(date +%s) + 3600 ))
PAST_EXPIRATION=$(( $(date +%s) - 3600 ))

if output=$(timeout --foreground "$TEST_TIMEOUT" nak event -k 1 -c 'NIP-40 future expiration test' \
    -t expiration="$FUTURE_EXPIRATION" --sec "$SECRET_KEY" "$RELAY" 2>&1) && [[ "$output" == *success* ]]; then
    pass 'future expiration event is accepted'
else
    fail 'future expiration event was not accepted'
fi

if output=$(timeout --foreground "$TEST_TIMEOUT" nak event -k 1 -c 'NIP-40 expired event test' \
    -t expiration="$PAST_EXPIRATION" --sec "$SECRET_KEY" "$RELAY" 2>&1); then
    if [[ "$output" == *'event has expired'* || "$output" == *'rejected'* || "$output" == *'failed'* ]]; then
        pass 'expired event is rejected'
    else
        fail 'expired event was accepted unexpectedly'
    fi
else
    if [[ "$output" == *'event has expired'* || "$output" == *'rejected'* || "$output" == *'failed'* ]]; then
        pass 'expired event is rejected'
    else
        fail 'expired event test did not produce an identifiable rejection'
    fi
fi

if output=$(timeout --foreground "$TEST_TIMEOUT" nak event -k 1 -c 'NIP-40 invalid expiration test' \
    -t expiration=not-a-timestamp --sec "$SECRET_KEY" "$RELAY" 2>&1); then
    if [[ "$output" == *'failed: msg:'* || "$output" == *'invalid expiration tag'* || "$output" == *'rejected'* ]]; then
        pass 'malformed expiration tag is rejected'
    else
        fail 'malformed expiration tag was accepted unexpectedly'
    fi
else
    if [[ "$output" == *'failed: msg:'* || "$output" == *'invalid expiration tag'* || "$output" == *'rejected'* ]]; then
        pass 'malformed expiration tag is rejected'
    else
        fail 'malformed expiration test did not produce an identifiable rejection'
    fi
fi

printf '\nSummary: passed=%d failed=%d\n' "$passed" "$failed"
((failed == 0))
