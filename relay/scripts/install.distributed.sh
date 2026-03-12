#!/bin/bash

# Shugur Relay - SECURE Distributed Cluster Installation Script
# Multi-node Relay + Caddy connecting to managed Aurora PostgreSQL
# - Each node runs Relay + Caddy containers
# - All nodes connect to a shared Aurora PostgreSQL endpoint
# - No local database containers needed (Aurora is managed)
#
# REQUIREMENTS:
# - Run locally with sudo/root (for Docker install on remote if needed)
# - ssh/sshpass/openssl/curl available locally
# - Remote nodes: Ubuntu/Debian, passwordless sudo for Docker install
# - Open ports: 22, 8080, 80, 443
# - Aurora PostgreSQL endpoint (provisioned separately)


set -euo pipefail

# ---------- cleanup trap ----------
cleanup_on_exit() {
  local exit_code=$?
  if [[ $exit_code -ne 0 ]]; then
    log_error "Installation failed with exit code $exit_code"
    log_info "Attempting cleanup of temporary files..."
    
    if [[ -d "$CLUSTER_DIR" ]]; then
      log_debug "Removing local deployment staging directory: $CLUSTER_DIR" 
      rm -rf "$CLUSTER_DIR" 2>/dev/null || sudo rm -rf "$CLUSTER_DIR" 2>/dev/null || true
    fi
    
    log_info "Cleanup completed. Please check the error above and retry installation."
  fi
}

# Set up cleanup trap for script failures
trap cleanup_on_exit EXIT

# ---------- colors / logging ----------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
NC='\033[0m'

