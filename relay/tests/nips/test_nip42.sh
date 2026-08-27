#!/usr/bin/env bash
# NIP-42: Client Authentication integration test.

set -u

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
RELAY_ROOT=$(cd -- "$SCRIPT_DIR/../.." && pwd)
RELAY="${NIP_AUTH_UPSTREAM_URL:-${RELAY_URL:-ws://localhost:8080}}"
AUTH_RELAY_URL="${NIP_AUTH_RELAY_URL:-$RELAY}"
PROBE_BINARY="${NIP_AUTH_PROBE_BINARY:-${TMPDIR:-/tmp}/nostr-nip42-probe}"

if [[ ! -x "$PROBE_BINARY" ]]; then
    if ! command -v go >/dev/null 2>&1; then
        printf 'FAIL: NIP-42 probe binary is unavailable and go is not installed\n' >&2
        exit 2
    fi
    if ! (cd "$RELAY_ROOT" && CGO_ENABLED=0 go build -trimpath -o "$PROBE_BINARY" ./tests/nips/tools/nip42_probe); then
        printf 'FAIL: could not build NIP-42 probe\n' >&2
        exit 2
    fi
fi

printf '=== NIP-42 Client Authentication Test ===\n'
printf 'Relay: %s\n' "$RELAY"
printf 'AUTH relay tag: %s\n' "$AUTH_RELAY_URL"

if output=$(timeout --foreground "${TEST_TIMEOUT:-60}" "$PROBE_BINARY" -relay "$RELAY" -auth-relay "$AUTH_RELAY_URL" 2>&1); then
    printf 'PASS: NIP-42 AUTH challenge and signed acknowledgement\n%s\n' "$output"
else
    printf 'FAIL: NIP-42 AUTH challenge or acknowledgement\n%s\n' "$output" >&2
    exit 1
fi
