#!/usr/bin/env bash
set -Eeuo pipefail

# Full deployment script for the nostr.ltd relay and Blossom media service
# Safely clones/pulls code, builds, and deploys both services to AWS EC2
# Ensures systemd units and service artifacts are updated together

# Configuration. AWS_HOST and AWS_KEY are required operator inputs; keep
# them in the shell environment or an ignored local configuration file.
NS_DIR="${NS_DIR:-/home/jack/Documents/ns}"
RELAY_DIR="${NS_DIR}/relay"
BLOSSOM_DIR="${NS_DIR}/blossom"
BLOSSOM_REMOTE_DIR="/opt/blossom"
AWS_HOST="${AWS_HOST:-}"
AWS_KEY="${AWS_KEY:-}"
GIT_REPO="${GIT_REPO:-https://github.com/psam21/ns.git}"
RELAY_BACKUP_RETAIN="${RELAY_BACKUP_RETAIN:-0}"
BLOSSOM_BACKUP_RETAIN="${BLOSSOM_BACKUP_RETAIN:-0}"

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

if [[ -z "$AWS_HOST" ]]; then
    log_error "AWS_HOST is required. Set it to the authorized SSH destination (for example, ubuntu@example-host)."
fi
if [[ -z "$AWS_KEY" ]]; then
    log_error "AWS_KEY is required. Set it to the authorized SSH private key path."
fi
for retention in "$RELAY_BACKUP_RETAIN" "$BLOSSOM_BACKUP_RETAIN"; do
    if [[ ! "$retention" =~ ^[0-9]+$ ]]; then
        log_error "Backup retention values must be non-negative integers."
    fi
done

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

    # Check tracked and untracked files so the deploy cannot build from
    # source that is not represented by the commit being deployed.
    if [[ -n "$(git -C "$NS_DIR" status --porcelain --untracked-files=all)" ]]; then
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
# CI=1 and --config.confirmModulesPurge=false are both load-bearing.
#
# pnpm prompts "The modules directory at ... will be removed and reinstalled
# from scratch. Proceed? (Y/n)" when admin/node_modules was built by a
# different pnpm major version than the current one. In an interactive shell
# that prompt blocks forever with no output; it stalled a deploy on
# 2026-10-02 until it was answered by hand.
#
# CI=1 makes pnpm non-interactive and takes the default (proceed).
# --config.confirmModulesPurge=false suppresses the prompt outright. Either
# alone is sufficient, but they fail in opposite directions, so both are set:
# if a future pnpm drops one of them the other still prevents a hang.
#
# Never run a command that can prompt from inside a deploy.
CI=1 pnpm install --frozen-lockfile 2>&1
log_info "Building Blossom server and admin assets..."
# `</dev/null` guarantees no step in the build can block on a prompt. A pnpm
# modules-purge prompt inside an unattended deploy hangs forever and looks
# like a slow build rather than a failure.
( cd "$BLOSSOM_DIR" && npx tsc && CI=1 pnpm build ) </dev/null 2>&1 || {
    log_error "Blossom build failed"
    exit 1
}
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

# Validate relay binary before staging. A missing or zero-byte binary
# would cause the remote relay to fail to start after the swap.
if [ ! -s "$STAGING/relay-arm64" ]; then
    log_error "Relay binary is missing or empty at $STAGING/relay-arm64"
fi

# Build a self-contained Blossom artifact bundle. The existing remote
# /opt/blossom/.env and data directory are intentionally not replaced.
if [ ! -d "$BLOSSOM_DIR/build" ] || [ ! -d "$BLOSSOM_DIR/admin/dist" ] || [ ! -d "$BLOSSOM_DIR/public" ] || [ ! -d "$BLOSSOM_DIR/patches" ]; then
    log_error "Blossom build artifacts are incomplete under $BLOSSOM_DIR"
fi
mkdir -p "$STAGING/blossom"
tar -C "$BLOSSOM_DIR" -czf "$STAGING/blossom-artifacts.tgz" build public admin/dist package.json pnpm-lock.yaml pnpm-workspace.yaml config.yml patches scripts/normalize-runtime-patches.mjs

# Ship the rules-defaults script so operators can patch a drifted
# production config without a full redeploy. The script is idempotent.
if [ -f "$NS_DIR/deploy/blossom-rules-defaults.sh" ]; then
    cp "$NS_DIR/deploy/blossom-rules-defaults.sh" "$STAGING/blossom-rules-defaults.sh"
    log_info "Copied deploy/blossom-rules-defaults.sh to staging"
else
    log_warn "deploy/blossom-rules-defaults.sh not found; config drift repair must be manual"
fi

# Copy web templates and static files to staging
cp "$RELAY_DIR/web/templates/index.html" "$STAGING/index.html"
cp "$RELAY_DIR/web/static/style.css" "$STAGING/style.css"
cp "$RELAY_DIR/web/static/script.js" "$STAGING/script.js"

# Validate web assets before staging. Missing templates or static
# files would cause the dashboard to render incorrectly after deploy.
for f in index.html style.css script.js; do
    if [ ! -s "$STAGING/$f" ]; then
        log_error "Web asset $f is missing or empty in staging"
    fi
done

# Copy systemd service unit (CRITICAL: was previously missing!)
if [ -f "$NS_DIR/deploy/relay.service" ]; then
    cp "$NS_DIR/deploy/relay.service" "$STAGING/relay.service"
    log_info "Copied deploy/relay.service to staging"
else
    log_error "deploy/relay.service not found at $NS_DIR/deploy/relay.service - this is a hard failure"
fi

# Stage the health-watcher assets.
#
# These were previously installed by hand and were NOT part of the deploy, so
# they silently drifted: every edit to relay-memory-watch.sh between deploys
# existed only on the local machine, and a fresh host would come up with no
# monitoring at all. Staging them here makes the watcher part of the release,
# so it can no longer diverge from the code it monitors.
#
# A missing file is a warning, not an error. The watcher is defence in depth;
# refusing to deploy the relay because an optional observer is absent would
# trade a real outage risk for a cosmetic one.
for f in relay-memory-watch.sh relay-alert.sh relay-watch.service relay-watch.timer relay-recover.service; do
    if [ -f "$NS_DIR/deploy/$f" ]; then
        cp "$NS_DIR/deploy/$f" "$STAGING/$f"
        log_info "Copied deploy/$f to staging"
    else
        log_warn "deploy/$f not found; monitoring asset will not be updated"
    fi
done

# Fail loudly rather than letting a missing asset degrade into a quiet no-op.
#
# The staging loop warns and continues, which is right for a missing optional
# asset -- but it is exactly what hid the first attempt at this: all five files
# staged cleanly and logged "Copied ... to staging", while the transfer step
# simply never sent them. Nothing in the deploy output said "this will not be
# installed".
#
# The check that matters is end-to-end, so it belongs here where the staging
# result is still in hand: if a file we have cannot be staged, that is a bug in
# this script or a corrupt checkout, and continuing would ship a release whose
# monitoring silently did not update.
for f in relay-memory-watch.sh relay-alert.sh relay-watch.service relay-watch.timer relay-recover.service; do
    if [ -f "$NS_DIR/deploy/$f" ] && [ ! -s "$STAGING/$f" ]; then
        log_error "deploy/$f exists but did not stage to $STAGING/$f - refusing to continue"
    fi
done

log_info "All files prepared in $STAGING"
echo ""

# ============================================
# Step 7: Copy files to AWS EC2 via SSH
# ============================================
log_info "=== Step 7: Copying files to AWS EC2 ==="

# Verify SSH key exists
if [ ! -f "$AWS_KEY" ]; then
    log_error "SSH key not found at $AWS_KEY"
fi

# Capture the short git commit hash of the source we are about to
# deploy. The dashboard reads this back via /opt/relay/.last-commit
# and the RELAY_GIT_COMMIT env var so operators can see which commit
# the running binary came from.
GIT_COMMIT_SHORT="$(git -C "$NS_DIR" rev-parse --short=7 HEAD 2>/dev/null || echo unknown)"
log_info "Deploying commit: $GIT_COMMIT_SHORT"

