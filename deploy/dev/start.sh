#!/usr/bin/env bash
# deploy/dev/start.sh
#
# Start and stop the Asty dev environment.
#
# Usage:
#   ./start.sh             — 1 node (server + agent)
#   ./start.sh 3           — 3 nodes (server + agent each, with leader election)
#   ./start.sh addnode     — grow a running cluster by one node
#   ./start.sh removenode [N] — shrink: tear down node N (default: highest)
#   ./start.sh stop        — stop everything

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
# Per-node PID file: $DATA_BASE/pids-$i, two lines (server, agent).
# Per-node so removenode can target a specific node's PIDs without
# scanning ps; stop_all iterates the whole set.
PID_FILE_TMPL="$DATA_BASE/pids"
# Shared peer-list file consumed by every agent (A_NATS_PEERS_FILE).
# Imitates a prod DNS A-record: one IP per line, agents re-read on
# every watcher tick, self-filter in code. addnode/removenode update
# this file; live agents pick up the change on their next tick.
PEERS_FILE="$DATA_BASE/peers.txt"

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
# Infrastructure (PostgreSQL only — NATS is supervised by each Asty agent)
# =============================================================================
start_infra() {
  log "starting infrastructure: PostgreSQL..."
  docker compose -f "$COMPOSE_FILE" down --remove-orphans --volumes 2>/dev/null || true
  docker compose -f "$COMPOSE_FILE" up -d postgres
  info "PostgreSQL up"
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

  # Fetch the nats-server binary the agent supervises at startup.
  # Makefile target is a no-op when the pinned version is already on disk.
  make -C "$ROOT_DIR" nats-server >/dev/null
  info "✓ nats-server"
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
    ensure_loopback_alias "$i"
  done
}

