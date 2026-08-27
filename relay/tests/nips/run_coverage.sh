#!/usr/bin/env bash
# Validate the complete advertised-NIP coverage matrix.
# Static mode is safe and runs without a live relay. Use --live only against an
# isolated or explicitly authorized relay because integration tests mutate data.

set -u

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
RELAY_ROOT=$(cd -- "$SCRIPT_DIR/../.." && pwd)
MATRIX="$SCRIPT_DIR/coverage.tsv"
RELAY_URL="${RELAY_URL:-ws://localhost:8080}"
HTTP_URL="${HTTP_URL:-http://localhost:8080}"
LIVE=false

if [[ "${1:-}" == "--live" ]]; then
    LIVE=true
elif [[ "${1:-}" != "" && "${1:-}" != "--static" ]]; then
    printf 'Usage: %s [--static|--live]\n' "$0" >&2
    exit 2
fi

failures=0

fail() {
    printf 'FAIL: %s\n' "$1" >&2
    ((failures += 1))
}

require_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        fail "missing command: $1"
    fi
}

printf 'Full NIP coverage validation\n'
printf 'Matrix: %s\n' "$MATRIX"
printf 'Mode:   %s\n\n' "$([[ "$LIVE" == true ]] && printf live || printf static)"

for command_name in awk sort comm find grep sed bash go node; do
    require_command "$command_name"
done
if [[ "$LIVE" == true ]]; then
    for command_name in curl jq nak; do
        require_command "$command_name"
    done
fi

if [[ ! -f "$MATRIX" ]]; then
    fail "coverage matrix is missing: $MATRIX"
else
    mapfile -t matrix_ids < <(awk -F '\t' '$1 !~ /^#/ && NF >= 5 {print $1}' "$MATRIX")
    if [[ "${#matrix_ids[@]}" -ne 77 ]]; then
        fail "coverage matrix contains ${#matrix_ids[@]} rows; expected 77"
    else
        printf 'PASS: coverage matrix contains 77 rows\n'
    fi

    duplicate_ids=$(printf '%s\n' "${matrix_ids[@]}" | sort | uniq -d)
    if [[ -n "$duplicate_ids" ]]; then
        fail "duplicate matrix identifiers: $duplicate_ids"
    else
        printf 'PASS: coverage matrix identifiers are unique\n'
    fi

    source_ids=$(sed -n '/var DefaultSupportedNIPs/,/^}/p' "$RELAY_ROOT/internal/constants/relay_metadata.go" | grep -oE 'NIP-[0-9A-Z]+' | sort -u)
    matrix_ids_sorted=$(printf '%s\n' "${matrix_ids[@]}" | sort -u)
    if [[ "$source_ids" == "$matrix_ids_sorted" ]]; then
        printf 'PASS: coverage matrix matches DefaultSupportedNIPs\n'
    else
        fail 'coverage matrix does not match DefaultSupportedNIPs'
        printf 'Source identifiers:\n%s\n' "$source_ids" >&2
        printf 'Matrix identifiers:\n%s\n' "$matrix_ids_sorted" >&2
    fi

    invalid_rows=$(awk -F '\t' '$1 !~ /^#/ && (NF != 5 || $1 !~ /^NIP-[0-9A-Z]+$/ || $2 !~ /^(relay-event|relay-control|client-ecosystem|blossom)$/ || $3 !~ /^(integration|contract|manual|external)$/ || $4 == "" || $5 == "") {print NR ":" $0}' "$MATRIX")
    if [[ -n "$invalid_rows" ]]; then
        fail "invalid coverage matrix rows:\n$invalid_rows"
    else
        printf 'PASS: coverage matrix schema is valid\n'
    fi

    automated_rows=0
    manual_rows=0
    while IFS=$'\t' read -r nip area execution evidence notes; do
        [[ -z "$nip" || "$nip" == \#* ]] && continue
        case "$execution" in
            integration)
                ((automated_rows += 1))
                if [[ ! -x "$SCRIPT_DIR/$evidence" ]]; then
                    fail "$nip references missing executable integration test: $evidence"
                fi
                ;;
            contract)
                ((automated_rows += 1))
                if [[ "$evidence" != "registry-contract" ]]; then
                    fail "$nip has unexpected registry-contract evidence: $evidence"
                fi
                ;;
            manual|external)
                ((manual_rows += 1))
                if [[ "$evidence" != "manual" ]]; then
                    fail "$nip must use manual evidence for $execution coverage"
                fi
                ;;
        esac
    done < "$MATRIX"
    printf 'PASS: every matrix row has valid evidence metadata\n'
    printf 'Coverage dispositions: %d automated, %d manual/service review\n' "$automated_rows" "$manual_rows"
