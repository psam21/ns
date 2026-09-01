#!/usr/bin/env bash
set -Eeuo pipefail

# Full deployment script for the nostr.ltd relay and Blossom media service
# Safely clones/pulls code, builds, and deploys both services to AWS EC2
# Ensures systemd units and service artifacts are updated together

# Configuration - override via environment or edit these values
NS_DIR="${NS_DIR:-/home/jack/Documents/ns}"
RELAY_DIR="${NS_DIR}/relay"
BLOSSOM_DIR="${NS_DIR}/blossom"
BLOSSOM_REMOTE_DIR="/opt/blossom"
AWS_HOST="${AWS_HOST:-ubuntu@13.201.250.44}"
AWS_KEY="${AWS_KEY:-$HOME/.ssh/nostr-relay-key.pem}"
GIT_REPO="${GIT_REPO:-https://github.com/psam21/ns.git}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# ============================================
# Step 1: Fetch latest code from GitHub
# ============================================
log_info "=== Step 1: Fetching latest code from GitHub ==="

# Ensure the directory exists before clone handling
if [ ! -d "$NS_DIR" ]; then
    mkdir -p "$NS_DIR"
fi

# Check git status - require clean worktree
if [ -d "$NS_DIR/.git" ]; then
    log_info "Repository exists at $NS_DIR, checking for uncommitted changes..."

    # Check for dirty worktree
    if ! git -C "$NS_DIR" diff-index --quiet HEAD --; then
        log_error "Local repository has uncommitted changes. Commit or stash them before deploying."
    fi

    # Switch to main BEFORE pulling, so the fast-forward lands on the
    # branch we intend to deploy. Otherwise a checkout already on (say)
    # `feature/x` would pull `main` into the wrong branch and then
    # `switch main` would point at a stale local ref.
    log_info "Switching to main branch..."
    git -C "$NS_DIR" switch main

    log_info "Pulling latest changes (fast-forward only)..."
    git -C "$NS_DIR" pull --ff-only origin main
else
    # Clean clone. The directory must be empty (or contain only a .git
    # we already handled above) - git refuses to clone into a non-empty
    # target, and silently clobbering existing content would be worse.
    if [ -n "$(ls -A "$NS_DIR" 2>/dev/null)" ]; then
        log_error "Directory $NS_DIR exists but is not a git repository and is not empty. Remove the stray contents (e.g. $NS_DIR/temp) before deploying."
    fi
    log_info "Cloning repository from $GIT_REPO..."
    git clone "$GIT_REPO" "$NS_DIR"
    git -C "$NS_DIR" switch main
fi

log_info "Code successfully fetched from GitHub"

# If this script was launched from Downloads or another old checkout, hand
# execution to the freshly fetched canonical copy. This prevents a stale
# bootstrap from fetching new code and then continuing with its old logic.
CURRENT_SCRIPT=$(readlink -f "${BASH_SOURCE[0]}")
CANONICAL_SCRIPT=$(readlink -f "$NS_DIR/deploy/ns-deploy-full.sh")
if [[ "${NS_DEPLOY_CANONICAL:-0}" != "1" && -f "$CANONICAL_SCRIPT" && "$CURRENT_SCRIPT" != "$CANONICAL_SCRIPT" ]]; then
    log_info "Handing off to canonical deployment script at $CANONICAL_SCRIPT"
    exec env NS_DEPLOY_CANONICAL=1 bash "$CANONICAL_SCRIPT" "$@"
fi
echo ""

# ============================================
# Step 2: Run tests before building
# ============================================
log_info "=== Step 2: Running Go tests ==="

cd "$RELAY_DIR" || log_error "Cannot cd to $RELAY_DIR"

# Make the local origin explicit: the tests run against the files on
# disk in this repo's relay/ folder. The module path printed by `go test`
# (e.g. github.com/Shugur-Network/relay/...) is just Go's package
# identifier, not a remote import.
GO_MODULE=$(head -1 go.mod 2>/dev/null | awk '{print $2}')
log_info "Testing local relay sources at $RELAY_DIR (module: ${GO_MODULE:-unknown})"

