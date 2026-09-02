# nostr.ltd Repository: Pending Work and Completion Plan

**Author:** Manus AI  
**Repository:** [psam21/ns](https://github.com/psam21/ns)  
**Primary services:** Go Nostr relay and TypeScript Blossom media server  
**Document purpose:** Capture all remaining work after the completed initial Shugur-to-`nostr.ltd` migration stages and identify the service-reliability work that must be completed before the migration can be considered finished.

> ## ⚠️ FULLY MIGRATED TO GIT — DO NOT EDIT
>
> This document has been fully decomposed into Git issues. Git is now the single source of truth for all work tracking.
>
> **Migration date:** 2026-09-02
> **Parent tracking issue:** [#113](https://github.com/psam21/ns/issues/113)
> **Status:** Ready for deletion.
>
> ### Issue map
>
> | Section | Issue(s) |
> |---|---|
> | §2 Blossom uploads | [#101](https://github.com/psam21/ns/issues/101), [#102](https://github.com/psam21/ns/issues/102), [#103](https://github.com/psam21/ns/issues/103), [#104](https://github.com/psam21/ns/issues/104) |
> | §3 Dashboard readiness | [#105](https://github.com/psam21/ns/issues/105), [#106](https://github.com/psam21/ns/issues/106) |
> | §4 OOM investigation | [#107](https://github.com/psam21/ns/issues/107) |
> | §5 Go module path | [#109](https://github.com/psam21/ns/issues/109) |
> | §6 Final audit | [#110](https://github.com/psam21/ns/issues/110) |
> | §7 Alias removal | [#111](https://github.com/psam21/ns/issues/111) |
> | §8 Validation matrix | [#112](https://github.com/psam21/ns/issues/112) |
> | §9 Deploy safeguards | [#108](https://github.com/psam21/ns/issues/108) |
> | §10 Execution order | [#113](https://github.com/psam21/ns/issues/113) (parent) |
> | §11 Definition of done | [#113](https://github.com/psam21/ns/issues/113) |
>
> The original document body is preserved below for historical reference only. Any new work, status changes, or decisions must be tracked in the linked issues, not here.

## 1. Executive status

The repository has completed the migration inventory, cosmetic/public-branding cleanup, configuration-alias work, and deployment-emitter migration. Stage 4 was committed and pushed as `345889f chore: make deployment emitters nostr-first` according to the inherited task state. The relay accepts canonical `NOSTR_*` variables while retaining `SHUGUR_*` fallbacks, and deployment templates and installers now emit the canonical names first.

The next work is not a single cosmetic cleanup. It consists of two parallel priorities: restoring reliable Blossom uploads and completing the higher-risk identity migration of the Go module path. The Blossom service can be reachable over HTTP while uploads still fail; therefore an HTTP 200 health check is insufficient as a functional deployment test.

| Area | Current state | Priority | Required before migration closeout |
|---|---|---:|---:|
| Inventory of legacy references | Complete | — | No |
| Public branding and dashboard cleanup | Complete | — | No |
| `NOSTR_*` configuration aliases with `SHUGUR_*` fallback | Complete | — | No |
| Deployment/Compose/installers emit canonical variables | Complete and pushed | — | No |
| Blossom upload functionality | **Pending investigation/fix** | P0 | Yes |
| Relay event count/cache readiness | **Pending reliability work** | P0 | Yes |
| Relay OOM behavior | **Pending operational investigation** | P0 | Yes |
| Go module-path migration | Pending evaluation | P1 | Yes, unless explicitly deferred |
| Repository-wide final audit | Pending | P1 | Yes |
| Removal of legacy compatibility aliases | Optional/deferred | P2 | No; only after production migration |

## 2. Immediate P0: repair Blossom uploads

### 2.1 What is known

The latest deployment log showed that Blossom compiled successfully, its runtime artifact was copied, `blossom.service` became active, and port 3000 returned HTTP 200. Those checks prove that the process and static routes are available, but they do not prove that `PUT /upload` or `PUT /media` accepts an authenticated blob.

The upload middleware in `blossom/src/api/upload.ts` checks the configured storage rules before accepting a blob. The committed production configuration at `blossom/config.yml` currently has `storage.backend: s3`, `removeWhenNoOwners: true`, and `storage.rules: []`, while uploads and media are enabled and authentication is required. An empty rule list is a strong candidate for rejecting every upload because `checkUpload` calls `getFileRule` and returns an unauthorized response when no matching rule exists. This must be confirmed in code and with a controlled authenticated request before changing production behavior.

### 2.2 Required investigation

The implementation owner should inspect `blossom/src/rules/index.ts` and confirm the exact behavior of `getFileRule` when `config.storage.rules` is empty. The result must be covered by a unit test. The production log should then be checked for the exact HTTP status and `X-Reason` response from a failed upload, without logging credentials, bearer tokens, Nostr private keys, or S3 secrets.

The investigation must distinguish the following failure classes:

| Failure class | Expected evidence | Correct repair direction |
|---|---|---|
| No matching storage rule | HTTP 401 and a reason indicating the content type is not accepted | Add deliberate production rules or change rule semantics only with tests and explicit policy |
| Missing/invalid Blossom auth event | HTTP 401 indicating missing or wrong auth event type | Repair client signing/auth construction, not server authorization |
| Missing `x` tag for blob hash | HTTP 400 indicating incorrect or missing SHA-256 binding | Repair upload client/auth event construction or preserve BUD-06 validation |
| S3 credential, bucket, or region failure | HTTP 5xx and storage/S3 error in server journal | Repair `/opt/blossom/.env`, IAM/bucket policy, or S3 configuration without exposing secrets |
| Filesystem permission failure | EACCES or write failure in journal | Ensure only `/opt/blossom/data` is writable and that the selected backend is consistent |
| Request-size or content-type rejection | HTTP 413/401 from middleware | Confirm intended 10 MB limit and add accepted MIME rules |
| Proxy/body handling issue | Request succeeds on localhost but fails through public hostname | Inspect Caddy method/body/header forwarding and test through HTTPS |

### 2.3 Likely configuration repair

If the empty-rule behavior is confirmed, add explicit production rules for the intended upload classes, for example `image/*`, `video/*`, `audio/*`, and other supported media types. The precise policy must be chosen deliberately. The rule list should not be made permissive merely to hide an authorization or S3 failure.

The repair must preserve these security properties:

1. Authentication remains required for uploads and media unless the operator explicitly changes the policy.
2. The authenticated event remains bound to the uploaded SHA-256 through the `x` tag.
3. The 10 MB upload limit remains enforced unless an independently reviewed change raises it.
4. S3 credentials remain supplied through `/opt/blossom/.env` or an equivalent secret mechanism and never enter Git, logs, tar listings, or generated output.
5. `removeWhenNoOwners` is reviewed together with the owner-registration path so accepted uploads are not immediately pruned because ownership was not persisted.

### 2.4 Required upload tests

The repository needs a testable upload matrix rather than a port check alone. At minimum, it should cover an accepted authenticated upload, missing authentication, wrong auth type, missing SHA-256 binding, mismatched SHA-256, oversized payload, rejected MIME type, duplicate blob behavior, retrieval of the accepted blob, and deletion authorization. The test should run against a local test storage backend where possible and against a staging S3 bucket only when explicitly configured.

A deployment verification step should perform a safe functional probe using a generated disposable blob and a test identity, then clean up the blob. It must not use a real user’s private key or a permanent production object. If no safe automated credential is available, the deployment script should clearly mark upload verification as manual instead of reporting the service fully verified.

## 3. Immediate P0: make relay event counts useful during cache warming

### 3.1 Current failure mode

The relay’s `/api/events` endpoint returns a warming state while the grouped archive query is still running. The implementation separately tracks stored totals, event totals, and grouped breakdown snapshots. The grouped query in `relay/internal/storage/queries.go` aggregates all events from 2026 onward by year, kind, and month. The deployment log showed that the grouped cache could remain in a warming state long enough to make the site appear empty, even though the relay itself and direct totals may already be available.

The user-facing requirement is that the event count must never present an unexplained `0`, indefinite `Loading…`, or generic failure when a direct database-backed total is already available. The slow grouped telemetry must remain non-blocking and should be presented as a secondary detail.

### 3.2 Required implementation

The relay should treat the direct event total as an independent readiness signal. The API response should expose explicit readiness for each layer:

| Layer | Purpose | Readiness rule |
|---|---|---|
| Stored event total | Primary user-facing count | Ready after a successful direct `COUNT(*)` query |
| 2026+ total | Current-period count | Ready after its bounded count query succeeds |
| Grouped breakdown | Top kinds and year/month tables | Ready only after aggregate query completes |
| Relay health | Process/database availability | Independent of all dashboard telemetry |

The response and dashboard should display a usable direct total immediately, with wording such as “grouped archive telemetry warming” when only the breakdown is pending. The endpoint may continue using HTTP 202 for an incomplete breakdown, but deployment verification must not interpret that status as a total-service failure when the direct totals are ready.

The grouped query should also be profiled on the production-sized dataset. The existing `created_at` index is useful for the range predicate, but the query still performs timestamp extraction and grouping across all matching rows. Candidate improvements include a bounded recent-period query, a persisted aggregate table refreshed incrementally, a materialized view, or a separate `kind` aggregate query for the initial dashboard. Any optimization must be measured with `EXPLAIN (ANALYZE, BUFFERS)` in a safe environment and must not degrade event ingestion.

### 3.3 Deployment-script correction

The deployment script must verify the direct event total separately from the grouped breakdown. It should parse JSON defensively and distinguish these outcomes:

- **Healthy:** HTTP service responds and direct totals are ready.
- **Partially ready:** direct totals are ready but grouped telemetry is warming; deployment continues with a warning.
- **Unhealthy:** endpoint is unreachable, database is unavailable, or direct totals report an error.

The uploaded script version was stricter and removed defensive `|| true` handling around the events curl calls. Under `set -Eeuo pipefail`, a transient curl failure can abort verification before the script can report the real state. The canonical repository version should remain the only version used for deployment, and the bootstrap script should hand off to it before executing deployment logic.

## 4. Immediate P0: investigate and contain relay OOM restarts

The deployment journal showed repeated relay termination by the Linux OOM killer, with the restart counter reaching 61. This is independent of Blossom’s HTTP availability and must be treated as a production incident. Repeated systemd restarts can also make dashboard totals, NIP tests, and upload-adjacent relay authentication appear intermittently broken.

The next investigation should capture memory usage over time, service limits, database-pool size, WebSocket counts, cache sizes, event processing queues, and the timing of grouped telemetry refreshes. The investigation must determine whether the aggregate query, event cache, NIP test traffic, connection pool, or a combination is responsible.

The immediate containment plan is to keep systemd restart behavior but prevent an unbounded memory-heavy dashboard refresh from competing with relay traffic. The grouped cache should have a bounded context, a single-flight refresh, a result-size limit, and a controlled backoff after failure. Memory limits should be used as a guardrail rather than as the primary fix. Any change to `MemoryMax`, pool sizing, or cache allocation should be tested against authenticated Nostr traffic and the NIP harness.

## 5. Stage 5: evaluate and, if approved, migrate the Go module path

The Go module still reports `github.com/Shugur-Network/relay` in `go.mod` and package paths. This is not merely a text replacement. A module-path migration affects every internal import, generated package reference, CI cache key, test tooling, documentation example, release artifact, and downstream consumer.

### 5.1 Decision required

Before changing the module path, decide whether `nostr.ltd` is a stable canonical import namespace. A bare domain is not automatically a usable Go module path unless the domain is configured for Go import discovery, or a complete path such as `nostr.ltd/relay` is selected and maintained. The chosen path must be tested with `go list`, `go test`, `go mod tidy`, and a clean clone.

| Option | Benefit | Risk | Recommendation |
|---|---|---|---|
| Keep old module path internally | Zero downstream breakage | Legacy identity remains in code | Safe short-term fallback |
| Move to `nostr.ltd/relay` | Canonical identity | Requires import, discovery, and downstream migration | Preferred only after discovery is verified |
| Use a GitHub vanity-compatible path | Easier Go tooling | Does not fully remove GitHub/Shugur identity | Transitional option |
| Change cosmetic references only | Low risk | Does not complete technical migration | Already largely completed |

### 5.2 Safe migration sequence

First, inventory all imports and references, including generated files, shell scripts, NIP tools, Docker metadata, and documentation. Second, add or verify Go vanity import metadata for the selected canonical path. Third, update `go.mod` and all internal imports in one controlled commit. Fourth, run the full unit, integration, NIP, cross-compilation, and clean-clone tests. Fifth, publish a compatibility note for any external consumers. Only after that should the old path be considered removable from documentation.

The module-path migration must not be combined with an unrelated upload or OOM fix. Separate commits make rollback and production diagnosis possible.

## 6. Stage 6: final repository-wide audit

After the service-reliability fixes and module-path decision, perform a final audit for legacy references. The audit must classify every remaining `Shugur` occurrence instead of deleting text blindly.

| Reference category | Final treatment |
|---|---|
| Public product name, page title, service description, README branding | Replace with `nostr.ltd` branding |
| Environment variables | Canonical `NOSTR_*` emitted; legacy fallback retained temporarily |
| Go module path and imports | Migrate only after Stage 5 decision and clean-clone validation |
| Historical migration documentation | Retain where useful, clearly label as historical |
| Copyright, license, attribution, or upstream provenance | Preserve unless legally reviewed |
| Database/table names and persisted data identifiers | Do not rename casually; document compatibility impact |
| Backup paths and old systemd units | Keep only if required for rollback; otherwise retire deliberately |
| Test fixtures and expected strings | Update when they describe current public identity |

The audit should cover the root README, Copilot instructions, deployment directory, NIP tracking documentation, test harness, Docker/Compose files, systemd units, package metadata, source comments, logs, and generated assets. Results should be recorded in a checklist with file path, line, classification, action, and reviewer status.

## 7. Optional Stage 7: remove legacy aliases after production migration

Removing `SHUGUR_*` fallbacks is optional and should not be part of the immediate branding work. It becomes safe only after all known production systems, local deployment scripts, Compose environments, CI jobs, and operator documentation use `NOSTR_*` variables.

The alias-removal gate should require a documented production inventory, at least one successful deployment using only canonical variables, a rollback test, and an announced deprecation period. Until those conditions are satisfied, the fallback logic is a deliberate compatibility feature rather than unfinished cleanup.

## 8. Validation matrix

The migration is complete only when the following validation matrix passes. Commands should be run from a clean checkout and should never print secret environment values.

| Validation | Scope | Completion condition |
|---|---|---|
| Shell syntax | Installers, deployment scripts, NIP scripts | `bash -n` passes for every script |
| YAML structure | Compose files, CI workflow, service-adjacent config | Parser succeeds and required keys exist |
| Go unit tests | Relay and NIP tools | `go test ./...` passes |
| Go static checks | Relay | `go vet ./...` and configured linters pass |
| ARM64 build | Production relay artifact | `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build` passes |
| Blossom build | Server and admin assets | `pnpm install --frozen-lockfile && pnpm build` passes |
| Blossom upload tests | Auth, hash binding, MIME, size, storage | Functional matrix passes |
| Relay endpoint checks | NIP-11, stats, event totals, grouped telemetry | Direct totals ready; grouped state explicit |
| NIP harness | Automated NIPs and manual/service review rows | Static coverage and live tests pass where configured |
| Security checks | Headers, auth parsing, dependency state | No regression and no secret exposure |
| Deployment rollback | Relay and Blossom artifacts/services | Failed restart restores prior working release |
| Public smoke tests | Correct hostnames and proxy paths | `nostr.ltd` relay and `blossom.nostr.ltd` Blossom respond as documented |

## 9. Deployment safeguards still required

The deployment process should be tightened before the next production push. The script must deploy only from a clean `main` checkout, use the canonical repository script, preserve production `.env` and persistent data, validate artifact completeness, and verify both services functionally.

Blossom deployment should include a pre-swap validation of the extracted artifact and a post-restart upload smoke test. The rollback trap must cover failures after Blossom is swapped but before the whole deployment completes, including a relay restart failure. The current logic marks Blossom deployed before the relay restart; if the relay restart then fails, the relay is restored but Blossom is not automatically restored. That may be acceptable if explicitly intended, but it is not an atomic two-service rollback and must be documented or corrected.

The script should also avoid reporting “deployment verification complete” when only static HTTP checks passed. It should print separate statuses for relay process, Blossom process, direct event totals, grouped telemetry, and upload functionality. Secret-bearing commands should redirect only safe status output and should not include environment files or full process environments.

## 10. Recommended execution order

The safest order is to repair and test Blossom uploads first because they are user-visible and the likely rule configuration issue can be isolated without changing the relay module path. Next, contain relay OOM restarts and make direct event totals independent from grouped telemetry. Then update deployment verification so it recognizes partial telemetry readiness while still failing on unavailable direct totals or failed uploads. After production stability is demonstrated, evaluate and execute the Go module-path migration. Finally, perform the repository-wide audit and defer alias removal until the production migration gate is satisfied.

| Sequence | Work item | Gate to proceed |
|---:|---|---|
| 1 | Confirm Blossom upload failure response and rule behavior | Reproducible test case with no secrets |
| 2 | Repair production rule/storage/auth path | Local upload matrix passes |
| 3 | Deploy Blossom repair | Public authenticated upload and retrieval pass |
| 4 | Profile relay event cache and OOM behavior | Memory/query evidence collected |
| 5 | Implement non-blocking totals and bounded grouped refresh | Dashboard shows totals during warming |
| 6 | Correct deployment verification and rollback semantics | Failure modes are accurately classified |
| 7 | Evaluate Go module namespace | Canonical path and vanity discovery confirmed |
| 8 | Execute module-path migration, if approved | Clean clone and all tests pass |
| 9 | Complete final audit | Every remaining legacy reference classified |
| 10 | Consider alias removal | Production migration inventory and rollback tested |

## 11. Definition of done

The migration and reliability program is complete when `nostr.ltd` is the public identity across current user-facing surfaces, deployment emitters produce canonical variables, existing `SHUGUR_*` deployments continue to work until deliberately retired, Blossom authenticated uploads succeed through the public hostname, relay direct event totals are available without waiting for grouped telemetry, repeated relay OOM restarts are resolved or bounded with an identified root cause, and the full validation matrix passes from a clean checkout.

The final production deployment must document the exact relay URL, Blossom URL, admin URL, required environment variables, rollback procedure, and known compatibility aliases. A deployment should never be considered successful solely because systemd reports both services as active.

## References

[1]: https://github.com/psam21/ns "psam21/ns repository"

[2]: https://github.com/psam21/ns/blob/main/docs/Shugur-Migration-Plan.md "Shugur migration plan"

[3]: https://github.com/psam21/ns/blob/main/blossom/src/api/upload.ts "Blossom upload route"

[4]: https://github.com/psam21/ns/blob/main/blossom/src/config.ts "Blossom configuration loader"

[5]: https://github.com/psam21/ns/blob/main/blossom/config.yml "Blossom production configuration"

[6]: https://github.com/psam21/ns/blob/main/relay/internal/storage/queries.go "Relay event aggregation queries"

[7]: https://github.com/psam21/ns/blob/main/deploy/ns-deploy-full.sh "Canonical full deployment script"

[8]: https://github.com/psam21/ns/blob/main/deploy/blossom.service "Blossom systemd unit"

[9]: https://github.com/psam21/ns/blob/main/relay/internal/config/config.go "Relay configuration aliases"

[10]: https://github.com/psam21/ns/blob/main/relay/go.mod "Relay Go module definition"

---

**Important operational note:** This document records the pending work and recommended sequence. It does not claim that the Blossom upload repair or the Go module-path migration has already been implemented. Those remain separate engineering tasks requiring validation and deployment.
