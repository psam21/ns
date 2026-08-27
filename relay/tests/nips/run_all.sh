#!/usr/bin/env bash
# Run every NIP integration script against an explicitly configured relay.

set -u

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
RELAY_URL="${RELAY_URL:-ws://localhost:8080}"
HTTP_URL="${HTTP_URL:-http://localhost:8080}"

required_commands=(bash curl jq nak)
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

printf 'NIP integration suite\n'
printf 'Relay: %s\n' "$RELAY_URL"
printf 'HTTP:  %s\n\n' "$HTTP_URL"

if ! curl --fail --silent --show-error --max-time 10 \
    -H 'Accept: application/nostr+json' "$HTTP_URL" >/dev/null; then
    printf 'Relay HTTP endpoint is not reachable: %s\n' "$HTTP_URL" >&2
    exit 2
fi

mapfile -t tests < <(find "$SCRIPT_DIR" -maxdepth 1 -type f -name 'test_nip*.sh' -print | sort)
if ((${#tests[@]} == 0)); then
    printf 'No NIP test scripts found in %s\n' "$SCRIPT_DIR" >&2
    exit 2
fi

passed=0
failed=0

for test_file in "${tests[@]}"; do
    test_name=$(basename "$test_file")
    printf '\n=== %s ===\n' "$test_name"
    if RELAY_URL="$RELAY_URL" HTTP_URL="$HTTP_URL" \
        bash "$test_file"; then
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
