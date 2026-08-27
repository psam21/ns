#!/usr/bin/env bash
# Run every NIP integration script against an explicitly configured relay.

set -u

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
RELAY_ROOT=$(cd -- "$SCRIPT_DIR/../.." && pwd)
RELAY_URL="${RELAY_URL:-ws://localhost:8080}"
HTTP_URL="${HTTP_URL:-http://localhost:8080}"
TEST_TIMEOUT="${TEST_TIMEOUT:-120}"
NIP_AUTH_BRIDGE="${NIP_AUTH_BRIDGE:-true}"
NIP_AUTH_RELAY_URL="${NIP_AUTH_RELAY_URL:-$RELAY_URL}"
UPSTREAM_RELAY_URL="$RELAY_URL"

required_commands=(bash curl jq timeout)
missing=()
for command_name in "${required_commands[@]}"; do
    if ! command -v "$command_name" >/dev/null 2>&1; then
        missing+=("$command_name")
    fi
done
if ((${#missing[@]} > 0)); then
    printf 'Missing required command(s): %s\n' "${missing[*]}" >&2
    printf 'Install the missing tools, then rerun the suite.\n' >&2
    exit 2
fi

resolve_nak() {
    if [[ -n "${NAK_BIN:-}" ]]; then
        if [[ -x "$NAK_BIN" ]]; then
            export PATH="$(dirname -- "$NAK_BIN"):$PATH"
            return 0
        fi
        printf 'NAK_BIN is not executable: %s\n' "$NAK_BIN" >&2
        return 1
    fi
    if command -v nak >/dev/null 2>&1; then
        return 0
    fi
    local candidate
    for candidate in "$RELAY_ROOT/bin/nak" "$RELAY_ROOT/tests/nips/bin/nak"; do
        if [[ -x "$candidate" ]]; then
            export PATH="$(dirname -- "$candidate"):$PATH"
            return 0
        fi
    done
    if command -v go >/dev/null 2>&1; then
        candidate="$(go env GOPATH 2>/dev/null)/bin/nak"
        if [[ -x "$candidate" ]]; then
            export PATH="$(dirname -- "$candidate"):$PATH"
            return 0
        fi
    fi
    printf 'Missing required command: nak\n' >&2
    printf 'Set NAK_BIN or install github.com/fiatjaf/nak@v0.20.6, then rerun the suite.\n' >&2
    return 1
}
if ! resolve_nak; then
    exit 2
fi

MATRIX="$SCRIPT_DIR/coverage.tsv"
printf 'NIP integration suite\n'
printf 'Coverage matrix: %s\n' "$MATRIX"
printf 'Relay: %s\n' "$RELAY_URL"
printf 'HTTP:  %s\n' "$HTTP_URL"
printf 'Per-test timeout: %ss\n' "$TEST_TIMEOUT"
printf 'NIP-42 auth bridge: %s\n\n' "$NIP_AUTH_BRIDGE"

if ! curl --fail --silent --show-error --max-time 10 \
    -H 'Accept: application/nostr+json' "$HTTP_URL" >/dev/null; then
    printf 'Relay HTTP endpoint is not reachable: %s\n' "$HTTP_URL" >&2
    exit 2
fi

if [[ ! -f "$MATRIX" ]]; then
    printf 'Coverage matrix is missing: %s\n' "$MATRIX" >&2
    exit 2
fi
mapfile -t tests < <(awk -F '\t' -v dir="$SCRIPT_DIR" '$1 !~ /^#/ && NF >= 5 && $3 == "integration" {print dir "/" $4}' "$MATRIX" | sort -u)
if ((${#tests[@]} == 0)); then
    printf 'No integration tests are declared in %s\n' "$MATRIX" >&2
    exit 2
fi
for test_file in "${tests[@]}"; do
    if [[ ! -x "$test_file" ]]; then
        printf 'Integration test is missing or not executable: %s\n' "$test_file" >&2
        exit 2
    fi
done

passed=0
failed=0
bridge_pid=""
bridge_log=""
auth_probe_binary=""
test_relay_url="$RELAY_URL"

cleanup_bridge() {
    if [[ -n "$bridge_pid" ]]; then
        kill "$bridge_pid" 2>/dev/null || true
        wait "$bridge_pid" 2>/dev/null || true
    fi
    if [[ -n "$bridge_log" && "${VERBOSE:-0}" == "1" && -s "$bridge_log" ]]; then
        printf '\nNIP-42 bridge log:\n' >&2
        sed -n '1,120p' "$bridge_log" >&2
    fi
}
trap cleanup_bridge EXIT INT TERM

if [[ "$NIP_AUTH_BRIDGE" == "true" && "$RELAY_URL" =~ ^wss?:// ]]; then
    if ! command -v go >/dev/null 2>&1; then
        printf 'NIP_AUTH_BRIDGE requires go to build the authenticated proxy\n' >&2
        exit 2
    fi
    bridge_binary="${NIP_AUTH_BRIDGE_BINARY:-${TMPDIR:-/tmp}/nostr-nip-relay-proxy}"
    auth_probe_binary="${NIP_AUTH_PROBE_BINARY:-${TMPDIR:-/tmp}/nostr-nip42-probe}"
    bridge_log="${TMPDIR:-/tmp}/nostr-nip-relay-proxy.$$.log"
    if ! (cd "$RELAY_ROOT" && CGO_ENABLED=0 go build -trimpath -o "$bridge_binary" ./tests/nips/tools/relay_proxy); then
        printf 'failed to build the NIP-42 authenticated proxy\n' >&2
        exit 2
    fi
    if ! (cd "$RELAY_ROOT" && CGO_ENABLED=0 go build -trimpath -o "$auth_probe_binary" ./tests/nips/tools/nip42_probe); then
        printf 'failed to build the NIP-42 probe\n' >&2
        exit 2
    fi
    bridge_port="${NIP_AUTH_BRIDGE_PORT:-18080}"
    "$bridge_binary" --listen "127.0.0.1:$bridge_port" --upstream "$RELAY_URL" --relay-url "$NIP_AUTH_RELAY_URL" >"$bridge_log" 2>&1 &
    bridge_pid=$!
    ready=false
    for _ in {1..20}; do
        if timeout 1 bash -c "</dev/tcp/127.0.0.1/$bridge_port" 2>/dev/null; then
            ready=true
            break
        fi
        sleep 0.25
    done
    if [[ "$ready" != true ]]; then
        printf 'NIP-42 authenticated proxy did not start on port %s\n' "$bridge_port" >&2
        exit 2
    fi
    test_relay_url="ws://127.0.0.1:$bridge_port"
    printf 'Authenticated test bridge: %s -> %s\n' "$test_relay_url" "$RELAY_URL"
fi

for test_file in "${tests[@]}"; do
	test_name=$(basename "$test_file")
	printf '\n=== %s ===\n' "$test_name"
	run_relay_url="$test_relay_url"
	if [[ "$test_name" == "test_nip17.sh" || "$test_name" == "test_nip42.sh" || "$test_name" == "test_nip59.sh" ]]; then
		# These tests must authenticate directly with their generated or probe
		# identity, so do not hide the upstream session behind the bridge signer.
		run_relay_url="$RELAY_URL"
	fi
	if RELAY_URL="$run_relay_url" HTTP_URL="$HTTP_URL" \
		NIP_AUTH_UPSTREAM_URL="$UPSTREAM_RELAY_URL" NIP_AUTH_RELAY_URL="$NIP_AUTH_RELAY_URL" \
		NIP_AUTH_PROBE_BINARY="$auth_probe_binary" \
		timeout --foreground "$TEST_TIMEOUT" bash "$test_file"; then
        ((passed += 1))
        printf 'PASS: %s\n' "$test_name"
    else
        ((failed += 1))
        printf 'FAIL: %s\n' "$test_name" >&2
    fi
done

printf '\n=== SUMMARY ===\n'
printf 'Total:  %d\n' "${#tests[@]}"
printf 'Passed: %d\n' "$passed"
printf 'Failed: %d\n' "$failed"

if ((failed > 0)); then
    exit 1
fi
