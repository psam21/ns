# nostr.ltd

Nostr relay deployment for **nostr.ltd**, powered by [Shugur Relay](https://github.com/Shugur-Network/relay).

## Live Relay

| | |
|---|---|
| **WebSocket** | `wss://www.nostr.ltd` |
| **Dashboard** | [https://www.nostr.ltd](https://www.nostr.ltd) |
| **NIP-11 Info** | `curl -H "Accept: application/nostr+json" https://www.nostr.ltd` |

## Architecture

```
Nostr Clients (Damus, Amethyst, Primal, etc.)
        │
        ▼ wss://
┌─────────────────┐
│   Caddy (TLS)   │  ← Auto Let's Encrypt certificates
│   Port 80/443   │
└────────┬────────┘
         │ reverse_proxy
         ▼
┌─────────────────┐
│  Shugur Relay   │  ← Go binary, WebSocket server
│   Port 8080     │
└────────┬────────┘
         │ PostgreSQL wire protocol
         ▼
┌─────────────────┐
│ CockroachDB     │  ← Managed serverless (CockroachDB Cloud)
│ Cloud           │
└─────────────────┘
```

## Infrastructure

- **Compute:** AWS EC2 t4g.small (ARM Graviton, 2 vCPU, 2 GB RAM) — ap-south-1 (Mumbai)
- **Database:** CockroachDB Cloud Serverless (free tier)
- **TLS:** Caddy with automatic Let's Encrypt
- **Domain:** nostr.ltd (BigRock registrar)

## Supported NIPs

NIP-01, 02, 03, 04, 09, 11, 15, 16, 17, 20, 22, 23, 24, 25, 28, 33, 40, 44, 45, 47, 50, 51, 52, 53, 54, 56, 57, 58, 59, 60, 65, 72, 78

## Repository Structure

```
├── SHUGUR_RELAY_ANALYSIS.md   # Deep code analysis of Shugur Relay
├── deploy/
│   ├── config.yaml            # Production config (credentials via env vars)
│   ├── relay.service          # systemd unit file
│   ├── Caddyfile              # Caddy reverse proxy config
│   └── test_relay.sh          # Relay test suite
└── relay/                     # Shugur Relay source (patched for CockroachDB Cloud)
```

## Patches Applied

The relay source includes patches for CockroachDB Cloud support:

- **`internal/config/database.go`** — Added `URL` field for direct connection strings
- **`internal/config/defaults.yaml`** — Added `URL` default and `RATE_LIMIT.BAN_DURATION`
- **`internal/config/config.go`** — Conditional validation when using URL vs Server+Port
- **`internal/application/node_builder.go`** — Cloud mode in `BuildDB()` with `replaceDBNameInURL()` helper

## Deployment

```bash
# Build for ARM64
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/relay-arm64 ./cmd

# On server: credentials are in /opt/relay/.env (never in git)
# SHUGUR_DATABASE_URL=postgresql://user:pass@host:26257/defaultdb?sslmode=verify-full

# Start
sudo systemctl start relay
```

## Security

- Database credentials injected via `EnvironmentFile=` in systemd (not in config files)
- TLS termination at Caddy layer
- systemd hardening: `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome=true`
