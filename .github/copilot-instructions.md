# Copilot Instructions — NostrRelayBlossom

## Project Overview

This monorepo contains two main components:

- **Shugur Relay** (`relay/`) — A high-performance Nostr relay written in Go, backed by CockroachDB
- **Blossom** (`blossom/`) — A media server (TypeScript/Node.js) for Nostr file storage
- **Deploy configs** (`deploy/`) — systemd services, Caddyfile, config templates

## Environment

| Item | Value |
|------|-------|
| Go SDK | `/home/jack/go-sdk/go/bin/go` (add to PATH: `export PATH=$PATH:/home/jack/go-sdk/go/bin`) |
| Go version | 1.24.4 |
| EC2 instance | `13.201.250.44` (ARM64 Graviton, t4g.small, Ubuntu) |
| EC2 user | `ubuntu` |
| SSH key | `~/.ssh/nostr-relay-key.pem` |
| GitHub repo | `https://github.com/psam21/ns.git` (branch: `main`) |
| Relay binary path (EC2) | `/opt/relay/relay-arm64` |
| Relay config (EC2) | `/opt/relay/config.yaml` |
| Web templates (EC2) | `/opt/relay/web/templates/` |
| Web static files (EC2) | `/opt/relay/web/static/` |
| Systemd service | `relay` |
| Relay port (internal) | `8080` |
| Metrics port | `2112` |
| Caddy | Reverse proxies HTTPS → localhost:8080 |

## Build & Deploy Workflow

### 1. Build locally (always from `relay/` dir)

```bash
cd relay
export PATH=$PATH:/home/jack/go-sdk/go/bin

# Check compilation
go build ./...

# Cross-compile for EC2 (ARM64)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/relay-arm64 ./cmd
```

### 2. Deploy to EC2

```bash
# Upload binary
scp -i ~/.ssh/nostr-relay-key.pem bin/relay-arm64 ubuntu@13.201.250.44:/tmp/relay-arm64

# Stop, replace, restart
ssh -i ~/.ssh/nostr-relay-key.pem ubuntu@13.201.250.44 \
  "sudo systemctl stop relay && \
   sudo cp /tmp/relay-arm64 /opt/relay/relay-arm64 && \
   sudo chmod +x /opt/relay/relay-arm64 && \
   sudo systemctl start relay && \
   sleep 2 && \
   sudo systemctl status relay --no-pager"
```

### 3. Deploy web files (templates/static are loaded from disk, NOT embedded)

```bash
# Upload changed web files
scp -i ~/.ssh/nostr-relay-key.pem relay/web/templates/index.html ubuntu@13.201.250.44:/tmp/index.html
scp -i ~/.ssh/nostr-relay-key.pem relay/web/static/style.css ubuntu@13.201.250.44:/tmp/style.css
scp -i ~/.ssh/nostr-relay-key.pem relay/web/static/script.js ubuntu@13.201.250.44:/tmp/script.js

ssh -i ~/.ssh/nostr-relay-key.pem ubuntu@13.201.250.44 \
  "sudo cp /tmp/index.html /opt/relay/web/templates/index.html && \
   sudo cp /tmp/style.css /opt/relay/web/static/style.css && \
   sudo cp /tmp/script.js /opt/relay/web/static/script.js"

# Web files are loaded from disk at runtime — restart only needed for Go code changes
ssh -i ~/.ssh/nostr-relay-key.pem ubuntu@13.201.250.44 "sudo systemctl restart relay"
```

### 4. Git commit & push

```bash
cd /home/jack/Documents/NostrRelayBlossom
git add -A && git commit -m "description" && git push
```

## Key Relay Source Files

### Core

| File | Purpose |
|------|---------|
| `relay/cmd/main.go` | Entry point |
| `relay/cmd/root.go` | CLI command setup |
| `relay/internal/relay/plugin_validator.go` | **Central event validator** — `AllowedKinds` map, `RequiredTags` map, kind range checks (ephemeral 20000-29999, DVM 5000-6999, NIP-29 groups 9000-9030/39000-39003) |
| `relay/internal/constants/relay_metadata.go` | **`DefaultSupportedNIPs`** list (displayed on homepage, advertised in NIP-11), `CustomNIP` structs, `DefaultRelayMetadata` |
| `relay/internal/relay/connection.go` | WebSocket connection handler, message routing (EVENT, REQ, CLOSE, COUNT, AUTH), NIP-42 auth challenge/response, NIP-70 protected event enforcement |
| `relay/internal/relay/subscription.go` | Subscription management, COUNT handling |
| `relay/internal/relay/filter.go` | Filter validation, NIP-50 search support |

### NIP Implementations

