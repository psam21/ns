# nostr.ltd

Nostr relay and media server deployment for **nostr.ltd**, powered by [Shugur Relay](https://github.com/Shugur-Network/relay) and [Blossom](https://github.com/hzrd149/blossom-server).

## Live Services

| Service | URL |
|---|---|
| **Relay (WebSocket)** | `wss://www.nostr.ltd` |
| **Relay Dashboard** | [https://www.nostr.ltd](https://www.nostr.ltd) |
| **Blossom Media Server** | `https://blossom.nostr.ltd` |
| **NIP-11 Info** | `curl -H "Accept: application/nostr+json" https://www.nostr.ltd` |

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
│  AWS S3      │          │ CockroachDB     │
│  (blobs)     │          │ Cloud           │
└──────────────┘          └─────────────────┘
```

## Infrastructure

- **Compute:** AWS EC2 t4g.small (ARM Graviton, 2 vCPU, 2 GB RAM) — ap-south-1 (Mumbai)
- **Database:** CockroachDB Cloud Serverless (free tier)
- **Blob Storage:** AWS S3 (`nostr-ltd-blossom` bucket, ap-south-1)
- **TLS:** Caddy with automatic Let's Encrypt
- **Domain:** nostr.ltd (BigRock registrar)

## Supported NIPs (61)

01, 02, 03, 09, 11, 13, 15, 17, 18, 22, 23, 24, 25, 28, 29, 30, 32, 34, 35, 37, 38, 40, 42, 44, 45, 47, 50, 51, 52, 53, 54, 56, 57, 58, 59, 60, 61, 62, 65, 69, 70, 71, 72, 75, 77, 78, 84, 85, 87, 88, 89, 90, 94, 99, 7D, A0, A4, B0, B7, C0, C7

Plus custom NIPs: XX (Time Capsules), YY (Nostr Web Pages)

## Blossom Media Server

[Blossom](https://github.com/hzrd149/blossom) (Blobs Stored Simply on Mediaservers) provides content-addressable file storage with Nostr authentication.

**Supported BUDs:** BUD-01, BUD-02, BUD-04, BUD-05, BUD-06, BUD-08

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/<sha256>` | GET | Retrieve blob by hash |
| `/<sha256>` | HEAD | Check if blob exists |
| `/upload` | PUT | Upload blob (auth required) |
| `/<sha256>` | DELETE | Delete blob (auth required) |
| `/mirror` | PUT | Mirror blob from URL |
| `/media` | PUT | Upload + optimize media |

Files are stored in S3 with no expiration (perpetual) and authenticated via kind `24242` Nostr events. Deletes are hard deletes — blobs are purged from S3 when no owners remain.

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
└── relay/                     # Shugur Relay source (patched for CockroachDB Cloud)
```

## Patches Applied

The relay source includes patches for CockroachDB Cloud support:

- **`internal/config/database.go`** — Added `URL` field for direct connection strings
- **`internal/config/defaults.yaml`** — Added `URL` default and `RATE_LIMIT.BAN_DURATION`
- **`internal/config/config.go`** — Conditional validation when using URL vs Server+Port
- **`internal/application/node_builder.go`** — Cloud mode in `BuildDB()` with `replaceDBNameInURL()` helper

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
