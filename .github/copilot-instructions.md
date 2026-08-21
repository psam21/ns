# Copilot Instructions — NostrRelayBlossom

## Project Overview

This monorepo contains two main components:

- **Shugur Relay** (`relay/`) — A high-performance Nostr relay written in Go, backed by PostgreSQL
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
| Database | PostgreSQL 16 (local on EC2), `postgresql://relay:relay@127.0.0.1:5432/shugur?sslmode=disable` |
| Relay port (internal) | `8080` |
| Metrics port | `2112` |
| Caddy | Reverse proxies HTTPS → localhost:8080 |
| NIP specs (local) | `temp/nips/` — full clone of nostr-protocol/nips repo. **Always read NIP specs from here first** instead of fetching from the web. |

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
| `relay/internal/relay/nips/nip45.go` | NIP-45: COUNT request handling with HyperLogLog support |
| `relay/internal/relay/nip77.go` | NIP-77: Negentropy syncing (NEG-OPEN/MSG/CLOSE/ERR) |
| `relay/internal/relay/nip86.go` | NIP-86: Relay Management API (JSON-RPC, NIP-98 auth, 17 methods) |
| `relay/internal/relay/nip29.go` | NIP-29: Relay-based Groups (group store, membership, moderation, relay-signed metadata) |
| `relay/internal/relay/nip43.go` | NIP-43: Relay Access Metadata (membership store, invite codes, join/leave flow) |
| `relay/internal/relay/nips/nip05.go` | NIP-05: DNS identifier mapping validator |
| `relay/internal/relay/nips/nip07.go` | NIP-07: window.nostr capability documentation |
| `relay/internal/relay/nips/nip10.go` | NIP-10: Text Notes and Threads validator |
| `relay/internal/relay/nips/nip19.go` | NIP-19: bech32-encoded entities (npub, nsec, note, nprofile, nevent, naddr) |
| `relay/internal/relay/nips/nip21.go` | NIP-21: nostr: URI scheme validator |
| `relay/internal/relay/nips/nip27.go` | NIP-27: Text Note References validator |
| `relay/internal/relay/nips/nip36.go` | NIP-36: Sensitive Content / Content Warning validator |
| `relay/internal/relay/nips/nip44.go` | NIP-44: Encrypted Payloads (Versioned) validator |
| `relay/internal/relay/nips/nip46.go` | NIP-46: Nostr Remote Signing validator |
| `relay/internal/relay/nips/nip47.go` | NIP-47: Nostr Wallet Connect validators |
| `relay/internal/relay/nips/nip48.go` | NIP-48: Bridged Events validator |
| `relay/internal/relay/nips/nip49.go` | NIP-49: Private Key Encryption (ncryptsec) validator |
| `relay/internal/relay/nips/nip5a.go` | NIP-5A: Static Websites (nsites) validator |
| `relay/internal/relay/nips/nip67.go` | NIP-67: EOSE Completeness Hint (in connection.go/subscription.go) |
| `relay/internal/relay/nips/nip87.go` | NIP-87: Cashu/Fedimint Discoverability validator |
| `relay/internal/relay/nips/nip92.go` | NIP-92: Media Attachments (imeta) validator |
| `relay/internal/relay/nips/nip94.go` | NIP-94: File Metadata validator |
| `relay/internal/relay/nips/nipa0.go` | NIP-A0: Voice Messages validator |
| `relay/internal/relay/nips/nipa4.go` | NIP-A4: Public Messages validator |
| `relay/internal/relay/nips/nipb0.go` | NIP-B0: Web Bookmarks validator |
| `relay/internal/relay/nips/nipc0.go` | NIP-C0: Code Snippets validator |
| `relay/internal/relay/nips/nipc7.go` | NIP-C7: Chats validator |
| `relay/internal/relay/nips/nipcc.go` | NIP-CC: Geocaching Events validator |
| `relay/internal/relay/nips/nipf4.go` | NIP-F4: Podcasts validator |

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
| `relay/internal/storage/schema.sql` | Embedded DDL — events table, btree/GIN indexes, `nostr_d_tag()` immutable function, partial unique indexes for replaceable/addressable events |
| `relay/internal/storage/schema.go` | Schema init with fast-path (skips DDL when events table exists), `splitSQL()` helper for pgx single-statement execution, `VerifySchema()` |
| `relay/internal/storage/queries.go` | PostgreSQL queries — `persistDeletion` (NIP-09 with `e`+`a` tag support), `persistVanish` (NIP-62 full pubkey wipe), `IsVanishedPubkey` |
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
- **Schema init fast-path**: `InitializeSchema` checks if the `events` table exists and skips all DDL if so. Without this, `CREATE INDEX IF NOT EXISTS` takes ~158s on 60K+ rows. The `splitSQL()` helper splits DDL at semicolons while respecting `$$` dollar-quoted function bodies, since pgx extended query protocol only supports single statements.
- **Database**: PostgreSQL 16 running locally on EC2 (migrated from CockroachDB Cloud). Connection via `127.0.0.1:5432`, user `relay`, database `shugur`.

## New NIP Validators Added (2026-08-21)

