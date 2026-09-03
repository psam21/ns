# Shugur Migration Completion Criteria

**Last updated**: 2026-09-03
**Source**: `docs/Shugur-Migration-Plan.md` §Completion criteria
**Tracking issue**: #122

This is the canonical completion criteria for the Shugur migration. It is published as an issue so the criteria are reviewable and editable independent of any specific phase.

## Completion criteria

The migration is complete when:

- Public and active defaults use `nostr.ltd`.
- Compatibility aliases have either been removed after the documented window (#111) or explicitly retained as permanent compatibility.
- Installers and deployment templates are internally consistent.
- The Go module-path decision is documented (#109 / #118).
- The final classified scan (#110) contains no unintended Shugur references.

## Current status (as of 2026-09-03)

| Criterion | Status | Evidence |
|---|---|---|
| Public and active defaults use `nostr.ltd` | ✅ MET | NIP-11 returns "nostr.ltd", all defaults updated |
| Compatibility aliases removed or explicitly retained | ⏸ Retained | `SHUGUR_*` fallbacks active (issue #111, gate condition 4 pending) |
| Installers and deployment templates internally consistent | ✅ MET | `deploy/ns-deploy-full.sh` uses canonical variables |
| Go module-path decision documented | ✅ MET | Issue #118 closed as DEFER (follow-up: 2027-03-01) |
| Final classified scan contains no unintended references | ✅ MET | Issue #110 closed, audit complete |

## Summary

4 of 5 completion criteria are met. The remaining criterion (compatibility aliases) is intentionally deferred per the production migration gate in issue #111. The `SHUGUR_*` fallbacks are a deliberate compatibility feature, not unfinished cleanup.

## Source

Migrated from `docs/Shugur-Migration-Plan.md` §Completion criteria.
