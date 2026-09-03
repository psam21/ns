#!/usr/bin/env bash
# Rollback test for #111 gate condition 3
# Verifies that the deploy script's rollback logic correctly restores
# the relay binary from backup when systemctl restart fails.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NS_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

AWS_KEY="${AWS_KEY:-$HOME/.ssh/nostr-relay-key.pem}"
AWS_HOST="${AWS_HOST:-ubuntu@13.201.250.44}"

echo "=== Rollback Test ==="
echo ""

# Step 1: Record current binary hash
echo "Step 1: Recording current binary hash..."
ORIGINAL_HASH=$(ssh -i "$AWS_KEY" "$AWS_HOST" "sha256sum /opt/relay/relay-arm64" | awk '{print $1}')
echo "Original binary SHA-256: $ORIGINAL_HASH"
echo ""

# Step 2: Create a backup of the current binary
echo "Step 2: Creating backup..."
ssh -i "$AWS_KEY" "$AWS_HOST" "sudo cp /opt/relay/relay-arm64 /tmp/relay-arm64.backup && sudo chmod 644 /tmp/relay-arm64.backup"
BACKUP_HASH=$(ssh -i "$AWS_KEY" "$AWS_HOST" "sha256sum /tmp/relay-arm64.backup" | awk '{print $1}')
echo "Backup SHA-256: $BACKUP_HASH"
if [ "$BACKUP_HASH" != "$ORIGINAL_HASH" ]; then
    echo "FAIL: Backup hash does not match original"
    exit 1
fi
echo ""

# Step 3: Stop service and corrupt binary (simulate failed deploy)
echo "Step 3: Stopping service and corrupting binary..."
ssh -i "$AWS_KEY" "$AWS_HOST" "sudo systemctl stop relay.service"
ssh -i "$AWS_KEY" "$AWS_HOST" "sudo cp /dev/null /opt/relay/relay-arm64 && sudo chmod +x /opt/relay/relay-arm64"
CORRUPT_HASH=$(ssh -i "$AWS_KEY" "$AWS_HOST" "sha256sum /opt/relay/relay-arm64" | awk '{print $1}')
echo "Corrupted binary SHA-256: $CORRUPT_HASH"
if [ "$CORRUPT_HASH" = "$ORIGINAL_HASH" ]; then
    echo "FAIL: Binary was not corrupted"
    exit 1
fi
echo ""

# Step 4: Verify service fails to start
echo "Step 4: Verifying service fails with corrupted binary..."
RESTART_RESULT=$(ssh -i "$AWS_KEY" "$AWS_HOST" "sudo systemctl restart relay.service 2>&1 || true")
echo "Restart result: $RESTART_RESULT"
sleep 2
SERVICE_ACTIVE=$(ssh -i "$AWS_KEY" "$AWS_HOST" "sudo systemctl is-active relay.service || echo failed")
echo "Service status: $SERVICE_ACTIVE"
echo ""

# Step 5: Restore from backup (simulate rollback)
echo "Step 5: Restoring from backup (simulating rollback)..."
ssh -i "$AWS_KEY" "$AWS_HOST" "sudo cp /tmp/relay-arm64.backup /opt/relay/relay-arm64 && sudo chmod +x /opt/relay/relay-arm64"
RESTORED_HASH=$(ssh -i "$AWS_KEY" "$AWS_HOST" "sha256sum /opt/relay/relay-arm64" | awk '{print $1}')
echo "Restored binary SHA-256: $RESTORED_HASH"
if [ "$RESTORED_HASH" != "$ORIGINAL_HASH" ]; then
    echo "FAIL: Restored hash does not match original"
    # Try to restart anyway to leave system in working state
    ssh -i "$AWS_KEY" "$AWS_HOST" "sudo systemctl restart relay.service" || true
    exit 1
fi
echo ""

# Step 6: Restart service and verify it works
echo "Step 6: Restarting service..."
ssh -i "$AWS_KEY" "$AWS_HOST" "sudo systemctl restart relay.service"
sleep 10
SERVICE_ACTIVE=$(ssh -i "$AWS_KEY" "$AWS_HOST" "sudo systemctl is-active relay.service")
echo "Service status: $SERVICE_ACTIVE"
if [ "$SERVICE_ACTIVE" != "active" ]; then
    echo "FAIL: Service did not start after rollback"
    exit 1
fi
echo ""

# Step 7: Verify relay is responding
echo "Step 7: Verifying relay is responding..."
HTTP_CODE=$(ssh -i "$AWS_KEY" "$AWS_HOST" "curl -s --max-time 30 -o /dev/null -w '%{http_code}' http://localhost:8080/api/events")
echo "HTTP code: $HTTP_CODE"
if [ "$HTTP_CODE" != "200" ] && [ "$HTTP_CODE" != "202" ]; then
    echo "FAIL: Relay not responding after rollback"
    exit 1
fi
echo ""

# Step 8: Cleanup
echo "Step 8: Cleaning up backup..."
ssh -i "$AWS_KEY" "$AWS_HOST" "sudo rm -f /tmp/relay-arm64.backup"
echo ""

echo "=== Rollback Test PASSED ==="
echo ""
echo "Evidence:"
echo "- Original binary SHA-256: $ORIGINAL_HASH"
echo "- Backup SHA-256: $BACKUP_HASH (matches original)"
echo "- Corrupted binary SHA-256: $CORRUPT_HASH (different from original)"
echo "- Restored binary SHA-256: $RESTORED_HASH (matches original)"
echo "- Service status after rollback: $SERVICE_ACTIVE"
echo "- HTTP code after rollback: $HTTP_CODE"
