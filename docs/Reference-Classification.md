# Shugur Reference Classification Table

**Last updated**: 2026-09-03
**Source**: `docs/Shugur-Migration-Plan.md` §Reference classes and planned treatment
**Tracking issue**: #120

This is the canonical reference-class table that drives the final audit (#110). It is published as an issue so the classification is reviewable and editable independent of the audit run.

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

## Migration principles (preserved from the original plan)

1. No blind global replacement. Every occurrence is classified before editing.
2. No secret or data migration in the same change as a branding change.
3. Compatibility aliases are deliberate, documented, and time-bounded.
4. Database identifiers and storage paths are preserved unless a dedicated migration is planned.
5. Go module-path changes are atomic and coordinated.

## Current classification status (as of 2026-09-03)

| Class | Status | Evidence |
|---|---|---|
| Public branding | ✅ Complete | NIP-11 returns "nostr.ltd", dashboard uses nostr.ltd branding |
| Active defaults | ✅ Complete | All defaults updated to nostr.ltd |
| Configuration keys | ⏸ Pending removal | NOSTR_* canonical, SHUGUR_* fallback active (issue #111) |
| Database identifiers | ✅ Preserved | No database migration performed |
| Container/image references | ✅ Local-build explicit | No GHCR images published |
| Go module/import path | ⏸ Deferred | Issue #118 (follow-up review: 2027-03-01) |
| Tests and fixtures | ✅ Complete | Tests updated to canonical names |
| Documentation and guidance | ✅ Complete | All docs reflect canonical contract |
