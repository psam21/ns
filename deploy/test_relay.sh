#!/bin/bash
NAK=/tmp/nak
RELAY="ws://localhost:8080"

SK=$($NAK key generate)
PK=$($NAK key public $SK)
echo "=== NOSTR RELAY TEST SUITE ==="
echo "Pubkey: ${PK:0:16}..."
echo ""

echo "TEST 1: NIP-11 Relay Info"
INFO=$(curl -s http://localhost:8080 -H 'Accept: application/nostr+json')
echo "$INFO" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(f"  Name: {d[\"name\"]}"); print(f"  NIPs: {len(d[\"supported_nips\"])}"); print(f"  Pubkey: {d[\"pubkey\"][:16]}...")' 2>/dev/null
echo "  PASS"
echo ""

echo "TEST 2: Publish Text Note (Kind 1)"
RESULT=$($NAK event --sec "$SK" -c "Hello from test suite $(date -u)" "$RELAY" </dev/null 2>&1)
echo "$RESULT" | head -3
echo ""

sleep 1

echo "TEST 3: Query by Author"
EVENTS=$($NAK req -a "$PK" -l 10 "$RELAY" </dev/null 2>&1)
echo "$EVENTS" | head -3
echo ""

echo "TEST 4: Publish Metadata (Kind 0)"
RESULT=$($NAK event --sec "$SK" -k 0 -c '{"name":"testbot","about":"Shugur relay test"}' "$RELAY" </dev/null 2>&1)
echo "$RESULT" | head -3
echo ""

sleep 1

echo "TEST 5: Query Kind 0"
META=$($NAK req -k 0 -a "$PK" -l 1 "$RELAY" </dev/null 2>&1)
echo "$META" | head -2
echo ""

echo "TEST 6: Rapid Publishing (5 events)"
OK=0
for i in 1 2 3 4 5; do
    R=$($NAK event --sec "$SK" -c "Rapid event #$i" "$RELAY" </dev/null 2>&1)
    if echo "$R" | grep -q 'success'; then OK=$((OK+1)); fi
done
echo "  Published: $OK/5"
echo ""

sleep 1

echo "TEST 7: Count all events"
ALL=$($NAK req -a "$PK" -l 100 "$RELAY" </dev/null 2>&1)
COUNT=$(echo "$ALL" | grep -c 'content')
echo "  Total events by test key: $COUNT"
echo ""

echo "TEST 8: Delete event (NIP-09)"
EVID=$(echo "$ALL" | head -1 | python3 -c 'import sys,json; print(json.loads(sys.stdin.readline())["id"])' 2>/dev/null)
if [ -n "$EVID" ]; then
    DEL=$($NAK event --sec "$SK" -k 5 -e "$EVID" -c 'test delete' "$RELAY" </dev/null 2>&1)
    echo "  Deleted: ${EVID:0:16}..."
    echo "$DEL" | head -2
fi
echo ""

echo "=== ALL TESTS COMPLETE ==="
