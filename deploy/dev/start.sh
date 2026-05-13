#!/usr/bin/env bash
# deploy/dev/start.sh
#
# Start and stop the Asty dev environment.
#
# Usage:
#   ./start.sh          — 1 node (server + agent)
#   ./start.sh 3        — 3 nodes (server + agent each, with leader election)
#   ./start.sh stop     — stop everything

set -euo pipefail

# =============================================================================
# Paths
# =============================================================================
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"
VARS_FILE="$SCRIPT_DIR/dev.vars"
BIN_DIR="$ROOT_DIR/bin"
DATA_BASE="/tmp/asty-dev"
PID_FILE="/tmp/asty-dev-pids"
NATS_CONF_RENDERED="/tmp/asty-dev-nats.conf"

# =============================================================================
# Output helpers
# =============================================================================
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; CYAN='\033[0;36m'; NC='\033[0m'
log()  { echo -e "${GREEN}▶${NC} $*"; }
info() { echo -e "${CYAN}  $*${NC}"; }
warn() { echo -e "${YELLOW}⚠${NC} $*"; }
die()  { echo -e "${RED}✗ $*${NC}" >&2; exit 1; }

# =============================================================================
# Dependency check
# =============================================================================
check_deps() {
  local missing=()
  for cmd in docker go; do
    command -v "$cmd" &>/dev/null || missing+=("$cmd")
  done
  [[ ${#missing[@]} -eq 0 ]] || die "missing dependencies: ${missing[*]}. Install them and retry."
}

# =============================================================================
# Render NATS config
# =============================================================================
render_nats_conf() {
  local nodes=$1

  cat > "$NATS_CONF_RENDERED" <<'CONF_HEAD'
port: 4222
http_port: 8222

jetstream {
  store_dir: "/data/jetstream"
  max_mem:   256M
  max_file:  10G
}
CONF_HEAD

  if [[ $nodes -gt 1 ]]; then
    cat >> "$NATS_CONF_RENDERED" <<'CONF_CLUSTER'

cluster {
  name:   "asty-dev"
  port:   6222
  routes: ["nats-route://nats:6222"]
}
CONF_CLUSTER
  fi
}

# =============================================================================
# Infrastructure (NATS + PostgreSQL)
# =============================================================================
start_infra() {
  local nodes=$1
  NATS_NODES=$nodes

  log "starting infrastructure: $NATS_NODES NATS nodes + PostgreSQL..."
  render_nats_conf "$NATS_NODES"
  export DEV_NATS_CONF="$NATS_CONF_RENDERED"

  if [[ $NATS_NODES -gt 1 ]]; then
    export NATS_CLIENT_PORTS="4222-4322:4222"
    export NATS_HTTP_PORTS="8222-8322:8222"
  else
    unset NATS_CLIENT_PORTS NATS_HTTP_PORTS
  fi

  docker compose -f "$COMPOSE_FILE" down --remove-orphans --volumes 2>/dev/null || true

  # Start 1 node first — it immediately becomes the leader.
  docker compose -f "$COMPOSE_FILE" up -d --scale nats=1
  info "NATS: 1 node started, waiting for readiness..."
  local port
  port=$(docker port dev-nats-1 8222/tcp 2>/dev/null | awk -F: '{print $NF; exit}')
  local elapsed=0
  until curl -s "http://127.0.0.1:$port/healthz?js-server-only=true" 2>/dev/null | grep -q '"ok"'; do
    sleep 1
    elapsed=$((elapsed + 1))
    [[ $elapsed -lt 30 ]] || die "NATS not ready after 30s"
  done
  info "NATS ready (${elapsed}s)"

  # Add the remaining nodes — they join the existing leader.
  if [[ $NATS_NODES -gt 1 ]]; then
    docker compose -f "$COMPOSE_FILE" up -d --scale nats="$NATS_NODES"
    info "NATS: scaled to $NATS_NODES nodes"
  fi

  info "NATS: $NATS_NODES nodes | monitoring: http://localhost:8222"
}

# =============================================================================
# Wait for NATS (JetStream) readiness
# =============================================================================
wait_nats() {
  local nodes=$1
  log "waiting for NATS JetStream readiness..."
  local max_wait=60
  local elapsed=0

  if [[ $nodes -gt 1 ]]; then
    until docker compose -f "$COMPOSE_FILE" exec -T nats \
          wget -q -O - --timeout=2 "http://localhost:8222/jsz" 2>/dev/null \
          | grep -Eq '"leader":[[:space:]]*"[^"]'; do
      sleep 1
      elapsed=$((elapsed + 1))
      [[ $elapsed -lt $max_wait ]] || die "NATS meta-leader not elected after ${max_wait}s"
    done
  fi

  until docker compose -f "$COMPOSE_FILE" exec -T --index 1 nats \
        wget -q -O /dev/null --timeout=2 "http://localhost:8222/healthz?js-server-only=true" \
        &>/dev/null; do
    sleep 1
    elapsed=$((elapsed + 1))
    [[ $elapsed -lt $max_wait ]] || die "NATS dev-nats-1 not ready after ${max_wait}s"
  done

  info "NATS ready (${elapsed}s)"
}

stop_infra() {
  log "stopping infrastructure..."
  docker compose -f "$COMPOSE_FILE" down --remove-orphans --volumes
}

# =============================================================================
# Build binaries
# =============================================================================
build_binaries() {
  log "building binaries → $BIN_DIR ..."
  mkdir -p "$BIN_DIR"
  cd "$ROOT_DIR"

  go build -o "$BIN_DIR/asty" ./asty/cmd
  info "✓ asty"

  for svc in xauth xhttp xws; do
    go build -o "$BIN_DIR/$svc" "./demo/cmd/$svc"
    info "✓ $svc"
  done
}

# =============================================================================
# Loopback aliases for multi-node cluster (macOS only)
# =============================================================================
setup_loopback_aliases() {
  local nodes=$1
  [[ "$(uname -s)" == "Darwin" ]] || return 0
  [[ $nodes -gt 1 ]] || return 0

  log "setting up loopback aliases 127.0.0.2..127.0.0.$nodes (requires sudo)..."
  for ((i=2; i<=nodes; i++)); do
    sudo ifconfig lo0 -alias "127.0.0.$i" 2>/dev/null || true
    sudo ifconfig lo0 alias "127.0.0.$i" up
    info "alias 127.0.0.$i"
  done
}

teardown_loopback_aliases() {
  [[ "$(uname -s)" == "Darwin" ]] || return 0
  local aliases
  aliases=$(ifconfig lo0 2>/dev/null | awk '/inet 127\.0\.0\.[0-9]+ / && $2!="127.0.0.1" {print $2}')
  [[ -n "$aliases" ]] || return 0
  log "removing loopback aliases (requires sudo)..."
  for addr in $aliases; do
    sudo ifconfig lo0 -alias "$addr" 2>/dev/null && info "removed $addr" || true
  done
}

# =============================================================================
# Asty: N nodes (each runs server + agent)
# =============================================================================
start_asty() {
  local nodes=$1
  log "starting Asty: $nodes nodes (server + agent on each)..."

  # Load dev.vars and export every variable.
  while IFS='=' read -r key value; do
    # Skip comments and empty lines
    [[ -z "$key" || "$key" =~ ^[[:space:]]*# ]] && continue
    # Strip leading/trailing whitespace
    key=$(echo "$key" | xargs)
    value=$(echo "$value" | xargs)
    # Export variable
    export "$key=$value"
  done < <(grep -v '^\s*#' "$VARS_FILE" | grep -v '^\s*$')

  # All Asty nodes connect to a single NATS endpoint — the host port of
  # dev-nats-1. With --scale nats>1, OrbStack assigns non-sequential host
  # ports in the 4222-4322 range (4228, 4229, ...), so a base+i-1 formula
  # breaks. The NATS cluster replicates JetStream itself — one entry
  # point is enough.
  local nats_host_port
  nats_host_port=$(docker port dev-nats-1 4222/tcp 2>/dev/null | awk -F: '/0\.0\.0\.0:/ {print $NF; exit}')
  [[ -n "$nats_host_port" ]] || die "failed to detect NATS host port"
  info "NATS host port: $nats_host_port"

  # Asty reads $SCRIPT_DIR/config.asty via -config; we only export the
  # per-node and secret bits that have to be runtime-different per node
  # (NodeID/IP/Ports) or kept out of the checked-in YAML (secrets).
  local config_file="$SCRIPT_DIR/config.asty"

  # sudo (agent) needs A_CPU_TOTAL/A_MEMORY_TOTAL from the parent
  # environment; sudo -E preserves them.
  export A_CPU_TOTAL="${A_CPU_TOTAL:-2200}"
  export A_MEMORY_TOTAL="${A_MEMORY_TOTAL:-466}"

  # Each node runs server + agent — both attach to the same NATS endpoint.
  # sudo is required to bind port 80 (the gateway).
  for ((i=1; i<=nodes; i++)); do
    local addr="127.0.0.$i"
    local server_log="/tmp/asty-dev-server-$i.log"
    local agent_log="/tmp/asty-dev-agent-$i.log"
    local ui_port=$((4747 + i - 1))
    mkdir -p "$DATA_BASE/node$i"

    # Per-node loopback binds for gateway so N agents on one host
    # don't collide on a shared port. Gateway HTTP serves traffic, the
    # metrics endpoint is internal — both bind 127.0.0.$i.
    local gw_addr="$addr:80"
    local gw_metrics="$addr:8081"

    A_NODE_ID="dev-node-$i" A_NODE_IP="$addr" A_NATS_PORT="$nats_host_port" \
      A_UI_ADDR="$addr:$ui_port" A_WORK_DIR="$DATA_BASE/work" \
      "$BIN_DIR/asty" -mode server -config "$config_file" >> "$server_log" 2>&1 &
    local server_pid=$!
    echo "$server_pid" >> "$PID_FILE"

    sudo -E A_NODE_ID="dev-node-$i" A_NODE_IP="$addr" A_NATS_PORT="$nats_host_port" \
      A_WORK_DIR="$DATA_BASE/work" \
      A_HTTP_ADDR="$gw_addr" A_GATEWAY_METRICS_ADDR="$gw_metrics" \
      "$BIN_DIR/asty" -mode agent -config "$config_file" >> "$agent_log" 2>&1 &
    local agent_pid=$!
    echo "$agent_pid" >> "$PID_FILE"

    info "Node $i: id=dev-node-$i | ip=$addr | nats=:$nats_host_port | server PID=$server_pid | agent PID=$agent_pid"
    info "  logs: $server_log | $agent_log"
    # Pause so JetStream confirms the KV bucket before the next agent
    # races to (re)create it — without it: "nats: no response from stream".
    sleep 0.5
  done
}

# =============================================================================
# Wait for Asty readiness
# =============================================================================
wait_asty() {
  log "waiting for Asty readiness..."
  local max_wait=30
  local elapsed=0

  # Simple check: are the processes alive?
  sleep 2

  while IFS= read -r pid; do
    if ! kill -0 "$pid" 2>/dev/null; then
      die "Asty process (PID=$pid) died. Check logs in /tmp/asty-dev-*.log"
    fi
  done < "$PID_FILE"

  info "Asty up"
}

# =============================================================================
# Cleanup orphan processes (our binaries, identified by exact path)
# =============================================================================
cleanup_orphans() {
  local killed=0
  sudo pkill -9 -f "$BIN_DIR/asty" 2>/dev/null && killed=1 || true
  sudo pkill -9 -f "$DATA_BASE/work/" 2>/dev/null && killed=1 || true
  for svc in gateway xauth xhttp xws; do
    sudo pkill -9 -f "$BIN_DIR/$svc" 2>/dev/null && killed=1 || true
  done
  [[ $killed -eq 1 ]] && info "✓ orphan processes removed" || true
}

# =============================================================================
# Status
# =============================================================================
print_status() {
  echo ""
  echo -e "${GREEN}═══════════════════════════════════════${NC}"
  echo -e "${GREEN}  Asty dev environment is up${NC}"
  echo -e "${GREEN}═══════════════════════════════════════${NC}"
  echo ""
  info "Asty UI:    http://localhost:4747 (node 1)"
  info "Gateway:    http://127.0.0.1:80 (node 1)"
  info "NATS:       http://localhost:8222"
  info "PostgreSQL: localhost:5432"
  info ""
  info "server-1 log: tail -f /tmp/asty-dev-server-1.log"
  info "agent-1 log:  tail -f /tmp/asty-dev-agent-1.log"
  echo ""
  info "Stop with: $SCRIPT_DIR/start.sh stop"
}

# =============================================================================
# Stop
# =============================================================================
stop_all() {
  log "stopping Asty..."

  # Asty processes by PID file (sudo — agents run as root).
  if [[ -f "$PID_FILE" ]]; then
    while IFS= read -r pid; do
      sudo kill "$pid" 2>/dev/null && info "✓ PID $pid terminated" || true
    done < "$PID_FILE"
    sleep 1
    while IFS= read -r pid; do
      sudo kill -0 "$pid" 2>/dev/null && sudo kill -9 "$pid" 2>/dev/null && info "✓ PID $pid (SIGKILL)" || true
    done < "$PID_FILE"
    rm -f "$PID_FILE"
  fi

  # Orphan processes (our binaries only).
  cleanup_orphans

  # Docker infrastructure.
  stop_infra

  # Temporary data (sudo — agents create files as root).
  sudo rm -rf "$DATA_BASE"
  rm -f "$NATS_CONF_RENDERED"
  rm -f /tmp/asty-dev-*.log 2>/dev/null || true

  # Loopback aliases (macOS).
  teardown_loopback_aliases

  log "✓ stopped"
}

# =============================================================================
# Entry point
# =============================================================================
CMD="${1:-1}"

if [[ "$CMD" == "stop" ]]; then
  stop_all
  exit 0
fi

if ! [[ "$CMD" =~ ^[0-9]+$ ]] || [[ "$CMD" -lt 1 ]]; then
  die "usage: $0 [NODES|stop]  (NODES ≥ 1, default 1)"
fi

NODES="$CMD"

check_deps
cleanup_orphans

if [[ -f "$PID_FILE" ]]; then
  warn "found a running environment. Stopping it before relaunch..."
  stop_all
fi

start_infra "$NODES"
build_binaries

setup_loopback_aliases "$NODES"
wait_nats "$NATS_NODES"

start_asty "$NODES"
wait_asty

print_status