log_info()    { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $1"; }
log_debug()   { echo -e "${BLUE}[DEBUG]${NC} $1"; }
log_cluster() { echo -e "${PURPLE}[CLUSTER]${NC} $1"; }

# ---------- globals ----------
CLUSTER_NODES=()
CLUSTER_URLS=()
SSH_USER="ubuntu"
SSH_PASSWORD=""
CLUSTER_DIR="./cluster-deploy"      # local staging for configs
REMOTE_DIR="~/shugur-relay"         # remote base dir (expands on remote shell)
MIN_NODES=3
MAX_NODES=10

# ---------- privilege check ----------
check_privileges() {
  if [[ $EUID -ne 0 && -z "${SUDO_USER:-}" ]]; then
    echo -e "${RED}[ERROR]${NC} This script requires root privileges for Docker installation on remote nodes"
    echo "Usage: sudo $0"
    exit 1
  fi
}

# ---------- local prerequisites ----------
check_prerequisites() {
  log_info "Checking prerequisites..."
  local required=("ssh" "openssl" "sshpass" "curl")
  for cmd in "${required[@]}"; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      if [[ "$cmd" == "sshpass" ]]; then
        log_error "sshpass is required. Install: sudo apt-get install -y sshpass"
      else
        log_error "Required command '$cmd' not found. Please install it."
      fi
      exit 1
    fi
  done
  log_info "✅ Prerequisites check completed"
}

# ---------- creds ----------
get_ssh_credentials() {
  log_info "SSH Authentication Setup"
  echo ""
  echo "Password will be passed to sshpass via env (not visible in process list)."
  echo ""

  read -p "SSH Username (default: ubuntu): " input_user
  SSH_USER="${input_user:-ubuntu}"

  echo -n "SSH Password for $SSH_USER: "
  read -s SSH_PASSWORD
  echo; echo

  if [[ -z "$SSH_PASSWORD" ]]; then
    log_error "Password cannot be empty"
    exit 1
  fi

  export SSHPASS="$SSH_PASSWORD"
  log_info "Credentials configured for user: $SSH_USER"
}

# ---------- cluster nodes ----------
configure_cluster_nodes() {
  set +e +u
  log_info "Configuring cluster nodes..."
  echo ""

  local total_nodes
  while true; do
    read -r -p "How many nodes? ($MIN_NODES-$MAX_NODES): " total_nodes
    if [[ "$total_nodes" =~ ^[0-9]+$ ]] && [[ $total_nodes -ge $MIN_NODES ]] && [[ $total_nodes -le $MAX_NODES ]]; then
      break
    else
      log_warn "Enter a number between $MIN_NODES and $MAX_NODES"
    fi
  done

  echo ""
  log_info "Configuring $total_nodes nodes..."
  echo "• Enter hostnames or IPs"
  echo "• Optional format: server,domain (default domain = server)"
  echo ""

  CLUSTER_NODES=()
  CLUSTER_URLS=()

  local node_count=0
  while [[ $node_count -lt $total_nodes ]]; do
    local node_input
    read -r -p "Node $((node_count + 1)) server[,domain]: " node_input

    if [[ -z "$node_input" ]]; then
      log_warn "Server cannot be empty."
      continue
    fi

    local server_name="" domain_name=""
    if [[ "$node_input" =~ ^([^,]+),(.+)$ ]]; then
      server_name="${BASH_REMATCH[1]}"
      domain_name="${BASH_REMATCH[2]}"
    else
      server_name="$node_input"
      domain_name="$node_input"
    fi

    server_name="${server_name// /}"
    domain_name="${domain_name// /}"

    if [[ -z "$server_name" ]]; then
      log_error "Parsed empty server; try again."
      continue
    fi

    CLUSTER_NODES+=("$server_name")
    CLUSTER_URLS+=("$domain_name")
    ((node_count++))
    log_info "✅ Added: $server_name (domain: $domain_name)"
    log_debug "Current count: $node_count/$total_nodes"
  done

  echo ""
  log_info "✅ Cluster configuration completed ($node_count nodes)"
  echo ""
  set -euo pipefail
}

# ---------- helpers ----------
is_ip() {
  local ip=$1
  [[ "$ip" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]
}

# ---------- ssh helpers ----------
ssh_exec() {
  local host="$1"; shift
  local command="$1"; shift
  local description="${1:-executing command}"

  log_debug "[$host] $description"
  if ! sshpass -e ssh \
      -o StrictHostKeyChecking=no \
      -o UserKnownHostsFile=/dev/null \
      -o ConnectTimeout=10 \
      "$SSH_USER@$host" "$command"; then
    log_error "Failed on $host: $description"
    return 1
  fi
}

scp_copy() {
  local source="$1" host="$2" destination="$3"
  local description="${4:-copying files}"

  log_debug "[$host] $description"
  sshpass -e ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$SSH_USER@$host" "mkdir -p $destination" >/dev/null 2>&1 || true

  if ! sshpass -e scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -r "$source" "$SSH_USER@$host:$destination"; then
    log_error "Failed to copy to $host: $description"
    return 1
  fi
}

# ---------- connectivity ----------
test_ssh_connectivity() {
  log_info "Testing SSH connectivity..."
  local failed=0
  for i in "${!CLUSTER_NODES[@]}"; do
    local node="${CLUSTER_NODES[$i]}" node_id="node$((i+1))"
    log_debug "Testing $node_id ($node)..."
    if ssh_exec "$node" "echo 'SSH OK'"; then
      log_info "✅ $node_id ($node) SSH OK"
    else
      log_error "❌ $node_id ($node) SSH FAILED"
      ((failed++))
    fi
  done
  if [[ $failed -gt 0 ]]; then
    log_error "SSH failed for $failed node(s)"
    exit 1
  fi
  log_info "✅ All nodes accessible via SSH"
}

# ---------- docker install (remote) ----------
install_docker_on_nodes() {
  log_cluster "Checking/installing Docker on nodes (requires passwordless sudo on remote)..."
  for i in "${!CLUSTER_NODES[@]}"; do
    local node="${CLUSTER_NODES[$i]}" node_id="node$((i+1))"

    if ssh_exec "$node" "command -v docker >/dev/null 2>&1" "checking Docker"; then
      log_debug "✅ Docker present on $node_id"
      continue
    fi

    log_info "Installing Docker on $node_id..."
    local docker_script='#!/bin/bash
if ! sudo -n true 2>/dev/null; then
  echo "ERROR: passwordless sudo required for Docker install"
  exit 1
fi
sudo apt-get update -y && \
sudo apt-get install -y apt-transport-https ca-certificates curl gnupg lsb-release && \
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg && \
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list >/dev/null && \
sudo apt-get update -y && \
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin && \
sudo systemctl enable --now docker && \
sudo usermod -aG docker '"$SSH_USER"'
'

    ssh_exec "$node" "cat > /tmp/install_docker.sh <<'EOS'
$docker_script
EOS
chmod +x /tmp/install_docker.sh && /tmp/install_docker.sh && rm /tmp/install_docker.sh" "installing Docker"
    log_info "✅ Docker installed on $node_id"
  done
  log_cluster "✅ Docker installation completed"
}

# ---------- deployment structure ----------
create_deployment_structure() {
  log_debug "Creating deployment directory structure..."
  rm -rf "$CLUSTER_DIR"
  mkdir -p "$CLUSTER_DIR"
  for i in "${!CLUSTER_NODES[@]}"; do
    local node_id="node$((i+1))"
    mkdir -p "$CLUSTER_DIR/$node_id"/{config,logs/{relay,caddy}}
  done
  log_debug "✅ Deployment structure created"
}

# ---------- per-node config staging ----------
create_node_config() {
  local idx=$1
  local node="${CLUSTER_NODES[$idx]}"
  local node_url="${CLUSTER_URLS[$idx]}"
  local node_id="node$((idx+1))"

  log_debug "Creating configuration for $node_id..."

  # relay config
  cat > "$CLUSTER_DIR/$node_id/config/config.yaml" << EOF
GENERAL: {}

LOGGING:
  LEVEL: info
  FILE:
  FORMAT: json
  MAX_SIZE: 100
  MAX_BACKUPS: 10
  MAX_AGE: 30

METRICS:
  ENABLED: true
  PORT: 8181

RELAY:
  NAME: "$node_url"
  DESCRIPTION: "High-performance, reliable, scalable Nostr relay"
  CONTACT: "support@shugur.com"
  ICON: "https://github.com/Shugur-Network/relay/raw/main/logo.png"
  BANNER: "https://github.com/Shugur-Network/relay/raw/main/banner.png"
  WS_ADDR: ":8080"
  PUBLIC_URL: "wss://$node_url"
  EVENT_CACHE_SIZE: 50000
  SEND_BUFFER_SIZE: 8192
  WRITE_TIMEOUT: 30s
  IDLE_TIMEOUT: 300s
  THROTTLING:
    MAX_CONTENT_LENGTH: 65536
    MAX_CONNECTIONS: 2000
    BAN_THRESHOLD: 5
    BAN_DURATION: 300
    RATE_LIMIT:
      ENABLED: true
      MAX_EVENTS_PER_SECOND: 200
      MAX_REQUESTS_PER_SECOND: 400
      BURST_SIZE: 100
      PROGRESSIVE_BAN: true
      BAN_DURATION: 5m
      MAX_BAN_DURATION: 24h

RELAY_POLICY:
  BLACKLIST:
    PUBKEYS: []
  WHITELIST:
    PUBKEYS: []

CAPSULES:
  ENABLED: true
  MAX_WITNESSES: 9

DATABASE:
  SERVER: "localhost"
  PORT: 5432
EOF

  # Caddyfile with comprehensive security headers
  cat > "$CLUSTER_DIR/$node_id/config/Caddyfile" << EOF
$node_url {
    handle /api/* {
        reverse_proxy localhost:8080
    }
    handle {
        reverse_proxy localhost:8080 {
            header_up Host {host}
            header_up X-Real-IP {remote}
            header_up X-Forwarded-For {remote}
            header_up X-Forwarded-Proto {scheme}
        }
    }
    @internal {
        remote_ip 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 127.0.0.1
    }
    handle_path /metrics {
        handle @internal {
            reverse_proxy localhost:8181
        }
        handle {
            respond "Access Denied" 403
        }
    }
    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        X-Content-Type-Options "nosniff"
        X-Frame-Options "SAMEORIGIN"
        Referrer-Policy "strict-origin-when-cross-origin"
        X-XSS-Protection "1; mode=block"
        X-Cluster-Node "$node_id"
        -Server
        -X-Powered-By
    }
    encode gzip zstd
    log {
        output file /var/log/caddy/access.log {
            roll_size 100mb
            roll_keep 10
        }
        format json
    }
}

:8090 {
    handle /health {
        respond "OK" 200
    }
}
EOF

  # Compose (Relay connects to managed Aurora PostgreSQL)
  cat > "$CLUSTER_DIR/$node_id/config/docker-compose.yml" << EOF
services:
  relay:
    image: ghcr.io/shugur-network/relay:latest
    container_name: relay
    hostname: relay
    environment:
      - SHUGUR_ENV=production
      - SHUGUR_DB_HOST=\${DB_HOST}
      - SHUGUR_DB_PORT=5432
      - SHUGUR_DB_DATABASE=shugur
      - SHUGUR_DB_USER=relay
      - SHUGUR_DB_PASSWORD=\${DB_PASSWORD}
      - SHUGUR_DB_SSL_MODE=require
      - SHUGUR_LOG_LEVEL=info
      - SHUGUR_LOG_FORMAT=json
      - SHUGUR_METRICS_ENABLED=true
      - SHUGUR_WS_PORT=8080
      - SHUGUR_METRICS_PORT=8181
      - SHUGUR_MAX_CONNECTIONS=2000
      - SHUGUR_CLUSTER_NODE=$node_id
    volumes:
      - ./config.yaml:/app/config.yaml:ro
      - ./logs/relay:/app/logs
    healthcheck:
      test: ["CMD-SHELL", "wget -q --spider http://localhost:8080/api/info || curl -fsI http://localhost:8080/api/info"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 30s
    restart: unless-stopped
    network_mode: host

  caddy:
    image: caddy:latest
    container_name: caddy
    hostname: caddy
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config
      - ./logs/caddy:/var/log/caddy
    depends_on:
      - relay
    healthcheck:
      test: ["CMD", "caddy", "validate", "--config", "/etc/caddy/Caddyfile"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s
    restart: unless-stopped
    network_mode: host

volumes:
  caddy_data:
    driver: local
  caddy_config:
    driver: local
EOF

  log_info "✅ Configuration & cert staging created for $node_id"
}

create_all_node_configs() {
  log_cluster "Creating node-specific configurations..."
  create_deployment_structure
  for i in "${!CLUSTER_NODES[@]}"; do
    create_node_config "$i"
  done
  log_cluster "All node configurations created"
}

# ---------- deploy ----------
deploy_files_to_nodes() {
  log_cluster "Deploying files to all nodes..."
  for i in "${!CLUSTER_NODES[@]}"; do
    local node="${CLUSTER_NODES[$i]}" node_id="node$((i+1))"

    log_debug "Deploying to $node_id ($node)..."
    
    # Create directories and ensure proper ownership BEFORE copying files
    ssh_exec "$node" "
      # Clean up any existing directories with wrong ownership
      sudo rm -rf $REMOTE_DIR 2>/dev/null || rm -rf $REMOTE_DIR 2>/dev/null || true
      
      # Create directories as ubuntu user
      mkdir -p $REMOTE_DIR/{logs/{relay,caddy}}
      
      # Ensure ubuntu user owns the entire directory structure
      CURRENT_USER=\$(id -u)
      CURRENT_GROUP=\$(id -g)
      sudo chown -R \$CURRENT_USER:\$CURRENT_GROUP $REMOTE_DIR 2>/dev/null || chown -R \$CURRENT_USER:\$CURRENT_GROUP $REMOTE_DIR 2>/dev/null || true
    " "preparing directories with correct ownership"

    scp_copy "$CLUSTER_DIR/$node_id/config/." "$node" "$REMOTE_DIR/" "copying config"

    log_debug "✅ Files deployed to $node_id"
  done
  log_cluster "✅ Deployment complete"
}

# ---------- start services ----------
start_relay_services() {
  log_cluster "Starting Relay services..."
  for i in "${!CLUSTER_NODES[@]}"; do
    local node="${CLUSTER_NODES[$i]}" node_id="node$((i+1))"
    log_debug "Starting relay on $node_id..."
    ssh_exec "$node" "cd $REMOTE_DIR && docker compose up -d relay" "starting relay"
    sleep 5
  done
  log_cluster "✅ Relay services started"
}

start_caddy_services() {
  log_cluster "Starting Caddy services..."
  for i in "${!CLUSTER_NODES[@]}"; do
    local node="${CLUSTER_NODES[$i]}" node_id="node$((i+1))"
    log_debug "Starting caddy on $node_id..."
    ssh_exec "$node" "cd $REMOTE_DIR && docker compose up -d caddy" "starting caddy"
    sleep 3
  done
  log_cluster "✅ Caddy services started"
}

verify_cluster_health() {
  log_cluster "Verifying cluster health..."
  local healthy=0 total=${#CLUSTER_NODES[@]}

  for i in "${!CLUSTER_NODES[@]}"; do
    local node="${CLUSTER_NODES[$i]}" node_id="node$((i+1))"
    log_debug "Checking $node_id local relay..."
    if sshpass -e ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$SSH_USER@$node" \
         "curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/api/info 2>/dev/null | grep -q '^200$'"; then
      log_debug "✅ $node_id relay healthy"
      ((healthy++))
    else
      log_warn  "❌ $node_id relay health check failed"
    fi

    sshpass -e ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$SSH_USER@$node" \
      "cd $REMOTE_DIR && docker compose ps --format 'table {{.Service}}\t{{.Status}}' | grep -E 'running|healthy'" >/dev/null 2>&1 \
      || log_warn "Some services on $node_id may not be running"
  done

  log_cluster "Health check: $healthy/$total nodes reported healthy"
  if [[ $healthy -eq $total ]]; then
    log_cluster "🎉 All nodes healthy"
  else
    log_warn "Some nodes may need attention - check logs above"
  fi
}

test_external_connectivity() {
  log_cluster "Testing external connectivity..."
  local working=0 total=${#CLUSTER_NODES[@]}

  for i in "${!CLUSTER_NODES[@]}"; do
    local node_url="${CLUSTER_URLS[$i]}" node_id="node$((i+1))"
    log_debug "Testing $node_id ($node_url)..."

    local http_status https_status relay_status
    http_status=$(curl -s -o /dev/null -w "%{http_code}" "http://$node_url/" 2>/dev/null || echo "000")
    https_status=$(curl -s -o /dev/null -w "%{http_code}" "https://$node_url/" 2>/dev/null || echo "000")
    relay_status=$(curl -s -o /dev/null -w "%{http_code}" "https://$node_url/api/info" 2>/dev/null || echo "000")

    [[ "$relay_status" == "200" ]] && { log_debug "✅ relay HTTPS /api/info OK ($relay_status)"; ((working++)); } || log_warn "❌ relay HTTPS /api/info FAILED ($relay_status)"
    [[ "$http_status" =~ ^(301|308)$ ]] && log_debug "✅ HTTP redirect OK ($http_status)" || log_warn "❌ HTTP redirect FAILED ($http_status)"
    [[ "$https_status" == "200" ]] && log_debug "✅ HTTPS OK ($https_status)" || log_warn "❌ HTTPS FAILED ($https_status) - check DNS/firewall"
  done

  log_cluster "External connectivity: $working/$total nodes reachable via HTTPS"
}

# ---------- local cleanup after installation ----------
cleanup_local_directories() {
  log_cluster "Cleaning up local working directories..."
  
  # Remove cluster deployment staging directory
  if [[ -d "$CLUSTER_DIR" ]]; then
    log_debug "Removing local deployment staging directory: $CLUSTER_DIR"
    rm -rf "$CLUSTER_DIR"
    log_debug "✅ Local deployment staging directory removed"
  else
    log_debug "No local deployment directory found to clean"
  fi
  
  log_cluster "✅ Local directories cleaned"
}

# ---------- summary ----------
show_completion_message() {
  echo ""
  echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════════════════════╗${NC}"
  echo -e "${GREEN}║                      🎉 SECURE DISTRIBUTED CLUSTER INSTALLATION COMPLETE! 🎉              ║${NC}"
  echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════════════════════╝${NC}"
  echo ""
  echo -e "${BLUE}🌐 Relay Access Points (per-node Caddy):${NC}"
  for i in "${!CLUSTER_NODES[@]}"; do
    local node_url="${CLUSTER_URLS[$i]}"
    echo -e "   ${GREEN}•${NC} Node $((i+1)) (${CLUSTER_NODES[$i]}):"
    echo -e "     - WebSocket: ${YELLOW}wss://$node_url${NC}"
    echo -e "     - Relay info: ${YELLOW}https://$node_url/api/info${NC}"
  done
  echo ""
  echo -e "${BLUE}🔧 Management & Monitoring:${NC}"
  echo -e "   ${GREEN}•${NC} Database: Connect via Aurora PostgreSQL endpoint"
  echo -e "   ${GREEN}•${NC} Local health (on node): ${YELLOW}curl http://localhost:8080/api/info${NC}"
  echo ""
  echo -e "${BLUE}📋 Next Steps:${NC}"
  echo -e "   ${GREEN}•${NC} Local working directories automatically cleaned after installation"
  echo -e "   ${GREEN}•${NC} To add nodes later: use ./scripts/add-cluster-node.sh"
  echo ""
  echo -e "${YELLOW}💡 Installation complete!${NC}"
  echo ""
}

# ---------- main ----------
main() {
  check_privileges
  log_cluster "Step 1: Checking prerequisites"
  check_prerequisites

  log_cluster "Step 2: SSH Authentication Setup"
  get_ssh_credentials

  log_cluster "Step 3: Configuring Cluster Nodes"
  configure_cluster_nodes

  log_cluster "Step 4: Testing SSH Connectivity"
  test_ssh_connectivity

  log_cluster "Step 5: Installing Docker on Nodes"
  install_docker_on_nodes

  log_cluster "Step 6: Creating Node Configs"
  create_all_node_configs

  log_cluster "Step 7: Deploying Files to Nodes"
  deploy_files_to_nodes

  log_cluster "Step 8: Starting Relay Services"
  start_relay_services

  log_cluster "Step 9: Starting Caddy Services"
  start_caddy_services

  log_cluster "Step 10: Verifying Cluster Health"
  sleep 30  # Increased wait time for services to stabilize
  verify_cluster_health

  log_cluster "Step 11: Testing External Connectivity"
  test_external_connectivity

  log_cluster "Step 12: Cleaning Up Local Directories"
  cleanup_local_directories

  show_completion_message
}

if [[ $# -gt 0 ]]; then
  case $1 in
    --help|-h)
      echo "Shugur Relay Distributed Cluster Installer"
      echo
      echo "Usage: sudo $0 [options]"
      echo
      echo "Options:"
      echo "  --help, -h         Show this help message"
      echo "  --debug            Debug remote logs (requires existing installation)"
      echo
      echo "Installs:"
      echo "  • Per-node Relay + Caddy (HTTPS)"
      echo "  • Connects to managed Aurora PostgreSQL"
      echo
      echo "Requirements:"
      echo "  • Run with sudo/root"
      echo "  • SSH access to all nodes; remote passwordless sudo for Docker install"
      echo "  • Ubuntu/Debian nodes assumed"
      echo "  • Open ports: 22, 8080, 80, 443"
      echo "  • Aurora PostgreSQL endpoint (set DB_HOST and DB_PASSWORD env vars)"
      echo
      exit 0
      ;;
    --debug)
      log_cluster "🔍 Debug mode - checking service status..."
      if [[ ${#CLUSTER_NODES[@]} -gt 0 ]] || [[ -f "./cluster-nodes.conf" ]]; then
        get_ssh_credentials
        if [[ -f "./cluster-nodes.conf" ]]; then
          source "./cluster-nodes.conf"
        fi
        for i in "${!CLUSTER_NODES[@]}"; do
          local node="${CLUSTER_NODES[$i]}" node_id="node$((i+1))"
          log_debug "Checking $node_id ($node)..."
          sshpass -e ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$SSH_USER@$node" \
            "cd $REMOTE_DIR && docker compose ps && docker compose logs --tail=20 relay" 2>/dev/null || log_warn "Failed to check $node_id"
        done
      else
        log_info "No cluster configuration found. Run normal installation first."
      fi
      exit 0
      ;;
    *)
      log_error "Unknown option: $1"
      echo "Use --help for usage."
      exit 1
      ;;
  esac
fi

main