# ensure_loopback_alias adds 127.0.0.$i to lo0 if it's not already
# bound. Idempotent and verifies the result with ifconfig so we never
# proceed to a process that will then fail with "can't assign requested
# address". macOS only — on Linux 127.0.0.0/8 is always bindable.
ensure_loopback_alias() {
  local i=$1
  [[ "$(uname -s)" == "Darwin" ]] || return 0
  local addr="127.0.0.$i"
  if ifconfig lo0 2>/dev/null | grep -qE "inet $addr "; then
    return 0
  fi
  sudo ifconfig lo0 alias "$addr" up
  if ! ifconfig lo0 2>/dev/null | grep -qE "inet $addr "; then
    die "failed to create loopback alias $addr (sudo ifconfig lo0 alias $addr)"
  fi
  info "alias $addr"
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
load_vars() {
  # Loads dev.vars and exports every K=V into the shell. Must run
  # before start_infra so docker-compose's ${A_MEMORY_TOTAL} substitution
  # sees the value.
  while IFS='=' read -r key value; do
    [[ -z "$key" || "$key" =~ ^[[:space:]]*# ]] && continue
    key=$(echo "$key" | xargs)
    value=$(echo "$value" | xargs)
    export "$key=$value"
  done < <(grep -v '^\s*#' "$VARS_FILE" | grep -v '^\s*$')
}

start_asty() {
  local nodes=$1
  log "starting Asty: $nodes nodes (server + agent on each)..."

  # Seed the shared peers file with every node's IP. Agents self-filter
  # in code, so a single file shared by all of them is enough.
  mkdir -p "$DATA_BASE"
  : > "$PEERS_FILE"
  for ((i=1; i<=nodes; i++)); do
    echo "127.0.0.$i" >> "$PEERS_FILE"
  done

  for ((i=1; i<=nodes; i++)); do
    start_node "$i"
    # Pause so JetStream confirms the KV bucket before the next agent
    # races to (re)create it — without it: "nats: no response from stream".
    sleep 0.5
  done
}

# start_node brings up one server + agent pair with the per-node env
# (NODE_ID/IP/UI/gateway address, fake disk type). Shared by start_asty
# (initial fan-out) and add_node (live cluster growth). The agent reads
# the peer list from PEERS_FILE — see watchNATSPeers in the asty agent
# for how live changes propagate.
start_node() {
  local i=$1
  local addr="127.0.0.$i"
  local server_log="/tmp/asty-dev-server-$i.log"
  local agent_log="/tmp/asty-dev-agent-$i.log"
  local config_file="$SCRIPT_DIR/config.asty"
  mkdir -p "$DATA_BASE/node$i"
  # i=1 binds to 127.0.0.1, no alias needed. For i≥2 verify the alias
  # is up; macOS aliases are ephemeral and prior stop/sleep cycles may
  # have left things half-configured.
  if (( i >= 2 )); then
    ensure_loopback_alias "$i"
  fi

  # Per-node loopback binds for gateway + orchestrator HTTP.
  local gw_addr="$addr:80"
  local http_addr="$addr:8080"

  # Random disk type per node so the cluster aggregates exercise
  # both ssd and hdd branches. Server inherits no disk-type env —
  # only the agent reports physical hardware.
  local disk_type
  if (( RANDOM % 2 == 0 )); then disk_type="ssd"; else disk_type="hdd"; fi

  A_NODE_ID="dev-node-$i" A_NODE_IP="$addr" \
    A_HTTP_ADDR="$http_addr" A_WORK_DIR="$DATA_BASE/work" \
    "$BIN_DIR/asty" -mode server -config "$config_file" >> "$server_log" 2>&1 &
  local server_pid=$!

  sudo -E A_NODE_ID="dev-node-$i" A_NODE_IP="$addr" \
    A_NATS_PEERS_FILE="$PEERS_FILE" \
    A_WORK_DIR="$DATA_BASE/work" \
    A_GATEWAY_ADDR="$gw_addr" \
    A_DISK_TYPE="$disk_type" \
    "$BIN_DIR/asty" -mode agent -config "$config_file" >> "$agent_log" 2>&1 &
  local agent_pid=$!

  printf '%s\n%s\n' "$server_pid" "$agent_pid" > "${PID_FILE_TMPL}-$i"

  info "Node $i: id=dev-node-$i | ip=$addr | disk=${A_DISK_TOTAL}M ${disk_type} | swap=${A_SWAP_TOTAL}M | server PID=$server_pid | agent PID=$agent_pid"
  info "  logs: $server_log | $agent_log"
}

# add_node grows a running cluster by one. Picks the next free index,
# brings up its loopback alias, appends its IP to PEERS_FILE, then
# starts the new node. Existing agents notice the file change on their
# next watcher tick (~5 s), pererender nats.conf with the new peer, and
# restart their nats-server child. Reaching steady-state takes the
# rendered-conf change + JetStream meta-leader re-election (~10–15 s).
add_node() {
  if ! compgen -G "${PID_FILE_TMPL}-*" > /dev/null || [[ ! -f "$PEERS_FILE" ]]; then
    die "no running cluster found (no ${PID_FILE_TMPL}-* / $PEERS_FILE). Start one with: $0 [N]"
  fi

  # Next free index = max existing + 1. Peers file is the source of
  # truth (PID file holds 2 lines per node so it would double-count).
  local max_i=0
  while IFS= read -r ip; do
    local last="${ip##*.}"
    [[ -n "$last" && "$last" =~ ^[0-9]+$ && $last -gt $max_i ]] && max_i=$last
  done < "$PEERS_FILE"
  local i=$((max_i + 1))
  local addr="127.0.0.$i"

  log "adding node $i (id=dev-node-$i, ip=$addr)..."

  echo "$addr" >> "$PEERS_FILE"
  start_node "$i"

  info "node $i started. Existing agents will pick up the new peer on"
  info "the next nats-watch tick (~5 s) and restart their nats-server."
}

# remove_node shrinks a running cluster by tearing down one node and
# removing its entry from PEERS_FILE. Without args, removes the
# highest-numbered node (symmetric with add_node). With an explicit
# index, removes that one. Refuses to take the last node down —
# stop_all is the right tool for that.
remove_node() {
  local target="${1:-}"

  if ! compgen -G "${PID_FILE_TMPL}-*" > /dev/null || [[ ! -f "$PEERS_FILE" ]]; then
    die "no running cluster found. Start one with: $0 [N]"
  fi

  # Resolve target: explicit index wins, otherwise highest in PEERS_FILE.
  if [[ -z "$target" ]]; then
    target=0
    while IFS= read -r ip; do
      local last="${ip##*.}"
      [[ -n "$last" && "$last" =~ ^[0-9]+$ && $last -gt $target ]] && target=$last
    done < "$PEERS_FILE"
  fi
  if ! [[ "$target" =~ ^[0-9]+$ ]] || [[ $target -lt 1 ]]; then
    die "usage: $0 removenode [N]  (N ≥ 1)"
  fi

  local pidfile="${PID_FILE_TMPL}-$target"
  if [[ ! -f "$pidfile" ]]; then
    die "node $target is not running ($pidfile missing)"
  fi
  local remaining
  remaining=$(compgen -G "${PID_FILE_TMPL}-*" | wc -l | tr -d ' ')
  if [[ "$remaining" -le 1 ]]; then
    die "refusing to remove the last running node — use '$0 stop' instead"
  fi

  local addr="127.0.0.$target"
  log "removing node $target (id=dev-node-$target, ip=$addr)..."

  # 1) SIGTERM the node's server + agent (sudo — agent runs as root).
  local pid
  while IFS= read -r pid; do
    [[ -n "$pid" ]] || continue
    sudo kill "$pid" 2>/dev/null && info "✓ PID $pid terminated" || true
  done < "$pidfile"
  sleep 1
  while IFS= read -r pid; do
    [[ -n "$pid" ]] || continue
    sudo kill -0 "$pid" 2>/dev/null && sudo kill -9 "$pid" 2>/dev/null && info "✓ PID $pid (SIGKILL)" || true
  done < "$pidfile"

  # 2) Drop the IP from PEERS_FILE so surviving agents notice on their
  #    next watcher tick and shrink their cluster.routes (SIGHUP hot
  #    reload when 2+ nodes remain, cold restart on the 2→1 step).
  local tmp
  tmp=$(mktemp "${PEERS_FILE}.XXXXXX")
  grep -v "^${addr}$" "$PEERS_FILE" > "$tmp" || true
  mv "$tmp" "$PEERS_FILE"

  # 3) Clean per-node state — pidfile, working dir, JS store. Old
  #    JetStream data would conflict with a future addnode reusing
  #    the same index.
  rm -f "$pidfile"
  sudo rm -rf "$DATA_BASE/work/dev-node-$target" "$DATA_BASE/jetstream/dev-node-$target" "$DATA_BASE/node$target"

  info "node $target removed. Loopback alias $addr left up; stop_all"
  info "tears down aliases at end-of-life."
}

# =============================================================================
# Wait for Asty readiness
# =============================================================================
wait_asty() {
  log "waiting for Asty readiness..."
  # Simple check: are the processes alive?
  sleep 2

  local pidfile pid
  for pidfile in "${PID_FILE_TMPL}-"*; do
    [[ -f "$pidfile" ]] || continue
    while IFS= read -r pid; do
      [[ -n "$pid" ]] || continue
      if ! kill -0 "$pid" 2>/dev/null; then
        die "Asty process (PID=$pid, from $pidfile) died. Check logs in /tmp/asty-dev-*.log"
      fi
    done < "$pidfile"
  done

  info "Asty up"
}

# =============================================================================
# Cleanup orphan processes (our binaries, identified by exact path)
# =============================================================================
cleanup_orphans() {
  local killed=0
  sudo pkill -9 -f "$BIN_DIR/asty" 2>/dev/null && killed=1 || true
  sudo pkill -9 -f "$BIN_DIR/nats-server" 2>/dev/null && killed=1 || true
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
  info "Asty UI:    http://127.0.0.1:8080 (node 1)"
  info "Gateway:    http://127.0.0.1:80 (node 1)"
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

  # Asty processes by per-node PID files (sudo — agents run as root).
  local pidfile pid
  for pidfile in "${PID_FILE_TMPL}-"*; do
    [[ -f "$pidfile" ]] || continue
    while IFS= read -r pid; do
      [[ -n "$pid" ]] || continue
      sudo kill "$pid" 2>/dev/null && info "✓ PID $pid terminated" || true
    done < "$pidfile"
  done
  sleep 1
  for pidfile in "${PID_FILE_TMPL}-"*; do
    [[ -f "$pidfile" ]] || continue
    while IFS= read -r pid; do
      [[ -n "$pid" ]] || continue
      sudo kill -0 "$pid" 2>/dev/null && sudo kill -9 "$pid" 2>/dev/null && info "✓ PID $pid (SIGKILL)" || true
    done < "$pidfile"
  done

  # Orphan processes (our binaries only).
  cleanup_orphans

  # Docker infrastructure.
  stop_infra

  # Temporary data (sudo — agents create files as root). pidfiles
  # live under $DATA_BASE so they go with it.
  sudo rm -rf "$DATA_BASE"
  rm -f /tmp/asty-dev-*.log 2>/dev/null || true

  # Loopback aliases (macOS).
  teardown_loopback_aliases

  log "✓ stopped"
}

# =============================================================================
# Entry point
# =============================================================================
CMD="${1:-1}"

# load_vars first — every docker compose call (incl. stop_infra's
# `down`) needs ${A_MEMORY_TOTAL}/${A_DOCKER_CPUS} substitution.
load_vars

if [[ "$CMD" == "stop" ]]; then
  stop_all
  exit 0
fi

if [[ "$CMD" == "addnode" ]]; then
  check_deps
  add_node
  exit 0
fi

if [[ "$CMD" == "removenode" ]]; then
  remove_node "${2:-}"
  exit 0
fi

if ! [[ "$CMD" =~ ^[0-9]+$ ]] || [[ "$CMD" -lt 1 ]]; then
  die "usage: $0 [NODES|stop|addnode|removenode [N]]  (NODES ≥ 1, default 1)"
fi

NODES="$CMD"

check_deps
cleanup_orphans

if compgen -G "${PID_FILE_TMPL}-*" > /dev/null; then
  warn "found a running environment. Stopping it before relaunch..."
  stop_all
fi

start_infra
build_binaries

setup_loopback_aliases "$NODES"

start_asty "$NODES"
wait_asty

print_status
