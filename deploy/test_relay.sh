#!/usr/bin/env bash
# Smoke-test a running Nostr relay. This publishes and deletes test events.

set -u

NAK="${NAK:-nak}"
RELAY="${RELAY_URL:-ws://localhost:8080}"
HTTP_URL="${HTTP_URL:-http://localhost:8080}"
passed=0
failed=0

pass() {
    printf '  PASS: %s\n' "$1"
    ((passed += 1))
}

fail() {
    printf '  FAIL: %s\n' "$1" >&2
    ((failed += 1))
}

require_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        printf 'Missing required command: %s\n' "$1" >&2
        exit 2
    fi
}

for command_name in "$NAK" curl jq python3; do
    require_command "$command_name"
done

SK=$("$NAK" key generate)
PK=$("$NAK" key public "$SK")

printf '=== NOSTR RELAY SMOKE TEST ===\n'
printf 'WebSocket: %s\n' "$RELAY"
printf 'HTTP:      %s\n' "$HTTP_URL"
printf 'Pubkey:    %s...\n\n' "${PK:0:16}"

printf 'TEST 1: NIP-11 Relay Info\n'
if INFO=$(curl --fail --silent --show-error --max-time 10 \
    -H 'Accept: application/nostr+json' "$HTTP_URL"); then
    if printf '%s' "$INFO" | jq -e '.name and (.supported_nips | type == "array")' >/dev/null; then
        printf '  Name: %s\n' "$(printf '%s' "$INFO" | jq -r '.name')"
        printf '  NIPs: %s\n' "$(printf '%s' "$INFO" | jq -r '.supported_nips | length')"
        pass 'NIP-11 response is valid JSON'
    else
        fail 'NIP-11 response is missing required fields'
    fi
else
    fail 'NIP-11 endpoint is unavailable'
fi

printf '\nTEST 2: Publish Text Note (Kind 1)\n'
if RESULT=$("$NAK" event --sec "$SK" -c "Hello from nostr.ltd smoke test $(date -u +%FT%TZ)" "$RELAY" </dev/null 2>&1); then
    printf '%s\n' "$RESULT" | head -3
    pass 'kind 1 accepted'
else
    printf '%s\n' "$RESULT"
    fail 'kind 1 rejected'
fi
sleep 1

printf '\nTEST 3: Query by Author\n'
if EVENTS=$("$NAK" req -a "$PK" -l 10 "$RELAY" </dev/null 2>&1); then
    printf '%s\n' "$EVENTS" | head -3
    pass 'author query completed'
else
    printf '%s\n' "$EVENTS"
    fail 'author query failed'
fi

printf '\nTEST 4: Publish Metadata (Kind 0)\n'
if RESULT=$("$NAK" event --sec "$SK" -k 0 -c '{"name":"testbot","about":"nostr.ltd relay smoke test"}' "$RELAY" </dev/null 2>&1); then
    printf '%s\n' "$RESULT" | head -3
    pass 'kind 0 accepted'
else
    printf '%s\n' "$RESULT"
    fail 'kind 0 rejected'
fi
sleep 1

printf '\nTEST 5: Query Kind 0\n'
if META=$("$NAK" req -k 0 -a "$PK" -l 1 "$RELAY" </dev/null 2>&1); then
    printf '%s\n' "$META" | head -2
    pass 'kind 0 query completed'
else
    printf '%s\n' "$META"
    fail 'kind 0 query failed'
fi

printf '\nTEST 6: Rapid Publishing (5 events)\n'
ok=0
for i in 1 2 3 4 5; do
    if RESULT=$("$NAK" event --sec "$SK" -c "Rapid smoke event #$i" "$RELAY" </dev/null 2>&1); then
        ((ok += 1))
    else
        printf '%s\n' "$RESULT"
    fi
done
printf '  Published: %d/5\n' "$ok"
if ((ok == 5)); then pass 'rapid publishing'; else fail 'rapid publishing'; fi
sleep 1

printf '\nTEST 7: Query Test Events\n'
if ALL=$("$NAK" req -a "$PK" -l 100 "$RELAY" </dev/null 2>&1); then
    COUNT=$(printf '%s\n' "$ALL" | grep -c 'content' || true)
    printf '  Total events by test key: %s\n' "$COUNT"
    if ((COUNT > 0)); then pass 'event query returned test events'; else fail 'event query returned no test events'; fi
else
    printf '%s\n' "$ALL"
    fail 'event query failed'
fi

printf '\nTEST 8: Delete One Event (NIP-09)\n'
EVID=$(printf '%s\n' "${ALL:-}" | head -1 | python3 -c 'import sys,json; print(json.loads(sys.stdin.readline())["id"])' 2>/dev/null || true)
if [ -n "$EVID" ]; then
    if DEL=$("$NAK" event --sec "$SK" -k 5 -e "$EVID" -c 'smoke test delete' "$RELAY" </dev/null 2>&1); then
        printf '  Deleted: %s...\n' "${EVID:0:16}"
        printf '%s\n' "$DEL" | head -2
        pass 'NIP-09 deletion accepted'
    else
        printf '%s\n' "$DEL"
        fail 'NIP-09 deletion rejected'
    fi
else
    fail 'could not identify an event to delete'
fi

printf '\n=== SUMMARY ===\n'
printf 'Passed: %d\n' "$passed"
printf 'Failed: %d\n' "$failed"

if ((failed > 0)); then
    exit 1
fi
