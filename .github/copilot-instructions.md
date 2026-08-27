# Copilot Instructions — nostr.ltd Relay and Blossom

## Project overview

This repository contains two independently maintained services deployed together:

- **Nostr relay** (`relay/`) — a Go relay backed by PostgreSQL, with WebSocket handling, event validation, storage, NIP-11 metadata, and the public operations dashboard.
- **Blossom media service** (`blossom/`) — a TypeScript/Node.js service for Nostr-authenticated, content-addressable media storage.
- **Deployment configuration** (`deploy/`) — systemd units, Caddy reverse-proxy configuration, a production relay configuration template, and a relay smoke-test script.

The public brand is **nostr.ltd**. The codebase originated from upstream open-source relay and Blossom projects but has diverged substantially. Preserve technical compatibility identifiers unless a deliberate migration is requested.

## Repository and environment

| Item | Value |
|---|---|
| GitHub repository | `https://github.com/psam21/ns.git` |
| Primary branch | `main` |
| Relay Go version | Go 1.25 or newer, as declared in `relay/go.mod` |
| Relay service path | `/opt/relay/relay-arm64` |
| Relay configuration | `/opt/relay/config.yaml` |
| Relay web templates | `/opt/relay/web/templates/` |
| Relay static assets | `/opt/relay/web/static/` |
| Relay systemd service | `relay` |
| Relay internal WebSocket port | `8080` |
| Metrics port | `2112` |
| Blossom service path | `/opt/blossom/` |
| Blossom systemd service | `blossom` |
| Blossom internal port | `3000` |
| Database | PostgreSQL 16, normally local to the production host |
| Advertised NIP registry | 77 entries in `relay/internal/constants/relay_metadata.go` |

Never commit host IP addresses, private SSH key paths, database passwords, complete connection URLs, or other environment-specific credentials. Use shell variables, an ignored environment file, or the deployment operator’s local configuration.

## Build and validate the relay

Run relay commands from `relay/`:

```bash
cd relay
go mod download
go test ./...
go test -race ./...
go vet ./...
go build ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/relay-arm64 ./cmd
node --check web/static/script.js
```

The Go module path remains `github.com/Shugur-Network/relay` for compatibility. Do not mass-replace it during public branding work. The production database identifier and legacy environment or identity-directory names are similarly compatibility-sensitive.

## Dashboard behavior

The public home page is a compact, pilot-style operations console rather than a marketing landing page. It uses a light theme by default and provides a persistent dark-mode switch. The page is designed to fit a single desktop viewport; long protocol registries are searchable and internally scrollable.

- `relay/web/templates/index.html` contains the cockpit layout, supported-NIP registry, observed event-kind panel, quick-connect area, limits, and service links.
- `relay/web/static/style.css` contains the responsive light/dark visual system and compact panel layout.
- `relay/web/static/script.js` hydrates `/api/stats`, refreshes cached `/api/events` telemetry, renders event-kind summaries, filters NIPs and observed kinds, maintains the theme preference, and drives copy/toast interactions.
- `relay/internal/web/handler.go` owns dashboard data models, the concurrency-safe event cache, `/api/stats`, `/api/events`, top-kind summaries, and template helpers.

Do not fabricate live dashboard metrics in production code. Preview renderers may use clearly labeled representative values, but the live dashboard must use relay/cache data.

## Deployment workflow

Build the ARM64 binary locally from `relay/`:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -o bin/relay-arm64 ./cmd
```

Deploy using the operator’s authorized host and key variables. Do not hard-code them into repository instructions:

```bash
export RELAY_HOST='user@example-host'
export SSH_KEY="$HOME/.ssh/your-relay-key.pem"

scp -i "$SSH_KEY" relay/bin/relay-arm64 "$RELAY_HOST:/tmp/relay-arm64"
scp -i "$SSH_KEY" relay/web/templates/index.html "$RELAY_HOST:/tmp/index.html"
scp -i "$SSH_KEY" relay/web/static/style.css "$RELAY_HOST:/tmp/style.css"
scp -i "$SSH_KEY" relay/web/static/script.js "$RELAY_HOST:/tmp/script.js"

