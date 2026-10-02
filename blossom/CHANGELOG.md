# Blossom changelog

Local changes to the vendored Blossom server since it was imported from
[hzrd149/blossom-server](https://github.com/hzrd149/blossom-server) at
upstream **5.2.0** (`babb004`, 2026-02-10).

Upstream's own release history is not reproduced here. This file records only
what this fork changed.

The declared version in `package.json` is still `5.2.0` and has not been
bumped: nothing here is published to npm, and the running service is deployed
from this repository by `deploy/ns-deploy-full.sh`. **The version field
therefore denotes "upstream base", not the state of this fork** — do not read
it as a changelog position.

## Unreleased

Everything below is on `main` and deployed to production.

### Security

- NIP-98 HTTP Auth implemented and advertised (`7eddbec`).
- SSRF guard shared across `mirror.ts` and `transport/http.ts`, validating
  every resolved address and re-checking redirects (`ecee2d9`, `77e89a9`).
- Media uploads validated by magic-byte sniffing rather than declared MIME
  type; ffmpeg log output sanitized; SQL `LIKE` input escaped (`2eda280`).
- CSP applied to the upload UI; admin SPA added to `.gitignore` (`66aed15`).
- CORS restricted to an allow-list; `X-Reason` header sanitized; admin
  password fails closed (`275e56a`, `e64147f`).
- Visible-NIPs and host allow-lists; 5xx logged to a file; `Content-Length`
  trusted only after validation (`eaa8e90`).
- NIP-29 secp256k1 key handling, empty-key limiter rejection, Bloom filter
  context cancellation, fail-closed blob URLs (`d6e0e81`).
- `pruneStorage` concurrency capped; dispatcher subscription made lazy
  (`327e812`).
- Mirror endpoint and dependency build chain hardened (`745d59b`, `b938b7f`).
- Incomplete sanitization and clear-text logging fixed; dependencies updated
  for known npm vulnerabilities (`a399038`, `0122f0d`, `104cc01`).

### CORS

- Configurable `extraCorsOrigins`, logged at startup (`c93dec8`).
- NIP-98 auth `type` derived from the URL path (`f7c2d23`).
- Auth accepted at all endpoints via a wildcard sentinel (`2bb5d72`).
- Defaulted to wildcard so any browser client can upload (`8d709a2`).
- `publicDomain` alone no longer triggers restrictive CORS (`87b9b4e`).
- Preflight rejected for non-allow-listed origins (`e64147f`).

### Correctness and compatibility

- Legacy blobs are served even when they carry no metadata row (`2ed73ca`).
  Blobs written before the metadata table existed were otherwise unreachable.
- Blossom attachments preserved for backward compatibility (`c612a14`).
- NIP-98 auth `type` no longer derived incorrectly from the path (`f7c2d23`).
- `saveFromResponse` accepts a `Readable` with an optional type (`d58934b`).
- Response stream split in `fetch.ts` to prevent double consumption
  (`b09c1d1`).
- Deliberate `storage.rules` added so authenticated uploads are accepted
  (`728d78b`).
- 10 MB hard upload size limit enforced (`d8b8316`).

### Performance

- Discovery latency bounded (`74e8e15`) — previously unbounded.
- SQLite index added on `blobs.type`; relay Bloom filter sized from event
  count (`a3a7ced`).

### Tests

- Unit suites for `getFileRule` empty-list behaviour (`8e50ba5`) and an
  11-case upload matrix (`56e4a9b`).
- SSRF, URL construction and upload validation coverage — the
  security-relevant paths (`02bdf75`).
- HTTP end-to-end coverage (`89a40bf`). Excluded from the CI unit step
  because it boots the app against a live database and storage backend.

### Deployment

- pnpm version pinned so the Blossom CI job can run (`52ebbab`).
- pnpm prompt prevented from hanging an unattended deploy (`d3cf271`).

## Removed

- `.changeset/` and `@changesets/cli`. Upstream's release automation set
  `"baseBranch": "master"`, but this repository has only ever had `main`, so
  `changeset version` had no base to diff against and could not run. Version
  bumps are manual edits to `package.json`.
- `blossom/.github/workflows/` — three upstream workflows
  (`docker-image.yml`, `publish-next.yml`, `version-or-publish.yml`). Nested
  `.github` directories are not executed by GitHub Actions; only the
  repository-root `.github/workflows/ci.yml` runs. Two of the three invoked
  the changesets tooling removed above.
- Upstream's `CHANGELOG.md` and `README.md`, both inherited from the vendored
  source. The README documented `npx` and Docker distribution paths that do not
  apply here, and a build command (`npx vite build`) that cannot work in this
  repository.