fi

if [[ -d "$SCRIPT_DIR" ]]; then
    shell_errors=0
    while IFS= read -r test_file; do
        if ! bash -n "$test_file"; then
            ((shell_errors += 1))
        fi
    done < <(find "$SCRIPT_DIR" -maxdepth 1 -type f -name '*.sh' ! -name 'run_coverage.sh' -print | sort)
    if ((shell_errors > 0)); then
        fail "$shell_errors NIP shell scripts failed syntax validation"
    else
        printf 'PASS: all NIP shell scripts pass syntax validation\n'
    fi
fi

if [[ -f "$RELAY_ROOT/web/static/script.js" ]]; then
    if node --check "$RELAY_ROOT/web/static/script.js"; then
        printf 'PASS: dashboard JavaScript syntax is valid\n'
    else
        fail 'dashboard JavaScript syntax validation failed'
    fi
fi

if (cd "$RELAY_ROOT" && go test ./...); then
    printf 'PASS: Go unit test suite\n'
else
    fail 'Go unit test suite failed'
fi

if [[ "$LIVE" == true && "$failures" -eq 0 ]]; then
    for command_name in curl jq nak; do
        if ! command -v "$command_name" >/dev/null 2>&1; then
            continue
        fi
    done

    if INFO=$(curl --fail --silent --show-error --max-time 10 \
        -H 'Accept: application/nostr+json' "$HTTP_URL"); then
        mapfile -t advertised < <(printf '%s' "$INFO" | jq -r '.supported_nips[] | ("NIP-" + (tostring | if length == 1 then "0" + . else . end))' | sort)
        mapfile -t expected < <(awk -F '\t' '$1 !~ /^#/ && NF >= 5 {print $1}' "$MATRIX" | sort)
        if [[ "${advertised[*]}" == "${expected[*]}" ]]; then
            printf 'PASS: live NIP-11 registry matches all 77 matrix identifiers\n'
        else
            fail 'live NIP-11 registry does not match coverage matrix'
            printf 'Expected:\n%s\n' "${expected[*]}" >&2
            printf 'Advertised:\n%s\n' "${advertised[*]}" >&2
        fi
    else
        fail "live NIP-11 endpoint is unavailable: $HTTP_URL"
    fi

    if ((failures == 0)); then
        if RELAY_URL="$RELAY_URL" HTTP_URL="$HTTP_URL" "$SCRIPT_DIR/run_all.sh"; then
            printf 'PASS: live integration suite\n'
        else
            fail 'live integration suite failed'
        fi
    fi
elif [[ "$LIVE" == true ]]; then
    fail 'live checks skipped because static preflight failed'
fi

printf '\n=== SUMMARY ===\n'
printf 'Matrix rows: 77\n'
printf 'Automated rows: %d\n' "${automated_rows:-0}"
printf 'Manual/service rows: %d\n' "${manual_rows:-0}"
printf 'Result: %s\n' "$([[ "$failures" -eq 0 ]] && printf PASS || printf FAIL)"

if ((failures > 0)); then
    exit 1
fi
