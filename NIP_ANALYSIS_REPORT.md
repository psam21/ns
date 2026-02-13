# Shugur Relay — NIP Compliance Analysis Report

**Generated:** 2026-02-13  
**Relay Version:** 2.0.0  
**NIP specs analyzed:** 95 files from `temp/nips/`

---

## Executive Summary

- **Already Implemented (claimed):** 34 NIPs — several have gaps (missing event kinds, deprecated status)
- **New NIPs to Consider:** ~30 NIPs with relay-side implications
- **Client-Only / Not Applicable:** ~31 NIPs  
- **Deprecated NIPs the relay still claims:** 2 (NIP-04, NIP-EE)
- **Critical missing kinds for already-claimed NIPs:** NIP-47 (NWC), NIP-18, NIP-23, NIP-89

---

## SECTION 1: Already Implemented NIPs (Claimed in `supported_nips`)

### NIP-01 — Basic protocol flow description
- **Status:** `draft` `mandatory` `relay`
- **Assessment:** ✅ Core protocol. Foundational.
- **Note:** NIP-12 (Generic Tag Queries), NIP-16 (Event Treatment), NIP-20 (Command Results), NIP-33 (Parameterized Replaceable Events) have all been **merged into NIP-01**. The relay still lists 16, 20, 33 separately — cosmetic issue only, not harmful.

### NIP-02 — Follow List
- **Status:** `final` `optional`
- **Assessment:** ✅ Kind 3 in AllowedKinds.

### NIP-03 — OpenTimestamps Attestations for Events
- **Status:** `draft` `optional`
- **Assessment:** ✅ Kind 1040 in AllowedKinds with required `e` tag.

### ⚠️ NIP-04 — Encrypted Direct Message
- **Status:** `final` **`unrecommended`** `optional` `relay`
- **Assessment:** ⚠️ **DEPRECATED** — superseded by NIP-17. Kind 4 is in AllowedKinds.
- **Action:** Consider removing from `supported_nips` advertisement (keep kind 4 in AllowedKinds for backward compat). Update docs to direct users to NIP-17.

### NIP-09 — Event Deletion Request
- **Status:** `draft` `optional` `relay`
- **Assessment:** ✅ Kind 5 in AllowedKinds with required `e` tag.
- **Note:** Spec says relays SHOULD also honor `a` tag deletions (delete all versions of replaceable events up to the deletion timestamp). Verify this is implemented.