# Run tests before building
go test ./... 2>&1

log_info "All Go tests passed"
echo ""

# ============================================
# Step 3: Build and validate Blossom
# ============================================
log_info "=== Step 3: Building Blossom service ==="

if [ ! -d "$BLOSSOM_DIR" ]; then
    log_error "Blossom source directory not found at $BLOSSOM_DIR"
fi

cd "$BLOSSOM_DIR" || log_error "Cannot cd to $BLOSSOM_DIR"
log_info "Installing Blossom dependencies from the committed lockfiles..."
pnpm install --frozen-lockfile 2>&1
log_info "Building Blossom server and admin assets..."
pnpm build 2>&1
log_info "Blossom build completed"
echo ""

# Return to the relay module before running Go commands.
cd "$RELAY_DIR" || log_error "Cannot cd to $RELAY_DIR"

# ============================================
# Step 4: Build the ARM64 relay binary
# ============================================
log_info "=== Step 4: Building ARM64 relay binary ==="

log_info "Downloading Go modules..."
go mod download 2>&1

log_info "Building ARM64 binary from local sources at $RELAY_DIR..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$RELAY_DIR/bin/relay-arm64" ./cmd 2>&1

log_info "Binary built successfully at $RELAY_DIR/bin/relay-arm64"
echo ""

# ============================================
# Step 5: Validate JavaScript (hard failure)
# ============================================
log_info "=== Step 5: Validating web static files ==="

if ! node --check "$RELAY_DIR/web/static/script.js" 2>&1; then
    log_error "JavaScript validation failed. Fix script.js before deploying."
fi

log_info "JavaScript validation passed"
echo ""

# ============================================
# Step 6: Prepare staging directory and copy files locally
# ============================================
log_info "=== Step 6: Preparing files for deployment ==="

STAGING=$(mktemp -d "/tmp/ns-deploy-XXXXXX")
# Install the local staging cleanup trap immediately so STAGING is
# removed even if Step 6 or Step 7 aborts before reaching the remote
# trap. Step 6 will replace this trap with one that also cleans the
# remote staging directory.
trap "rm -rf '$STAGING'" EXIT

# Copy relay binary to staging
cp "$RELAY_DIR/bin/relay-arm64" "$STAGING/relay-arm64"

# Build a self-contained Blossom artifact bundle. The existing remote
# /opt/blossom/.env and data directory are intentionally not replaced.
if [ ! -d "$BLOSSOM_DIR/build" ] || [ ! -d "$BLOSSOM_DIR/admin/dist" ] || [ ! -d "$BLOSSOM_DIR/public" ]; then
    log_error "Blossom build artifacts are incomplete under $BLOSSOM_DIR"
fi
mkdir -p "$STAGING/blossom"
tar -C "$BLOSSOM_DIR" -czf "$STAGING/blossom-artifacts.tgz" build public admin/dist package.json pnpm-lock.yaml

# Copy web templates and static files to staging
cp "$RELAY_DIR/web/templates/index.html" "$STAGING/index.html"
cp "$RELAY_DIR/web/static/style.css" "$STAGING/style.css"
cp "$RELAY_DIR/web/static/script.js" "$STAGING/script.js"

# Copy systemd service unit (CRITICAL: was previously missing!)
if [ -f "$NS_DIR/deploy/relay.service" ]; then
    cp "$NS_DIR/deploy/relay.service" "$STAGING/relay.service"
    log_info "Copied deploy/relay.service to staging"
else
    log_error "deploy/relay.service not found at $NS_DIR/deploy/relay.service - this is a hard failure"
fi

log_info "All files prepared in $STAGING"
echo ""

# ============================================
# Step 6: Copy files to AWS EC2 via SSH
# ============================================
log_info "=== Step 6: Copying files to AWS EC2 ==="

