# Shugur Relay — Pending NIP Items

**Updated:** 2026-02-13  
**Current NIP count:** 59 (52 numeric + 7 string)

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

## ~~Missing Kinds for Already-Claimed NIPs~~ ✅ DONE

Added kinds 16, 34236, 1244, 30403, 10063. Added NIP-18 and NIP-B7 to supported list.

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

## ~~Deprecation Cleanup~~ ✅ DONE

Removed NIPs 04, 16, 20, 33, EE from `supported_nips` (kinds kept for backwards compat).
