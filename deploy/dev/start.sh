#!/usr/bin/env bash
# deploy/dev/start.sh
#
# Start and stop the Asty dev environment.
#
# Usage:
#   ./start.sh             — 1 node (server + agent)
#   ./start.sh 3           — 3 nodes (server + agent each, with leader election)
#   ./start.sh add         — grow a running cluster by one node
#   ./start.sh remove [N]  — shrink: tear down node N (default: highest-numbered)
#   ./start.sh stop        — stop everything

set -euo pipefail

# =============================================================================
# Paths
# =============================================================================
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"
ENV_FILE="$SCRIPT_DIR/.env"
BIN_DIR="$ROOT_DIR/bin"
DATA_BASE="/tmp/asty-dev"
# Per-node PID file: $DATA_BASE/pids-$i, two lines (server, agent).
# Per-node so remove can target a specific node's PIDs without scanning
# ps, and each add/remove leaves a self-contained record; stop_all
# iterates the whole set.
PID_FILE_TMPL="$DATA_BASE/pids"
# /etc/hosts name that is BOTH the dashboard SPA target (VITE_ASTY_ORIGIN
# = http://asty.test:7060) and the cluster domain agents resolve for
# NATS peer discovery (LookupIP — the same code path as prod). sync_hosts
# maps it to 127.0.0.<i> for every live node (one A-record each), so the
# browser and the agents fail over across nodes at the DNS layer. The
# node set comes from the per-node pidfiles, so no separate peer file is
# kept. .test is RFC 6761 reserved and (unlike .dev) not HSTS-forced to
# HTTPS.
HOSTS_NAME="asty.test"
HOSTS_BEGIN="# >>> asty-dev (asty.test) >>>"
HOSTS_END="# <<< asty-dev <<<"

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
# /etc/hosts — asty.test name for the dashboard SPA (both macOS and Linux)
# =============================================================================
# node_indices prints the index of every live node (one per line) from
# the per-node pidfiles — the authoritative record of which nodes exist
# (start_node writes pids-$i, remove deletes it). Index $i maps to IP
# 127.0.0.$i.
node_indices() {
  local pf i
  for pf in "${PID_FILE_TMPL}-"*; do
    [[ -f "$pf" ]] || continue
    i="${pf##*-}"
    [[ "$i" =~ ^[0-9]+$ ]] && echo "$i"
  done
}

# sync_hosts <index...> rewrites the asty.test block in /etc/hosts to
# "127.0.0.<index> asty.test" for each given node. The resolver returns
# all addresses for the name, so agents (LookupIP) and the browser fail
# over across nodes. Built from explicit indices, not pidfiles, because
# at initial start the pidfiles don't exist yet and agents must resolve
# the full set on first boot. Requires sudo.
sync_hosts() {
  local block tmp i count=0
  block="$HOSTS_BEGIN"$'\n'
  for i in "$@"; do
    block+="127.0.0.$i $HOSTS_NAME"$'\n'
    count=$((count + 1))
  done
  block+="$HOSTS_END"
  # Strip any prior block (read needs no sudo — /etc/hosts is world-
  # readable), append the fresh one, swap the file back with sudo.
  tmp=$(mktemp)
  sed "/^# >>> asty-dev/,/^# <<< asty-dev/d" /etc/hosts > "$tmp"
  printf '%s\n' "$block" >> "$tmp"
  sudo cp "$tmp" /etc/hosts
  rm -f "$tmp"
  info "$HOSTS_NAME → $count node(s) in /etc/hosts"
}

# teardown_hosts removes the asty.test block from /etc/hosts.
teardown_hosts() {
  grep -q "^# >>> asty-dev" /etc/hosts 2>/dev/null || return 0
  log "removing $HOSTS_NAME from /etc/hosts (requires sudo)..."
  local tmp
  tmp=$(mktemp)
  sed "/^# >>> asty-dev/,/^# <<< asty-dev/d" /etc/hosts > "$tmp"
  sudo cp "$tmp" /etc/hosts
  rm -f "$tmp"
}

