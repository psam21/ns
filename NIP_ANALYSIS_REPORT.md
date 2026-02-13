# Shugur Relay — Pending NIP Items

**Updated:** 2026-02-13  
**Current NIP count:** 62 (55 numeric + 7 string)

---

## Compliance Improvements

### NIP-45 — Event Counts
- Add HyperLogLog (`hll`) field support for merging counts across relays (latest spec addition)

### NIP-57 — Lightning Zaps
- Consider validating `bolt11` tag presence on kind 9735 zap receipts

---

## New NIPs to Implement

### 🟡 MEDIUM PRIORITY

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