# Verify SSH key exists
if [ ! -f "$AWS_KEY" ]; then
    log_error "SSH key not found at $AWS_KEY"
fi

# Use a unique remote staging directory to avoid collisions with concurrent
# or interrupted runs that may have left files in /tmp.
REMOTE_STAGE="/tmp/ns-deploy-$(date +%s)-$RANDOM"
ssh -i "$AWS_KEY" "$AWS_HOST" "sudo mkdir -p '$REMOTE_STAGE' && sudo chown ubuntu:ubuntu '$REMOTE_STAGE' && sudo chmod 0755 '$REMOTE_STAGE'"

# Update the exit trap to also clean the remote staging directory on exit
trap "rm -rf '$STAGING'; ssh -i '$AWS_KEY' '$AWS_HOST' 'sudo rm -rf \"$REMOTE_STAGE\"' >/dev/null 2>&1 || true" EXIT

# Copy relay binary
log_info "Copying relay binary..."
scp -i "$AWS_KEY" "$STAGING/relay-arm64" "$AWS_HOST:$REMOTE_STAGE/relay-arm64"

# Copy web templates and static files
log_info "Copying web templates..."
scp -i "$AWS_KEY" "$STAGING/index.html" "$AWS_HOST:$REMOTE_STAGE/index.html"

log_info "Copying style.css..."
scp -i "$AWS_KEY" "$STAGING/style.css" "$AWS_HOST:$REMOTE_STAGE/style.css"

log_info "Copying script.js..."
scp -i "$AWS_KEY" "$STAGING/script.js" "$AWS_HOST:$REMOTE_STAGE/script.js"

# Copy systemd service unit (updates the service description).
# A missing unit file is now a hard failure: stale systemd branding was the
# original bug and must never silently re-occur.
if [ ! -f "$STAGING/relay.service" ]; then
    log_error "relay.service not staged at $STAGING/relay.service - refusing to deploy"
fi
log_info "Copying relay.service systemd unit..."
scp -i "$AWS_KEY" "$STAGING/relay.service" "$AWS_HOST:$REMOTE_STAGE/relay.service"

if [ -f "$NS_DIR/deploy/blossom.service" ]; then
    cp "$NS_DIR/deploy/blossom.service" "$STAGING/blossom.service"
else
    log_error "deploy/blossom.service not found at $NS_DIR/deploy/blossom.service - refusing to deploy Blossom"
fi
log_info "Copying Blossom runtime artifacts..."
scp -i "$AWS_KEY" "$STAGING/blossom-artifacts.tgz" "$AWS_HOST:$REMOTE_STAGE/blossom-artifacts.tgz"
scp -i "$AWS_KEY" "$STAGING/blossom.service" "$AWS_HOST:$REMOTE_STAGE/blossom.service"

log_info "All files copied to $AWS_HOST:$REMOTE_STAGE"
echo ""

# ============================================
# Step 8: Restart relay and Blossom services on AWS
# ============================================
log_info "=== Step 8: Restarting relay and Blossom services on AWS ==="

# Remote commands executed via SSH.
# Strategy:
#   1. Snapshot the current live release to a timestamped backup dir.
#   2. Stage the new release in /opt/relay/releases/<ts>/ (a fresh
#      directory; if any file fails, we abort before swapping).
#   3. Atomically swap each live path via rename(2). Each rename is
#      atomic on the same filesystem, so a reader sees either the old
#      file or the new file - never a half-written one. The release as a
#      whole is not atomic (a few syscalls elapse between renames), but
#      the running process only re-reads a file when it opens it, so the
#      practical window is tiny and the rollback below covers failures.
#   4. Install the systemd unit, daemon-reload, restart.
#   5. Verify the unit is active. If restart fails, restore the backup
#      files so the relay is not left pointing at a non-running binary.
#
# IMPORTANT: `bash -s` is required so the heredoc is read as a script on
# the remote side; without it the remote command would just be the
# variable assignment and the heredoc body would never execute.
ssh -i "$AWS_KEY" "$AWS_HOST" \
    "REMOTE_STAGE='$REMOTE_STAGE' BLOSSOM_REMOTE_DIR='$BLOSSOM_REMOTE_DIR' bash -s" << 'REMOTE_EOF'
