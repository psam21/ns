# nostr.ltd

**nostr.ltd** is an open public Nostr relay and media service. Connect a compatible Nostr client to `wss://nostr.ltd` to publish and read signed events, or use the companion Blossom server for Nostr-authenticated media.

## What is Nostr?

Nostr is an open protocol for sharing cryptographically signed events across a network of independent relays. Your identity is a keypair rather than an account owned by one platform, and clients can connect to multiple relays so users keep control over where they publish and read.

Learn more from the [NIP-01 protocol specification](https://nips.nostr.com/1) and the [Nostr protocol community site](https://nostr.org/).

## Live Services

| Service | URL |
|---|---|
| **Relay WebSocket** | `wss://nostr.ltd` |
| **Relay landing page** | [https://nostr.ltd](https://nostr.ltd) |
| **Event activity** | [https://nostr.ltd/events](https://nostr.ltd/events) |
| **Blossom media server** | [https://blossom.nostr.ltd](https://blossom.nostr.ltd) |
| **NIP-11 relay information** | `curl -H "Accept: application/nostr+json" https://nostr.ltd` |

## Why use nostr.ltd?

The service is designed as transparent public infrastructure for Nostr. It offers a broad protocol surface, PostgreSQL-backed event storage, a live status dashboard, an inspectable event breakdown, and a companion media server. There is no account to create for relay access; add the WebSocket endpoint to a compatible client and keep your other relay connections as you prefer.

## Architecture

```text
Nostr clients (desktop, web, and mobile)
             │
             ├── wss://nostr.ltd ────────┐
             ├── https:// media ─────┐   │
             ▼                       ▼   ▼
       ┌─────────────────────────────────────┐
       │             Caddy (TLS)              │
       │             Ports 80 / 443          │
       └──────────────┬──────────────┬───────┘
                      │              │
                      ▼              ▼
             ┌──────────────┐  ┌────────────────┐
             │    Blossom   │  │  Nostr relay   │
             │   Port 3000  │  │   Port 8080    │
             └──────┬───────┘  └────────┬───────┘
                    ▼                   ▼
             ┌──────────────┐  ┌────────────────┐
             │    AWS S3    │  │ PostgreSQL 16  │
             │    (blobs)   │  │    (events)    │
             └──────────────┘  └────────────────┘
```

## Supported NIPs

The relay currently advertises the supported NIPs listed in [`relay/internal/constants/relay_metadata.go`](relay/internal/constants/relay_metadata.go). The landing page presents the live list with links to each specification.

Custom protocol work includes **XX — Time Capsules** and **YY — Nostr Web Pages**. Their implementation lives in the relay source under [`relay/internal/relay/nips`](relay/internal/relay/nips).

## Blossom media server

The companion [Blossom](https://github.com/hzrd149/blossom-server) service provides content-addressable media storage with Nostr authentication.

| Endpoint | Method | Purpose |
|---|---|---|
| `/<sha256>` | GET | Retrieve a blob by hash |
| `/<sha256>` | HEAD | Check whether a blob exists |
| `/upload` | PUT | Upload a blob (authentication required) |
| `/<sha256>` | DELETE | Delete a blob (authentication required) |
| `/mirror` | PUT | Mirror a blob from a URL |
| `/media` | PUT | Upload and optimize media |

The upload limit is 10 MB. Files are stored in S3, authenticated with kind `24242` events, and deleted from S3 when no owners remain.

## Repository structure

```text
├── deploy/
│   ├── config.yaml            # Production relay configuration
│   ├── relay.service          # Relay systemd unit
│   ├── blossom.service        # Blossom systemd unit
│   ├── Caddyfile              # TLS reverse proxy configuration
│   └── test_relay.sh          # Relay smoke-test suite
├── blossom/                   # Blossom media server and admin dashboard
│   ├── src/                   # Server source (TypeScript)
│   ├── admin/                 # Admin dashboard (React)
│   └── public/                # Upload UI
└── relay/                     # Nostr relay source
    ├── internal/
    │   ├── constants/         # Relay metadata and supported NIPs
    │   ├── relay/             # WebSocket handling and validation
    │   ├── storage/           # PostgreSQL storage layer
    │   └── web/                # Dashboard handlers and templates
    └── cmd/                   # CLI entry point
```

## Deployment

Build the relay for the AWS Graviton ARM64 host:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/relay-arm64 ./cmd
```

The production service uses `/opt/relay/relay-arm64`, `/opt/relay/config.yaml`, and `/opt/relay/.env`. Credentials remain outside the repository and are injected through the systemd environment file.

Build the Blossom service with:

```bash
pnpm install && npx tsc && npx vite build
```

See [`deploy/relay.service`](deploy/relay.service), [`deploy/blossom.service`](deploy/blossom.service), and [`deploy/Caddyfile`](deploy/Caddyfile) for the production service definitions.

## Security

Production credentials are injected through systemd environment files. The relay uses a dedicated S3 IAM identity, TLS terminates at Caddy, and the systemd services apply hardening such as `NoNewPrivileges`, `ProtectSystem`, and `ProtectHome`.

For questions or operational contact, use the address configured in the deployment environment rather than committing credentials or secrets to the repository.