All 45 GitHub issues for NIP implementations have been closed. The following validators were added:

| NIP | File | Kinds | Description |
|-----|------|-------|-------------|
| NIP-05 | `nip05.go` | 0 | DNS identifier mapping |
| NIP-07 | `nip07.go` | — | window.nostr capability docs |
| NIP-10 | `nip10.go` | 1 | Text Notes and Threads |
| NIP-19 | `nip19.go` | — | bech32 entities (npub, nsec, etc.) |
| NIP-21 | `nip21.go` | 1, 30023 | nostr: URI scheme |
| NIP-27 | `nip27.go` | 1, 30023 | Text Note References |
| NIP-36 | `nip36.go` | 1 | Sensitive Content / Content Warning |
| NIP-44 | `nip44.go` | — | Encrypted Payloads (updated v2 nonce=32) |
| NIP-46 | `nip46.go` | — | Nostr Remote Signing (timeout fix) |
| NIP-47 | `nip47.go` | 13194, 23194, 23195 | Nostr Wallet Connect |
| NIP-48 | `nip48.go` | 1 | Bridged Events (proxy tags) |
| NIP-49 | `nip49.go` | — | ncryptsec validation |
| NIP-5A | `nip5a.go` | 15128, 35128, 5128 | Static Websites (nsites) |
| NIP-67 | `connection.go` | — | EOSE Completeness Hint (finish hint) |
| NIP-87 | `nip87.go` | 38172, 38173, 38000 | Cashu/Fedimint Discovery |
| NIP-92 | `nip92.go` | 1 | Media Attachments (imeta) |
| NIP-94 | `nip94.go` | 1063 | File Metadata |
| NIP-A0 | `nipa0.go` | 1222, 1244 | Voice Messages |
| NIP-A4 | `nipa4.go` | 24 | Public Messages |
| NIP-B0 | `nipb0.go` | 39701 | Web Bookmarks |
| NIP-C0 | `nipc0.go` | 1337 | Code Snippets |
| NIP-C7 | `nipc7.go` | 9 | Chats |
| NIP-CC | `nipcc.go` | 37516, 37517, 7516, 7517 | Geocaching Events |
| NIP-F4 | `nipf4.go` | 54, 10154, 10064, 10054 | Podcasts |

All validators are registered in `plugin_validator.go` switch statement and advertised in `DefaultSupportedNIPs` in `relay_metadata.go`.

## Blossom Media Server

| Item | Value |
|------|-------|
| Source | `blossom/src/` (TypeScript/Node.js) |
| Config | `blossom/config.yml` |
| EC2 path | `/opt/blossom/` |
| Systemd service | `blossom` |
| Port (internal) | `3000` |
| Storage | AWS S3 (`nostr-ltd-blossom` bucket, ap-south-1) |
| Upload limit | 10MB hard cap (`maxUploadSize` in config) |

### Key Blossom Source Files

| File | Purpose |
|------|--------|
| `blossom/src/index.ts` | Koa app setup, middleware, static serving |
| `blossom/src/config.ts` | Config type definition, YAML/env var loading, defaults |
| `blossom/src/api/upload.ts` | `/upload` endpoint, `checkUpload` middleware (auth, rules, size limit) |
| `blossom/src/api/media.ts` | `/media` endpoint (upload + optimize) |
| `blossom/src/api/mirror.ts` | `/mirror` endpoint (fetch + store from URL) |
| `blossom/src/storage/upload.ts` | Stream handling, temp files, 10MB streaming enforcement |
| `blossom/config.yml` | Production config (S3, auth, upload limit) |

### Blossom Architecture Notes

- **Upload size limit**: `maxUploadSize` (default 10MB) enforced at 3 levels: `Content-Length` header check in `checkUpload` middleware (returns 413), streaming byte count in `saveFromUploadRequest`, and streaming byte count in `saveFromResponse` (for mirrors).
- **Auth**: Kind `24242` Nostr events, BUD-06 sha256 binding via `x` tag.
- **Storage**: S3 backend with `removeWhenNoOwners: true` — blobs are hard-deleted from S3 when last owner removes.
- **Media optimization**: Images → WebP (1920×1080 max, quality 90), Video → MP4 (libx264, 1080p max, 30fps).

## Conventions

- Commit messages should be descriptive with what changed and why
- Always `go build ./...` before cross-compiling to catch errors fast
- Always verify deployment with `sudo systemctl status relay --no-pager`
- The `temp/` directory is gitignored (used for scratch work like cloning NIP specs)
- Contact: `epochshield@proton.me`

## Current NIP Support (78 NIPs)

01, 02, 05, 07, 09, 10, 11, 13, 17, 18, 19, 21, 22, 23, 24, 25, 27, 29, 30, 32, 34, 35, 36, 37, 38, 39, 40, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 56, 57, 58, 59, 60, 61, 62, 64, 65, 66, 67, 69, 70, 71, 75, 77, 78, 84, 85, 86, 87, 88, 89, 90, 92, 94, 98, 99, 7D, A0, A4, B0, B7, C0, C7, CC, F4

Plus custom NIPs: XX (Time Capsules), YY (Nostr Web Pages)
