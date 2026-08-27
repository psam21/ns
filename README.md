# nostr.ltd

**nostr.ltd** is an open public Nostr relay and companion Blossom media service. Connect a compatible Nostr client to `wss://nostr.ltd` to publish and read signed events, or use the media service for Nostr-authenticated content-addressable storage.

The repository began from existing open-source relay and Blossom codebases and has since evolved into an independently maintained deployment with its own validation, storage, dashboard, operational tooling, and protocol extensions. Compatibility identifiers retained in the source tree are intentional; they do not describe the public service branding.

## What is Nostr?

Nostr is an open protocol for sharing cryptographically signed events across a network of independent relays. An identity is represented by a keypair rather than an account owned by one platform, and clients can connect to multiple relays so users retain control over where they publish and read.

Learn more from the [NIP-01 protocol specification](https://nips.nostr.com/1) and the [Nostr protocol community site](https://nostr.org/).

## Live Services

| Service | URL |
|---|---|
| **Relay WebSocket** | `wss://nostr.ltd` |
| **Relay operations console** | [https://nostr.ltd](https://nostr.ltd) |
| **Event activity** | [https://nostr.ltd/events](https://nostr.ltd/events) |
| **Blossom media server** | [https://blossom.nostr.ltd](https://blossom.nostr.ltd) |
| **NIP-11 relay information** | `curl -H "Accept: application/nostr+json" https://nostr.ltd` |

## Why use nostr.ltd?

The service is designed as transparent public infrastructure for Nostr. It provides PostgreSQL-backed event storage, a live operator-oriented dashboard, cached event telemetry, a broad advertised protocol surface, and a companion media server. Relay access does not require an account: add the WebSocket endpoint to a compatible client and keep other relay connections as you prefer.

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

The relay advertises **77 supported NIP identifiers** through NIP-11 and the dashboard. The canonical source is [`relay/internal/constants/relay_metadata.go`](relay/internal/constants/relay_metadata.go); the dashboard renders the complete registry in a searchable, internally scrollable panel.

An advertised NIP is a protocol-surface declaration, not a claim that every part of the specification is enforced by a dedicated validator. Relay behavior is implemented across the connection, filter, validation, storage, and web packages. The integration test inventory is documented in [`relay/tests/nips/README.md`](relay/tests/nips/README.md), and the broader status and upgrade matrix is in [`docs/NIP-Tracking.md`](docs/NIP-Tracking.md).

The current registry includes: NIP-01, NIP-02, NIP-05, NIP-07, NIP-09, NIP-10, NIP-11, NIP-13, NIP-17, NIP-18, NIP-19, NIP-21, NIP-22, NIP-23, NIP-24, NIP-25, NIP-27, NIP-29, NIP-30, NIP-32, NIP-34, NIP-35, NIP-36, NIP-37, NIP-38, NIP-39, NIP-40, NIP-42, NIP-43, NIP-44, NIP-45, NIP-46, NIP-47, NIP-48, NIP-49, NIP-50, NIP-51, NIP-52, NIP-53, NIP-5A, NIP-54, NIP-56, NIP-57, NIP-58, NIP-59, NIP-60, NIP-61, NIP-62, NIP-64, NIP-65, NIP-66, NIP-67, NIP-69, NIP-70, NIP-71, NIP-75, NIP-77, NIP-78, NIP-84, NIP-85, NIP-86, NIP-87, NIP-88, NIP-89, NIP-92, NIP-94, NIP-98, NIP-99, NIP-7D, NIP-A0, NIP-A4, NIP-B0, NIP-C0, NIP-F4, NIP-CC, NIP-C7, and NIP-B7.

In addition to the standard registry, the relay contains two project-specific protocol extensions: **XX — Time Capsules** and **YY — Nostr Web Pages**. Their implementation is under [`relay/internal/relay/nips`](relay/internal/relay/nips).

For full coverage, use [`relay/tests/nips/coverage.tsv`](relay/tests/nips/coverage.tsv) and the registry validator:

```bash
cd relay
./tests/nips/run_coverage.sh --static

# Add --live only with a disposable or explicitly authorized relay.
RELAY_URL=ws://localhost:8080 HTTP_URL=http://localhost:8080 \
  ./tests/nips/run_coverage.sh --live
```

The matrix contains one row for every advertised identifier and marks whether the evidence is a relay integration test, registry-contract coverage, client/ecosystem review, or Blossom/service review. This is the honest way to cover all 77 NIPs: client-only specifications and external media protocols cannot be meaningfully validated by pretending they are relay event tests.

## Blossom media service

The companion [Blossom](https://github.com/hzrd149/blossom-server) service provides content-addressable media storage with Nostr authentication.

| Endpoint | Method | Purpose |
|---|---|---|
| `/<sha256>` | GET | Retrieve a blob by hash |
| `/<sha256>` | HEAD | Check whether a blob exists |
| `/upload` | PUT | Upload a blob (authentication required) |
| `/<sha256>` | DELETE | Delete a blob (authentication required) |
| `/mirror` | PUT | Mirror a blob from a URL |
| `/media` | PUT | Upload and optimize media |

The configured upload limit is 10 MB. Files are stored in S3, authenticated with kind `24242` events, and deleted from S3 when no owners remain.

## Repository structure

```text
├── .github/
│   └── copilot-instructions.md  # Repository-specific engineering guidance
├── deploy/
│   ├── config.yaml              # Production relay configuration template
│   ├── relay.service            # Relay systemd unit
│   ├── blossom.service          # Blossom systemd unit
│   ├── Caddyfile                # TLS reverse proxy configuration
│   └── test_relay.sh            # Relay smoke-test suite
├── docs/
│   └── NIP-Tracking.md          # NIP registry, coverage, and upgrade notes
├── blossom/                     # Blossom media service and administration UI
│   ├── src/                     # Server source (TypeScript)
│   ├── admin/                   # Admin dashboard (React)
│   └── public/                  # Upload UI
└── relay/                       # Nostr relay source
    ├── internal/
    │   ├── constants/           # Relay metadata and supported NIPs
    │   ├── relay/               # WebSocket handling and validation
    │   ├── storage/             # PostgreSQL storage layer
    │   └── web/                 # Dashboard handlers, templates, and assets
    ├── tests/nips/              # Full registry matrix, integration scripts, and runners
    └── cmd/                     # CLI entry point
```

## Local development

The relay requires Go 1.25 or newer and PostgreSQL for storage. From the repository root:

```bash
cd relay
go mod download
go test ./...
go build ./...
```

The development configuration listens on `ws://localhost:8080` and reads its database settings from the local configuration. Start a local PostgreSQL instance, review [`relay/config.yaml`](relay/config.yaml), and run:

```bash
cd relay
go run ./cmd start --config config.yaml
```

The web templates and static assets are loaded from disk at runtime. Changes under `relay/web/templates` or `relay/web/static` therefore do not require recompiling the Go binary, although a service restart is a safe way to reload them in production.

## NIP integration tests

The NIP scripts are network integration tests. Run them only against a disposable local relay or a specifically authorized test relay; they publish and delete events. The suite contains 35 shell scripts covering core protocol behavior, authentication, encryption, event kinds, metadata, specialized extensions, and project-specific protocols.

From the `relay/` directory, after starting a test relay:

```bash
# Check shell syntax without contacting a relay
for test in tests/nips/test_nip*.sh; do bash -n "$test"; done

# Run the complete suite against the local relay
RELAY_URL=ws://localhost:8080 \
HTTP_URL=http://localhost:8080 \
  ./tests/nips/run_all.sh

# Run one script
RELAY_URL=ws://localhost:8080 ./tests/nips/test_nip01.sh
```

The runner stops using the old hard-coded public endpoints, passes a shared `RELAY_URL` and `HTTP_URL` to each script, reports pass/fail totals, and exits non-zero if any script fails. For all 77 advertised identifiers, run `./tests/nips/run_coverage.sh --static`; use `--live` only against an isolated or authorized relay. See [`relay/tests/nips/README.md`](relay/tests/nips/README.md) and [`relay/tests/nips/coverage.tsv`](relay/tests/nips/coverage.tsv) for the complete matrix.

## Production deployment

Build the relay for the AWS Graviton ARM64 host from the `relay/` directory:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -o bin/relay-arm64 ./cmd
```

The production service uses `/opt/relay/relay-arm64`, `/opt/relay/config.yaml`, `/opt/relay/web/templates/`, `/opt/relay/web/static/`, and `/opt/relay/.env`. Credentials remain outside the repository and are injected through the systemd environment file. Deploy the binary and web files using your authorized host access, then verify with:

```bash
sudo systemctl restart relay
sudo systemctl status relay --no-pager
curl -H "Accept: application/nostr+json" https://nostr.ltd
curl https://nostr.ltd/api/stats
curl https://nostr.ltd/api/events
```

Build the Blossom service with:

```bash
cd blossom
pnpm install
npx tsc
npx vite build
```

See [`deploy/relay.service`](deploy/relay.service), [`deploy/blossom.service`](deploy/blossom.service), [`deploy/Caddyfile`](deploy/Caddyfile), and [`deploy/config.yaml`](deploy/config.yaml) for the production service definitions and configuration template.

## Security and compatibility

Production credentials are injected through systemd environment files. The relay uses a dedicated S3 IAM identity, TLS terminates at Caddy, and the systemd services apply hardening such as `NoNewPrivileges`, `ProtectSystem`, and `ProtectHome`.

The Go module path, the internal database identifier, and legacy environment or identity-directory names are retained for operational compatibility. Do not mass-rename those identifiers as part of a branding change; migrate them separately only with a planned data and deployment transition.

For operational questions, use the contact address configured in the deployment environment rather than committing credentials or secrets to the repository.
