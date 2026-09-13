#!/bin/bash
# Patch /opt/blossom/config.yml to add the default storage.rules.
#
# Background: an empty `storage.rules: []` in /opt/blossom/config.yml
# causes every upload to be rejected with 401 "Server dose not accept
# <type> blobs" (sic) at upload.ts:58, because getFileRule() returns
# null when no rule matches. This is silent at deploy time -- the
# service starts fine, /api/health returns 200, and the relay is
# unaffected. Users only see it as "upload failed" in the browser.
#
# This script is idempotent. It repairs an empty rules list and migrates
# the old time-limited production defaults to permanent retention. Re-running
# after the canonical `never` rules are present is a no-op.
#
# Run on the production host:
#   ssh "$AWS_HOST" 'bash /opt/blossom/scripts/blossom-rules-defaults.sh'
#
# This script lives in the deploy/ directory so it is shipped with
# the rest of the deploy tooling and tracked in git. ns-deploy-full.sh runs
# it automatically when it detects empty or legacy expiring rules; run it by
# hand when repairing the same drift outside a full deployment.
set -e

CONFIG="/opt/blossom/config.yml"
if [ ! -f "$CONFIG" ]; then
  echo "ERROR: $CONFIG not found; this script must run on the production host"
  exit 1
fi

# Show current rules
echo "=== current storage.rules ==="
sudo grep -A 2 "^  rules:" "$CONFIG" | head -10

# Repair empty or legacy expiring rules
if sudo grep -q "^  rules: \[\]" "$CONFIG" || sudo grep -Eq '^      expiration: (1 month|2 days)$' "$CONFIG"; then
  echo "=== legacy or empty rules detected, applying permanent defaults ==="
  sudo python3 - <<'PY'
import re
from pathlib import Path

config_path = Path("/opt/blossom/config.yml")
content = config_path.read_text()
new_rules = """  rules:
    - type: text/*
      expiration: never
    - type: "image/*"
      expiration: never
    - type: "video/*"
      expiration: never
    - type: "audio/*"
      expiration: never
    - type: "model/*"
      expiration: never
    - type: "*"
      expiration: never
"""
lines = content.splitlines()
start = next((i for i, line in enumerate(lines) if line == "  rules:" or line == "  rules: []"), None)
if start is None:
    raise SystemExit("storage.rules block not found")
end = start + 1
while end < len(lines) and (not lines[end] or lines[end][0].isspace()):
    end += 1
replacement = new_rules.rstrip("\n").splitlines()
updated = "\n".join(lines[:start] + replacement + lines[end:]) + "\n"
config_path.write_text(updated)
print("patched")
PY
  echo "=== new storage.rules ==="
  sudo grep -A 14 "^  rules:" "$CONFIG" | head -16
  echo "=== restarting Blossom ==="
  sudo systemctl restart blossom
  sleep 3
  echo "=== last 5 lines of blossom log ==="
  sudo journalctl -u blossom -n 5 --no-pager
else
  echo "=== permanent rules already configured, no change ==="
  sudo grep -A 14 "^  rules:" "$CONFIG" | head -16
fi
