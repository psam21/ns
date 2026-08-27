# NIP support and upgrade tracking

**Last reviewed:** 2026-08-27
**Canonical source:** [`relay/internal/constants/relay_metadata.go`](../relay/internal/constants/relay_metadata.go)
**Project:** nostr.ltd relay and companion Blossom media service

## How to read this document

The relay advertises a protocol surface through NIP-11. The canonical `DefaultSupportedNIPs` list currently contains **77 identifiers**, and the public dashboard renders that complete list in a searchable, internally scrollable panel.

An advertised identifier is not a claim that every sentence of the corresponding specification is enforced by a dedicated validator. Relay support is distributed across connection handling, filters, event validation, persistence, the web/API layer, and the companion Blossom service. Conformance work should therefore be tracked against concrete behavior and tests, not only against the NIP-11 list.

The integration suite under [`relay/tests/nips`](../relay/tests/nips) currently contains **35 shell scripts**. It intentionally covers a smaller, behavior-focused subset and should be run only against a disposable or explicitly authorized relay because the scripts publish and sometimes delete events.

## Current advertised registry

The following identifiers are the exact current contents of `DefaultSupportedNIPs`:

| # | NIP | # | NIP | # | NIP | # | NIP |
|---:|---|---:|---|---:|---|---:|---|
| 1 | NIP-01 | 21 | NIP-34 | 41 | NIP-54 | 61 | NIP-86 |
| 2 | NIP-02 | 22 | NIP-35 | 42 | NIP-56 | 62 | NIP-87 |
| 3 | NIP-05 | 23 | NIP-36 | 43 | NIP-57 | 63 | NIP-88 |
| 4 | NIP-07 | 24 | NIP-37 | 44 | NIP-58 | 64 | NIP-89 |
| 5 | NIP-09 | 25 | NIP-38 | 45 | NIP-59 | 65 | NIP-92 |
| 6 | NIP-10 | 26 | NIP-39 | 46 | NIP-60 | 66 | NIP-94 |
| 7 | NIP-11 | 27 | NIP-40 | 47 | NIP-61 | 67 | NIP-98 |
| 8 | NIP-13 | 28 | NIP-42 | 48 | NIP-62 | 68 | NIP-99 |
| 9 | NIP-17 | 29 | NIP-43 | 49 | NIP-64 | 69 | NIP-7D |
| 10 | NIP-18 | 30 | NIP-44 | 50 | NIP-65 | 70 | NIP-A0 |
| 11 | NIP-19 | 31 | NIP-45 | 51 | NIP-66 | 71 | NIP-A4 |
| 12 | NIP-21 | 32 | NIP-46 | 52 | NIP-67 | 72 | NIP-B0 |
| 13 | NIP-22 | 33 | NIP-47 | 53 | NIP-69 | 73 | NIP-C0 |
| 14 | NIP-23 | 34 | NIP-48 | 54 | NIP-70 | 74 | NIP-F4 |
| 15 | NIP-24 | 35 | NIP-49 | 55 | NIP-71 | 75 | NIP-CC |
| 16 | NIP-25 | 36 | NIP-50 | 56 | NIP-75 | 76 | NIP-C7 |
| 17 | NIP-27 | 37 | NIP-51 | 57 | NIP-77 | 77 | NIP-B7 |
| 18 | NIP-29 | 38 | NIP-52 | 58 | NIP-78 |  |  |
| 19 | NIP-30 | 39 | NIP-53 | 59 | NIP-84 |  |  |
| 20 | NIP-32 | 40 | NIP-5A | 60 | NIP-85 |  |  |

The table above is grouped for readability; the ordinal column is only a display index. The identifiers themselves are the source of truth. To avoid accidental drift, any change to the registry should update the regression test in `relay/internal/web/handler_test.go` and the dashboard count expectations together.

> **Correction note:** The previous version of this document described approximately 97 tracked items and listed many obsolete “remove” and “not implemented” tasks. Those numbers mixed advertised NIPs, possible future work, and Blossom-related capabilities. They were not a reliable implementation inventory and have been removed.

## Integration-test coverage

The current scripts are listed in [`relay/tests/nips/README.md`](../relay/tests/nips/README.md). The coverage set is:

| Area | Scripts |
|---|---|
| Core and event lifecycle | NIP-01, 02, 03, 04, 09, 15, 16, 17, 20, 22, 23, 25, 28, 33, 40 |
| Encryption, privacy, and counting | NIP-44, 45, 50, 59, 60, 61, 65, 72, 78 |
| Metadata, social, and application events | NIP-11, 47, 51, 52, 53, 54, 56, 57, 58 |
| Project extensions | XX Time Capsules and YY Nostr Web Pages |

The suite is not a conformance certificate. For example, some scripts exercise event acceptance and round trips but do not cover every validator branch, storage invariant, authorization path, or negative case in the related specification.

## Full-registry coverage contract

[`relay/tests/nips/coverage.tsv`](../relay/tests/nips/coverage.tsv) contains one row for each advertised identifier. Each row classifies the appropriate evidence as relay integration, registry-contract coverage, client/ecosystem review, or Blossom/service review. [`relay/tests/nips/run_coverage.sh`](../relay/tests/nips/run_coverage.sh) verifies that the matrix exactly matches `DefaultSupportedNIPs`, validates all test-script syntax, runs the dashboard JavaScript check and Go test suite, and can optionally perform live NIP-11 and integration checks.

Use static validation during normal development:

```bash
cd relay
./tests/nips/run_coverage.sh --static
```