# Preflight: refuse to deploy onto a host that is already in trouble.
#
# Restarting the relay is the single most disruptive thing this script does.
# Doing that while the host is already thrashing on swap is how you turn a
# degraded relay into an unreachable one — on 2026-10-02 `systemctl restart`
# hung in `deactivating (stop-sigterm)` because the wedged process could not
# drain connections, and only a manual SIGKILL recovered it.
#
# These are warnings, not hard failures: the operator may be deploying
# precisely because the host is sick. Set RELAY_PREFLIGHT_STRICT=1 to make
# them fatal.
log_info "Checking host health before restart..."
PREFLIGHT=$(ssh -i "$AWS_KEY" "$AWS_HOST" "bash -s" <<'PREFLIGHT_EOF' 2>/dev/null || true
set -u
# /proc/pressure/memory lines look like:
#   full avg10=0.00 avg60=0.00 avg300=0.00 total=...
# so avg10 is field 3 ($3), not $2. Taking $2 yields the literal "avg10="
# and every numeric comparison downstream silently passes.
full_avg10=$(awk '/^full/{split($3,a,"="); print a[2]; exit}' /proc/pressure/memory 2>/dev/null)
some_avg10=$(awk '/^some/{split($2,a,"="); print a[2]; exit}' /proc/pressure/memory 2>/dev/null)
swap_free_kb=$(awk '/^SwapFree/{print $2; exit}' /proc/meminfo 2>/dev/null)
swap_total_kb=$(awk '/^SwapTotal/{print $2; exit}' /proc/meminfo 2>/dev/null)
# ss -H prints a stats preamble before the socket table, so grep the LISTEN
# line rather than taking head -1, and Recv-Q is the first field on it.
# (Taking head -1 of the raw output previously read the Send-Q of an
# unrelated row and reported a healthy 0 backlog as 259.)
recv_q=$(ss -sntlH "sport = :8080" 2>/dev/null | awk '$1=="LISTEN"{print $2; exit}')
[ -z "$recv_q" ] && recv_q="unknown"
relay_state=$(systemctl is-active relay.service 2>/dev/null || echo unknown)
relay_http=$(curl -s -o /dev/null -w '%{http_code}' --max-time 8 http://localhost:8080/ 2>/dev/null || echo 000)
echo "full_avg10=${full_avg10:-unknown}"
echo "some_avg10=${some_avg10:-unknown}"
echo "swap_free_kb=${swap_free_kb:-0}"
echo "swap_total_kb=${swap_total_kb:-0}"
echo "recv_q=${recv_q}"
echo "relay_state=${relay_state}"
echo "relay_http=${relay_http}"
PREFLIGHT_EOF
)

if [ -n "$PREFLIGHT" ]; then
    p_full=$(printf '%s\n' "$PREFLIGHT" | awk -F= '/^full_avg10/{print $2}')
    p_swap_free=$(printf '%s\n' "$PREFLIGHT" | awk -F= '/^swap_free_kb/{print $2}')
    p_swap_total=$(printf '%s\n' "$PREFLIGHT" | awk -F= '/^swap_total_kb/{print $2}')
    p_recvq=$(printf '%s\n' "$PREFLIGHT" | awk -F= '/^recv_q/{print $2}')
    p_http=$(printf '%s\n' "$PREFLIGHT" | awk -F= '/^relay_http/{print $2}')
    p_state=$(printf '%s\n' "$PREFLIGHT" | awk -F= '/^relay_state/{print $2}')

    log_info "  memory pressure (full avg10): ${p_full}"
    log_info "  swap free:                   $((p_swap_free / 1024))MB of $((p_swap_total / 1024))MB"
    log_info "  port 8080 Recv-Q:            ${p_recvq}"
    log_info "  relay systemd state:         ${p_state}"
    log_info "  relay HTTP response:         ${p_http}"

    PREFLIGHT_PROBLEMS=0
    if [ "$p_http" != "200" ] && [ "$p_http" != "202" ]; then
        log_warn "  relay is not serving HTTP right now (got ${p_http}); it is already degraded"
        PREFLIGHT_PROBLEMS=$((PREFLIGHT_PROBLEMS + 1))
    fi
    if [ -n "$p_full" ] && [ "$p_full" != "unknown" ] && awk "BEGIN{exit !($p_full > 40)}"; then
        log_warn "  host is under heavy memory pressure (full avg10=${p_full}); a restart may hang"
        PREFLIGHT_PROBLEMS=$((PREFLIGHT_PROBLEMS + 1))
    fi
    if [ "${p_swap_total:-0}" -gt 0 ] && [ "${p_swap_free:-0}" -eq 0 ]; then
        log_warn "  swap is fully exhausted; expect a slow restart"
        PREFLIGHT_PROBLEMS=$((PREFLIGHT_PROBLEMS + 1))
    fi
    if [ "$p_recvq" != "unknown" ] && [ "${p_recvq:-0}" -gt 0 ]; then
        log_warn "  port 8080 has a non-zero accept backlog (Recv-Q=${p_recvq}); the relay is not accepting"
        PREFLIGHT_PROBLEMS=$((PREFLIGHT_PROBLEMS + 1))
    fi

    if [ "$PREFLIGHT_PROBLEMS" -gt 0 ]; then
        if [ "${RELAY_PREFLIGHT_STRICT:-0}" = "1" ]; then
            log_error "Preflight found ${PREFLIGHT_PROBLEMS} problem(s) and RELAY_PREFLIGHT_STRICT=1; aborting."
            exit 1
        fi
        log_warn "Preflight found ${PREFLIGHT_PROBLEMS} issue(s). Continuing, but the restart may hang."
        log_warn "If it does: 'sudo systemctl kill -s SIGKILL relay' then 'sudo systemctl start relay'."
    else
        log_info "  host looks healthy; proceeding."
    fi
else
    log_warn "Could not run host preflight (SSH or tooling issue); proceeding without it."
fi

# Use a unique remote staging directory to avoid collisions with concurrent
# or interrupted runs that may have left files in /tmp.
REMOTE_STAGE="/tmp/ns-deploy-$(date +%s)-$RANDOM"
ssh -i "$AWS_KEY" "$AWS_HOST" "mkdir -p '$REMOTE_STAGE' && chmod 0755 '$REMOTE_STAGE'"

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
printf '%s\n' "$GIT_COMMIT_SHORT" > "$STAGING/last-commit"
scp -i "$AWS_KEY" "$STAGING/last-commit" "$AWS_HOST:$REMOTE_STAGE/last-commit"

if [ -f "$NS_DIR/deploy/blossom.service" ]; then
    cp "$NS_DIR/deploy/blossom.service" "$STAGING/blossom.service"
else
    log_error "deploy/blossom.service not found at $NS_DIR/deploy/blossom.service - refusing to deploy Blossom"
fi
log_info "Copying Blossom runtime artifacts..."
scp -i "$AWS_KEY" "$STAGING/blossom-artifacts.tgz" "$AWS_HOST:$REMOTE_STAGE/blossom-artifacts.tgz"
scp -i "$AWS_KEY" "$STAGING/blossom.service" "$AWS_HOST:$REMOTE_STAGE/blossom.service"

# Ship the rules-defaults script so operators can repair config drift
# on the production host without a full redeploy.
if [ -f "$STAGING/blossom-rules-defaults.sh" ]; then
    scp -i "$AWS_KEY" "$STAGING/blossom-rules-defaults.sh" "$AWS_HOST:$REMOTE_STAGE/blossom-rules-defaults.sh"
fi

# Ship the health-watcher assets to the remote stage.
#
# The list here must match the list staged in Step 6. They did not match on
# the first attempt: Step 6 staged all five monitoring files and logged
# "Copied deploy/relay-alert.sh to staging", but this step transfers an
# explicit per-file list that did not include them. Every asset therefore
# reached the remote as absent, the install loops printed "not staged", and
# the deploy reported success.
#
# The failure was quiet in the worst way: the staging step's success message
# made it look like the assets were on their way. /usr/local/bin kept the
# hand-installed copy, so relay-memory-watch.sh on the host was a stale build
# with none of the fixes, and relay-alert.sh did not exist at all.
#
# Derived from the same filename list as Step 6 so the two cannot drift.
# Anything staged but not transferred is a silent no-op, so both the copy and
# the install iterate this one list.
for f in relay-memory-watch.sh relay-alert.sh relay-watch.service relay-watch.timer relay-recover.service; do
    if [ -f "$STAGING/$f" ]; then
        scp -i "$AWS_KEY" "$STAGING/$f" "$AWS_HOST:$REMOTE_STAGE/$f"
    fi
done
log_info "Copied health-watcher assets to $REMOTE_STAGE"

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
REMOTE_RUN_ID="$(date +%s)-$RANDOM-$$"
ssh -i "$AWS_KEY" "$AWS_HOST" \
    "sudo flock -n -E 75 /var/lock/nostr-ltd-deploy.lock bash -s -- '$REMOTE_STAGE' '$BLOSSOM_REMOTE_DIR' '$GIT_COMMIT_SHORT' '$RELAY_BACKUP_RETAIN' '$BLOSSOM_BACKUP_RETAIN' '$REMOTE_RUN_ID'; status=\$?; if [ \"\$status\" -eq 75 ]; then echo 'ERROR: another nostr.ltd deployment is already running'; fi; exit \"\$status\"" << 'REMOTE_EOF'
set -Eeuo pipefail

: "${1:?REMOTE_STAGE must be passed by caller}"
: "${2:?BLOSSOM_REMOTE_DIR must be passed by caller}"
: "${3:?RELAY_GIT_COMMIT must be passed by caller}"
: "${4:?RELAY_BACKUP_RETAIN must be passed by caller}"
: "${5:?BLOSSOM_BACKUP_RETAIN must be passed by caller}"
: "${6:?REMOTE_RUN_ID must be passed by caller}"

REMOTE_STAGE="$1"
BLOSSOM_REMOTE_DIR="$2"
RELAY_GIT_COMMIT="$3"
RELAY_BACKUP_RETAIN="$4"
BLOSSOM_BACKUP_RETAIN="$5"
REMOTE_RUN_ID="$6"

for retention in "$RELAY_BACKUP_RETAIN" "$BLOSSOM_BACKUP_RETAIN"; do
    case "$retention" in
        ''|*[!0-9]*)
            echo "ERROR: backup retention values must be non-negative integers"
            exit 2
            ;;
    esac