# =============================================================================
# Orphan sweep
# =============================================================================
# sweep_orphan_nodes drops pidfiles whose agent PID is gone — dashboard
# kill, crash, or external SIGTERM all leave the file behind otherwise.
# It also picks up the orphaned server process from the same pidfile
# (servers don't die on their own when the agent does). Removing the
# pidfile drops the node from node_indices; the caller (add/remove)
# rebuilds /etc/hosts via sync_hosts so surviving nats-servers stop
# routing to the dead peer. Called at the top of add_node and
# remove_node so subsequent counts/index-picks see ground truth.
#
# Why `sudo kill -0`: the agent runs under sudo, so its PID is owned by
# root; an unprivileged `kill -0` from this script's user gets EPERM
# and falsely reports the process dead. A single `sudo kill -0` works
# for both root-owned (agent) and user-owned (server) PIDs.
sweep_orphan_nodes() {
  local pf server_pid agent_pid
  for pf in "${PID_FILE_TMPL}-"*; do
    [[ -f "$pf" ]] || continue
    server_pid=$(sed -n '1p' "$pf" 2>/dev/null)
    agent_pid=$(sed -n '2p' "$pf" 2>/dev/null)
    # Agent alive — real node, leave it.
    if [[ -n "$agent_pid" ]] && sudo kill -0 "$agent_pid" 2>/dev/null; then
      continue
    fi
    info "sweeping stale node (pidfile $pf: agent gone)"
    if [[ -n "$server_pid" ]] && sudo kill -0 "$server_pid" 2>/dev/null; then
      sudo kill "$server_pid" 2>/dev/null && info "  ✓ killed orphan server PID $server_pid"
    fi
    rm -f "$pf"
  done
}