Use live validation only against a disposable or explicitly authorized relay because the integration scripts publish and delete events:

```bash
RELAY_URL=ws://localhost:8080 HTTP_URL=http://localhost:8080 \
  ./tests/nips/run_coverage.sh --live
```

A successful static run means all 77 identifiers have an explicit coverage disposition and all automated evidence is structurally runnable. It does not turn client-only or external-service NIPs into relay conformance tests; those rows remain intentionally marked for manual or service-level review.

## Current implementation areas

The following areas have direct relay code or operational coverage and should remain synchronized with the advertised list:

| Area | Primary implementation locations | Current verification |
|---|---|---|
| Basic relay protocol, subscriptions, filters, and event lifecycle | `relay/internal/relay/connection.go`, `subscription.go`, `filter.go`, `event_processor.go` | Go unit tests and NIP shell scripts |
| Relay metadata and supported registry | `relay/internal/constants/relay_metadata.go`, `relay/internal/web/handler.go` | NIP-11 checks and exact 77-entry regression test |
| Authentication and protected events | `relay/internal/relay/connection.go`, `nips/nip42.go` | Go tests and targeted integration tests |
| Deletion and vanish flows | `relay/internal/storage/queries.go`, `event_processor.go` | Go storage tests and NIP-09 coverage |
| COUNT, search, and negentropy | `relay/internal/relay/nip45.go`, `filter.go`, `nip77.go` | Go tests and targeted relay tests |
| Relay groups and access metadata | `relay/internal/relay/nip29.go`, `nip43.go` | Go tests; expand integration coverage |
| Nostr Wallet Connect and encrypted payloads | `relay/internal/relay/nips/nip47.go`, `nip44.go` | Targeted NIP scripts and validator tests |
| Media-related event metadata | `relay/internal/relay/nips/nip92.go`, `nip94.go`; `blossom/` | Partial; add negative and round-trip cases |
| Blossom storage and authentication | `blossom/src/`, `deploy/blossom.service` | Service-level tests and authorized smoke tests |
| Dashboard telemetry and event cache | `relay/internal/web/handler.go`, `relay/web/` | Go handler tests, JS syntax check, browser preview |
| Project extensions | `relay/internal/relay/nips/`, `relay/tests/nips/test_nip_time_capsules.sh`, `test_nip_nostr_web.sh` | Specialized integration tests; external dependencies may be required |

## Upgrade backlog

### Priority 1: maintain truthful advertising and repeatable tests

1. Keep `DefaultSupportedNIPs`, NIP-11 output, the dashboard registry, the coverage matrix, and the exact-count regression test synchronized.
2. Keep all integration scripts on the shared `RELAY_URL` and `HTTP_URL` variables, with local `ws://localhost:8080` and `http://localhost:8080` defaults.
3. Run `relay/tests/nips/run_all.sh` only against a disposable or authorized test relay, and preserve its non-zero exit status when a script fails.
4. Add negative cases for validator rejection, authorization failures, malformed filters, tag limits, and rate limits before calling a NIP behavior complete.

### Priority 2: review high-impact and recently changing behavior

Review the current specifications and implementation together for NIP-29, NIP-42, NIP-44, NIP-45, NIP-47, NIP-51, NIP-67, NIP-77, NIP-86, NIP-87, NIP-92, NIP-94, NIP-98, and NIP-B7. Each review should produce one of three outcomes: a focused code change with tests, a documented reason the existing behavior is sufficient, or a clearly scoped follow-up issue.

Pay particular attention to authentication boundaries, protected events, event deletion and vanish semantics, relay-management authorization, COUNT/negentropy correctness, media metadata, and the distinction between relay event handling and client-side NIPs.

### Priority 3: expand behavior coverage

Add focused integration or Go tests for currently advertised areas that have little direct coverage, including NIP-05, NIP-07, NIP-10, NIP-19, NIP-21, NIP-27, NIP-36, NIP-46, NIP-48, NIP-49, NIP-5A, NIP-64, NIP-66, NIP-69, NIP-70, NIP-71, NIP-75, NIP-84, NIP-85, NIP-88, NIP-89, NIP-99, NIP-A0, NIP-A4, NIP-B0, NIP-C0, NIP-C7, NIP-CC, and NIP-F4. Prioritize relay-enforced behavior; client-only specifications should be documented as metadata or ecosystem support rather than over-claimed as server validation.

### Priority 4: dependency and operations hygiene

Review Go and Node dependencies on a planned cadence, test upgrades in isolation, and record compatibility changes. Keep production configuration free of secrets, verify systemd hardening after service changes, and run the ARM64 build before deploying to the AWS Graviton host.

## Validation commands

From the repository root:

```bash
# Relay unit, race, vet, and build checks
cd relay
go test ./...
go test -race ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/relay-arm64 ./cmd
node --check web/static/script.js

# Full 77-row static coverage validation
./tests/nips/run_coverage.sh --static

# Authorized live validation against a disposable relay
RELAY_URL=ws://localhost:8080 HTTP_URL=http://localhost:8080 \
  ./tests/nips/run_coverage.sh --live
```

From the repository root, the relay smoke test is:

```bash
RELAY_URL=ws://localhost:8080 HTTP_URL=http://localhost:8080 \
  ./deploy/test_relay.sh
```

## References

- [Nostr NIP repository](https://github.com/nostr-protocol/nips)
- [Nostr protocol site](https://nostr.org/)
- [Blossom protocol and server ecosystem](https://github.com/hzrd149/blossom-server)
- [Project repository](https://github.com/psam21/ns)