set -Eeuo pipefail

: "${REMOTE_STAGE:?REMOTE_STAGE must be set by caller}"
: "${BLOSSOM_REMOTE_DIR:?BLOSSOM_REMOTE_DIR must be set by caller}"

RELEASES="/opt/relay/releases"
NEW_RELEASE="${RELEASES}/$(date +%Y%m%d_%H%M%S)"

# 1. Create a timestamped backup directory (each run gets its own).
BACKUP="/opt/relay/backup_$(date +%Y%m%d_%H%M%S)"
sudo mkdir -p "$BACKUP"

# Snapshot the live release and the live systemd unit.
sudo cp -a /opt/relay/relay-arm64             "$BACKUP/relay-arm64.bak"
sudo cp -a /opt/relay/web/templates/index.html "$BACKUP/index.html.bak"
sudo cp -a /opt/relay/web/static/style.css    "$BACKUP/style.css.bak"
sudo cp -a /opt/relay/web/static/script.js    "$BACKUP/script.js.bak"
sudo cp -a /etc/systemd/system/relay.service  "$BACKUP/relay.service.bak"

# 2. Stage the new release in a fresh directory. If any install fails,
#    the live paths are untouched and we abort before any swap.
sudo mkdir -p "$NEW_RELEASE/web/templates" "$NEW_RELEASE/web/static"
sudo chown -R root:root "$NEW_RELEASE"
sudo chmod 0755 "$NEW_RELEASE"

sudo install -o root  -g root  -m 0755 "$REMOTE_STAGE/relay-arm64" "$NEW_RELEASE/relay-arm64"
sudo install -o relay  -g relay  -m 0644 "$REMOTE_STAGE/index.html" "$NEW_RELEASE/web/templates/index.html"
sudo install -o relay  -g relay  -m 0644 "$REMOTE_STAGE/style.css"  "$NEW_RELEASE/web/static/style.css"
sudo install -o relay  -g relay  -m 0644 "$REMOTE_STAGE/script.js"  "$NEW_RELEASE/web/static/script.js"

# Install the systemd unit and reload before the swap so the unit is
# valid by the time the service is restarted.
sudo install -o root -g root -m 0644 "$REMOTE_STAGE/relay.service" /etc/systemd/system/relay.service
sudo systemctl daemon-reload

# 3. Atomic swap: rename(2) within the same filesystem is atomic, so a
#    reader sees either the old file or the new file, never a
#    half-written one. We stage -> .tmp then rename over the live path.
sudo install -o root  -g root  -m 0755 "$NEW_RELEASE/relay-arm64" /opt/relay/relay-arm64.tmp
sudo install -o relay  -g relay  -m 0644 "$NEW_RELEASE/web/templates/index.html" /opt/relay/web/templates/index.html.tmp
sudo install -o relay  -g relay  -m 0644 "$NEW_RELEASE/web/static/style.css"     /opt/relay/web/static/style.css.tmp
sudo install -o relay  -g relay  -m 0644 "$NEW_RELEASE/web/static/script.js"     /opt/relay/web/static/script.js.tmp

sudo mv -f /opt/relay/relay-arm64.tmp             /opt/relay/relay-arm64
sudo mv -f /opt/relay/web/templates/index.html.tmp /opt/relay/web/templates/index.html
sudo mv -f /opt/relay/web/static/style.css.tmp     /opt/relay/web/static/style.css
sudo mv -f /opt/relay/web/static/script.js.tmp     /opt/relay/web/static/script.js