# =============================================================================
# Asty: N nodes (each runs server + agent)
# =============================================================================
load_vars() {
  # Loads .env and exports every K=V into the shell. Must run before
  # start_infra so docker-compose's ${A_MEMORY_TOTAL} substitution
  # sees the value.
  while IFS='=' read -r key value; do
    [[ -z "$key" || "$key" =~ ^[[:space:]]*# ]] && continue
    key=$(echo "$key" | xargs)
    value=$(echo "$value" | xargs)
    export "$key=$value"
  done < <(grep -v '^\s*#' "$ENV_FILE" | grep -v '^\s*$')
}

start_asty() {
  local nodes=$1
  log "starting Asty: $nodes nodes (server + agent on each)..."

  # Publish asty.test → all node IPs in /etc/hosts BEFORE the agents
  # boot, so each resolves the full peer set on its first bootstrap and
  # comes up clustered (not standalone-then-cold-restart). Agents
  # self-filter their own IP.
  mkdir -p "$DATA_BASE"
  sync_hosts $(seq 1 "$nodes")

  for ((i=1; i<=nodes; i++)); do
    start_node "$i"
    # Pause so JetStream confirms the KV bucket before the next agent
    # races to (re)create it — without it: "nats: no response from stream".
    sleep 0.5
  done
}

# start_node brings up one server + agent pair with the per-node env
# (NODE_ID/IP/UI/gateway address, fake disk type). Shared by start_asty
# (initial fan-out) and add_node (live cluster growth). The agent
# resolves peers via DNS (asty.test in /etc/hosts) — see watchNATSPeers
# in the asty agent for how live changes propagate.
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

  # Per-node loopback binds. Dashboard + Prometheus share :7060
  # (default), gateway lives on :80. Each node uses its own loopback
  # alias 127.0.0.$i so multiple agents can coexist on the same dev
  # box without port collisions.
  local dashboard_host="$addr"
  local gateway_host="$addr"

  # Random disk type per node so the cluster aggregates exercise
  # both ssd and hdd branches. Server inherits no disk-type env —
  # only the agent reports physical hardware.
  local disk_type
  if (( RANDOM % 2 == 0 )); then disk_type="ssd"; else disk_type="hdd"; fi

  # Server runs WITHOUT sudo — no privileged ports, drop-root is a
  # no-op (resolveDropTarget sees euid != 0). Dashboard listens on
  # the per-node loopback alias so multiple servers don't race for
  # the same socket.
  A_NODE_ID="dev-node-$i" A_NODE_IP="$addr" \
    A_DASHBOARD_HOST="$dashboard_host" A_PROMETHEUS_HOST="$dashboard_host" \
    A_WORK_DIR="$DATA_BASE/work" \
    "$BIN_DIR/asty" -mode server -config "$config_file" >> "$server_log" 2>&1 &
  local server_pid=$!

  # Agent runs under sudo so it can bind :80 for the gateway and then
  # drop to `asty` (if the user exists on this dev box). On macOS dev
  # boxes without an `asty` user the drop is a no-op and the agent
  # stays as root for the session — start.sh-only convenience, see
  # asty/internal/agent/privileges.go.
  sudo -E A_NODE_ID="dev-node-$i" A_NODE_IP="$addr" \
    A_WORK_DIR="$DATA_BASE/work" \
    A_GATEWAY_HOST="$gateway_host" \
    A_DISK_TYPE="$disk_type" \
    "$BIN_DIR/asty" -mode agent -config "$config_file" >> "$agent_log" 2>&1 &
  local agent_pid=$!

  printf '%s\n%s\n' "$server_pid" "$agent_pid" > "${PID_FILE_TMPL}-$i"

  info "Node $i: id=dev-node-$i | ip=$addr | disk=${A_DISK_TOTAL}M ${disk_type} | swap=${A_SWAP_TOTAL}M | server PID=$server_pid | agent PID=$agent_pid"
  info "  logs: $server_log | $agent_log"
}

# add_node grows a running cluster by one. Picks the next free index,
# brings up its loopback alias, publishes its asty.test A-record, then
# starts the new node. Existing agents notice the DNS change on their
# next watcher tick (~5 s), re-render nats.conf with the new peer, and
# restart their nats-server child. Reaching steady-state takes the
# rendered-conf change + JetStream meta-leader re-election (~10–15 s).
add_node() {
  sweep_orphan_nodes
  if ! compgen -G "${PID_FILE_TMPL}-*" > /dev/null; then
    die "no running cluster found (no ${PID_FILE_TMPL}-*). Start one with: $0 [N]"
  fi

  # Next free index = max existing + 1, from the pidfiles.
  local max_i=0 idx
  for idx in $(node_indices); do
    [[ $idx -gt $max_i ]] && max_i=$idx
  done
  local i=$((max_i + 1))
  local addr="127.0.0.$i"

  log "adding node $i (id=dev-node-$i, ip=$addr)..."

  # Publish the new A-record (existing nodes + i) before booting the new
  # agent so it resolves the full peer set on its first bootstrap.
  sync_hosts $(node_indices) "$i"
  start_node "$i"

  info "node $i started. Existing agents will pick up the new peer on"
  info "the next nats-watch tick (~5 s) and restart their nats-server."
}

# remove_node shrinks a running cluster by tearing down one node and
# dropping it from the asty.test records. Without args, removes the
# highest-numbered node (symmetric with add_node). With an explicit
# index, removes that one. Removing the last node prompts for an
# explicit 'yes' — the cluster is fully dismantled in that case and
# data dirs go with it.
#
# Surviving agents pick up the file change on their next watcher tick
# (~5 s) and shrink their cluster.routes via SIGHUP (or cold restart
# on the 2→1 step). The departing agent's graceful-shutdown path drops
# its node.<id> entry from the asty-cluster KV, so /metrics and the
# dashboard stop listing it within one scrape cycle.
remove_node() {
  local target="${1:-}"

  sweep_orphan_nodes

  if ! compgen -G "${PID_FILE_TMPL}-*" > /dev/null; then
    die "no running cluster found. Start one with: $0 [N]"
  fi

  # Resolve target: explicit index wins, otherwise highest live node.
  if [[ -z "$target" ]]; then
    target=0
    local idx
    for idx in $(node_indices); do
      [[ $idx -gt $target ]] && target=$idx
    done
  fi
  if ! [[ "$target" =~ ^[0-9]+$ ]] || [[ $target -lt 1 ]]; then
    die "usage: $0 remove [N]  (N ≥ 1)"
  fi

  local pidfile="${PID_FILE_TMPL}-$target"
  if [[ ! -f "$pidfile" ]]; then
    die "node $target is not running ($pidfile missing)"
  fi
  # Cluster size from the dashboard (= KV), not from local pidfiles.
  # The two diverge whenever an out-of-band kill (UI button, crash)
  # removes a node from KV while its host-side processes are still
  # winding down — pidfile count would over-report and skip the
  # last-node warning. Try each pidfile's IP until one dashboard
  # answers successfully; if none do, skip the check with a warning.
  local cluster_size="" addr_try
  for pf_try in "${PID_FILE_TMPL}-"*; do
    [[ -f "$pf_try" ]] || continue
    addr_try="127.0.0.${pf_try##*-}"
    cluster_size=$(curl -fsS --max-time 2 "http://${addr_try}:7060/dashboard/v1/nodes" 2>/dev/null \
      | python3 -c "import sys,json; print(json.load(sys.stdin).get('count', ''))" 2>/dev/null || true)
    [[ "$cluster_size" =~ ^[0-9]+$ ]] && break
    cluster_size=""
  done
  if [[ -z "$cluster_size" ]]; then
    warn "cannot reach the dashboard on any node — skipping last-node guard"
  else
    info "cluster size: $cluster_size"
    if [[ "$cluster_size" -le 1 ]]; then
      warn "this is the last running node — the cluster will be fully dismantled"
      printf "${YELLOW}Type 'yes' to confirm:${NC} "
      local confirm
      read -r confirm
      [[ "$confirm" == "yes" ]] || die "aborted"
    fi
  fi

  local addr="127.0.0.$target"
  log "removing node $target (id=dev-node-$target, ip=$addr)..."

  # 1) Dashboard kill first. Drives the same flow as the UI button:
  #    CmdShutdown to the agent + RemoveNode in KV. On a healthy
  #    cluster this is what clears the node from the dashboard list —
  #    the agent's own graceful path can fail to deregister when the
  #    JS bucket is degraded, and SIGKILL three seconds later won't
  #    retry. Best-effort: 5 s timeout, failure ignored, the local
  #    process kill below still wraps up the host side.
  local kill_url="http://${addr}:7060/dashboard/v1/nodes/dev-node-${target}/kill"
  if curl -fsS -X POST \
      -H "Authorization: Bearer ${A_TOKEN:-}" \
      -H "Content-Type: application/json" \
      -d "{\"confirm_name\":\"dev-node-${target}\"}" \
      --max-time 5 \
      "$kill_url" >/dev/null 2>&1; then
    info "✓ dashboard kill dispatched (KV cleanup pending)"
  else
    warn "dashboard kill unreachable — relying on local SIGTERM only (KV record may linger)"
  fi

  # 2) SIGTERM the node's server + agent (sudo — agent runs as root).
  #    The agent's ctx-cancel path deregisters node.<id> from KV (if
  #    the dashboard call above didn't already), then signals the NATS
  #    supervisor to SIGTERM the nats-server child. 3s grace covers
  #    deregister round-trip + nats-server JetStream flush before we
  #    escalate to SIGKILL.
  local pid
  while IFS= read -r pid; do
    [[ -n "$pid" ]] || continue
    sudo kill "$pid" 2>/dev/null && info "✓ PID $pid terminated" || true
  done < "$pidfile"
  sleep 3
  while IFS= read -r pid; do
    [[ -n "$pid" ]] || continue
    sudo kill -0 "$pid" 2>/dev/null && sudo kill -9 "$pid" 2>/dev/null && info "✓ PID $pid (SIGKILL)" || true
  done < "$pidfile"

  # 3) Clean per-node state — pidfile, working dir, JS store. Old
  #    JetStream data would conflict with a future add reusing the same
  #    index. Dropping the pidfile also drops the node from node_indices.
  rm -f "$pidfile"
  sudo rm -rf "$DATA_BASE/work/dev-node-$target" "$DATA_BASE/jetstream/dev-node-$target" "$DATA_BASE/node$target"

  # 4) Rebuild asty.test from the survivors so they notice on their next
  #    watcher tick and shrink their cluster.routes (SIGHUP hot reload
  #    when 2+ remain, cold restart on the 2→1 step).
  sync_hosts $(node_indices)

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
  info "Dashboard:  http://asty.test:7060/dashboard/v1  (all nodes; SPA target, DNS failover)"
  info "Prometheus: http://127.0.0.1:7060/metrics       (shared listener)"
  info "Health:     http://127.0.0.1:7060/health"
  info "Gateway:    http://127.0.0.1:80/api/v1          (node 1; user traffic)"
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
  # 3s SIGTERM grace mirrors remove_node: agent runs deregister +
  # supervisor SIGTERM + nats-server JS flush in series.
  local pidfile pid
  for pidfile in "${PID_FILE_TMPL}-"*; do
    [[ -f "$pidfile" ]] || continue
    while IFS= read -r pid; do
      [[ -n "$pid" ]] || continue
      sudo kill "$pid" 2>/dev/null && info "✓ PID $pid terminated" || true
    done < "$pidfile"
  done
  sleep 3
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

  # /etc/hosts asty.test block.
  teardown_hosts

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

if [[ "$CMD" == "add" ]]; then
  check_deps
  add_node
  exit 0
fi

if [[ "$CMD" == "remove" ]]; then
  remove_node "${2:-}"
  exit 0
fi

if ! [[ "$CMD" =~ ^[0-9]+$ ]] || [[ "$CMD" -lt 1 ]]; then
  die "usage: $0 [NODES|stop|add|remove [N]]  (NODES ≥ 1, default 1)"
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
