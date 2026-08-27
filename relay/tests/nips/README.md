# Nostr Implementation Protocol (NIP) integration tests

This directory contains shell-based integration tests for the relay’s Nostr protocol surface and project-specific extensions. The scripts publish, query, and in some cases delete events, so they must run only against a disposable local relay or a relay for which testing has been explicitly authorized.

The suite currently contains **35 shell scripts**. These scripts exercise relay behavior; they do not prove complete conformance to every section of a NIP, and a script marked present in this inventory is not the same as an advertised NIP in NIP-11.

## Prerequisites

Run commands from the `relay/` directory. Start a test relay using a disposable PostgreSQL database and the development configuration, which listens on `ws://localhost:8080` by default.

Required tools for the common suite are:

- `bash` and `timeout`
- [`nak`](https://github.com/fiatjaf/nak) for event publishing and querying
- `curl` and `jq` for NIP-11 and HTTP checks
- `python3`, `openssl`, `base64`, `od`, and `sha256sum` for selected scripts

The project-specific Time Capsules test may require additional Python packages listed in `nip-xx-time-capsules/requirements-test.txt` when that directory is present.

## Configuration

All standardized relay-facing scripts accept these environment variables:

```bash
export RELAY_URL="ws://localhost:8080"   # WebSocket endpoint
export HTTP_URL="http://localhost:8080"  # HTTP/NIP-11 endpoint
export TEST_TIMEOUT=30                    # Optional per-test timeout
export VERBOSE=1                          # Optional script-specific verbosity
```

The default is intentionally local. The suite no longer contains hard-coded production or legacy fork-domain endpoints. Override `RELAY_URL` and `HTTP_URL` only when you have permission to test another relay.

## Run the suite

First check shell syntax without connecting to a relay:

```bash
for test in tests/nips/test_nip*.sh; do
  bash -n "$test" || exit 1
done
```

Then run all scripts through the aggregate runner:

```bash
RELAY_URL=ws://localhost:8080 \
HTTP_URL=http://localhost:8080 \
  ./tests/nips/run_all.sh
```

`run_all.sh` checks prerequisites, runs each `test_nip*.sh` script in sorted order, prints a pass/fail summary, and exits non-zero if any script fails. The suite is intentionally not part of the default unit-test command because it needs a live relay and mutates test data.

### Validate all 77 advertised identifiers

[`coverage.tsv`](coverage.tsv) is the one-row-per-identifier coverage contract. [`run_coverage.sh`](run_coverage.sh) validates that the matrix has exactly 77 unique rows, matches `DefaultSupportedNIPs` in source, points every integration row at an executable script, checks every shell script and the dashboard JavaScript, and runs the Go test suite including the registry-contract test.

Run the safe static validator from `relay/`:

```bash
./tests/nips/run_coverage.sh --static
```

Run live checks only against a disposable or explicitly authorized relay:

```bash
RELAY_URL=ws://localhost:8080 HTTP_URL=http://localhost:8080 \
  ./tests/nips/run_coverage.sh --live
```

Static validation marks client-only and external-service NIPs as intentional `manual` coverage. Live validation additionally compares the relay’s NIP-11 registry with all 77 matrix identifiers and runs the mutating integration suite.

Run one test directly when debugging:

```bash
RELAY_URL=ws://localhost:8080 ./tests/nips/test_nip01.sh
VERBOSE=1 RELAY_URL=ws://localhost:8080 ./tests/nips/test_nip44.sh
bash -x ./tests/nips/test_nip01.sh
```

The deployment smoke test is separate and can be run from the repository root against the HTTP/WebSocket listener:

```bash
RELAY_URL=ws://localhost:8080 HTTP_URL=http://localhost:8080 ./deploy/test_relay.sh
```

## Coverage inventory

| Script | NIP or extension | Focus |
|---|---|---|
| `test_nip01.sh` | NIP-01 | Basic event and protocol flow |
| `test_nip02.sh` | NIP-02 | Follow lists and petnames |
| `test_nip03.sh` | NIP-03 | OpenTimestamps attestations |
| `test_nip04.sh` | NIP-04 | Encrypted direct messages |
| `test_nip09.sh` | NIP-09 | Event deletion |
| `test_nip11.sh` | NIP-11 | Relay information document |
| `test_nip15.sh` | NIP-15 | Marketplace events |
| `test_nip16.sh` | NIP-16 | Event treatment |
| `test_nip17.sh` | NIP-17 | Private direct messages and related events |
| `test_nip20.sh` | NIP-20 | Command results |
| `test_nip22.sh` | NIP-22 | Comment events and timestamp limits |
| `test_nip23.sh` | NIP-23 | Long-form content |
| `test_nip25.sh` | NIP-25 | Reactions |
| `test_nip28.sh` | NIP-28 | Public chat |
| `test_nip33.sh` | NIP-33 | Addressable events |
| `test_nip40.sh` | NIP-40 | Expiration timestamps |
| `test_nip44.sh` | NIP-44 | Versioned encrypted payloads |
| `test_nip45.sh` | NIP-45 | COUNT requests and counting results |
| `test_nip47.sh` | NIP-47 | Nostr Wallet Connect event validation |
| `test_nip50.sh` | NIP-50 | Search capability |
| `test_nip51.sh` | NIP-51 | Lists |
| `test_nip52.sh` | NIP-52 | Calendar events |
| `test_nip53.sh` | NIP-53 | Live activities |
| `test_nip54.sh` | NIP-54 | Wiki events |
| `test_nip56.sh` | NIP-56 | Reporting |
| `test_nip57.sh` | NIP-57 | Lightning zaps |
| `test_nip58.sh` | NIP-58 | Badges |
| `test_nip59.sh` | NIP-59 | Gift wrapping |
| `test_nip60.sh` | NIP-60 | Cashu Wallets |
| `test_nip61.sh` | NIP-61 | Nutzaps |
| `test_nip65.sh` | NIP-65 | Relay list metadata |
| `test_nip72.sh` | NIP-72 | Moderated communities |
| `test_nip78.sh` | NIP-78 | Application-specific data |
| `test_nip_nostr_web.sh` | YY | Nostr Web Pages extension |
| `test_nip_time_capsules.sh` | XX | Time Capsules extension |

The full advertised registry is maintained separately in [`relay/internal/constants/relay_metadata.go`](../../internal/constants/relay_metadata.go) and tracked in [`docs/NIP-Tracking.md`](../../../docs/NIP-Tracking.md). The relay currently advertises 77 NIP identifiers, while this integration directory covers a smaller, behavior-focused subset.

## Specialized tests

### Time Capsules (`test_nip_time_capsules.sh`)

This test exercises the project-specific time-lock capsule format, including public and private modes and NIP-59 gift wrapping where configured. It may depend on the local Time Capsules helper package and external time-lock services. Treat it as an integration test rather than a fast smoke test.

### Nostr Web Pages (`test_nip_nostr_web.sh`)

This test exercises the project-specific static website event flow and content-addressed asset references. It uses temporary test keys and should run only against an isolated relay.

## Troubleshooting

If the suite cannot connect, verify that the relay is running on port 8080 and that PostgreSQL is available. Check the HTTP metadata endpoint with:

```bash
curl -sS -H 'Accept: application/nostr+json' "$HTTP_URL" | jq .
nak relay info "$RELAY_URL"
```

If a script fails, rerun it directly with `VERBOSE=1` or `bash -x`, inspect the relay logs, and confirm that its event kinds are accepted by the current validator. Some scripts target protocol behavior that may be advertised but not fully implemented; such failures should be recorded as implementation work rather than hidden by changing the runner’s exit status.

## Adding or upgrading tests

When adding a test, use the `test_nipXX.sh` naming convention, consume `RELAY_URL` rather than embedding an endpoint, fail clearly when prerequisites are missing, and clean up any events that do not need to persist. Document special dependencies and add the script to the coverage table. When a NIP specification changes, review both the validator and its integration test, then run shell syntax checks and the affected script against a disposable database before updating the tracking document.

For authoritative specifications, consult the [Nostr NIP repository](https://github.com/nostr-protocol/nips) and the project’s current [`docs/NIP-Tracking.md`](../../../docs/NIP-Tracking.md).