# 4. Deploy and restart Blossom. The existing /opt/blossom/.env and
#    /opt/blossom/data are deliberately preserved.
BLOSSOM_BACKUP="/opt/blossom-backup_$(date +%Y%m%d_%H%M%S)"
BLOSSOM_NEW="${BLOSSOM_REMOTE_DIR}/.release_$(date +%Y%m%d_%H%M%S)"
sudo mkdir -p "$BLOSSOM_BACKUP" "$BLOSSOM_NEW"
sudo cp -a "$BLOSSOM_REMOTE_DIR/build" "$BLOSSOM_BACKUP/build.bak"
sudo cp -a "$BLOSSOM_REMOTE_DIR/public" "$BLOSSOM_BACKUP/public.bak"
sudo cp -a "$BLOSSOM_REMOTE_DIR/admin/dist" "$BLOSSOM_BACKUP/admin-dist.bak"
sudo cp -a /etc/systemd/system/blossom.service "$BLOSSOM_BACKUP/blossom.service.bak"
sudo tar -xzf "$REMOTE_STAGE/blossom-artifacts.tgz" -C "$BLOSSOM_NEW"
if ! command -v pnpm >/dev/null 2>&1; then
    echo "ERROR: pnpm is required on the AWS host to install Blossom production dependencies"
    exit 1
fi
if ! command -v make >/dev/null 2>&1 || ! command -v g++ >/dev/null 2>&1 || ! command -v python3 >/dev/null 2>&1; then
    if command -v apt-get >/dev/null 2>&1; then
        echo "Installing native build prerequisites for Blossom dependencies..."
        sudo apt-get update
        sudo DEBIAN_FRONTEND=noninteractive apt-get install -y build-essential python3
    else
        echo "ERROR: Blossom native dependencies require make, g++, and python3; apt-get is unavailable"
        exit 1
    fi
fi
sudo chown -R www-data:www-data "$BLOSSOM_NEW"
sudo -u www-data env HOME=/tmp pnpm --dir "$BLOSSOM_NEW" install --prod --frozen-lockfile
sudo chown -R root:root "$BLOSSOM_NEW"
sudo find "$BLOSSOM_NEW" -type d -exec chmod 0755 {} +
sudo find "$BLOSSOM_NEW" -type f -exec chmod 0644 {} +

restore_blossom() {
    sudo rm -rf "$BLOSSOM_REMOTE_DIR/build" "$BLOSSOM_REMOTE_DIR/public" "$BLOSSOM_REMOTE_DIR/admin/dist"
    sudo mkdir -p "$BLOSSOM_REMOTE_DIR/admin"
    sudo cp -a "$BLOSSOM_BACKUP/build.bak" "$BLOSSOM_REMOTE_DIR/build"
    sudo cp -a "$BLOSSOM_BACKUP/public.bak" "$BLOSSOM_REMOTE_DIR/public"
    sudo cp -a "$BLOSSOM_BACKUP/admin-dist.bak" "$BLOSSOM_REMOTE_DIR/admin/dist"
    sudo install -o root -g root -m 0644 "$BLOSSOM_BACKUP/blossom.service.bak" /etc/systemd/system/blossom.service
    sudo systemctl daemon-reload
}

BLOSSOM_DEPLOYED=0
BLOSSOM_SWAPPED=0
rollback_blossom_on_exit() {
    local status=$?
    if [ "$status" -ne 0 ] && [ "$BLOSSOM_DEPLOYED" -eq 0 ] && [ "$BLOSSOM_SWAPPED" -eq 1 ]; then
        echo "ERROR: Blossom deployment did not complete; restoring backup"
        restore_blossom || true
        sudo systemctl restart blossom.service || true
    fi
    exit "$status"
}
trap rollback_blossom_on_exit EXIT