done

RELEASES="/opt/relay/releases"
NEW_RELEASE="${RELEASES}/${REMOTE_RUN_ID}"

# Retention policy: keep zero on-disk backups. The full source is
# tracked in git, and every artifact we ship (the Go binary, the
# Blossom bundle) is reproducible from a clean build. Operators
# who want on-disk rollback can set RELAY_BACKUP_RETAIN=1 (or any
# positive number) in the environment. With the default 0, the
# backup directory created below is removed at the end of the
# current deploy, freeing ~30MB of relay and ~270MB of Blossom
# space each run.
# 1. Create a timestamped backup directory (each run gets its own).
#    This is the rollback anchor for THIS deploy; it is removed at
#    the end of the deploy if RELAY_BACKUP_RETAIN=0 (the default).
BACKUP="/opt/relay/backup_${REMOTE_RUN_ID}"
BLOSSOM_BACKUP="/opt/blossom-backup_${REMOTE_RUN_ID}"
BLOSSOM_NEW="${BLOSSOM_REMOTE_DIR}/.release_${REMOTE_RUN_ID}"
sudo mkdir -p "$BACKUP"
sudo mkdir -p "$BLOSSOM_BACKUP" "$BLOSSOM_NEW"

# Snapshot the live release and the live systemd unit.
sudo cp -a /opt/relay/relay-arm64             "$BACKUP/relay-arm64.bak"
sudo cp -a /opt/relay/web/templates/index.html "$BACKUP/index.html.bak"
sudo cp -a /opt/relay/web/static/style.css    "$BACKUP/style.css.bak"
sudo cp -a /opt/relay/web/static/script.js    "$BACKUP/script.js.bak"
sudo cp -a /etc/systemd/system/relay.service  "$BACKUP/relay.service.bak"
if [ -f /opt/relay/.last-commit ]; then
    sudo cp -a /opt/relay/.last-commit "$BACKUP/last-commit.bak"
else
    sudo touch "$BACKUP/last-commit.absent"
fi

# Snapshot Blossom before the relay is changed. The dependency tree and
# manifests are part of the rollback boundary because the service runs from
# /opt/blossom, not from the temporary release directory.
sudo cp -a "$BLOSSOM_REMOTE_DIR/build" "$BLOSSOM_BACKUP/build.bak"
sudo cp -a "$BLOSSOM_REMOTE_DIR/public" "$BLOSSOM_BACKUP/public.bak"
sudo cp -a "$BLOSSOM_REMOTE_DIR/admin/dist" "$BLOSSOM_BACKUP/admin-dist.bak"
sudo cp -a /etc/systemd/system/blossom.service "$BLOSSOM_BACKUP/blossom.service.bak"
for path in node_modules package.json pnpm-lock.yaml pnpm-workspace.yaml patches config.yml scripts/normalize-runtime-patches.mjs; do
    if [ -e "$BLOSSOM_REMOTE_DIR/$path" ]; then
        sudo cp -a "$BLOSSOM_REMOTE_DIR/$path" "$BLOSSOM_BACKUP/${path//\//-}.bak"
    else
        sudo touch "$BLOSSOM_BACKUP/${path//\//-}.absent"
    fi
done

RELAY_SWAPPED=0
BLOSSOM_SWAPPED=0
RELAY_STOPPED=0
BLOSSOM_STOPPED=0
DEPLOYMENT_COMPLETED=0

restore_relay() {
    echo "Restoring relay release from $BACKUP"
    sudo systemctl stop relay.service || true
    sudo cp -a "$BACKUP/relay-arm64.bak" /opt/relay/relay-arm64
    sudo cp -a "$BACKUP/index.html.bak" /opt/relay/web/templates/index.html
    sudo cp -a "$BACKUP/style.css.bak" /opt/relay/web/static/style.css
    sudo cp -a "$BACKUP/script.js.bak" /opt/relay/web/static/script.js
    sudo install -o root -g root -m 0644 "$BACKUP/relay.service.bak" /etc/systemd/system/relay.service
    if [ -f "$BACKUP/last-commit.bak" ]; then
        sudo install -o relay -g relay -m 0644 "$BACKUP/last-commit.bak" /opt/relay/.last-commit
    else
        sudo rm -f /opt/relay/.last-commit
    fi
    sudo systemctl daemon-reload
    sudo systemctl restart relay.service || true
}

restore_blossom() {
    echo "Restoring Blossom release from $BLOSSOM_BACKUP"
    sudo systemctl stop blossom.service || true
    sudo rm -rf "$BLOSSOM_REMOTE_DIR/build" "$BLOSSOM_REMOTE_DIR/public" \
        "$BLOSSOM_REMOTE_DIR/admin/dist" "$BLOSSOM_REMOTE_DIR/node_modules" \
        "$BLOSSOM_REMOTE_DIR/package.json" "$BLOSSOM_REMOTE_DIR/pnpm-lock.yaml" \
        "$BLOSSOM_REMOTE_DIR/pnpm-workspace.yaml" \
        "$BLOSSOM_REMOTE_DIR/patches" "$BLOSSOM_REMOTE_DIR/config.yml" \
        "$BLOSSOM_REMOTE_DIR/scripts/normalize-runtime-patches.mjs"
    sudo mkdir -p "$BLOSSOM_REMOTE_DIR/admin"
    sudo cp -a "$BLOSSOM_BACKUP/build.bak" "$BLOSSOM_REMOTE_DIR/build"
    sudo cp -a "$BLOSSOM_BACKUP/public.bak" "$BLOSSOM_REMOTE_DIR/public"
    sudo cp -a "$BLOSSOM_BACKUP/admin-dist.bak" "$BLOSSOM_REMOTE_DIR/admin/dist"
    for path in node_modules package.json pnpm-lock.yaml pnpm-workspace.yaml patches config.yml scripts/normalize-runtime-patches.mjs; do
        backup_path="$BLOSSOM_BACKUP/${path//\//-}.bak"
        if [ -e "$backup_path" ]; then
            sudo cp -a "$backup_path" "$BLOSSOM_REMOTE_DIR/$path"
        fi
    done
    sudo install -o root -g root -m 0644 "$BLOSSOM_BACKUP/blossom.service.bak" /etc/systemd/system/blossom.service
    sudo systemctl daemon-reload
    sudo systemctl restart blossom.service || true
}

rollback_deployment_on_exit() {
    local status=$?
    if [ "$status" -ne 0 ] && [ "$DEPLOYMENT_COMPLETED" -eq 0 ]; then
        echo "ERROR: deployment failed; restoring any live service state that was changed"
        if [ "$BLOSSOM_SWAPPED" -eq 1 ]; then
            restore_blossom || true
        elif [ "$BLOSSOM_STOPPED" -eq 1 ]; then
            sudo systemctl restart blossom.service || true
        fi
        if [ "$RELAY_SWAPPED" -eq 1 ]; then
            restore_relay || true
        elif [ "$RELAY_STOPPED" -eq 1 ]; then
            sudo systemctl restart relay.service || true
        fi
    fi
    sudo rm -rf "$NEW_RELEASE" "$BLOSSOM_NEW" 2>/dev/null || true
    exit "$status"
}
trap rollback_deployment_on_exit EXIT