### NIP-11 — Relay Information Document
- **Status:** `draft` `optional` `relay`
- **Assessment:** ✅ Implemented.
- **Gaps:** The latest spec adds these fields that may be missing: `self` (relay's own pubkey), `banner`, `privacy_policy`, `terms_of_service`. Verify these are exposed. The `self` field is **critical** for NIP-29 and NIP-43.

### NIP-15 — Nostr Marketplace
- **Status:** `draft` `optional`
- **Assessment:** ✅ Kinds 30017, 30018, 30019, 30020, 1021, 1022 all in AllowedKinds.

### NIP-16 — Event Treatment
- **Status:** `final` `mandatory` — **Merged into NIP-01**
- **Assessment:** ✅ Already part of core. Can remove from `supported_nips` list (cosmetic).

### NIP-17 — Private Direct Messages
- **Status:** `draft` `optional` `relay`
- **Assessment:** ✅ Kinds 14, 15, 1059, 10050 in AllowedKinds.
- **Note:** The spec says relays MUST NOT leak events to non-recipients; check that the relay enforces AUTH for querying kind 14/15 or gift-wrapped events.

### NIP-20 — Command Results
- **Status:** `final` `mandatory` — **Merged into NIP-01**
- **Assessment:** ✅ Part of core. Can remove from `supported_nips` (cosmetic).

### NIP-22 — Comment
- **Status:** `draft` `optional`
- **Assessment:** ✅ Kind 1111 in AllowedKinds.

### NIP-23 — Long-form Content
- **Status:** `draft` `optional`
- **Assessment:** ⚠️ Kind 30023 in AllowedKinds, but **kind 30024 (long-form drafts) is MISSING**.
- **Action:** Add kind `30024` to AllowedKinds.

### NIP-24 — Extra metadata fields and tags
- **Status:** `draft` `optional`
- **Assessment:** ✅ Metadata conventions only. No special relay behavior.

### NIP-25 — Reactions
- **Status:** `draft` `optional`
- **Assessment:** ✅ Kind 7 in AllowedKinds with required `e` and `p` tags.

### NIP-28 — Public Chat
- **Status:** `draft` `optional`
- **Assessment:** ✅ Kinds 40–44 in AllowedKinds with required tags.

### NIP-33 — Parameterized Replaceable Events
- **Status:** `final` `mandatory` — **Merged into NIP-01**
- **Assessment:** ✅ Part of core. Can remove from `supported_nips` (cosmetic).

### NIP-40 — Expiration Timestamp
- **Status:** `draft` `optional` `relay`
- **Assessment:** ✅ Expiration handling present.
- **Note:** Verify relay actually deletes expired events and rejects expired events on ingestion.

### NIP-44 — Encrypted Payloads (Versioned)
- **Status:** `optional`
- **Assessment:** ✅ Client-side encryption format. Relay just stores/relays events. No special handling needed.

### NIP-45 — Event Counts
- **Status:** `draft` `optional` `relay`
- **Assessment:** ✅ `COUNT` verb implemented.
- **Note:** Latest spec adds HyperLogLog support for merging counts across relays. Verify if `hll` field is returned.

### ⚠️ NIP-47 — Nostr Wallet Connect (NWC)
- **Status:** `draft` `optional`
- **Assessment:** ⚠️ **CRITICAL GAP** — Kind 13194 is in AllowedKinds, but the **request/response/notification kinds are MISSING**:
  - `23194` — NWC request
  - `23195` — NWC response
  - `23196` — NWC notification (NIP-04 compat)
  - `23197` — NWC notification
- These are in the ephemeral range (20000–29999) but the relay only whitelists 20000, 20001. If AllowedKinds acts as a strict whitelist, **NWC is broken**.
- **Action:** Add kinds `23194`, `23195`, `23196`, `23197` to AllowedKinds. Alternatively, allow all ephemeral kinds (20000–29999) through.

### NIP-50 — Search Capability
- **Status:** `draft` `optional` `relay`
- **Assessment:** ✅ `search` filter field supported.

### NIP-51 — Lists
- **Status:** `draft` `optional`
- **Assessment:** ✅ Comprehensive coverage. All standard list kinds (10000–10102) and set kinds (30000–39092) present.
- **Minor gap:** Kind `10013` (Relay List for Private Content, defined in NIP-37) is not in AllowedKinds.

### NIP-52 — Calendar Events
- **Status:** `draft` `optional`
- **Assessment:** ✅ Kinds 31922, 31923, 31924, 31925 in AllowedKinds with full tag validation.

### NIP-53 — Live Activities
- **Status:** `draft` `optional`
- **Assessment:** ✅ Kinds 30311, 1311, 30312, 30313, 10312 in AllowedKinds.

### NIP-54 — Wiki
- **Status:** `draft` `optional`
- **Assessment:** ✅ Kinds 30818, 818, 30819 in AllowedKinds.

### NIP-56 — Reporting
- **Status:** `optional`
- **Assessment:** ✅ Kind 1984 in AllowedKinds.

### NIP-57 — Lightning Zaps
- **Status:** `draft` `optional`
- **Assessment:** ✅ Kinds 9734, 9735 in AllowedKinds.
- **Note:** The relay should verify zap receipt signatures and ensure `bolt11` tag is present on kind 9735. This is a validation enhancement.

### NIP-58 — Badges
- **Status:** `draft` `optional`
- **Assessment:** ✅ Kinds 8, 30008, 30009 in AllowedKinds with proper tag validation.

### NIP-59 — Gift Wrap
- **Status:** `optional` `relay`
- **Assessment:** ✅ Kind 1059 in AllowedKinds with required `p` tag.
- **Note:** Kind 13 (seal) is an intermediate event wrapped inside gift wraps and typically does NOT need to be stored independently. Current implementation is OK.

### NIP-60 — Cashu Wallets
- **Status:** `draft` `optional`
- **Assessment:** ✅ Kinds 17375, 7375, 7376, 7374 in AllowedKinds.

### NIP-65 — Relay List Metadata
- **Status:** `draft` `optional`
- **Assessment:** ✅ Kind 10002 in AllowedKinds.

### NIP-72 — Moderated Communities
- **Status:** `draft` `optional`
- **Assessment:** ✅ Kinds 34550, 4550 in AllowedKinds.

### NIP-78 — Arbitrary custom app data
- **Status:** `draft` `optional`
- **Assessment:** ✅ Kind 30078 in AllowedKinds.
- **Note:** The RequiredTags for 30078 specifies `"p"` tag — the spec says content/tags can be anything. The required `p` tag is an unnecessary restriction that could break compatibility with apps using NIP-78 for generic data storage. Consider relaxing to require only `"d"` tag (since it's an addressable event).

### ⚠️ NIP-EE — E2EE Messaging via MLS
- **Status:** `final` **`unrecommended`** `optional`
- **Assessment:** ⚠️ **DEPRECATED** — superseded by the Marmot Protocol. Kinds 443, 444, 445, 10051 are in AllowedKinds.
- **Action:** Consider removing from `supported_nips`. Keep kinds for now but note that this spec is abandoned.

---

## SECTION 2: New NIPs to Add (Relay-Relevant, Not Currently Supported)

### 🔴 HIGH PRIORITY

#### NIP-42 — Authentication of clients to relays
- **Status:** `draft` `optional` `relay`
- **Why:** Core relay protocol feature. Required by NIP-70 (Protected Events), NIP-43 (Relay Access), NIP-29 groups, and private DM enforcement. Adds `AUTH` message to the relay protocol.
- **Work:** Implement `AUTH` challenge/response flow. No new event kinds (kind 22242 is ephemeral and not stored).
- **Ecosystem adoption:** Very widely adopted.

#### NIP-70 — Protected Events
- **Status:** `draft` `optional` `relay`
- **Why:** Security feature. Relay MUST reject events with `["-"]` tag by default. If NIP-42 AUTH is implemented, relay MAY accept them from authenticated authors.
- **Work:** Add validation rule: reject events containing `["-"]` tag unless author is authenticated.
- **Depends on:** NIP-42.

#### NIP-62 — Request to Vanish
- **Status:** `draft` `optional` `relay`
- **Why:** Privacy/legal compliance. Kind `62` requests deletion of ALL events from a pubkey. Legally binding in some jurisdictions.
- **Work:** Handle kind 62 events: delete all events from the pubkey, delete gift wraps p-tagged to that pubkey, prevent re-broadcast.
- **Kinds to add:** `62`

#### NIP-77 — Negentropy Syncing
- **Status:** `draft` `optional` `relay`
- **Why:** Efficient relay-relay and client-relay syncing. Uses bandwidth-efficient set reconciliation. Important for relay operators.
- **Work:** Implement `NEG-OPEN`, `NEG-MSG`, `NEG-CLOSE`, `NEG-ERR` protocol messages. No new event kinds.

#### NIP-86 — Relay Management API
- **Status:** `draft` `optional`
- **Why:** Standardized relay admin API (ban/allow pubkeys, list banned, etc.). The relay already has an admin API — this would standardize it.
- **Work:** Implement JSON-RPC over HTTP with `application/nostr+json+rpc` content type.

### 🟡 MEDIUM PRIORITY

#### NIP-13 — Proof of Work
- **Status:** `draft` `optional` `relay`
- **Why:** Spam deterrence. Relays can require minimum PoW for publishing events.
- **Work:** Validate `nonce` tags and check leading zero bits in event IDs. Optional difficulty requirement.

#### NIP-18 — Reposts
- **Status:** `draft` `optional`
- **Why:** Kind 6 (repost) IS in AllowedKinds, but **kind 16 (generic repost) is MISSING**. Generic reposts for non-kind-1 events are common.
- **Kinds to add:** `16`

#### NIP-29 — Relay-based Groups
- **Status:** `draft` `optional` `relay`
- **Why:** Full relay-managed group system. Very complex to implement properly but increasingly adopted.
- **Kinds to add:** `9` (already present), `11`, `12`, `9000`–`9009`, `9021`, `9022`, `39000`, `39001`, `39002`, `39003`
- **Work:** Relay must sign its own group events, enforce membership rules, handle moderation events. Requires relay keypair (`self` in NIP-11).

#### NIP-7D — Threads
- **Status:** `draft` `optional`
- **Why:** Simple threads using kind 11. Replies use NIP-22 kind 1111 (already supported).
- **Kinds to add:** `11`

#### NIP-32 — Labeling
- **Status:** `draft` `optional`
- **Why:** Distributed moderation, content classification. Used by many clients.
- **Kinds to add:** `1985`

#### NIP-68 — Picture-first feeds
- **Status:** `draft` `optional`
- **Why:** Instagram/Flickr-style picture posts. Growing ecosystem (Olas, etc).
- **Kinds to add:** `20`

#### NIP-71 — Video Events
- **Status:** `draft` `optional`
- **Why:** YouTube/TikTok-style video events. Growing ecosystem.
- **Kinds to add:** `21`, `22`, `34235`, `34236`

#### NIP-89 — Recommended Application Handlers
- **Status:** `draft` `optional`
- **Assessment:** Kind 31989 IS in AllowedKinds, but **kind 31990 (handler information) is MISSING**.
- **Kinds to add:** `31990`

#### NIP-94 — File Metadata
- **Status:** `draft` `optional`
- **Why:** File sharing classification. Kind 1063 for file metadata events.
- **Kinds to add:** `1063`

#### NIP-B7 — Blossom media
- **Status:** `draft` `optional`
- **Why:** Replaces deprecated NIP-96. Blossom server lists (kind 10063). Particularly relevant since this project IS a Blossom setup.
- **Kinds to add:** `10063`

#### NIP-C7 — Chats
- **Status:** `draft` `optional`
- **Assessment:** Kind 9 is already in AllowedKinds. ✅ No action needed.

### 🟢 LOW PRIORITY (Niche but useful)

#### NIP-34 — git stuff
- **Status:** `draft` `optional`
- **Kinds to add:** `30617`, `30618`, `1617`, `1621`, `1622`, `1630`–`1633`
- **Note:** Only relevant if you want to support Nostr-native git collaboration.

#### NIP-35 — Torrents
- **Status:** `draft` `optional`
- **Kinds to add:** `2003`

#### NIP-37 — Draft Wraps
- **Status:** `draft` `optional`
- **Kinds to add:** `31234`, `10013`

#### NIP-38 — User Statuses
- **Status:** `draft` `optional`
- **Kinds to add:** `30315`

#### NIP-39 — External Identities in Profiles
- **Status:** `draft` `optional`
- **Kinds to add:** `10011`

#### NIP-43 — Relay Access Metadata and Requests
- **Status:** `draft` `optional` `relay`
- **Kinds to add:** `13534`, `8000`, `8001`, `28934`, `10010`
- **Note:** Complex relay integration. The relay publishes its own membership events.

#### NIP-46 — Nostr Remote Signing
- **Status:** (no explicit status markers)
- **Assessment:** Kind 24133 is already in AllowedKinds. ✅ Adequate.

#### NIP-61 — Nutzaps
- **Status:** `draft` `optional`
- **Assessment:** Kinds 9321 and 10019 are already in AllowedKinds, but NIP-61 is NOT in `supported_nips`.
- **Action:** Add `61` to DefaultSupportedNIPs.

#### NIP-64 — Chess (PGN)
- **Kinds to add:** `64`

#### NIP-66 — Relay Discovery and Liveness Monitoring
- **Status:** `draft` `optional` `relay`
- **Kinds to add:** `30166`, `10166`

#### NIP-69 — Peer-to-peer Order events
- **Kinds to add:** `38383`

#### NIP-75 — Zap Goals
- **Kinds to add:** `9041`

#### NIP-84 — Highlights
- **Kinds to add:** `9802`

#### NIP-85 — Trusted Assertions
- **Kinds to add:** `30382`, `30383`, `30384`, `30385`

#### NIP-87 — Ecash Mint Discoverability
- **Kinds to add:** `38173`, `38172`, `38000`

#### NIP-88 — Polls
- **Kinds to add:** `1068` (poll), `1018` (response)

#### NIP-90 — Data Vending Machine
- **Status:** `draft` `optional`
- **Note:** Reserves entire range 5000–7000. Adding all is impractical. Consider supporting commonly used DVMs:
  - `5000`–`5999` (job requests), `6000`–`6999` (job results), `7000` (feedback)

#### NIP-99 — Classified Listings
- **Kinds to add:** `30402`, `30403`

#### NIP-A0 — Voice Messages
- **Kinds to add:** `1222`, `1244`

#### NIP-A4 — Public Messages
- **Kinds to add:** `24`

#### NIP-B0 — Web Bookmarking
- **Kinds to add:** `39701`

#### NIP-C0 — Code Snippets
- **Kinds to add:** `1337`

---

## SECTION 3: Client-Only / Not Applicable NIPs

These NIPs have **zero relay-side implications**. The relay just stores/relays events with no special handling needed.

| NIP | Title | Status | Notes |
|-----|-------|--------|-------|
| 05 | DNS-based identifiers | `final` | Client-side verification only |
| 06 | Key derivation from mnemonic | `draft` | Client-side key management |
| 07 | `window.nostr` browser capability | `draft` | Browser extension API |
| 08 | Handling Mentions | `final` **`unrecommended`** | Deprecated → NIP-27. Client rendering |
| 10 | Text Notes and Threads | `draft` | Kind 1 already supported; threading is client-side |
| 12 | Generic Tag Queries | `final` | Merged into NIP-01 |
| 14 | Subject tag in Text events | `draft` | Tag convention only |
| 19 | bech32-encoded entities | `draft` | Encoding format, client-side |
| 21 | `nostr:` URI scheme | `draft` | URI scheme, client rendering |
| 26 | Delegated Event Signing | `draft` **`unrecommended`** | Deprecated. Don't implement |
| 27 | Text Note References | `draft` | Client-side rendering of mentions |
| 30 | Custom Emoji | `draft` | Tag convention only |
| 31 | Unknown event kinds | `draft` | `alt` tag convention, client rendering |
| 36 | Content Warning | `draft` | Tag convention only |
| 48 | Proxy Tags | `draft` | Tag convention for bridged events |
| 49 | Private Key Encryption | `draft` | Client-side key encryption |
| 55 | Android Signer Application | `draft` | Android platform-specific |
| 73 | External Content IDs | `draft` | Tag convention for external references |
| 92 | Media Attachments | (none) | `imeta` tag convention only |
| 96 | HTTP File Storage | `draft` **`unrecommended`** | Deprecated → NIP-B7. HTTP API, not relay |
| 98 | HTTP Auth | `draft` | HTTP authentication, not relay protocol |
| BE | Nostr BLE Communications | `draft` | BLE device communication |

---

## SECTION 4: Deprecated/Superseded NIPs the Relay Claims to Support

| NIP | Title | Status | Superseded By | Action |
|-----|-------|--------|---------------|--------|
| **04** | Encrypted DM | `unrecommended` | NIP-17 | Remove from `supported_nips`; keep kind 4 in AllowedKinds for compat |
| **16** | Event Treatment | merged into NIP-01 | NIP-01 | Remove from `supported_nips` (cosmetic) |
| **20** | Command Results | merged into NIP-01 | NIP-01 | Remove from `supported_nips` (cosmetic) |
| **33** | Parameterized Replaceable Events | merged into NIP-01 | NIP-01 | Remove from `supported_nips` (cosmetic) |
| **EE** | E2EE via MLS | `unrecommended` | Marmot Protocol | Remove from `supported_nips`; keep kinds for compat |

---

## SECTION 5: Critical Action Items Summary

### Immediate Fixes (bugs in current claimed support)

1. **NIP-47 NWC is broken** — Add kinds `23194`, `23195`, `23196`, `23197` to AllowedKinds (or open ephemeral range 20000–29999)
2. **NIP-23 incomplete** — Add kind `30024` (long-form drafts)
3. **NIP-89 incomplete** — Add kind `31990` (handler information)
4. **NIP-18 missing generic repost** — Add kind `16`
5. **NIP-78 over-validated** — RequiredTags for kind 30078 specifies `"p"` but spec says content/tags are arbitrary. Should only require `"d"` tag.
6. **NIP-61 kinds present but undeclared** — Add `61` to DefaultSupportedNIPs

### High Priority New Features

7. **NIP-42** — AUTH protocol (required by NIP-70, NIP-43, NIP-29)
8. **NIP-70** — Protected events (requires NIP-42)
9. **NIP-62** — Request to Vanish (privacy/legal)
10. **NIP-77** — Negentropy Syncing (relay efficiency)
11. **NIP-86** — Relay Management API (standardize admin)

### Deprecation Cleanup

12. Remove NIP-04 and NIP-EE from `supported_nips`
13. Optionally remove NIP-16, NIP-20, NIP-33 from `supported_nips` (they're now part of NIP-01)

### Full AllowedKinds Addition List

```
// Immediate fixes for claimed NIPs:
16      // NIP-18: Generic Repost
30024   // NIP-23: Long-form Draft
31990   // NIP-89: Handler Information
23194   // NIP-47: NWC Request (ephemeral)
23195   // NIP-47: NWC Response (ephemeral)
23196   // NIP-47: NWC Notification compat (ephemeral)
23197   // NIP-47: NWC Notification (ephemeral)

// High priority additions:
62      // NIP-62: Request to Vanish
1985    // NIP-32: Labels
20      // NIP-68: Picture events
21      // NIP-71: Normal video
22      // NIP-71: Short video
34235   // NIP-71: Addressable normal video
34236   // NIP-71: Addressable short video
11      // NIP-7D: Threads
1063    // NIP-94: File Metadata
10063   // NIP-B7: Blossom Server List

// Medium priority additions:
30315   // NIP-38: User Statuses
10011   // NIP-39: External Identities
10013   // NIP-37: Relay List for Private Content
31234   // NIP-37: Draft Wraps
30402   // NIP-99: Classified Listing
30403   // NIP-99: Draft Classified Listing
2003    // NIP-35: Torrents
9041    // NIP-75: Zap Goals
9802    // NIP-84: Highlights
1068    // NIP-88: Poll
1018    // NIP-88: Poll Response
1337    // NIP-C0: Code Snippets
1222    // NIP-A0: Voice Message
1244    // NIP-A0: Voice Reply
24      // NIP-A4: Public Messages
39701   // NIP-B0: Web Bookmarks
64      // NIP-64: Chess

// NIP-34 git (if desired):
30617, 30618, 1617, 1621, 1622, 1630-1633

// NIP-29 groups (complex, if desired):
9000-9009, 9021, 9022, 39000, 39001, 39002, 39003

// NIP-66 relay discovery:
30166, 10166

// NIP-85 trusted assertions:
30382, 30383, 30384, 30385

// NIP-87 ecash mint:
38173, 38172, 38000

// NIP-69 P2P orders:
38383

// NIP-90 DVM (range):
5000-5999, 6000-6999, 7000

// NIP-43 relay access:
13534, 8000, 8001, 28934, 10010
```

---

## SECTION 6: Ephemeral Event Range Recommendation

The relay currently only whitelists ephemeral kinds `20000` and `20001`. Per NIP-01, the **entire range 20000–29999** should be treated as ephemeral (not stored, only relayed to connected clients). This would automatically cover:

- NIP-47 NWC kinds (23194–23197)
- NIP-46 remote signing (24133 — already added)
- Any future ephemeral kinds

**Recommendation:** Instead of whitelisting individual ephemeral kinds, accept ALL kinds in the 20000–29999 range as ephemeral events and relay them without storage. This matches NIP-01 spec behavior.