sudo systemctl stop blossom.service
BLOSSOM_SWAPPED=1
if ! sudo mv "$BLOSSOM_REMOTE_DIR/build" "$BLOSSOM_BACKUP/build.live" ||
   ! sudo mv "$BLOSSOM_REMOTE_DIR/public" "$BLOSSOM_BACKUP/public.live" ||
   ! sudo mkdir -p "$BLOSSOM_REMOTE_DIR/admin" ||
   ! sudo mv "$BLOSSOM_REMOTE_DIR/admin/dist" "$BLOSSOM_BACKUP/admin-dist.live" ||
   ! sudo mv "$BLOSSOM_NEW/build" "$BLOSSOM_REMOTE_DIR/build" ||
   ! sudo mv "$BLOSSOM_NEW/public" "$BLOSSOM_REMOTE_DIR/public" ||
   ! sudo mkdir -p "$BLOSSOM_REMOTE_DIR/admin" ||
   ! sudo mv "$BLOSSOM_NEW/admin/dist" "$BLOSSOM_REMOTE_DIR/admin/dist"; then
    echo "ERROR: Blossom artifact swap failed"
    exit 1
fi
sudo install -o root -g root -m 0644 "$REMOTE_STAGE/blossom.service" /etc/systemd/system/blossom.service
sudo systemctl daemon-reload

if ! sudo systemctl restart blossom.service; then
    echo "ERROR: blossom.service restart failed"
    sudo systemctl --no-pager --full status blossom.service || true
    exit 1
fi
sleep 2
if sudo systemctl is-active --quiet blossom.service; then
    echo "blossom.service is ACTIVE"
else
    echo "ERROR: blossom.service is NOT active after restart"
    sudo systemctl --no-pager --full status blossom.service || true
    exit 1
fi
BLOSSOM_DEPLOYED=1

# 5. Restart the relay service. If `restart` fails, restore the backup files
#    so the relay is not left pointing at a non-running binary.
if ! sudo systemctl restart relay.service; then
    echo "ERROR: systemctl restart relay.service failed; rolling back"
    sudo cp -a "$BACKUP/relay-arm64.bak"             /opt/relay/relay-arm64
    sudo cp -a "$BACKUP/index.html.bak"               /opt/relay/web/templates/index.html
    sudo cp -a "$BACKUP/style.css.bak"                /opt/relay/web/static/style.css
    sudo cp -a "$BACKUP/script.js.bak"                /opt/relay/web/static/script.js
    sudo systemctl restart relay.service || true
    sudo systemctl --no-pager --full status relay.service || true
    exit 1
fi
sleep 2

# 5. Verify the unit is active.
if sudo systemctl is-active --quiet relay.service; then
    echo "relay.service is ACTIVE"
else
    echo "ERROR: relay.service is NOT active after restart!"
    sudo cp -a "$BACKUP/relay-arm64.bak"             /opt/relay/relay-arm64
    sudo cp -a "$BACKUP/index.html.bak"               /opt/relay/web/templates/index.html
    sudo cp -a "$BACKUP/style.css.bak"                /opt/relay/web/static/style.css
    sudo cp -a "$BACKUP/script.js.bak"                /opt/relay/web/static/script.js
    sudo systemctl restart relay.service || true
    sudo systemctl --no-pager --full status relay.service || true
    exit 1
fi

# Show full service status
echo "Service status:"
sudo systemctl --no-pager --full status relay.service
sudo systemctl --no-pager --full status blossom.service

# Show recent journal entries
echo "Recent relay journal entries (last 30 lines):"
sudo journalctl -u relay.service -n 30 --no-pager
echo "Recent Blossom journal entries (last 30 lines):"
sudo journalctl -u blossom.service -n 30 --no-pager
REMOTE_EOF

log_info "Remote service restart completed"
echo ""