# 2. Stage the new release in a fresh directory. If any install fails,
#    the live paths are untouched and we abort before any swap.
sudo mkdir -p "$NEW_RELEASE/web/templates" "$NEW_RELEASE/web/static"
sudo chown -R root:root "$NEW_RELEASE"
sudo chmod 0755 "$NEW_RELEASE"

sudo install -o root  -g root  -m 0755 "$REMOTE_STAGE/relay-arm64" "$NEW_RELEASE/relay-arm64"
sudo install -o relay  -g relay  -m 0644 "$REMOTE_STAGE/index.html" "$NEW_RELEASE/web/templates/index.html"
sudo install -o relay  -g relay  -m 0644 "$REMOTE_STAGE/style.css"  "$NEW_RELEASE/web/static/style.css"
sudo install -o relay  -g relay  -m 0644 "$REMOTE_STAGE/script.js"  "$NEW_RELEASE/web/static/script.js"

# Stage the commit hash so it is swapped with the relay release and
# never describes a different binary during rollback.
if [ -n "${RELAY_GIT_COMMIT:-}" ] && [ "${RELAY_GIT_COMMIT}" != "unknown" ]; then
    echo "${RELAY_GIT_COMMIT}" | sudo tee "$NEW_RELEASE/last-commit" >/dev/null
    sudo chown relay:relay "$NEW_RELEASE/last-commit"
    sudo chmod 0644 "$NEW_RELEASE/last-commit"
fi

# Stage the systemd unit; install it into /etc only with the rest of
# the relay swap after all rollback handlers are active.
sudo install -o root -g root -m 0644 "$REMOTE_STAGE/relay.service" "$NEW_RELEASE/relay.service"

# 3. Atomic swap: rename(2) within the same filesystem is atomic, so a
#    reader sees either the old file or the new file, never a
#    half-written one. We stage -> .tmp then rename over the live path.
sudo install -o root  -g root  -m 0755 "$NEW_RELEASE/relay-arm64" /opt/relay/relay-arm64.tmp
sudo install -o relay  -g relay  -m 0644 "$NEW_RELEASE/web/templates/index.html" /opt/relay/web/templates/index.html.tmp
sudo install -o relay  -g relay  -m 0644 "$NEW_RELEASE/web/static/style.css"     /opt/relay/web/static/style.css.tmp
sudo install -o relay  -g relay  -m 0644 "$NEW_RELEASE/web/static/script.js"     /opt/relay/web/static/script.js.tmp
sudo install -o root -g root -m 0644 "$NEW_RELEASE/relay.service" /etc/systemd/system/relay.service.tmp
if [ -f "$NEW_RELEASE/last-commit" ]; then
    sudo install -o relay -g relay -m 0644 "$NEW_RELEASE/last-commit" /opt/relay/.last-commit.tmp
fi

# Install the health-watcher assets.
#
# Deliberately NOT transactional and NOT part of RELAY_SWAPPED: the watcher
# observes the relay rather than constituting it, so a failure here must not
# trigger a relay rollback. Each file is installed independently and a missing
# one is reported without aborting.
#
# The webhook URL is deliberately not managed here. It is a secret, it belongs
# in a systemd drop-in rather than a tracked file, and overwriting it on every
# deploy would clobber an operator's configuration. Enabling alerting is:
#
#   sudo systemctl edit relay-watch.service
#   # [Service]
#   # Environment=ALERT_WEBHOOK=https://...
#
# With ALERT_WEBHOOK unset the notifier logs to stderr and sends nothing, so
# shipping it is inert until that drop-in exists.
for f in relay-memory-watch.sh relay-alert.sh; do
    if [ -f "$REMOTE_STAGE/$f" ]; then
        sudo install -o root -g root -m 0755 "$REMOTE_STAGE/$f" "/usr/local/bin/$f"
    else
        echo "WARN: $f not staged; existing /usr/local/bin/$f left untouched"
    fi
done

for f in relay-watch.service relay-watch.timer relay-recover.service; do
    if [ -f "$REMOTE_STAGE/$f" ]; then
        sudo install -o root -g root -m 0644 "$REMOTE_STAGE/$f" "/etc/systemd/system/$f"
    else
        echo "WARN: $f not staged; existing unit left untouched"
    fi
done

RELAY_SWAPPED=1
sudo mv -f /etc/systemd/system/relay.service.tmp /etc/systemd/system/relay.service
sudo mv -f /opt/relay/relay-arm64.tmp             /opt/relay/relay-arm64
sudo mv -f /opt/relay/web/templates/index.html.tmp /opt/relay/web/templates/index.html
sudo mv -f /opt/relay/web/static/style.css.tmp     /opt/relay/web/static/style.css
sudo mv -f /opt/relay/web/static/script.js.tmp     /opt/relay/web/static/script.js
if [ -f /opt/relay/.last-commit.tmp ]; then
    sudo mv -f /opt/relay/.last-commit.tmp /opt/relay/.last-commit
fi
sudo systemctl daemon-reload

# The per-deploy staging tree at /opt/relay/releases/<ts>/ is dead
# weight now: we never read from it again. Drop it to free ~30MB.
sudo rm -rf "$NEW_RELEASE"

# Enable the health watcher.
#
# `enable --now` is idempotent: on a host that already has it running this is a
# no-op, and on a fresh host it starts the observer. A failed enable is
# reported but does not abort the deploy, for the same reason the install above
# is non-transactional -- the watcher guards the relay, it is not the relay.
if [ -f "$REMOTE_STAGE/relay-watch.timer" ]; then
    if sudo systemctl enable --now relay-watch.timer >/dev/null 2>&1; then
        echo "OK: relay-watch.timer enabled and active"
    else
        echo "WARN: could not enable relay-watch.timer; health watching is OFF"
        echo "      investigate with: journalctl -u relay-watch.service"
    fi
fi

# 4. Deploy and restart Blossom. The existing /opt/blossom/.env and
#    /opt/blossom/data are deliberately preserved.
sudo tar -xzf "$REMOTE_STAGE/blossom-artifacts.tgz" -C "$BLOSSOM_NEW"
for required in \
    package.json pnpm-lock.yaml pnpm-workspace.yaml scripts/normalize-runtime-patches.mjs \
    patches/minio@8.0.7.patch patches/stream-json@3.6.0.patch \
    build public admin/dist; do
    if [ ! -e "$BLOSSOM_NEW/$required" ]; then
        echo "ERROR: Blossom artifact is missing $required"
        exit 1
    fi
done
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

# better-sqlite3 is a native addon, so its postinstall runs `node-gyp rebuild`.
# pnpm bundles node-gyp at dist/node-gyp-bin/node-gyp but does NOT put it on
# PATH for lifecycle scripts, and the install below runs as www-data, which
# does not inherit root's PATH. The result is:
#
#   .../better-sqlite3 install: sh: 1: node-gyp: not found
#   ELIFECYCLE  Command failed.
#
# which aborts the whole deploy. Observed 2026-10-02; the transaction rollback
# correctly restored both services, so this cost a deploy and not an outage,
# but it blocks every future deploy of this script until fixed.
#
# node-gyp is verified present and executable by www-data at the path below
# (v11.5.0 on the production host), so prepending it to PATH is sufficient.
NODE_GYP_BIN="/usr/lib/node_modules/pnpm/dist/node-gyp-bin"
if [ ! -x "$NODE_GYP_BIN/node-gyp" ]; then
    # Fall back to wherever node-gyp actually lives on this host rather than
    # assuming the pnpm layout.
    NODE_GYP_BIN=$(dirname "$(find /usr/lib/node_modules /usr/local/lib/node_modules \
        -maxdepth 4 -name node-gyp -type f 2>/dev/null | head -1)")
fi
if [ ! -x "$NODE_GYP_BIN/node-gyp" ]; then
    echo "ERROR: node-gyp not found; better-sqlite3 cannot build"
    echo "       Install it with: sudo npm install -g node-gyp"
    exit 1
fi
echo "Using node-gyp from $NODE_GYP_BIN"

