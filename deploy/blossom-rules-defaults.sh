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
# This script is idempotent. It only patches if `rules: []` is
# currently in the file. Re-running after a non-empty rules array
# is configured is a no-op.
#
# Run on the production host:
#   ssh ubuntu@13.201.250.44 'bash /opt/blossom/scripts/blossom-rules-defaults.sh'
#
# This script lives in the deploy/ directory so it is shipped with
# the rest of the deploy tooling and tracked in git. It is NOT
# executed automatically by ns-deploy-full.sh; run it by hand if
# you suspect the production config has drifted.
set -e

CONFIG="/opt/blossom/config.yml"
if [ ! -f "$CONFIG" ]; then
  echo "ERROR: $CONFIG not found; this script must run on the production host"
  exit 1
fi

# Show current rules
echo "=== current storage.rules ==="
sudo grep -A 2 "^  rules:" "$CONFIG" | head -10

# Patch only if rules is empty
if sudo grep -q "^  rules: \[\]" "$CONFIG"; then
  echo "=== empty rules detected, patching with defaults ==="
  sudo python3 - <<'PY'
import re
with open("/opt/blossom/config.yml", "r") as f:
    content = f.read()
new_rules = """  rules:
    - type: text/*
      expiration: 1 month
    - type: "image/*"
      expiration: 1 month
    - type: "video/*"
      expiration: 1 month
    - type: "audio/*"
      expiration: 1 month
    - type: "model/*"
      expiration: 1 month
    - type: "*"
      expiration: 2 days
"""
content = re.sub(r"^  rules:\s*\[\]\s*$", new_rules.rstrip(), content, count=1, flags=re.MULTILINE)
with open("/opt/blossom/config.yml", "w") as f:
    f.write(content)
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
  echo "=== rules already configured, no change ==="
  sudo grep -A 14 "^  rules:" "$CONFIG" | head -16
fi