# Retention: prune old backups NOW (after a successful swap, not after
# the backup is created — otherwise the retention step would delete the
# just-created backup if it leaves only N-1 older ones, leaving the
# rollback path with nothing to restore from). Each var defaults to 1
# (keep only the immediately previous release). Older history is
# reconstructable from git.
RELAY_BACKUP_RETAIN="${RELAY_BACKUP_RETAIN:-1}"
sudo bash -c "ls -dt /opt/relay/backup_* 2>/dev/null | tail -n +\$((RELAY_BACKUP_RETAIN + 1)) | xargs -r rm -rf"
BLOSSOM_BACKUP_RETAIN="${BLOSSOM_BACKUP_RETAIN:-1}"
sudo bash -c "ls -dt /opt/blossom-backup_* 2>/dev/null | tail -n +\$((BLOSSOM_BACKUP_RETAIN + 1)) | xargs -r rm -rf"

# ============================================
# Step 9: Verify deployment
# ============================================
log_info "=== Step 9: Verifying deployment ==="

# Check if relay is responding with NIP-11
log_info "Checking relay NIP-11 endpoint..."

# Fetch the COMPLETE NIP-11 response (the 77-entry registry exceeds 500
# bytes, so we must not truncate before piping to jq). Relay startup can take
# several seconds after systemd reports the unit active, so use bounded retry.
NIP11_RESPONSE=""
for attempt in {1..12}; do
    NIP11_RESPONSE=$(ssh -i "$AWS_KEY" "$AWS_HOST" \
        "curl --silent --max-time 10 -H 'Accept: application/nostr+json' http://localhost:8080/" 2>/dev/null || true)
    if printf '%s' "$NIP11_RESPONSE" | jq -e '.supported_nips | length == 77' >/dev/null 2>&1; then
        log_info "Relay NIP-11 endpoint responding (attempt $attempt)"
        break
    fi
    log_info "Relay NIP-11 not ready; waiting (attempt $attempt/12)..."
    sleep 5
done

if ! printf '%s' "$NIP11_RESPONSE" | jq -e '.supported_nips | length == 77' >/dev/null 2>&1; then
    log_error "Relay NIP-11 endpoint did not become ready after 60 seconds"
else
    # Verify key fields in NIP-11 response
    if echo "$NIP11_RESPONSE" | grep -q "nostr.ltd"; then
        log_info "Relay NIP-11 response contains 'nostr.ltd' - branding verified"
    else
        log_warn "Relay NIP-11 response does not contain 'nostr.ltd' - branding may need attention"
    fi

    if echo "$NIP11_RESPONSE" | grep -q '"name"'; then
        log_info "Relay NIP-11 response has 'name' field"
    fi

    # Check supported NIPs count - require exactly 77 entries.
    if echo "$NIP11_RESPONSE" | jq -e '.supported_nips | length == 77' >/dev/null 2>&1; then
        NIPS_COUNT=$(echo "$NIP11_RESPONSE" | jq '.supported_nips | length')
        log_info "Relay NIP-11 registry has exactly 77 supported NIPs"
    else
        NIPS_COUNT=$(echo "$NIP11_RESPONSE" | jq '.supported_nips | length' 2>/dev/null || echo "unknown")
        log_error "Relay NIP-11 registry does not have exactly 77 supported NIPs (found: $NIPS_COUNT)"
    fi
fi

# Check Blossom HTTP service
log_info "Checking Blossom HTTP service..."
BLOSSOM_HTTP=""
for attempt in {1..12}; do
    BLOSSOM_HTTP=$(ssh -i "$AWS_KEY" "$AWS_HOST" \
        "curl --silent --output /dev/null --write-out '%{http_code}' --max-time 10 http://localhost:3000/" 2>/dev/null || true)
    if [[ "$BLOSSOM_HTTP" =~ ^2|^3 ]]; then
        log_info "Blossom service responding on port 3000 with HTTP $BLOSSOM_HTTP (attempt $attempt)"
        break
    fi
    log_info "Blossom HTTP not ready; waiting (attempt $attempt/12)..."
    sleep 5
done
if [[ ! "$BLOSSOM_HTTP" =~ ^2|^3 ]]; then
    log_error "Blossom service verification failed after 60 seconds with HTTP ${BLOSSOM_HTTP:-unknown}"
fi