sudo -u www-data env \
    HOME=/tmp \
    PATH="$NODE_GYP_BIN:$PATH" \
    npm_config_nodedir=/usr \
    bash -c 'cd "$1" && pnpm install --prod --frozen-lockfile' bash "$BLOSSOM_NEW"
if [ ! -d "$BLOSSOM_NEW/node_modules" ]; then
    echo "ERROR: Blossom production dependency installation did not create node_modules"
    exit 1
fi

# The install above can succeed while leaving the native addon unbuilt if the
# lifecycle script was skipped (pnpm's build-approval can disable it). Verify
# the compiled artifact exists, and rebuild explicitly if it does not. Failing
# here is much cheaper than discovering a broken Blossom after the swap.
SQLITE_ADDON=$(find "$BLOSSOM_NEW/node_modules/.pnpm" -name 'better_sqlite3.node' \
    -path '*linux-arm64*' -print -quit 2>/dev/null)
if [ -z "$SQLITE_ADDON" ]; then
    SQLITE_ADDON=$(find "$BLOSSOM_NEW/node_modules" -name 'better_sqlite3.node' \
        -print -quit 2>/dev/null)
fi
if [ -z "$SQLITE_ADDON" ]; then
    echo "Native addon better_sqlite3.node is missing; building it explicitly..."
    sudo -u www-data env \
        HOME=/tmp \
        PATH="$NODE_GYP_BIN:$PATH" \
        npm_config_nodedir=/usr \
        bash -c 'cd "$1" && pnpm rebuild better-sqlite3' bash "$BLOSSOM_NEW" || true
    SQLITE_ADDON=$(find "$BLOSSOM_NEW/node_modules" -name 'better_sqlite3.node' \
        -print -quit 2>/dev/null)
fi
if [ -z "$SQLITE_ADDON" ]; then
    echo "ERROR: better_sqlite3.node was not built; Blossom would fail at runtime"
    echo "       Check: sudo -u www-data env PATH=$NODE_GYP_BIN:\$PATH pnpm rebuild better-sqlite3"
    exit 1
fi
echo "Native addon present: $SQLITE_ADDON"

# pnpm 9 records these patched packages in the virtual-store name but can
# leave the case-compatibility patch files unapplied on a clean host. Apply
# the two small runtime compatibility changes explicitly, then validate the
# files that MinIO loads at startup before allowing a live swap.
STREAM_JSON_DIR=$(find "$BLOSSOM_NEW/node_modules/.pnpm" -type d -path '*/node_modules/stream-json' -print -quit)
MINIO_DIR=$(find "$BLOSSOM_NEW/node_modules/.pnpm" -type d -path '*/node_modules/minio' -print -quit)
if [ -z "$STREAM_JSON_DIR" ] || [ -z "$MINIO_DIR" ]; then
    echo "ERROR: Blossom runtime dependency directories are missing after pnpm install"
    exit 1
fi
if [ ! -f "$STREAM_JSON_DIR/src/jsonl/Parser.js" ]; then
    if [ -f "$STREAM_JSON_DIR/src/jsonl/parser.js" ]; then
        cp "$STREAM_JSON_DIR/src/jsonl/parser.js" "$STREAM_JSON_DIR/src/jsonl/Parser.js"
    else
        echo "ERROR: stream-json parser compatibility file is missing"
        exit 1
    fi
fi
for minio_notification in \
    "$MINIO_DIR/dist/esm/notification.mjs" \
    "$MINIO_DIR/dist/main/notification.js"; do
    if [ -f "$minio_notification" ]; then
        sed -i 's#stream-json/jsonl/Parser.js#stream-json/jsonl/parser.js#g' "$minio_notification"
    fi
done
if grep -Fq 'stream-json/jsonl/Parser.js' "$MINIO_DIR/dist/esm/notification.mjs" || \
   [ ! -f "$STREAM_JSON_DIR/src/jsonl/Parser.js" ]; then
    echo "ERROR: Blossom runtime compatibility normalization failed"
    exit 1
fi
sudo chown -R root:root "$BLOSSOM_NEW"
sudo find "$BLOSSOM_NEW" -type d -exec chmod 0755 {} +
sudo find "$BLOSSOM_NEW" -type f -exec chmod 0644 {} +

sudo systemctl stop blossom.service
BLOSSOM_STOPPED=1

# Pre-swap validation: confirm the extracted artifact is complete
# before swapping it into the live tree. This catches tar extraction
# failures, missing directories, and pnpm install errors that would
# otherwise leave the live tree in a broken state.
echo "Validating extracted Blossom artifact before swap..."
if [ ! -d "$BLOSSOM_NEW/build" ] || [ ! -d "$BLOSSOM_NEW/public" ] || [ ! -d "$BLOSSOM_NEW/admin/dist" ] || [ ! -f "$BLOSSOM_NEW/build/index.js" ] || [ ! -d "$BLOSSOM_NEW/node_modules" ]; then
    echo "ERROR: Extracted Blossom artifact is incomplete; aborting before swap"
    echo "  Expected: $BLOSSOM_NEW/build/index.js, build/, public/, admin/dist/, node_modules/"
    sudo ls -la "$BLOSSOM_NEW" 2>/dev/null || true
    exit 1
fi
echo "Blossom artifact validation passed"

BLOSSOM_SWAPPED=1
if ! sudo mv "$BLOSSOM_REMOTE_DIR/build" "$BLOSSOM_BACKUP/build.live" ||
   ! sudo mv "$BLOSSOM_REMOTE_DIR/public" "$BLOSSOM_BACKUP/public.live" ||
   ! sudo mkdir -p "$BLOSSOM_REMOTE_DIR/admin" ||
   ! sudo mv "$BLOSSOM_REMOTE_DIR/admin/dist" "$BLOSSOM_BACKUP/admin-dist.live"; then
    echo "ERROR: Blossom live artifact backup failed"
    exit 1
fi
for path in node_modules package.json pnpm-lock.yaml pnpm-workspace.yaml patches scripts/normalize-runtime-patches.mjs; do
    if [ -e "$BLOSSOM_REMOTE_DIR/$path" ]; then
        sudo mv "$BLOSSOM_REMOTE_DIR/$path" "$BLOSSOM_BACKUP/${path//\//-}.live"
    fi
done
sudo mv "$BLOSSOM_NEW/build" "$BLOSSOM_REMOTE_DIR/build"
sudo mv "$BLOSSOM_NEW/public" "$BLOSSOM_REMOTE_DIR/public"
sudo mkdir -p "$BLOSSOM_REMOTE_DIR/admin"
sudo mv "$BLOSSOM_NEW/admin/dist" "$BLOSSOM_REMOTE_DIR/admin/dist"
sudo mv "$BLOSSOM_NEW/node_modules" "$BLOSSOM_REMOTE_DIR/node_modules"
sudo mv "$BLOSSOM_NEW/package.json" "$BLOSSOM_REMOTE_DIR/package.json"
sudo mv "$BLOSSOM_NEW/pnpm-lock.yaml" "$BLOSSOM_REMOTE_DIR/pnpm-lock.yaml"
sudo mv "$BLOSSOM_NEW/pnpm-workspace.yaml" "$BLOSSOM_REMOTE_DIR/pnpm-workspace.yaml"
sudo mv "$BLOSSOM_NEW/patches" "$BLOSSOM_REMOTE_DIR/patches"
sudo mkdir -p "$BLOSSOM_REMOTE_DIR/scripts"
sudo mv "$BLOSSOM_NEW/scripts/normalize-runtime-patches.mjs" "$BLOSSOM_REMOTE_DIR/scripts/normalize-runtime-patches.mjs"
sudo rm -rf "$BLOSSOM_NEW"
# Keep the preserved production config aligned with the canonical relay URL.
# The live config is intentionally not replaced wholesale because it is
# operator-managed, but this alias correction is safe and restart-required.
if sudo grep -q 'wss://www.nostr.ltd' "$BLOSSOM_REMOTE_DIR/config.yml"; then
    echo "Updating Blossom discovery relay to wss://nostr.ltd"
    sudo sed -i 's#wss://www.nostr.ltd#wss://nostr.ltd#g' "$BLOSSOM_REMOTE_DIR/config.yml"
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

