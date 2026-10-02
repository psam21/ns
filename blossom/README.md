# Blossom media service

The Blossom server for **nostr.ltd**, providing content-addressable media
storage with Nostr authentication. Runs at `https://blossom.nostr.ltd`.

This is a vendored fork of
[hzrd149/blossom-server](https://github.com/hzrd149/blossom-server) (upstream
version 5.2.0), diverged under `psam21/ns`. Upstream's own README documents the
`npx` and Docker distribution paths; neither applies here, because this
repository builds and deploys the service itself rather than publishing it to a
registry.

## Layout

| Path | What it is |
|---|---|
| `src/` | Server source (TypeScript, ESM) |
| `admin/` | Admin dashboard — a **separate** Vite app with its own lockfile |
| `public/` | Upload UI |
| `scripts/` | Build-time helpers |

The server and admin are **separate packages with separate lockfiles**. A
`pnpm-workspace.yaml` exists at this level, but it declares `packages: []` — it
carries only dependency `overrides` (`qs`, `decode-uri-component`,
`stream-json`) and does not make `admin/` a workspace member. There is no root
`vite.config`, and `vite` is not a dependency of the server; it lives only in
`blossom/admin`. `postbuild` installs and builds `admin/` explicitly rather than
relying on workspace hoisting.

## Build

```bash
pnpm install
npx tsc       # server only — compiles src/ to build/
pnpm build    # tsc, then postbuild -> installs and builds admin/
```

`build/` is gitignored and every test imports compiled output from it, so a
fresh checkout has no `build/` at all. Run `tsc` **before** the tests. A stale
`build/` left over from a previous local build will make a test pass that CI
cannot — "verified locally" is true and meaningless in that state.

**Never run `npx vite build` here.** There is no vite config at this level and
vite is not installed, so `npx` tries to fetch `vite@8.x` from the registry: it
hangs, then fails with `missing packages and no YES option`. Build the admin
with `cd admin && pnpm build`.

## Test

```bash
node --test \
  src/rules/index.test.mjs \
  src/helpers/security.test.mjs \
  src/api/upload.test.mjs
```

Those are the three unit suites. They are named explicitly rather than globbed
so that `src/api/e2e.test.mjs` — which boots the app against a live database and
storage backend — cannot be pulled in by a future `test_*.mjs`. Run it with
`pnpm test:e2e` against a configured environment instead.

The unit suites cover SSRF address blocking, URL construction, and upload
validation — the security-relevant paths in this service.

## Configuration

Copy `config.example.yml` to `config.yml` and edit it. In production the
process is started by `deploy/blossom.service`, which reads secrets from an
environment file:

| Variable | Purpose |
|---|---|
| `BLOSSOM_ADMIN_PASSWORD` | Admin dashboard password |
| `S3_ACCESS_KEY` / `S3_SECRET_KEY` | S3 credentials |
| `BLOSSOM_ALLOW_GENERATED_PASSWORD` | Fallback password generation |
| `BLOSSOM_EXTRA_CORS_ORIGINS` | CORS allow-list |

The service exposes port `3000` internally; Caddy terminates TLS and publishes
443 only. Port 3000 is firewalled from the internet.

## Endpoints

| Endpoint | Method | Purpose |
|---|---|---|
| `/<sha256>` | GET | Retrieve a blob by hash |
| `/<sha256>` | HEAD | Check whether a blob exists |
| `/upload` | PUT | Upload a blob (authentication required) |
| `/<sha256>` | DELETE | Delete a blob (authentication required) |
| `/mirror` | PUT | Mirror a blob from a URL |
| `/media` | PUT | Upload and optimize media |
| `/admin` | GET | Admin dashboard |

Authentication uses kind `24242` events. The configured upload limit is 10 MB.

Blobs with no remaining owners are cleaned up when cleanup is enabled.

## Relationship to upstream

Upstream releases via `changesets` and generates its changelog from them. That
machinery has been **removed here**: `.changeset/config.json` set
`"baseBranch": "master"` while this repository has only ever had `main`, so
`changeset version` had no base to compare against and could not run. Upstream's
generated `CHANGELOG.md` was a frozen copy of 5.2.0's history that recorded
none of the local fixes, so it was replaced by a changelog written by hand.

Version bumps here are manual edits to `package.json`. The version field
denotes the upstream base this fork started from, not a release position —
nothing is published to npm. `blossom/.github/workflows/` was removed for the
same reason: nested `.github` directories are never executed by GitHub Actions,
so its three upstream workflows could not have run.

## Documentation

Project-wide documentation — operations runbook, deployment, NIP coverage, and
the engineering lessons from production incidents — is in a single file:
[`.github/copilot-instructions.md`](../.github/copilot-instructions.md).