#!/usr/bin/env bash
# Validation matrix for #112
# Runs all 12 validation rows from a clean checkout and reports pass/fail.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NS_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

PASS=0
FAIL=0

run_row() {
    local row_num="$1"
    local row_name="$2"
    local cmd="$3"

    echo ""
    echo "=========================================="
    echo "Row $row_num: $row_name"
    echo "=========================================="

    if (eval "$cmd") > "$SCRIPT_DIR/row-${row_num}.out" 2>&1; then
        echo "RESULT: PASS"
        PASS=$((PASS + 1))
    else
        local exit_code=$?
        echo "RESULT: FAIL (exit code: $exit_code)"
        FAIL=$((FAIL + 1))
    fi
}

# Row 1: Shell syntax
run_row 1 "Shell syntax" \
    "cd '$NS_DIR' && find deploy relay/tests blossom -name '*.sh' -exec bash -n {} \;"

# Row 2: YAML structure
run_row 2 "YAML structure" \
    "cd '$NS_DIR' && python3 -c 'import yaml; yaml.safe_load(open(\"blossom/config.yml\")); yaml.safe_load(open(\"deploy/config.yaml\")); yaml.safe_load(open(\"relay/.github/workflows/ci.yml\"))'"

# Row 3: Go unit tests
run_row 3 "Go unit tests" \
    "cd '$NS_DIR/relay' && go test ./..."

# Row 4: Go static checks
run_row 4 "Go static checks" \
    "cd '$NS_DIR/relay' && go vet ./..."

# Row 5: ARM64 build
run_row 5 "ARM64 build" \
    "cd '$NS_DIR/relay' && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /tmp/relay-arm64-test ./cmd"

# Row 6: Blossom build
run_row 6 "Blossom build" \
    "cd '$NS_DIR/blossom' && pnpm install --frozen-lockfile && pnpm build"

# Row 7: Blossom upload tests
run_row 7 "Blossom upload tests" \
    "cd '$NS_DIR/blossom' && node --test src/api/upload.test.mjs src/rules/index.test.mjs"

# Row 8: Relay endpoint checks (production)
run_row 8 "Relay endpoint checks" \
    "curl -s -H 'Accept: application/nostr+json' https://nostr.ltd | python3 -c 'import sys,json; d=json.load(sys.stdin); assert d.get(\"name\") and len(d.get(\"supported_nips\", [])) == 77' && curl -s https://nostr.ltd/api/events | python3 -c 'import sys,json; d=json.load(sys.stdin); assert d.get(\"relay_health\") == \"ok\"; assert d.get(\"stored_events_ready\") == True'"

# Row 9: NIP harness (static coverage)
run_row 9 "NIP harness" \
    "cd '$NS_DIR/relay' && ./tests/nips/run_coverage.sh --static"

# Row 10: Security checks
run_row 10 "Security checks" \
    "python3 -c '
import os
files = []
for root, dirs, fs in os.walk(\"$NS_DIR/relay/internal\"):
    for f in fs:
        if f.endswith(\".go\"):
            p = os.path.join(root, f)
            with open(p) as fh:
                if \"crypto/rand\" in fh.read():
                    files.append(p)
assert files, \"No crypto/rand usage found\"
print(\"crypto/rand found in:\", len(files), \"files\")

files2 = []
for root, dirs, fs in os.walk(\"$NS_DIR/blossom/src\"):
    for f in fs:
        if f.endswith(\".ts\"):
            p = os.path.join(root, f)
            with open(p) as fh:
                if \"X-Content-Type-Options\" in fh.read():
                    files2.append(p)
assert files2, \"No X-Content-Type-Options header found\"
print(\"X-Content-Type-Options found in:\", len(files2), \"files\")
'"

# Row 11: Deployment rollback (script syntax + structure)
run_row 11 "Deployment rollback" \
    "bash -n '$NS_DIR/deploy/ns-deploy-full.sh' && python3 -c 'content=open(\"$NS_DIR/deploy/ns-deploy-full.sh\").read(); assert \"restore\" in content.lower() or \"rollback\" in content.lower(), \"No rollback logic found\"; print(\"Rollback logic found\")'"

# Row 12: Public smoke tests
run_row 12 "Public smoke tests" \
    "python3 -c '
import urllib.request
for url in [\"https://nostr.ltd\", \"https://blossom.nostr.ltd\"]:
    r = urllib.request.urlopen(url, timeout=10)
    assert r.status == 200, f\"{url} returned {r.status}\"
    print(f\"{url}: HTTP {r.status}\")
'"

echo ""
echo "=========================================="
echo "VALIDATION MATRIX SUMMARY"
echo "=========================================="
echo "PASS: $PASS"
echo "FAIL: $FAIL"
echo "Total: $((PASS + FAIL))"
echo ""

if [ "$FAIL" -eq 0 ]; then
    echo "ALL ROWS PASSED"
    exit 0
else
    echo "SOME ROWS FAILED"
    exit 1
fi