ssh -i "$SSH_KEY" "$RELAY_HOST" \
  'sudo systemctl stop relay && \
   sudo cp /tmp/relay-arm64 /opt/relay/relay-arm64 && \
   sudo chmod +x /opt/relay/relay-arm64 && \
   sudo cp /tmp/index.html /opt/relay/web/templates/index.html && \
   sudo cp /tmp/style.css /opt/relay/web/static/style.css && \
   sudo cp /tmp/script.js /opt/relay/web/static/script.js && \
   sudo systemctl start relay && \
   sleep 2 && \
   sudo systemctl status relay --no-pager'
```

Templates and static files are loaded from disk at runtime. A relay restart is recommended after web-file changes; a binary rebuild and restart are required for Go source changes. Verify the public endpoints after deployment:

```bash
curl -H 'Accept: application/nostr+json' https://nostr.ltd
curl https://nostr.ltd/api/stats
curl https://nostr.ltd/api/events
```

## NIP implementation and tests

`DefaultSupportedNIPs` in `relay/internal/constants/relay_metadata.go` is the canonical advertised registry and must remain synchronized with NIP-11 and the dashboard. An advertised NIP identifies the relay’s protocol surface; it is not automatically proof that every part of a NIP is enforced by a dedicated validator.

The NIP integration scripts live under `relay/tests/nips/`. They publish and sometimes delete events, so run them only against a disposable local relay or an explicitly authorized test relay. The suite currently contains 35 shell scripts plus project-specific tests.

The complete advertised registry is governed by `relay/tests/nips/coverage.tsv`, with one row for each of the 77 advertised identifiers. Each row records whether evidence is an integration test, registry-contract coverage, client/ecosystem review, or Blossom/service review. This avoids pretending that client-only or external media protocols can be tested as relay event validators.

From `relay/`, static validation is safe and should run in every change:

```bash
./tests/nips/run_coverage.sh --static
make test-nip-coverage
```

After starting a disposable test relay on port 8080, run the live registry and integration checks:

```bash
RELAY_URL=ws://localhost:8080 HTTP_URL=http://localhost:8080 \
  ./tests/nips/run_coverage.sh --live
```

Use `./tests/nips/test_nip01.sh` for a single test or `./tests/nips/run_all.sh` for the 35 mutating integration scripts. The runners share `RELAY_URL` and `HTTP_URL`, report totals, and exit non-zero on failure. Some scripts require additional tools such as `nak`, `jq`, Python, or `openssl`; see `relay/tests/nips/README.md` before running the complete suite.

## Key source files

| File | Purpose |
|---|---|
| `relay/cmd/main.go` | Relay entry point |
| `relay/cmd/root.go` | CLI commands and configuration loading |
| `relay/internal/constants/relay_metadata.go` | Relay metadata, 77 supported NIPs, custom NIPs, and advertised limits |
| `relay/internal/relay/plugin_validator.go` | Central event validation and allowed-kind rules |
| `relay/internal/relay/connection.go` | WebSocket message routing, AUTH, protected events, and subscriptions |
| `relay/internal/relay/filter.go` | Filter validation and search support |
| `relay/internal/relay/nip77.go` | Negentropy synchronization |
| `relay/internal/relay/nip86.go` | Relay Management API |
| `relay/internal/storage/queries.go` | PostgreSQL persistence and event aggregation |
| `relay/internal/web/handler.go` | HTTP handlers, event cache, dashboard APIs, and template data |
| `relay/web/templates/index.html` | Public operations console markup |
| `relay/web/static/style.css` | Dashboard styles and theme system |
| `relay/web/static/script.js` | Dashboard hydration, filtering, refresh, and interactions |
| `relay/tests/nips/` | Full 77-row coverage matrix, integration scripts, and suite runners |
| `docs/NIP-Tracking.md` | Current advertised registry, test coverage, and upgrade backlog |

## Blossom service

Blossom configuration is under `blossom/config.yml`; production service files are under `deploy/`. The service uses Nostr kind `24242` authentication and S3-backed blob storage. Keep credentials in the host environment, not in tracked configuration.

Build the service with:

```bash
cd blossom
pnpm install
npx tsc
npx vite build
```

## Engineering conventions

Use descriptive commit messages that explain what changed and why. Run `git diff --check` before committing. Prefer direct commits to `main` for validated work in this repository unless the user explicitly requests a pull request. Review NIP specifications before changing validators or the advertised registry, and add or update focused tests whenever behavior changes.
