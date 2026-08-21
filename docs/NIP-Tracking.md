# NIP Implementation Tracking Document

**Generated:** 2026-08-21  
**Source:** https://github.com/nostr-protocol/nips (master branch)  
**Project:** NostrRelayBlossom (relay + blossom)

---

## Legend
- 🔴 **Critical** - Security, deprecated/unrecommended NIPs, breaking changes
- 🟡 **High** - Recent spec updates, missing high-value NIPs
- 🟢 **Medium** - Missing useful NIPs, spec clarifications
- 🔵 **Low** - Nice-to-have, niche NIPs
- ✅ **Done** - Already implemented and current

---

## 🔴 CRITICAL: Unrecommended/Deprecated NIPs (Remove or Deprecate)

| # | NIP | Current Status | Official Status | Action Required | Files to Modify |
|---|-----|----------------|-----------------|-----------------|-----------------|
| 1 | NIP-03 | ✅ Implemented | **Unrecommended**: "vulnerable to one specific attack, needs update" | Remove from advertised list or add warning | `relay/internal/constants/relay_metadata.go`, `relay/internal/relay/nips/nip03.go` |
| 2 | NIP-04 | ✅ Implemented | **Unrecommended**: "deprecated in favor of NIP-17" | Remove from advertised list, keep NIP-17 only | `relay/internal/constants/relay_metadata.go`, `relay/internal/relay/nips/nip04.go` |
| 3 | NIP-15 | ✅ Implemented | **Unrecommended**: "too complicated, try NIP-99 instead" | Remove from advertised list, implement NIP-99 | `relay/internal/constants/relay_metadata.go`, `relay/internal/relay/nips/nip15.go` |
| 4 | NIP-26 | ✅ Implemented | **Unrecommended**: "adds unnecessary burden for little gain" | Remove from advertised list | `relay/internal/constants/relay_metadata.go`, `relay/internal/relay/nips/nip26.go` |
| 5 | NIP-28 | ✅ Implemented | **Unrecommended**: "try NIP-29 instead" | Remove from advertised list, ensure NIP-29 complete | `relay/internal/constants/relay_metadata.go`, `relay/internal/relay/nips/nip28.go` |
| 6 | NIP-72 | ✅ Implemented | **Unrecommended**: "try NIP-29 instead" | Remove from advertised list, ensure NIP-29 complete | `relay/internal/constants/relay_metadata.go`, `relay/internal/relay/nips/nip72.go` |
| 7 | NIP-90 | ✅ Implemented | **Unrecommended**: "got totally out of control" | Remove from advertised list | `relay/internal/constants/relay_metadata.go`, `relay/internal/relay/nips/nip90.go` |
| 8 | NIP-96 | ✅ Implemented | **Unrecommended**: "replaced by Blossom" | Remove from advertised list (Blossom handles this) | `relay/internal/constants/relay_metadata.go`, `relay/internal/relay/nips/nip96.go` |
| 9 | NIP-EE | ✅ Implemented | **Unrecommended**: "superseded by Marmot Protocol" | Remove from advertised list, reference Marmot | `relay/internal/constants/relay_metadata.go`, `relay/internal/relay/nips/nip_ee.go` |

---

## 🟡 HIGH: Recent Spec Updates (Validator Reviews Needed)