# 5b. Repair storage.rules drift if needed. The deploy ships the
#     canonical config.yml in the artifact bundle, but the live
#     /opt/blossom/config.yml is preserved (it holds S3 credentials
#     via env interpolation). If the live config has an empty
#     storage.rules list, the rules-defaults script patches it with
#     the same defaults that ship in the tracked config. The script
#     is idempotent: it only patches if rules: [] is present.
if [ -f "$REMOTE_STAGE/blossom-rules-defaults.sh" ]; then
    echo "Checking for storage.rules drift..."
    if sudo grep -q "^  rules: \[\]" /opt/blossom/config.yml || sudo grep -Eq '^      expiration: (1 month|2 days)$' /opt/blossom/config.yml; then
        echo "Legacy storage.rules retention detected; running blossom-rules-defaults.sh"
        sudo mkdir -p /opt/blossom/scripts
        sudo cp "$REMOTE_STAGE/blossom-rules-defaults.sh" /opt/blossom/scripts/blossom-rules-defaults.sh
        sudo chmod +x /opt/blossom/scripts/blossom-rules-defaults.sh
        sudo bash /opt/blossom/scripts/blossom-rules-defaults.sh || echo "WARN: rules-defaults script failed; manual repair required"
    else
        echo "storage.rules already configured; no drift repair needed"
    fi
fi

# 5. Restart the relay service. If `restart` fails, restore the backup files
#    so the relay is not left pointing at a non-running binary. Also
#    restore Blossom from its backup since the two-service deploy is
#    not atomic; a relay failure after Blossom was swapped leaves
#    Blossom in the new state unless we explicitly roll it back.
if ! sudo systemctl restart relay.service; then
    echo "ERROR: systemctl restart relay.service failed; the transaction rollback handler will restore both services"
    sudo systemctl --no-pager --full status relay.service || true
    exit 1
fi

# 5a. Wait for the relay to actually accept requests, not merely for systemd
#     to report the unit as active.
#
#     `systemctl is-active` is not a health check. During the 2026-10-02
#     outage the relay reported `active (running)` for five days while serving
#     zero bytes on every request: threads were wedged in D state on swap, the
#     accept backlog was saturated (Recv-Q 2624), and the port was bound but
#     nothing was accepted. A deploy that only asserts is-active would have
#     reported success throughout that entire outage.
#
#     So poll the local HTTP surface until it answers. On the production host
#     this went from 56s+ (bloom rebuild blocking before ListenAndServe) to
#     ~1s once that was moved off the startup path.
RELAY_READY_TIMEOUT="${RELAY_READY_TIMEOUT:-120}"
RELAY_READY_INTERVAL="${RELAY_READY_INTERVAL:-2}"
RELAY_READY_URL="http://localhost:8080/"
RELAY_WAITED=0
RELAY_READY=0
while [ "$RELAY_WAITED" -lt "$RELAY_READY_TIMEOUT" ]; do
    # HTTP 200 or 202 both mean the handler ran. A wedged relay returns 000,
    # because curl is giving up on a connection that never delivers a byte.
    relay_code=$(curl -s -o /dev/null --write-out '%{http_code}' \
        --max-time 5 "$RELAY_READY_URL" 2>/dev/null || true)
    case "$relay_code" in
        200|202)
            RELAY_READY=1
            break
            ;;
    esac
    sleep "$RELAY_READY_INTERVAL"
    RELAY_WAITED=$((RELAY_WAITED + RELAY_READY_INTERVAL))
done

if [ "$RELAY_READY" -ne 1 ]; then
    echo "ERROR: relay did not serve HTTP within ${RELAY_READY_TIMEOUT}s after restart"
    echo "       (systemd may report 'active' while the process is wedged; this is the check that catches it)"
    sudo systemctl --no-pager --full status relay.service || true
    echo "--- listener state (Recv-Q should be 0 on a healthy relay) ---"
    sudo ss -sntl "sport = :8080" || true
    echo "--- memory pressure ---"
    cat /proc/pressure/memory || true
    echo "--- recent relay journal ---"
    sudo journalctl -u relay.service -n 40 --no-pager || true
    echo "ERROR: rolling back via the transaction handler"
    exit 1
fi
echo "relay is serving HTTP after ${RELAY_WAITED}s"

# 5b. Verify the unit is active.
if sudo systemctl is-active --quiet relay.service; then
    echo "relay.service is ACTIVE"
else
    echo "ERROR: relay.service is NOT active after restart; the transaction rollback handler will restore both services"
    sudo systemctl --no-pager --full status relay.service || true
    exit 1
fi

DEPLOYMENT_COMPLETED=1

# Show full service status
echo "Service status:"
sudo systemctl --no-pager --full status relay.service
sudo systemctl --no-pager --full status blossom.service

# Show recent journal entries
echo "Recent relay journal entries (last 30 lines):"
sudo journalctl -u relay.service -n 30 --no-pager
echo "Recent Blossom journal entries (last 30 lines):"
sudo journalctl -u blossom.service -n 30 --no-pager

# Retention: prune old backups NOW (after a successful swap, not
# after the backup is created — otherwise the retention step would
# delete the just-created backup if it leaves only N-1 older ones,
# leaving the rollback path with nothing to restore from).
#
# Each var defaults to 0: with the full source tracked in git, every
# shipped artifact is reproducible from `git log` + a clean build,
# and keeping on-disk copies of every previous release rapidly
# exhausts the production host's 19GB root volume (each Blossom
# release is ~270MB, each relay binary is ~30MB). Operators who want
# a one-step on-host rollback can set RELAY_BACKUP_RETAIN=1 /
# BLOSSOM_BACKUP_RETAIN=1 in the environment.
prune_backups() {
    local base="$1"
    local prefix="$2"
    local retain="$3"
    local index=0
    local path
    while IFS= read -r -d '' path; do
        index=$((index + 1))
        if [ "$index" -gt "$retain" ]; then
            sudo rm -rf "$path"
        fi
    done < <(sudo find "$base" -mindepth 1 -maxdepth 1 -type d -name "${prefix}*" -print0 | sort -z -r)
}

prune_backups /opt/relay "backup_" "$RELAY_BACKUP_RETAIN"
prune_backups /opt "blossom-backup_" "$BLOSSOM_BACKUP_RETAIN"

echo "On-host cleanup: retained relay=$RELAY_BACKUP_RETAIN, blossom=$BLOSSOM_BACKUP_RETAIN backups"
df -h /
REMOTE_EOF

log_info "Remote service restart completed"
echo ""

# ============================================
# Step 9: Verify deployment
# ============================================
log_info "=== Step 9: Verifying deployment ==="

# Check if relay is responding with NIP-11
log_info "Checking relay NIP-11 endpoint..."
NIP11_BRANDING_STATUS="UNKNOWN"

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
        NIP11_BRANDING_STATUS="VERIFIED"
    else
        log_warn "Relay NIP-11 response does not contain 'nostr.ltd' - branding may need attention"
        NIP11_BRANDING_STATUS="WARNING"
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

# Functional upload probe: verify that blossom.nostr.ltd actually accepts
# an authenticated upload. This catches the class of failure where the
# service is up (HTTP 200 on /) but uploads are rejected because of
# misconfigured storage.rules, missing S3 credentials, or auth
# middleware regressions.
#
# The probe uses a disposable test identity generated on the fly and a
# random 1KB blob. It uploads, retrieves, and cleans up. No real user
# credentials or production data are involved.
#
# The probe is skipped automatically if BLOSSOM_UPLOAD_PROBE=n is set.
# The remote probe reports SKIPPED when neither nak nor its npx fallback
# is available, and the verification log marks that as MANUAL.
log_info "Checking Blossom upload functionality (functional probe)..."
BLOSSOM_UPLOAD_PROBE="${BLOSSOM_UPLOAD_PROBE:-y}"

if [[ "$BLOSSOM_UPLOAD_PROBE" != "y" ]]; then
    log_warn "Blossom upload probe disabled (BLOSSOM_UPLOAD_PROBE=$BLOSSOM_UPLOAD_PROBE); upload verification is MANUAL"
    BLOSSOM_UPLOAD_PROBE_STATUS="DISABLED"
