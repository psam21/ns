# Production Environment Inventory

**Last updated**: 2026-09-03
**Purpose**: Document every known production system, local deployment script, Compose environment, CI job, and operator documentation that uses `NOSTR_*` or `SHUGUR_*` variables. This inventory is a gate condition for removing `SHUGUR_*` legacy aliases (issue #111).

## Production Host

| Item | Value |
|---|---|
| Hostname | ip-172-31-20-20 (AWS EC2) |
| Public IP | 13.201.250.44 |
| SSH access | ubuntu@13.201.250.44 via /home/jack/.ssh/nostr-relay-key.pem |
| Relay service | /etc/systemd/system/relay.service |
| Blossom service | /etc/systemd/system/blossom.service |

## Service Endpoints

| Service | URL | Port |
|---|---|---|
| Relay WebSocket | wss://nostr.ltd | 443 (Caddy) → 8080 (relay) |
| Relay HTTP | https://nostr.ltd | 443 (Caddy) → 8080 (relay) |
| Blossom API | https://blossom.nostr.ltd | 443 (Caddy) → 3000 (blossom) |
| Blossom Admin | https://blossom.nostr.ltd/admin | 443 (Caddy) → 3000 (blossom) |
| Relay Metrics | http://localhost:2112/metrics | 2112 (relay) |

## Environment Variables in Use

### Relay (NOSTR_* canonical)

| Variable | Purpose | Source |
|---|---|---|
| NOSTR_RELAY_NAME | Relay display name | /opt/relay/config.yaml |
| NOSTR_RELAY_DESCRIPTION | Relay description | /opt/relay/config.yaml |
| NOSTR_RELAY_CONTACT | Operator contact | /opt/relay/config.yaml |
| NOSTR_DATABASE_URL | PostgreSQL connection | /opt/relay/.env |
| NOSTR_LISTEN_ADDR | WebSocket listen address | /opt/relay/config.yaml |
| NOSTR_METRICS_ADDR | Metrics listen address | /opt/relay/config.yaml |

### Relay (SHUGUR_* fallback — still supported)

| Variable | Fallback for | Status |
|---|---|---|
| SHUGUR_RELAY_NAME | NOSTR_RELAY_NAME | Active fallback |
| SHUGUR_RELAY_DESCRIPTION | NOSTR_RELAY_DESCRIPTION | Active fallback |
| SHUGUR_RELAY_CONTACT | NOSTR_RELAY_CONTACT | Active fallback |
| SHUGUR_DATABASE_URL | NOSTR_DATABASE_URL | Active fallback |
| SHUGUR_LISTEN_ADDR | NOSTR_LISTEN_ADDR | Active fallback |
| SHUGUR_METRICS_ADDR | NOSTR_METRICS_ADDR | Active fallback |

### Blossom

| Variable | Purpose | Source |
|---|---|---|
| BLOSSOM_ADMIN_PASSWORD | Dashboard password | /opt/blossom/.env |
| S3_ACCESS_KEY | S3 credentials | /opt/blossom/.env |
| S3_SECRET_KEY | S3 credentials | /opt/blossom/.env |
| BLOSSOM_ALLOW_GENERATED_PASSWORD | Fallback password generation | systemd unit |
| BLOSSOM_EXTRA_CORS_ORIGINS | CORS allow-list | systemd unit (optional) |

## Deployment Scripts

| Script | Location | Uses NOSTR_* | Uses SHUGUR_* |
|---|---|---|---|
| ns-deploy-full.sh | deploy/ | ✅ | ❌ |
| blossom-rules-defaults.sh | deploy/ | ✅ | ❌ |
| relay.service | deploy/ | ✅ (EnvironmentFile) | ❌ |
| blossom.service | deploy/ | ✅ (EnvironmentFile) | ❌ |

## CI/CD

| Job | Location | Uses NOSTR_* | Uses SHUGUR_* |
|---|---|---|---|
| CI | relay/.github/workflows/ci.yml | ✅ | ❌ |
| Deploy | (manual via deploy script) | ✅ | ❌ |

## Local Development

| Tool | Location | Uses NOSTR_* | Uses SHUGUR_* |
|---|---|---|---|
| relay/config.yaml | relay/ | ✅ | ❌ |
| blossom/config.yml | blossom/ | ✅ | ❌ |

## Documentation

| Document | Location | Uses NOSTR_* | Uses SHUGUR_* |
|---|---|---|---|
| copilot-instructions.md | .github/ | ✅ | ✅ (Go import paths, mandated by instructions) |
| NIP-Tracking.md | docs/ | ✅ | ❌ |
| Shugur-Audit.md | docs/ | ✅ | ✅ (historical reference) |
| Production-Inventory.md | docs/ | ✅ | ✅ (this document) |

## Gate Condition Status for #111

| Condition | Status | Evidence |
|---|---|---|
| 1. Documented production inventory | ✅ MET | This document |
| 2. Successful canonical-only deploy | ✅ MET | Deploy at 2026-09-03 05:36 UTC used only NOSTR_* variables |
| 3. Rollback test | ⏸ PENDING | Not yet executed |
| 4. Deprecation period elapsed | ⏸ PENDING | Not yet announced |

## Next Steps

1. Execute rollback test (gate condition 3)
2. Announce deprecation period (gate condition 4)
3. After deprecation period elapses, execute #111 removal steps
