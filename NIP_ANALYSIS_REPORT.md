# Shugur Relay — Pending NIP Items

**Updated:** 2026-02-13  
**Current NIP count:** 62 (55 numeric + 7 string)

---

## Compliance Improvements for Claimed NIPs

### NIP-11 — Relay Information Document
- Add optional fields: `self` (relay's own pubkey), `banner`, `privacy_policy`, `terms_of_service`
- The `self` field is needed for NIP-29 groups and NIP-43 relay access

### NIP-17 — Private Direct Messages
- Enforce AUTH (NIP-42) for querying kind 14/15 and gift-wrapped events to prevent leaking to non-recipients

### NIP-40 — Expiration Timestamp
- Verify relay rejects expired events on ingestion and periodically purges expired events from storage

### NIP-45 — Event Counts
- Add HyperLogLog (`hll`) field support for merging counts across relays (latest spec addition)

### NIP-57 — Lightning Zaps
- Consider validating `bolt11` tag presence on kind 9735 zap receipts

---

## Missing Kinds for Already-Claimed NIPs

| NIP | Missing Kind | Description |
|-----|-------------|-------------|
| NIP-18 | `16` | Generic repost (for non-kind-1 events) |
| NIP-71 | `22`, `34236` | Short video event, addressable short video |
| NIP-A0 | `1244` | Voice reply |
| NIP-99 | `30403` | Draft classified listing |
| NIP-B7 | `10063` | Blossom server list (relevant since this project IS a Blossom setup) |

---

## New NIPs to Implement

### 🔴 HIGH PRIORITY

#### NIP-77 — Negentropy Syncing
- Efficient relay-relay and client-relay syncing via set reconciliation
- Implement `NEG-OPEN`, `NEG-MSG`, `NEG-CLOSE`, `NEG-ERR` protocol messages
- No new event kinds

#### NIP-86 — Relay Management API
- Standardized admin API (ban/allow pubkeys, list banned, etc.)
- The relay already has a custom admin API — this would standardize it
- Implement JSON-RPC over HTTP with `application/nostr+json+rpc` content type

### 🟡 MEDIUM PRIORITY

#### NIP-13 — Proof of Work
- Spam deterrence via `nonce` tag validation and leading zero bit checks
- Optional configurable difficulty requirement

#### NIP-29 — Relay-based Groups
- Full relay-managed group system (complex)
- Kinds: `9000`–`9009`, `9021`, `9022`, `39000`–`39003`
- Requires relay keypair (`self` in NIP-11), membership enforcement, moderation events

#### NIP-43 — Relay Access Metadata and Requests
- Kinds: `13534`, `8000`, `8001`, `28934`, `10010`
- Complex relay integration — relay publishes its own membership events

#### NIP-66 — Relay Discovery and Liveness Monitoring
- Kinds: `30166`, `10166`

### 🟢 LOW PRIORITY

#### NIP-39 — External Identities in Profiles
- Kind `10011`

#### NIP-64 — Chess (PGN)
- Kind `64`

---

## Deprecation Cleanup

| NIP | Status | Superseded By | Action |
|-----|--------|---------------|--------|
| **04** | `unrecommended` | NIP-17 | Remove from `supported_nips`; keep kind 4 in AllowedKinds for compat |
| **16** | merged into NIP-01 | NIP-01 | Remove from `supported_nips` (cosmetic) |
| **20** | merged into NIP-01 | NIP-01 | Remove from `supported_nips` (cosmetic) |
| **33** | merged into NIP-01 | NIP-01 | Remove from `supported_nips` (cosmetic) |
| **EE** | `unrecommended` | Marmot Protocol | Remove from `supported_nips`; keep kinds for compat |
