# Shugur Migration Rollback Policy

**Last updated**: 2026-09-03
**Source**: `docs/Shugur-Migration-Plan.md` §Rollback policy
**Tracking issue**: #121

This is the canonical rollback policy for the Shugur migration. It is published as an issue so the policy is reviewable and editable independent of any specific phase.

## Rollback policy by change type

| Change type | Rollback mechanism |
|---|---|
| Cosmetic and documentation changes | Revert the commit. |
| Configuration changes | Preserve legacy aliases and restore the previous precedence contract. |
| Deployment changes | Roll back to the previous service artifacts and units while preserving `.env`, database files, and blob data. |
| Go module-path migration | Roll back only as a coordinated release; partial import-path changes are prohibited. |

## Constraints

- `.env`, database files, and blob data must never be touched by a rollback.
- Partial import-path changes are prohibited (a Go module-path migration is atomic or not at all).
- Configuration rollbacks must restore the previous precedence contract, not just remove the new keys.

## Implementation evidence

### Cosmetic and documentation changes
- Reverted via `git revert <commit-sha>`
- No production state changes

### Configuration changes
- `SHUGUR_*` environment variables remain as fallbacks for `NOSTR_*`
- Precedence: `NOSTR_*` takes precedence over `SHUGUR_*`
- Deprecation warning emitted without exposing values
- Removal gated by issue #111

### Deployment changes
- `deploy/ns-deploy-full.sh` includes two-service rollback logic
- Backup files created before swap: `relay-arm64.bak`, `index.html.bak`, `style.css.bak`, `script.js.bak`
- If relay restart fails after Blossom swap, Blossom is restored from backup
- Rollback test verified: `scripts/rollback-test.sh` PASSED

### Go module-path migration
- Deferred per issue #118
- When implemented: atomic release only, no partial changes

## Rollback test results (2026-09-03)

```
=== Rollback Test ===

Step 1: Recording current binary hash...
Original binary SHA-256: c9f26697db13baf412a032dba0f13ea0c72738d7571b52c3a64ac814c9b5c4d9

Step 2: Creating backup...
Backup SHA-256: c9f26697db13baf412a032dba0f13ea0c72738d7571b52c3a64ac814c9b5c4d9 (matches original)

Step 3: Stopping service and corrupting binary...
Corrupted binary SHA-256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 (different from original)

Step 4: Verifying service fails with corrupted binary...
Service status: activating/failed

Step 5: Restoring from backup (simulating rollback)...
Restored binary SHA-256: c9f26697db13baf412a032dba0f13ea0c72738d7571b52c3a64ac814c9b5c4d9 (matches original)

Step 6: Restarting service...
Service status: active

Step 7: Verifying relay is responding...
HTTP code: 202

=== Rollback Test PASSED ===
```
