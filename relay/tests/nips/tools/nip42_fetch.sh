#!/usr/bin/env bash

# Fetch one event over a fresh, authenticated NIP-42 connection. The secret key
# is supplied through the child process environment and never appears in args.
authenticated_fetch() {
    local secret_key=$1
    local event_id=$2
    local probe_binary="${NIP_AUTH_PROBE_BINARY:-${TMPDIR:-/tmp}/nostr-nip42-probe}"
    if [[ ! -x "$probe_binary" ]]; then
        local script_dir relay_root
        script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
        relay_root=$(cd -- "$script_dir/../../.." && pwd)
        (cd "$relay_root" && CGO_ENABLED=0 go build -trimpath -o "$probe_binary" ./tests/nips/tools/nip42_probe) || return 1
    fi
    NIP_TEST_SECRET_KEY="$secret_key" "$probe_binary" \
        -relay "$RELAY" \
        -auth-relay "${NIP_AUTH_RELAY_URL:-$RELAY}" \
        -query-id "$event_id"
}
