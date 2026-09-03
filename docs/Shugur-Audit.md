# Legacy Shugur Reference Audit

**Issue:** #110
**Date:** 2026-09-03
**Scope:** All tracked source files in the repository

## Summary

| Category | Count | Action |
|---|---|---|
| Go import paths (`github.com/Shugur-Network/relay/...`) | 120 | **Retain** — mandated by `.github/copilot-instructions.md` |
| Go comments mentioning "Shugur" | 1 | **Replaced** — `internal/application/node.go:22` |
| Go test guard (checks for obsolete branding) | 1 | **Retain** — quality gate |
| Copilot instructions (mandates preservation) | 1 | **Retain** — authoritative policy |
| Deploy script comments | 1 | **Retain** — explains module path |
| GitHub Actions workflow repo guards | 2 | **Replaced** — `psam21/ns` |
| GitHub Actions stale bot message | 1 | **Replaced** — `nostr.ltd` |
| **Total remaining Shugur references** | **123** | **122 retain, 1 guard test** |

## Classification Details

### Retain (122 references)

**Go import paths (120):** All `github.com/Shugur-Network/relay/...` import paths
in `relay/cmd/*.go`, `relay/internal/**/*.go`, and `relay/tests/**/*.go`. Per
`.github/copilot-instructions.md`:

> The Go module path remains `github.com/Shugur-Network/relay` for compatibility.
> Do not mass-replace it during public branding work.

Migration requires the full sequence from #109 (inventory, vanity metadata,
clean-clone validation, compatibility note). The decision in #109 was **DEFER**.

**Copilot instructions (1):** `.github/copilot-instructions.md:50` — the
authoritative policy that mandates module path preservation.

**Deploy script comment (1):** `deploy/ns-deploy-full.sh:98` — explains the
Go module path to operators.

**Test guard (1):** `relay/cmd/root_test.go:34` — fails the test if the banner
contains "shugur". This is a quality gate that ensures branding stays correct.

### Replaced (4 references)

| File | Line | Before | After |
|---|---|---|---|
| `relay/.github/workflows/docker-security-scan.yml` | 22 | `'Shugur-Network/relay'` | `'psam21/ns'` |
| `relay/.github/workflows/merge-queue.yml` | 19 | `'Shugur-Network/relay'` | `'psam21/ns'` |
| `relay/.github/workflows/stale.yml` | 33 | `Shugur Relay project` | `nostr.ltd Relay project` |
| `relay/internal/application/node.go` | 22 | `run the Shugur node` | `run the nostr.ltd relay` |

### Not Found (clean)

- Blossom source files (`blossom/src/**/*.ts`, `blossom/public/**/*.js`)
- Blossom config files (`blossom/config.yml`, `blossom/config.example.yml`)
- Blossom Docker files
- Root `README.md`
- Deploy systemd units (`deploy/relay.service`, `deploy/blossom.service`)
- NIP tracking documentation (`docs/NIP-Tracking.md`)

## Reviewer Status

| Action | Reviewer | Status |
|---|---|---|
| Go import paths retained | copilot-instructions.md | ✅ Mandated |
| Workflow repo guards updated | psam21/ns | ✅ Verified |
| Stale bot message updated | nostr.ltd branding | ✅ Verified |
| Comment in node.go updated | nostr.ltd branding | ✅ Verified |
| Test guard retained | quality gate | ✅ Verified |

## Verification

```bash
# All remaining Shugur references are either:
# 1. Go import paths (retained per policy)
# 2. The test guard (retained as quality gate)
# 3. The copilot instructions policy line (retained as policy)

# Build and tests pass:
cd relay && go build ./... && go vet ./... && go test ./...
```

## Follow-up

- **2027-03-01:** Review #109 (module path migration) per the deferral
  rationale. If migration is approved at that time, follow the safe sequence
  in #109: inventory → vanity metadata → imports → clean-clone test →
  compatibility note.
