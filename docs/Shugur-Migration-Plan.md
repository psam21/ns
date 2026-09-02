# Shugur Reference Migration Plan

## Purpose

This document defines a deliberate migration from inherited Shugur naming to the `nostr.ltd` identity across the `psam21/ns` repository. The migration is staged so that public branding can be cleaned immediately while existing deployments, configuration, database layouts, container consumers, and Go import users are not broken by a blind global replacement.

> ## ⚠️ FULLY MIGRATED TO GIT — DO NOT EDIT
>
> This document has been fully decomposed into Git issues. Git is now the single source of truth for all work tracking.
>
> **Migration date:** 2026-09-02
> **Parent tracking issue:** [#113](https://github.com/psam21/ns/issues/113)
> **Status:** Ready for deletion.
>
> ### Issue map
>
> | Section | Issue(s) |
> |---|---|
> | Reference classes and treatment | [#120](https://github.com/psam21/ns/issues/120) |
> | Phase 1 — Inventory and baseline | [#114](https://github.com/psam21/ns/issues/114) (done) |
> | Phase 2 — Cosmetic and documentation cleanup | [#115](https://github.com/psam21/ns/issues/115) (done) |
> | Phase 3 — Configuration compatibility migration | [#116](https://github.com/psam21/ns/issues/116) (done) |
> | Phase 4 — Installer, image, and deployment migration | [#117](https://github.com/psam21/ns/issues/117) (done) |
> | Phase 5 — Go module-path decision and migration | [#118](https://github.com/psam21/ns/issues/118), [#109](https://github.com/psam21/ns/issues/109) |
> | Phase 6 — Deprecation and removal | [#119](https://github.com/psam21/ns/issues/119), [#110](https://github.com/psam21/ns/issues/110), [#111](https://github.com/psam21/ns/issues/111) |
> | Validation contract | [#112](https://github.com/psam21/ns/issues/112) |
> | Rollback policy | [#121](https://github.com/psam21/ns/issues/121) |
> | Completion criteria | [#122](https://github.com/psam21/ns/issues/122) |
>
> The original document body is preserved below for historical reference only. Any new work, status changes, or decisions must be tracked in the linked issues, not here.

## Current state

The active production-facing relay and Blossom paths already use `nostr.ltd` in their current service metadata and dashboard branding. Legacy references remain primarily in the inherited relay module, fallback configuration, source metadata, installer scripts, Docker Compose templates, database/environment compatibility names, comments, and repository guidance. Blossom does not carry the same breadth of legacy naming, although its deployment is part of the final consistency review.

The Go module currently declares:

```go
module github.com/Shugur-Network/relay
```

This is a technical import identity, not a runtime network dependency. It must not be changed until all imports, build tooling, release workflows, downstream consumers, and any compatibility strategy have been evaluated.

## Migration principles

1. **No blind global replacement.** Every occurrence is classified before editing.
2. **No secret or data migration through Git.** Credentials, `.env` files, database contents, private keys, and production data remain outside the repository.
3. **Active production defaults take priority.** Public metadata, fallback URLs, support addresses, service descriptions, and dashboard text must describe `nostr.ltd`.
4. **Compatibility-sensitive names are migrated by aliasing first.** Existing `SHUGUR_*` environment names and database identifiers remain accepted during a deprecation window while `NOSTR_*` names become canonical.
5. **Container references must point to real published artifacts.** A legacy image reference is not replaced with a guessed image path.
6. **Every stage has a validation gate and rollback path.** Deployment scripts must be tested without requiring production credentials.

## Reference classes and planned treatment

| Class | Examples | Treatment |
|---|---|---|
| Public branding | Relay names, banners, icons, support text, installer titles, comments | Replace with `nostr.ltd` or neutral relay wording. |
| Active defaults | `shugur-relay`, old public URL, old contact and asset URLs | Replace with `nostr.ltd` defaults and validate NIP-11 output. |
| Configuration keys | `SHUGUR_*` environment variables | Add canonical `NOSTR_*` names, retain documented legacy aliases temporarily, define precedence, emit a deprecation warning without exposing values. |
| Database identifiers | Existing database/schema names | Preserve current production identifiers; support new names only for fresh installs or an explicitly planned database migration. |
| Container/image references | Old GHCR image names and floating tags | Replace only when a verified `psam21/ns` image exists; otherwise make local-build behavior explicit. Pin versions/digests. |
| Go module/import path | `github.com/Shugur-Network/relay` | Separate final migration phase. Update the complete import graph and downstream guidance together, with a compatibility release or documented breaking release. |
| Tests and fixtures | Expected old names, generated test configuration | Update assertions to canonical names while preserving compatibility tests for legacy aliases. |
| Documentation and guidance | README, Copilot instructions, NIP tracking, deployment docs | Update after behavior and aliases are settled so documentation reflects the real contract. |

## Staged execution

### Phase 1 — Inventory and baseline

Synchronize with `origin/main`, produce a tracked-file inventory, and record each reference with its class, owning subsystem, runtime impact, and proposed replacement. Exclude generated bundles, build directories, dependency stores, and Git metadata from the source inventory, but inspect release manifests separately. Record baseline results for Go tests, Blossom builds, NIP static coverage, deployment-script syntax, dependency scans, and current service configuration.

**Gate:** no unclassified functional reference remains in the planned migration scope.

### Phase 2 — Cosmetic and documentation cleanup

Update public text, source comments, fallback metadata, default support/asset URLs, installer titles, and repository guidance. Do not change module paths, environment keys, database identifiers, or image coordinates in this phase. Validate dashboard JavaScript, NIP-11 metadata, shell syntax, and all documentation links.

**Gate:** public repository text and active fallback metadata contain no unintended Shugur branding.

### Phase 3 — Configuration compatibility migration

Introduce canonical `NOSTR_*` environment names where the relay currently consumes inherited `SHUGUR_*` names. The precedence contract will be `NOSTR_*` first, then `SHUGUR_*` as a temporary compatibility alias, then the documented default. Emit a one-time warning naming the deprecated key but never print its value. Update fresh-install templates to use `NOSTR_*`, and add tests for canonical, legacy, conflicting, and absent values.

**Gate:** new installs use canonical names; existing deployments continue to start unchanged; conflicts resolve deterministically and are visible to operators without leaking secrets.

### Phase 4 — Installer, image, and deployment migration

Update standalone/distributed installers, Compose templates, the combined relay/Blossom deployer, service metadata, and release documentation. Preserve production database and data paths. Replace legacy image coordinates only after verifying the replacement artifact exists and supports the required architecture. Add deployment checks for both `relay.service` and `blossom.service`, NIP-11, Blossom HTTP health, artifact ownership, and rollback behavior.

**Gate:** disposable deployment tests pass and the script cannot silently deploy a stale legacy configuration.

### Phase 5 — Go module-path decision and migration

First identify all internal and external consumers of the current module path. Decide between retaining the path permanently for compatibility or issuing a deliberate breaking module-path release. If migration is approved, update `go.mod`, every import, test/tool build path, workflow, documentation, and release metadata in one coordinated change. Publish a compatibility note and validate clean builds from a fresh checkout. Do not partially change the module path.

**Gate:** `go test ./...`, race tests, vet, vulnerability scan, ARM64 builds, NIP tools, and downstream import checks pass from a clean checkout.

### Phase 6 — Deprecation and removal

After an operator-defined compatibility window, remove legacy aliases and stale compatibility documentation in a separate change. The removal must include a migration note, a clear release boundary, and a final repository scan proving that only explicitly documented historical references remain.

## Validation contract

Each implementation batch must pass the applicable checks below before it is pushed:

```text
git diff --check
bash -n on every tracked deployment/install script
go test ./...
go test -race ./...
go vet ./...
govulncheck ./...
pnpm install --frozen-lockfile && pnpm build (Blossom)
pnpm audit --prod --audit-level high
./tests/nips/run_coverage.sh --static
```

A final scan will report references by class rather than treating every historical or compatibility string as a defect. Production deployment remains a separate operator action and must not be performed until the relevant staging or disposable checks pass.

## Rollback policy

Cosmetic and documentation changes roll back by reverting the commit. Configuration changes roll back by preserving legacy aliases and restoring the previous precedence contract. Deployment changes roll back to the previous service artifacts and units while preserving `.env`, database files, and blob data. A Go module-path migration rolls back only as a coordinated release; partial import-path changes are prohibited.

## Completion criteria

The migration is complete when public and active defaults use `nostr.ltd`, compatibility aliases have either been removed after the documented window or explicitly retained as permanent compatibility, installers and deployment templates are internally consistent, the Go module-path decision is documented, and the final classified scan contains no unintended Shugur references.