else
    PROBE_RESULT=$(ssh -i "$AWS_KEY" "$AWS_HOST" "BLOSSOM_PUBLIC_URL='${BLOSSOM_PUBLIC_URL:-https://blossom.nostr.ltd}' bash -s" << 'PROBE_EOF' 2>&1 || true
set -uo pipefail

: "${BLOSSOM_PUBLIC_URL:?BLOSSOM_PUBLIC_URL must be set by caller}"

# Generate a disposable test identity (not a real user's key).
# Prefer nak (fiatjaf/nak) when available; fall back to nostr-tools
# via npx. The previous fallback used @nostr/tools which is not a
# real npm package; the correct name is nostr-tools.
TEST_NSEC=$(nak key generate private 2>/dev/null || npx -y nostr-tools key generate private 2>/dev/null || true)
if [ -z "$TEST_NSEC" ]; then
    echo "PROBE_STATUS=skipped"
    echo "PROBE_REASON=unable to generate test key (nak or nostr-tools not available)"
    exit 0
fi

# Create a 1KB random blob and compute its SHA-256.
TMPDIR=$(mktemp -d)
PROBE_UPLOADED=0
PROBE_DELETED=0
cleanup_probe_blob() {
    if [ "${PROBE_UPLOADED:-0}" -eq 1 ] && [ "${PROBE_DELETED:-0}" -eq 0 ] && [ -n "${BLOB_SHA256:-}" ] && [ -n "${TEST_NSEC:-}" ]; then
        CLEANUP_EVENT=$(nak event --kind 24242 \
            --tag t=delete \
            --tag expiration=$(( $(date +%s) + 300 )) \
            --tag x="$BLOB_SHA256" \
            --content "" \
            --sec "$TEST_NSEC" </dev/null 2>/dev/null || true)
        if [ -n "$CLEANUP_EVENT" ]; then
            CLEANUP_B64=$(printf '%s' "$CLEANUP_EVENT" | base64 -w 0)
            curl --silent --output /dev/null --max-time 10 \
                -X DELETE \
                -H "Authorization: Nostr $CLEANUP_B64" \
                "$BLOSSOM_PUBLIC_URL/$BLOB_SHA256" 2>/dev/null || true
        fi
    fi
    rm -rf "$TMPDIR"
}
trap cleanup_probe_blob EXIT
dd if=/dev/urandom of="$TMPDIR/blob.bin" bs=1024 count=1 status=none
BLOB_SHA256=$(sha256sum "$TMPDIR/blob.bin" | awk '{print $1}')

# Build and sign a NIP-42 auth event (kind 24242) for the upload.
# Redirect stdin from /dev/null: nak event tries to read a partial
# event from stdin when stdin is a TTY, which makes it hang
# indefinitely under non-interactive SSH sessions.
AUTH_EVENT=$(nak event --kind 24242 \
    --tag t=upload \
    --tag expiration=$(( $(date +%s) + 300 )) \
    --tag x="$BLOB_SHA256" \
    --content "" \
    --sec "$TEST_NSEC" </dev/null 2>/dev/null || true)

if [ -z "$AUTH_EVENT" ]; then
    echo "PROBE_STATUS=skipped"
    echo "PROBE_REASON=unable to build auth event"
    exit 0
fi

# Blossom expects the Authorization header to carry a base64-encoded
# JSON event (the auth middleware calls atob() on the value after the
# "Nostr " prefix). nak emits raw JSON, so we encode here. Without
# this step the server returns 400 Bad Request with no body and no
# log line, which makes the failure look like a transport problem.
AUTH_B64=$(printf '%s' "$AUTH_EVENT" | base64 -w 0)

# Upload the blob exactly once and keep the response body out of logs
# unless a bounded, secret-free diagnostic is needed.
UPLOAD_BODY_FILE="$TMPDIR/upload-response"
UPLOAD_HTTP=$(curl --silent --show-error --max-time 30 \
    --output "$UPLOAD_BODY_FILE" --write-out '%{http_code}' \
    -X PUT \
    -H "Authorization: Nostr $AUTH_B64" \
    -H "Content-Type: image/png" \
    -H "X-Sha-256: $BLOB_SHA256" \
    --data-binary "@$TMPDIR/blob.bin" \
    "$BLOSSOM_PUBLIC_URL/upload" 2>/dev/null || echo "000")

if [[ "$UPLOAD_HTTP" =~ ^2 ]]; then
    PROBE_UPLOADED=1
    # Verify retrieval and its content hash before deleting the probe blob.
    RETRIEVE_FILE="$TMPDIR/retrieved.bin"
    RETRIEVE_HTTP=$(curl --silent --output "$RETRIEVE_FILE" --write-out '%{http_code}' \
        --max-time 10 \
        "$BLOSSOM_PUBLIC_URL/$BLOB_SHA256" 2>/dev/null || echo "000")
    RETRIEVE_SHA256=""
    if [[ "$RETRIEVE_HTTP" == "200" && -f "$RETRIEVE_FILE" ]]; then
        RETRIEVE_SHA256=$(sha256sum "$RETRIEVE_FILE" | awk '{print $1}')
    fi

    # Blossom deletion uses the same kind-24242 identity with t=delete and
    # the blob hash in the x tag. Attempt cleanup even when retrieval fails.
    DELETE_EVENT=$(nak event --kind 24242 \
        --tag t=delete \
        --tag expiration=$(( $(date +%s) + 300 )) \
        --tag x="$BLOB_SHA256" \
        --content "" \
        --sec "$TEST_NSEC" </dev/null 2>/dev/null || true)
    DELETE_HTTP="000"
    if [ -n "$DELETE_EVENT" ]; then
        DELETE_B64=$(printf '%s' "$DELETE_EVENT" | base64 -w 0)
        DELETE_HTTP=$(curl --silent --output /dev/null --write-out '%{http_code}' \
            --max-time 10 \
            -X DELETE \
            -H "Authorization: Nostr $DELETE_B64" \
            "$BLOSSOM_PUBLIC_URL/$BLOB_SHA256" 2>/dev/null || echo "000")
        if [[ "$DELETE_HTTP" =~ ^2 ]]; then
            PROBE_DELETED=1
        fi
    fi

    if [[ "$RETRIEVE_HTTP" == "200" && "$RETRIEVE_SHA256" == "$BLOB_SHA256" && "$DELETE_HTTP" =~ ^2 ]]; then
        echo "PROBE_STATUS=pass"
        echo "PROBE_UPLOAD_HTTP=$UPLOAD_HTTP"
        echo "PROBE_RETRIEVE_HTTP=$RETRIEVE_HTTP"
        echo "PROBE_DELETE_HTTP=$DELETE_HTTP"
        echo "PROBE_SHA256=$BLOB_SHA256"
    else
        echo "PROBE_STATUS=fail"
        echo "PROBE_REASON=upload HTTP $UPLOAD_HTTP, retrieve HTTP $RETRIEVE_HTTP (sha256 $RETRIEVE_SHA256), delete HTTP $DELETE_HTTP"
        echo "PROBE_SHA256=$BLOB_SHA256"
    fi
else
    echo "PROBE_STATUS=fail"
    echo "PROBE_REASON=upload returned HTTP $UPLOAD_HTTP"
    echo "PROBE_BODY=$(tr '\n' ' ' < "$UPLOAD_BODY_FILE" | cut -c1-200)"
fi
PROBE_EOF
)

# Parse and report probe results. Never log the auth event or nsec.
PROBE_STATUS=$(echo "$PROBE_RESULT" | grep -oE 'PROBE_STATUS=[a-z]+' | cut -d= -f2 || echo "unknown")
PROBE_REASON=$(echo "$PROBE_RESULT" | grep -oE 'PROBE_REASON=[^[:space:]].*' | cut -d= -f2- || echo "")
PROBE_UPLOAD_HTTP=$(echo "$PROBE_RESULT" | grep -oE 'PROBE_UPLOAD_HTTP=[0-9]+' | cut -d= -f2 || echo "n/a")
PROBE_RETRIEVE_HTTP=$(echo "$PROBE_RESULT" | grep -oE 'PROBE_RETRIEVE_HTTP=[0-9]+' | cut -d= -f2 || echo "n/a")
PROBE_DELETE_HTTP=$(echo "$PROBE_RESULT" | grep -oE 'PROBE_DELETE_HTTP=[0-9]+' | cut -d= -f2 || echo "n/a")

case "$PROBE_STATUS" in
    pass)
        log_info "Blossom upload probe PASSED (upload HTTP $PROBE_UPLOAD_HTTP, retrieve HTTP $PROBE_RETRIEVE_HTTP, delete HTTP $PROBE_DELETE_HTTP)"
        BLOSSOM_UPLOAD_PROBE_STATUS="PASS (upload $PROBE_UPLOAD_HTTP, retrieve $PROBE_RETRIEVE_HTTP, delete $PROBE_DELETE_HTTP)"
        ;;
    fail)
        log_error "Blossom upload probe FAILED: $PROBE_REASON"
        BLOSSOM_UPLOAD_PROBE_STATUS="FAIL ($PROBE_REASON)"
        ;;
    skipped)
        log_warn "Blossom upload probe SKIPPED: $PROBE_REASON (upload verification is MANUAL)"
        BLOSSOM_UPLOAD_PROBE_STATUS="SKIPPED (MANUAL: $PROBE_REASON)"
        ;;
    *)
        log_warn "Blossom upload probe returned unexpected status: $PROBE_STATUS"
        BLOSSOM_UPLOAD_PROBE_STATUS="UNKNOWN ($PROBE_STATUS)"
        ;;