# Check /api/stats endpoint
log_info "Checking relay /api/stats endpoint..."
STATS_RESPONSE=$(ssh -i "$AWS_KEY" "$AWS_HOST" "curl -s --max-time 5 -H 'Accept: application/nostr+json' http://localhost:8080/api/stats 2>&1 || true" 2>/dev/null || true)

if printf '%s' "$STATS_RESPONSE" | jq -e . >/dev/null 2>&1; then
    log_info "/api/stats endpoint responding"
else
    log_warn "/api/stats endpoint not responding (may be normal during initial startup)"
fi

# Check /api/events endpoint. The grouped archive telemetry warmup
# can take a long time on a cold start with millions of stored events
# (observed ~25 minutes for 1M+ events on 2026-09-01). Poll for
# `status == "ready"` for up to 30 minutes; the cold-start case
# uses 15s between checks (so we still progress), the warm case
# resolves on the first or second check.
log_info "Checking /api/events endpoint..."

EVENTS_HTTP=""
EVENTS_STATUS="unknown"
EVENTS_MAX_ATTEMPTS="${EVENTS_MAX_ATTEMPTS:-120}"   # 120 * 15s = 30 min
EVENTS_INTERVAL="${EVENTS_INTERVAL:-15}"
EVENTS_ATTEMPT=0
while [ "$EVENTS_ATTEMPT" -lt "$EVENTS_MAX_ATTEMPTS" ]; do
    EVENTS_ATTEMPT=$((EVENTS_ATTEMPT + 1))
    EVENTS_JSON=$(ssh -i "$AWS_KEY" "$AWS_HOST" \
        "curl --silent --show-error --max-time 10 http://localhost:8080/api/events || true" 2>/dev/null || true)
    EVENTS_STATUS=$(printf '%s' "$EVENTS_JSON" | jq -r '.status // "unknown"' 2>/dev/null || echo "unknown")
    EVENTS_HTTP=$(ssh -i "$AWS_KEY" "$AWS_HOST" \
        "curl --silent --output /dev/null --write-out '%{http_code}' --max-time 10 http://localhost:8080/api/events || true" 2>/dev/null || true)

    if [[ "$EVENTS_HTTP" == "200" && "$EVENTS_STATUS" == "ready" ]]; then
        echo "Event cache is ready (attempt $EVENTS_ATTEMPT, after ~$((EVENTS_ATTEMPT * EVENTS_INTERVAL))s)."
        break
    fi

    echo "Event cache state: HTTP $EVENTS_HTTP / $EVENTS_STATUS; waiting (attempt $EVENTS_ATTEMPT/$EVENTS_MAX_ATTEMPTS)..."
    sleep "$EVENTS_INTERVAL"
done

if [[ "$EVENTS_HTTP" != "200" || "$EVENTS_STATUS" != "ready" ]]; then
    echo "ERROR: event cache is not ready after $((EVENTS_MAX_ATTEMPTS * EVENTS_INTERVAL)) seconds"
    ssh -i "$AWS_KEY" "$AWS_HOST" \
        "sudo journalctl -u relay.service --since '10 minutes ago' --no-pager | grep -Ei 'cache|event|postgres|database|query|error|fatal|panic' || true"
    exit 1
fi

log_info "Deployment verification complete"
echo ""

# ============================================
# Deployment Summary
# ============================================
log_info "=========================================="
log_info "DEPLOYMENT SUMMARY"
log_info "=========================================="
log_info "Repository: $NS_DIR"
log_info "Relay binary: $RELAY_DIR/bin/relay-arm64"
log_info "Blossom artifacts: $BLOSSOM_DIR/build, $BLOSSOM_DIR/public, $BLOSSOM_DIR/admin/dist"
log_info "AWS Host: $AWS_HOST"
log_info "Services: nostr.ltd Nostr Relay + Blossom Media Server"
log_info "NIP-11 branding: verified (nostr.ltd in response)"
log_info "Blossom HTTP: verified on localhost:3000"
log_info "=========================================="
