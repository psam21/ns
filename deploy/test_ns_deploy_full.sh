#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
DEPLOY_SCRIPT="$SCRIPT_DIR/ns-deploy-full.sh"
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

fail() {
    printf 'FAIL: %s\n' "$1" >&2
    exit 1
}

assert_contains() {
    local needle="$1"
    local file="$2"
    rg -F --quiet "$needle" "$file" || fail "missing expected text: $needle"
}

bash -n "$DEPLOY_SCRIPT"

REMOTE_SCRIPT="$TEMP_DIR/remote.sh"
awk '
    /<< '\''REMOTE_EOF'\''/ { capture = 1; next }
    capture && /^REMOTE_EOF$/ { exit }
    capture { print }
' "$DEPLOY_SCRIPT" > "$REMOTE_SCRIPT"
bash -n "$REMOTE_SCRIPT"

assert_contains 'patches/minio@8.0.7.patch' "$DEPLOY_SCRIPT"
assert_contains 'patches/stream-json@3.6.0.patch' "$DEPLOY_SCRIPT"
assert_contains "cd \"\$1\" && pnpm install --prod --frozen-lockfile" "$DEPLOY_SCRIPT"
assert_contains 'sudo mv "$BLOSSOM_NEW/node_modules" "$BLOSSOM_REMOTE_DIR/node_modules"' "$DEPLOY_SCRIPT"
assert_contains 'sudo flock -n -E 75 /var/lock/nostr-ltd-deploy.lock' "$DEPLOY_SCRIPT"
assert_contains 'RELAY_BACKUP_RETAIN' "$DEPLOY_SCRIPT"
assert_contains 'BLOSSOM_BACKUP_RETAIN' "$DEPLOY_SCRIPT"
assert_contains 'restore_relay' "$DEPLOY_SCRIPT"
assert_contains 'restore_blossom' "$DEPLOY_SCRIPT"
assert_contains 'NIP11_BRANDING_STATUS' "$DEPLOY_SCRIPT"

PROBE_SCRIPT="$TEMP_DIR/probe.sh"
awk '
    /<< '\''PROBE_EOF'\''/ { capture = 1; next }
    capture && /^PROBE_EOF$/ { exit }
    capture { print }
' "$DEPLOY_SCRIPT" > "$PROBE_SCRIPT"
bash -n "$PROBE_SCRIPT"

PUT_COUNT=$(rg -c --fixed-strings -- '-X PUT' "$PROBE_SCRIPT")
DELETE_COUNT=$(rg -c --fixed-strings -- '-X DELETE' "$PROBE_SCRIPT")
[[ "$PUT_COUNT" == "1" ]] || fail "expected one probe PUT, found $PUT_COUNT"
[[ "$DELETE_COUNT" -ge 1 ]] || fail "expected a probe DELETE cleanup path, found $DELETE_COUNT"
assert_contains 'DELETE_EVENT=' "$PROBE_SCRIPT"
assert_contains 'RETRIEVE_SHA256' "$PROBE_SCRIPT"

FIXTURE="$TEMP_DIR/blossom"
mkdir -p "$FIXTURE/build" "$FIXTURE/public" "$FIXTURE/admin/dist" "$FIXTURE/patches"
printf 'build\n' > "$FIXTURE/build/index.js"
printf 'public\n' > "$FIXTURE/public/index.html"
printf 'admin\n' > "$FIXTURE/admin/dist/index.html"
printf 'package\n' > "$FIXTURE/package.json"
printf 'lock\n' > "$FIXTURE/pnpm-lock.yaml"
printf 'config\n' > "$FIXTURE/config.yml"
printf 'minio\n' > "$FIXTURE/patches/minio@8.0.7.patch"
printf 'stream-json\n' > "$FIXTURE/patches/stream-json@3.6.0.patch"
tar -C "$FIXTURE" -czf "$TEMP_DIR/blossom-artifacts.tgz" \
    build public admin/dist package.json pnpm-lock.yaml config.yml patches

tar -tzf "$TEMP_DIR/blossom-artifacts.tgz" > "$TEMP_DIR/archive.list"
for required in \
    build/index.js public/index.html admin/dist/index.html package.json \
    pnpm-lock.yaml config.yml patches/minio@8.0.7.patch \
    patches/stream-json@3.6.0.patch; do
    rg -F --quiet "$required" "$TEMP_DIR/archive.list" || fail "archive missing $required"
done

printf 'PASS: deployment script, remote transaction, probe, and Blossom artifact checks\n'
