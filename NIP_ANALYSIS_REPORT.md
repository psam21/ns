# Shugur Relay — Pending NIP Items

**Updated:** 2026-02-13  
**Current NIP count:** 62 (55 numeric + 7 string)

---

## Compliance Improvements for Claimed NIPs

### ~~NIP-11 — Relay Information Document~~ ✅ DONE\n- Banner already supported. Added `posting_policy` and `relay_countries` config fields.\n- `self` field and `privacy_policy`/`terms_of_service` not in go-nostr NIP-11 struct — skip until upstream adds them.

### ~~NIP-17 — Private Direct Messages~~ ✅ DONE\n- Requires NIP-42 AUTH for querying kinds 4, 14, 15, 1059 (gift wrap)\n- Only sends DM/gift-wrap events to the authenticated user (author or p-tagged recipient)

### ~~NIP-40 — Expiration Timestamp~~ ✅ ALREADY IMPLEMENTED\n- Ingestion rejection in `plugin_validator.go` (rejects expired events)\n- Periodic purge via `StartExpiredEventsCleaner` (hourly) in `node_builder.go`

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

#### ~~NIP-77 — Negentropy Syncing~~ ✅ DONE
- Implemented NEG-OPEN, NEG-MSG, NEG-CLOSE, NEG-ERR handlers
- Uses go-nostr's built-in negentropy library for set reconciliation
- Per-connection session management with limits (5 concurrent, 500K records, 2min timeout)

#### ~~NIP-86 — Relay Management API~~ ✅ DONE
- JSON-RPC management endpoint (Content-Type: application/nostr+json+rpc)
- NIP-98 authorization with kind 27235 + payload hash verification
- 18 methods: supportedmethods, ban/allow pubkeys, ban/allow events,
  change relay name/description/icon, allow/disallow kinds, block/unblock IPs
- Admin auth via relay PUBLIC_KEY or ADMIN_PUBKEYS config

### 🟡 MEDIUM PRIORITY

#### ~~NIP-13 — Proof of Work~~ ✅ DONE
- Validates `nonce` tag committed difficulty, counts leading zero bits
- Configurable `MIN_POW_DIFFICULTY` in config (default 0 = no requirement)
- Advertised in NIP-11 `min_pow_difficulty` field

#### ~~NIP-29 — Relay-based Groups~~ ✅ DONE
- Full in-memory group store with membership, admin roles, and metadata
- Moderation events (9000-9009): create group, put/remove user, edit metadata, delete, invites
- Join/leave requests (9021/9022) with invite code support
- Relay-signed metadata events (39000-39003): metadata, admins, members, roles
- Relay secp256k1 keypair for signing (PRIVATE_KEY config, auto-generated if empty)

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