| File | NIPs |
|------|------|
| `relay/internal/relay/nips/nip42.go` | NIP-42: AUTH challenge/validate, NIP-70: IsProtectedEvent |
| `relay/internal/relay/nips/nip62.go` | NIP-62: Request to Vanish validation |
| `relay/internal/relay/nips/nip_ee.go` | NIP-EE: MLS E2EE validators (kinds 443, 444, 445, 10051) |
| `relay/internal/relay/nips/nip45.go` | NIP-45: COUNT request handling |
| `relay/internal/relay/nip77.go` | NIP-77: Negentropy syncing (NEG-OPEN/MSG/CLOSE/ERR) |
| `relay/internal/relay/nip86.go` | NIP-86: Relay Management API (JSON-RPC, NIP-98 auth, 18 methods) |

### Web / Dashboard

| File | Purpose |
|------|---------|
| `relay/internal/web/handler.go` | Dashboard HTTP handler, `formatNIP` FuncMap, `StatsData` struct, `liveSince` from `.first_boot` file, `/api/stats` endpoint |
| `relay/web/templates/index.html` | Dashboard template — dark theme, NIP badges, stats, config panel |
| `relay/web/static/style.css` | Dark techy theme (#0a0a0a bg, #00e599 accent, JetBrains Mono) |
| `relay/web/static/script.js` | Fetches `/api/stats`, updates active-connections and events-stored |

### Other

| File | Purpose |
|------|---------|
| `relay/internal/metrics/relay.go` | Prometheus metrics, atomic counters |
| `relay/internal/application/node.go` | Application node, event processing |
| `relay/internal/application/node_builder.go` | Node initialization |
| `relay/internal/storage/queries.go` | CockroachDB queries — `persistDeletion` (NIP-09 with `e`+`a` tag support), `persistVanish` (NIP-62 full pubkey wipe), `IsVanishedPubkey` |
| `relay/internal/storage/event_processor.go` | Event processing worker pool — `QueueEvent`, `QueueDeletion`, `QueueVanish`, `processEvents` switch (ephemeral→vanish→deletion→replaceable→addressable→default) |
| `deploy/config.yaml` | Production relay config (contact, description, etc.) |

## Architecture Notes

- **Event kind allowlisting**: The relay uses an explicit `AllowedKinds` map plus range checks. To support a new event kind, add it to `AllowedKinds` in `plugin_validator.go`. For kind ranges (DVM, ephemeral, groups), range checks in `ValidateEvent()` handle acceptance.
- **SupportedNIPs**: Uses `[]interface{}` to support both `int` (e.g., `1`) and `string` (e.g., `"EE"`, `"7D"`, `"C7"`). The `formatNIP` template function handles rendering: ints → zero-padded (`%02d`), strings → as-is.
- **RequiredTags**: Validation map in `plugin_validator.go` that enforces mandatory tags per event kind. Check actual NIP specs before adding — some specs have changed (e.g., NIP-78 needs `"d"` not `"p"`).
- **Web templates are NOT embedded** in the binary. They are read from `/opt/relay/web/` on EC2 at runtime. This means template/CSS/JS changes don't require a rebuild, just file upload + restart.
- **The `.first_boot` file** persists the relay's initial boot timestamp for the "live since" stat.
- **NIP-42 AUTH flow**: On WebSocket connect, relay generates a 32-byte hex challenge and sends `["AUTH", challenge]`. Client responds with `["AUTH", signedEvent]` (kind 22242). Validated via `go-nostr/nip42.ValidateAuthEvent`. Authenticated pubkeys stored per-connection in `authedPubkeys` map. `relayURL` from `config.RelayConfig.PublicURL` (`wss://nostr.ltd`).
- **NIP-70 Protected Events**: Events with `["-"]` tag are rejected with `auth-required` unless the author's pubkey is in the connection's `authedPubkeys`.
- **NIP-62 Vanish pipeline**: Kind 62 → `IsVanishEvent` check in `event_processor.go` → `persistVanish` deletes ALL events from pubkey + gift-wrapped (kind 1059) events p-tagged to pubkey, then stores vanish request.
- **NIP-09 Deletion**: `persistDeletion` supports both `"e"` tags (delete by event ID) and `"a"` tags (delete addressable events by `kind:pubkey:d-tag` up to `created_at`).

## Conventions

- Commit messages should be descriptive with what changed and why
- Always `go build ./...` before cross-compiling to catch errors fast
- Always verify deployment with `sudo systemctl status relay --no-pager`
- The `temp/` directory is gitignored (used for scratch work like cloning NIP specs)
- Contact: `epochshield@proton.me`

## Current NIP Support (62 NIPs)

01, 02, 03, 09, 11, 13, 15, 17, 18, 22, 23, 24, 25, 28, 29, 30, 32, 34, 35, 37, 38, 40, 42, 44, 45, 47, 50, 51, 52, 53, 54, 56, 57, 58, 59, 60, 61, 62, 65, 69, 70, 71, 72, 75, 77, 78, 84, 85, 86, 87, 88, 89, 90, 94, 99, 7D, A0, A4, B0, B7, C0, C7

Plus custom NIPs: XX (Time Capsules), YY (Nostr Web Pages)

## Future Work (requires deeper protocol implementation)

- **NIP-29** (Relay-based Groups) — full relay-managed group system with membership enforcement