| # | NIP | Last Update | Change Summary | Files to Review/Update |
|---|-----|-------------|----------------|------------------------|
| 10 | NIP-29 | 2 weeks ago | Added "previous" tag example | `relay/internal/relay/nips/nip29.go`, `relay/internal/relay/nip29.go` |
| 11 | NIP-47 | 3 weeks ago | Simplified core spec, added extensions | `relay/internal/relay/nips/nip47.go` |
| 12 | NIP-78 | 3 months ago | "a normal application-specific kind" | `relay/internal/relay/nips/nip78.go` |
| 13 | NIP-45 | 6 months ago | Added HyperLogLog relay response | `relay/internal/relay/nips/nip45.go` |
| 14 | NIP-44 | 2 months ago | Allow encrypting >65535 bytes | `relay/internal/relay/nips/nip44.go`, `relay/internal/constants/capsules.go` |
| 15 | NIP-43 | 2 months ago | Added relay roles event | `relay/internal/relay/nips/nip43.go`, `relay/internal/relay/nip43.go` |
| 16 | NIP-86 | 2 months ago | Added relay roles event | `relay/internal/relay/nips/nip86.go` |
| 17 | NIP-51 | Last month | Added kind:10011, favorite follow sets | `relay/internal/relay/nips/nip51.go` |
| 18 | NIP-34 | Last month | Dropped GRASP hosting instructions | `relay/internal/relay/nips/nip34.go` |
| 19 | NIP-46 | Last month | Avoid silent timeouts | `relay/internal/relay/nips/nip46.go` |
| 20 | NIP-15 | Last month | Try NIP-99 instead (see #3) | `relay/internal/relay/nips/nip15.go` |
| 21 | NIP-69 | 9 months ago | Order expiration support | `relay/internal/relay/nips/nip69.go` |
| 22 | NIP-60 | 10 months ago | Added "unit" tag to NutZap | `relay/internal/relay/nips/nip60.go` |

---

## 🟢 MEDIUM: Missing High-Value NIPs (Not Implemented)

| # | NIP | Description | Priority | Implementation Notes |
|---|-----|-------------|----------|---------------------|
| 23 | NIP-05 | DNS-based identifier mapping | High | Identity resolution, relay should support |
| 24 | NIP-07 | window.nostr for browsers | High | Client-side, relay advertises support |
| 25 | NIP-10 | Text Notes and Threads | High | Threading support (reply chains) |
| 26 | NIP-19 | bech32-encoded entities | Medium | npub/nsec/note encoding |
| 27 | NIP-21 | nostr: URI scheme | Medium | URI handling |
| 28 | NIP-27 | Text Note References | High | Replaces NIP-08, reply threading |
| 29 | NIP-36 | Sensitive Content | Medium | Content warnings |
| 30 | NIP-46 | Nostr Remote Signing | High | Signer/bunker support |
| 31 | NIP-48 | Bridged Events | Medium | Cross-relay events |
| 32 | NIP-49 | Private Key Encryption (ncryptsec) | Medium | Encrypted nsec |
| 33 | NIP-55 | Android Signer Application | Low | Client-side |
| 33 | NIP-5A | Static Websites (nsites) | Medium | Web hosting on Nostr |
| 34 | NIP-67 | EOSE Completeness Hint | Medium | Sync optimization |
| 35 | NIP-68 | Picture-first feeds | Low | Media feeds |
| 36 | NIP-73 | External Content IDs | Low | Content addressing |
| 37 | NIP-87 | Cashu/Fedimint Discovery | Medium | Ecash mint discovery |
| 38 | NIP-89 | Recommended App Handlers | Low | Client hints |
| 39 | NIP-92 | Media Attachments (imeta) | **High** | **Blossom integration critical** |
| 40 | NIP-94 | File Metadata | Medium | File info events |
| 41 | NIP-98 | HTTP Auth | Medium | REST API auth |
| 42 | NIP-A0 | Voice Messages | Low | Audio events |
| 43 | NIP-A4 | Public Messages | Low | Public DMs |
| 44 | NIP-B0 | Web Bookmarks | Low | Bookmark events |
| 44 | NIP-B7 | Blossom | ✅ **Implemented in Blossom service** | Already done |
| 45 | NIP-C0 | Code Snippets | Low | Dev-focused |
| 46 | NIP-C7 | Chats | Medium | Chat events |
| 47 | NIP-CC | Geocaching | Low | Niche |
| 48 | NIP-F4 | Podcasts | Low | Podcast events |

---

## 🔵 LOW: Niche/Specialized NIPs

| # | NIP | Description | Notes |
|---|-----|-------------|-------|
| 49 | NIP-06 | Mnemonic seed phrase | Unrecommended |
| 50 | NIP-08 | Mentions | Unrecommended (use NIP-27) |
| 51 | NIP-14 | Subject tag | Niche |
| 52 | NIP-20 | Command Results | Niche |
| 53 | NIP-31 | Unknown Events | Unrecommended |
| 54 | NIP-35 | Torrents | Niche |
| 55 | NIP-55 | Android Signer | Client-side |
| 56 | NIP-66 | Relay Discovery | Already have NIP-65/66 |
| 57 | NIP-75 | Zap Goals | Implemented? |
| 58 | NIP-77 | Negentropy Syncing | Implemented? |
| 59 | NIP-7D | Forum Threads | Implemented? |
| 60 | NIP-84 | Highlights | Implemented? |
| 61 | NIP-85 | Trusted Assertions | Implemented? |
| 62 | NIP-88 | Polls | Implemented? |
| 63 | NIP-89 | App Handlers | Low |
| 64 | NIP-99 | Classified Listings | Implemented? |

---

## 📋 Blossom-Specific NIPs

| # | NIP | Status | Notes |
|---|-----|--------|-------|
| 65 | NIP-B7 | ✅ **Implemented** | Blossom protocol - media server |
| 66 | NIP-96 | ❌ Deprecated | Replaced by NIP-B7 (Blossom) |
| 67 | NIP-92 | ❌ Missing | **Critical** - imeta tags for media metadata |
| 68 | NIP-94 | ❌ Missing | File metadata events |
| 69 | NIP-98 | ❌ Missing | HTTP Auth for Blossom API |

---

## 📊 Implementation Status Summary

| Category | Count |
|----------|-------|
| 🔴 Critical (Remove/Deprecate) | 9 |
| 🟡 High (Recent Updates) | 13 |
| 🟢 Medium (Missing High-Value) | 20 |
| 🔵 Low (Niche) | 15 |
| ✅ Already Implemented & Current | ~40 |
| **Total Tracked** | **~97** |

---

## 🎯 Next Steps Priority Order

### Phase 1: Cleanup (Week 1)
- [ ] Remove NIP-03, NIP-04, NIP-15, NIP-26, NIP-28, NIP-72, NIP-90, NIP-96, NIP-EE from advertised list
- [ ] Add deprecation notices to validators

### Phase 2: Spec Compliance (Week 2-3)
- [ ] Update validators for NIP-29, NIP-47, NIP-78, NIP-45, NIP-44, NIP-43, NIP-86, NIP-51, NIP-34, NIP-46, NIP-69, NIP-60
- [ ] Implement NIP-92 (imeta) for Blossom

### Phase 3: High-Value Additions (Week 3-5)
- [ ] Implement NIP-05, NIP-07, NIP-10, NIP-27, NIP-46, NIP-98
- [ ] Add NIP-19, NIP-21, NIP-36, NIP-48, NIP-49

### Phase 4: Nice-to-Have (Ongoing)
- [ ] NIP-5A, NIP-67, NIP-87, NIP-C7, NIP-F4, etc.

---

## 📝 GitHub Issues to Create

Each row above should become a GitHub issue with:
- **Title**: `[NIP-XX] Description`
- **Labels**: `nip`, `priority: critical/high/medium/low`, `type: cleanup/update/new`
- **Description**: Links to spec, files to modify, acceptance criteria
- **Assignee**: (to be assigned)
- **Milestone**: Phase 1/2/3/4

---

## 🔗 References
- Official NIPs: https://github.com/nostr-protocol/nips
- NIP Registry: https://github.com/nostr-protocol/registry-of-kinds
- Project Repo: https://github.com/psam21/ns