esac
fi

# Check /api/stats endpoint
log_info "Checking relay /api/stats endpoint..."
STATS_RESPONSE=$(ssh -i "$AWS_KEY" "$AWS_HOST" "curl -s --max-time 5 -H 'Accept: application/nostr+json' http://localhost:8080/api/stats 2>&1 || true" 2>/dev/null || true)

if printf '%s' "$STATS_RESPONSE" | jq -e . >/dev/null 2>&1; then
    log_info "/api/stats endpoint responding"
else
    log_warn "/api/stats endpoint not responding (may be normal during initial startup)"
fi

# Check /api/events endpoint with partial-readiness recognition.
# The response now exposes four independent readiness signals:
#   - relay_health:        ok | unavailable  (process + database)
#   - stored_events_ready: direct COUNT(*) succeeded
#   - total_ready:         2026+ bounded count succeeded
#   - status:              grouped breakdown cache state
#
# Classification:
#   Healthy:        relay_health=ok AND stored_events_ready=true
#   Partially ready: relay_health=ok AND stored_events_ready=true
#                    but grouped status != "ready" (warming)
#   Unhealthy:      relay_health != "ok" OR stored_events_ready=false
#                    after the timeout, OR endpoint unreachable
#
# A warming grouped breakdown no longer aborts verification when
# direct totals are ready. The grouped cold-start can take ~25
# minutes on a 1M+ event database (observed 2026-09-01).
log_info "Checking /api/events endpoint (partial-readiness aware)..."

EVENTS_HTTP=""
EVENTS_JSON=""
EVENTS_OUTCOME="unknown"
RELAY_HEALTH="unknown"
STORED_READY="false"
TOTAL_READY="false"
GROUPED_STATUS="unknown"
EVENTS_MAX_ATTEMPTS="${EVENTS_MAX_ATTEMPTS:-120}"   # 120 * 15s = 30 min
EVENTS_INTERVAL="${EVENTS_INTERVAL:-15}"
EVENTS_ATTEMPT=0
while [ "$EVENTS_ATTEMPT" -lt "$EVENTS_MAX_ATTEMPTS" ]; do
    EVENTS_ATTEMPT=$((EVENTS_ATTEMPT + 1))
    # Defensive curl: || true prevents set -e from aborting on
    # transient network failures before we can classify the state.
    EVENTS_JSON=$(ssh -i "$AWS_KEY" "$AWS_HOST" \
        "curl --silent --show-error --max-time 10 http://localhost:8080/api/events || true" 2>/dev/null || true)
    EVENTS_HTTP=$(ssh -i "$AWS_KEY" "$AWS_HOST" \
        "curl --silent --output /dev/null --write-out '%{http_code}' --max-time 10 http://localhost:8080/api/events || true" 2>/dev/null || true)

    # Parse each readiness layer defensively. Missing fields default
    # to false/unknown so a malformed response is classified as
    # Unhealthy, not silently treated as Healthy.
    if [[ "$EVENTS_HTTP" == "200" || "$EVENTS_HTTP" == "202" ]]; then
        RELAY_HEALTH=$(printf '%s' "$EVENTS_JSON" | jq -r '.relay_health // "unknown"' 2>/dev/null || echo "unknown")
        STORED_READY=$(printf '%s' "$EVENTS_JSON" | jq -r '.stored_events_ready // false' 2>/dev/null || echo "false")
        TOTAL_READY=$(printf '%s' "$EVENTS_JSON" | jq -r '.total_ready // false' 2>/dev/null || echo "false")
        GROUPED_STATUS=$(printf '%s' "$EVENTS_JSON" | jq -r '.status // "unknown"' 2>/dev/null || echo "unknown")

        if [[ "$RELAY_HEALTH" == "ok" && "$STORED_READY" == "true" && "$GROUPED_STATUS" == "ready" ]]; then
            EVENTS_OUTCOME="healthy"
            echo "Event telemetry is HEALTHY (attempt $EVENTS_ATTEMPT, after ~$((EVENTS_ATTEMPT * EVENTS_INTERVAL))s)."
            break
        elif [[ "$RELAY_HEALTH" == "ok" && "$STORED_READY" == "true" ]]; then
            EVENTS_OUTCOME="partially_ready"
            echo "Event telemetry is PARTIALLY READY: direct totals available, grouped status=$GROUPED_STATUS (attempt $EVENTS_ATTEMPT/$EVENTS_MAX_ATTEMPTS)."
            # Continue polling for the grouped breakdown to finish,
            # but cap at the timeout. If it doesn't finish, we
            # accept Partially ready as a non-fatal outcome below.
        elif [[ "$RELAY_HEALTH" != "ok" ]]; then
            EVENTS_OUTCOME="unhealthy"
            echo "Event telemetry is UNHEALTHY: relay_health=$RELAY_HEALTH (attempt $EVENTS_ATTEMPT/$EVENTS_MAX_ATTEMPTS)."
            break
        fi
    else
        echo "Event endpoint not ready or unreachable: HTTP $EVENTS_HTTP (attempt $EVENTS_ATTEMPT/$EVENTS_MAX_ATTEMPTS)."
    fi

    sleep "$EVENTS_INTERVAL"
done

# Final classification. Partially ready is a non-fatal warning when
# direct totals are available; only Unhealthy or unreachable is fatal.
case "$EVENTS_OUTCOME" in
    healthy)
        log_info "Relay dashboard: HEALTHY (all layers ready)"
        ;;
    partially_ready)
        log_warn "Relay dashboard: PARTIALLY READY (direct totals available, grouped breakdown still warming after $((EVENTS_MAX_ATTEMPTS * EVENTS_INTERVAL))s)"
        log_warn "Deployment continues; grouped breakdown will complete in the background"
        ;;
    unhealthy|unknown)
        log_warn "Relay dashboard: UNHEALTHY (relay_health=$RELAY_HEALTH, stored_events_ready=$STORED_READY, grouped=$GROUPED_STATUS, http=${EVENTS_HTTP:-unknown})"
        ssh -i "$AWS_KEY" "$AWS_HOST" \
            "sudo journalctl -u relay.service --since '10 minutes ago' --no-pager | grep -Ei 'cache|event|postgres|database|query|error|fatal|panic' || true"
        log_error "Relay dashboard verification failed"
        ;;
esac

log_info "Deployment verification complete"
echo ""

# ============================================
# Separate Status Reporting (issue #108)
# ============================================
# Print five separate status lines so operators can see at a glance
# which layer is healthy and which is degraded. No secrets in any
# line; only HTTP status codes and readiness flags.
log_info "=========================================="
log_info "DEPLOYMENT STATUS (separate layers)"
log_info "=========================================="
log_info "1. Relay process:        $(ssh -i "$AWS_KEY" "$AWS_HOST" "sudo systemctl is-active --quiet relay.service && echo ACTIVE || echo INACTIVE" 2>/dev/null || echo UNKNOWN)"
log_info "2. Blossom process:      $(ssh -i "$AWS_KEY" "$AWS_HOST" "sudo systemctl is-active --quiet blossom.service && echo ACTIVE || echo INACTIVE" 2>/dev/null || echo UNKNOWN)"
log_info "3. Direct event totals:  $(printf '%s' "$EVENTS_JSON" | jq -r '.stored_events_ready // false' 2>/dev/null | sed 's/true/READY/; s/false/PENDING/')"
log_info "4. Grouped telemetry:    $(printf '%s' "$EVENTS_JSON" | jq -r '.status // "unknown"' 2>/dev/null | tr '[:lower:]' '[:upper:]')"
log_info "5. Upload functionality: ${BLOSSOM_UPLOAD_PROBE_STATUS:-SKIPPED}"
log_info "=========================================="
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
log_info "NIP-11 branding: $NIP11_BRANDING_STATUS"
log_info "Blossom HTTP: verified on localhost:3000"
log_info "=========================================="
