# nostr.ltd

Nostr relay and media server deployment for **nostr.ltd**, powered by [Shugur Relay](https://github.com/Shugur-Network/relay) and [Blossom](https://github.com/hzrd149/blossom-server).

## Live Services

| Service | URL |
|---|---|
| **Relay (WebSocket)** | `wss://nostr.ltd` |
| **Relay Dashboard** | [https://nostr.ltd](https://nostr.ltd) |
| **Blossom Media Server** | `https://blossom.nostr.ltd` |
| **NIP-11 Info** | `curl -H "Accept: application/nostr+json" https://nostr.ltd` |

## Architecture

```
Nostr Clients (Damus, Amethyst, Primal, etc.)
        │
        ├─── wss:// ──────────────────┐
        │                             │
        ├─── https:// (media) ───┐    │
        ▼                        ▼    ▼
┌─────────────────────────────────────────┐
│              Caddy (TLS)                │  ← Auto Let's Encrypt
│              Port 80/443                │
└────┬───────────────────────────┬────────┘
     │ blossom.nostr.ltd         │ nostr.ltd / www.nostr.ltd
     ▼                           ▼
┌──────────────┐          ┌─────────────────┐
│   Blossom    │          │  Shugur Relay   │
│  Port 3000   │          │   Port 8080     │
└──────┬───────┘          └────────┬────────┘
       │                           │
       ▼                           ▼
┌──────────────┐          ┌─────────────────┐
│  AWS S3      │          │  PostgreSQL 16  │
│  (blobs)     │          │  (local)        │
└──────────────┘          └─────────────────┘
```

## Infrastructure

- **Compute:** AWS EC2 t4g.small (ARM Graviton, 2 vCPU, 2 GB RAM) — ap-south-1 (Mumbai)
- **Database:** PostgreSQL 16 (local on EC2)
- **Blob Storage:** AWS S3 (`nostr-ltd-blossom` bucket, ap-south-1)
- **TLS:** Caddy with automatic Let's Encrypt
- **Domain:** nostr.ltd (BigRock registrar)

## Supported NIPs (78)

01, 02, 05, 07, 09, 10, 11, 13, 17, 18, 19, 21, 22, 23, 24, 25, 27, 29, 30, 32, 34, 35, 36, 37, 38, 39, 40, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 56, 57, 58, 59, 60, 61, 62, 64, 65, 66, 67, 69, 70, 71, 75, 77, 78, 84, 85, 86, 87, 88, 89, 90, 92, 94, 98, 99, 7D, A0, A4, B0, B7, C0, C7, CC, F4

Plus custom NIPs: XX (Time Capsules), YY (Nostr Web Pages)

## Blossom Media Server

[Blossom](https://github.com/hzrd149/blossom) (Blobs Stored Simply on Mediaservers) provides content-addressable file storage with Nostr authentication.

**Supported BUDs:** BUD-01, BUD-02, BUD-03, BUD-04, BUD-05, BUD-06, BUD-07, BUD-08

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/<sha256>` | GET | Retrieve blob by hash |
| `/<sha256>` | HEAD | Check if blob exists |
| `/upload` | PUT | Upload blob (auth required) |
| `/<sha256>` | DELETE | Delete blob (auth required) |
| `/mirror` | PUT | Mirror blob from URL |
| `/media` | PUT | Upload + optimize media |

**Upload limit:** 10MB hard cap (enforced at header check and during streaming).

Files are stored in S3 with no expiration (perpetual) and authenticated via kind `24242` Nostr events (BUD-06). Deletes are hard deletes — blobs are purged from S3 when no owners remain.

**Admin Dashboard:** `https://blossom.nostr.ltd/admin/` (basic auth, credentials in config)

## Repository Structure

```
├── deploy/
│   ├── config.yaml            # Relay production config (credentials via env vars)
│   ├── relay.service          # Relay systemd unit
│   ├── blossom.service        # Blossom systemd unit
│   ├── Caddyfile              # Caddy reverse proxy config
│   └── test_relay.sh          # Relay test suite
├── blossom/                   # Vendored fork of hzrd149/blossom-server
│   ├── config.yml             # Production config (S3 backend, credentials via env vars)
│   ├── src/                   # Server source (TypeScript)
│   ├── admin/                 # Admin dashboard (React)
│   └── public/                # Upload UI
└── relay/                     # Shugur Relay source (PostgreSQL backend)
    ├── internal/
    │   ├── constants/         # Relay metadata, NIP lists
    │   ├── relay/
    │   │   ├── nips/          # NIP validators (23+ files)
    │   │   ├── plugin_validator.go  # Central event validator
    │   │   ├── connection.go  # WebSocket handler, NIP-42/67
    │   │   └── subscription.go
    │   ├── storage/           # PostgreSQL storage layer
    │   ├── web/               # Dashboard HTTP handler
    │   └── config/            # Configuration
    └── cmd/                   # CLI entry point
```

## Patches Applied

The relay source includes patches for PostgreSQL support:

- **`internal/config/database.go`** — Added `URL` field for direct connection strings
- **`internal/config/defaults.yaml`** — Added `URL` default and `RATE_LIMIT.BAN_DURATION`
- **`internal/config/config.go`** — Conditional validation when using URL vs Server+Port
- **`internal/application/node_builder.go`** — Cloud mode in `BuildDB()` with `replaceDBNameInURL()` helper
- **`internal/storage/schema.go`** — Fast-path schema init (skips DDL when tables exist), `splitSQL()` for pgx compatibility
- **`internal/storage/schema.sql`** — PostgreSQL-optimized schema with `nostr_d_tag()` immutable function

## Security

All security checks pass:
- ✅ 0 Dependabot alerts
- ✅ 0 npm vulnerabilities (relay, blossom, blossom/admin)
- ✅ 0 Code scanning alerts
- ✅ 0 Open PRs

## Deployment

### Relay

```bash
# Build for ARM64
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/relay-arm64 ./cmd

# On server: credentials are in /opt/relay/.env (never in git)
sudo systemctl start relay
```

### Blossom

```bash
# Build TypeScript + admin dashboard
pnpm install && npx tsc && npx vite build

# Deploy to /opt/blossom/ on server
# Credentials are in /opt/blossom/.env (S3_ACCESS_KEY, S3_SECRET_KEY, etc.)
sudo systemctl start blossom
```

## Security

- All credentials injected via `EnvironmentFile=` in systemd (never in config files or git)
- S3 access via dedicated IAM user with least-privilege policy
- TLS termination at Caddy layer
- systemd hardening: `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome=true`
