#!/usr/bin/env bash
# deploy/dev/start.sh
#
# Start and stop the Asty dev environment.
#
# Usage:
#   ./start.sh             — 1 node (server + agent)
#   ./start.sh 3           — 3 nodes (server + agent each, with leader election)
#   ./start.sh add         — grow a running cluster by one node
#   ./start.sh kill [N]    — shrink: tear down node N (default: highest-numbered)
#   ./start.sh degrade     — hard N→1: SIGKILL the current leader every step
#   ./start.sh stop        — stop everything

set -euo pipefail

# Dev-only: ensure every file the cluster creates (nats.conf, log files,
# JetStream store, pidfiles) is world-readable, so Claude can inspect
# them WITHOUT sudo. The agent runs under `sudo -E ...` and its default
# root umask (022) already produces 644 files, but the JetStream store
# is created by nats-server with restrictive 700/600 permissions; we
# override below via post-creation chmod -R a+rX on $DATA_BASE.
#
# This is local dev convenience only. The asty binary itself, and the
# prod start scripts under deploy/prod/, are untouched and keep their
# default permissions.
umask 022

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
# Per-node so kill can target a specific node's PIDs without scanning
# ps, and each add/kill leaves a self-contained record; stop_all
# iterates the whole set.
PID_FILE_TMPL="$DATA_BASE/pids"
# /etc/hosts publishes TWO kinds of names for the dev cluster:
#
#   - HOSTS_NAME ("asty.test")        — common round-robin name with one
#                                       A-record per live node. The SPA
#                                       (VITE_ASTY_ORIGIN) hits this so
#                                       the browser picks a node by RR.
#   - n<i>.asty.test                  — per-node name with a single
#                                       A-record (127.0.0.<i>). Used by
#                                       the frontend balancer to address
#                                       a specific node when it wants to
#                                       pin a session, and exported into
#                                       each agent as A_NODE_HOST so the
#                                       agent writes it into KV
#                                       (NodeInfo.Host). Asty itself
#                                       does NOT resolve these for peer
#                                       discovery — that flows through
#                                       A_NATS_SEED + cluster KV.
# .test is RFC 6761 reserved and (unlike .dev) not HSTS-forced to HTTPS.
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

# ensure_paths_writable bails out with one precise fix-up command if
# any of DATA_BASE / per-node log files are root-owned and the current
# user can't write to them. This happens after a previous run under
# sudo left files owned by root; without sudo, today's run would fail
# with "Permission denied" inside redirects to those logs (which bash
# opens BEFORE the wrapped command can chown them itself).
ensure_paths_writable() {
  if [[ "${EUID}" -eq 0 ]]; then return; fi
  local dirty=()
  [[ -e "$DATA_BASE" && ! -w "$DATA_BASE" ]] && dirty+=("$DATA_BASE")
  local f
  for f in /tmp/asty-dev-*.log; do
    [[ -e "$f" && ! -w "$f" ]] && dirty+=("$f")
  done
  if (( ${#dirty[@]} > 0 )); then
    warn "previous sudo run left ${#dirty[@]} path(s) root-owned; re-claiming them..."
    sudo -n chown -R "${USER}":staff "${dirty[@]}" 2>/dev/null && return
    sudo chown -R "${USER}":staff "${dirty[@]}" 2>/dev/null && return
    die "cannot reclaim root-owned paths. Run once: sudo chown -R ${USER}:staff /tmp/asty-dev /tmp/asty-dev-*.log"
  fi
}

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

  # -buildvcs=false: when start.sh is invoked via sudo the build runs as
  # root while the repo is owned by the operator, git refuses the safe.
  # directory check, and `go build` aborts on VCS stamping. We don't ship
  # the VCS stamp into dev binaries so disabling it is a clean fix.
  go build -buildvcs=false -o "$BIN_DIR/asty" ./asty/cmd
  info "✓ asty"

  for svc in xauth xhttp xws; do
    go build -buildvcs=false -o "$BIN_DIR/$svc" "./demo/cmd/$svc"
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
# (start_node writes pids-$i, kill deletes it). Index $i maps to IP
# 127.0.0.$i.
node_indices() {
  local pf i
  for pf in "${PID_FILE_TMPL}-"*; do
    [[ -f "$pf" ]] || continue
    i="${pf##*-}"
    [[ "$i" =~ ^[0-9]+$ ]] && echo "$i"
  done
}

# sync_hosts <index...> rewrites the asty-dev block in /etc/hosts so it
# carries TWO kinds of records for every given node index <i>:
#
#   127.0.0.<i> asty.test           ← appended once per node; the
#                                     resolver returns the whole set,
#                                     giving the SPA a round-robin pick
#                                     across nodes.
#   127.0.0.<i> n<i>.asty.test      ← one A-record per node, exported
#                                     into the agent as A_NODE_HOST so
#                                     it lands in KV (NodeInfo.Host)
#                                     and the frontend balancer can
#                                     pin a request to a specific node.
#
# Built from explicit indices, not pidfiles, because at initial start
# the pidfiles don't exist yet. Requires sudo.
sync_hosts() {
  local block tmp i count=0
  block="$HOSTS_BEGIN"$'\n'
  for i in "$@"; do
    block+="127.0.0.$i $HOSTS_NAME"$'\n'
    block+="127.0.0.$i n${i}.${HOSTS_NAME}"$'\n'
    count=$((count + 1))
  done
  block+="$HOSTS_END"
  # Strip any prior block via `sudo sed` (root-owned `/etc/hosts` may be
  # mode 0600 — see comment above macOS default; an unprivileged sed
  # would fail "Permission denied"), append the fresh one, swap the file
  # back with `install` so the result keeps 0644 root:wheel even if the
  # mktemp temp file is 0600 (which `sudo cp` would otherwise inherit).
  tmp=$(mktemp)
  sudo sed "/^# >>> asty-dev/,/^# <<< asty-dev/d" /etc/hosts > "$tmp"
  printf '%s\n' "$block" >> "$tmp"
  sudo install -m 0644 -o root -g wheel "$tmp" /etc/hosts
  rm -f "$tmp"
  info "$HOSTS_NAME (RR) + n<i>.$HOSTS_NAME → $count node(s) in /etc/hosts"
}

# teardown_hosts removes the asty.test block from /etc/hosts.
teardown_hosts() {
  sudo grep -q "^# >>> asty-dev" /etc/hosts 2>/dev/null || return 0
  log "removing $HOSTS_NAME from /etc/hosts (requires sudo)..."
  local tmp
  tmp=$(mktemp)
  sudo sed "/^# >>> asty-dev/,/^# <<< asty-dev/d" /etc/hosts > "$tmp"
  sudo install -m 0644 -o root -g wheel "$tmp" /etc/hosts
  rm -f "$tmp"
}

# =============================================================================
# Orphan sweep
# =============================================================================
# sweep_orphan_nodes drops pidfiles whose agent PID is gone — dashboard
# kill, crash, or external SIGTERM all leave the file behind otherwise.
# It also picks up the orphaned server process from the same pidfile
# (servers don't die on their own when the agent does). Removing the
# pidfile drops the node from node_indices; the caller (add/kill)
# rebuilds /etc/hosts via sync_hosts so surviving nats-servers stop
# routing to the dead peer. Called at the top of add_node and
# kill_node so subsequent counts/index-picks see ground truth.
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
  ensure_paths_writable
  mkdir -p "$DATA_BASE"
  # When the script runs via sudo, ensure DATA_BASE + per-node log
  # files belong to the invoking user so subsequent invocations
  # without sudo can still write to them. Otherwise a sudo-`add`
  # followed by a plain `add` gets Permission denied on
  # /tmp/asty-dev/nodeN or /tmp/asty-dev-{server,agent}-N.log.
  if [[ -n "${SUDO_USER:-}" && "${SUDO_USER}" != "root" ]]; then
    chown -R "${SUDO_USER}":staff "$DATA_BASE" 2>/dev/null || true
    chown "${SUDO_USER}":staff /tmp/asty-dev-*.log 2>/dev/null || true
  fi
  # Bring nodes up ONE AT A TIME via add_node, which returns only after
  # the cluster has fully normalized (stabilized=1) — so the next node
  # never outruns NATS settling the previous replica change (piling them
  # on left an 8-node cold start with leaderless streams). add_node picks
  # the next free index: iteration 1 → node 1 (standalone bootstrap), the
  # rest join node 1 as the seed. Same function as the live `add` — one
  # path, no duplication.
  local n
  for ((n=1; n<=nodes; n++)); do
    add_node
  done
}

# wait_node_registered polls a live node's /nodes endpoint until the named
# node id shows up in the response, or fails out after a generous
# wall-clock budget. The query endpoint is the seed the join used (caller
# passes it; falls back to any live node) — node 1 is NOT assumed alive, a
# degradation run may have killed it. The response is a JSON
# {nodes:[{id,...}]} that we grep — no jq dep.
wait_node_registered() {
  local i=$1
  local id="dev-node-$i"
  local seed_addr="${2:-$(live_seed_ip)}"
  local url="http://${seed_addr}:7060/dashboard/v1/nodes"
  local deadline=$((SECONDS + 60))
  local probe_interval=0.5
  # Node 1 starts standalone — there's nothing to wait for, its own
  # heartbeat populates KV in <1s and we'd just be polling ourselves.
  if (( i == 1 )); then
    return 0
  fi
  while (( SECONDS < deadline )); do
    if curl -fsS --max-time 2 \
         -H "Authorization: Bearer ${A_TOKEN:-}" \
         "$url" 2>/dev/null \
       | grep -q "\"id\":\"${id}\""; then
      info "  ✓ $id registered in KV"
      return 0
    fi
    sleep "$probe_interval"
  done
  warn "$id did not register within $((deadline - SECONDS + 60))s — continuing anyway"
  return 1
}

# wait_cluster_stable blocks until the cluster reports it has fully
# normalized — `asty_cluster_stabilized 1` on the metrics endpoint: every
# KV stream at its target replica count AND current, no dead peers, leader
# stable. This is the gate that makes a multi-node bring-up safe — the
# NEXT node is not started until the cluster has fully absorbed the
# previous one. Adding nodes faster than NATS settles each replica change
# leaves stream RAFT groups leaderless (an 8-node cold start without this
# wait got stuck at stabilized=0). Queries the live seed (caller passes it;
# falls back to any live node) — node 1 may be gone after a degradation run.
wait_cluster_stable() {
  local explicit="${1:-}"
  local deadline=$((SECONDS + 90))
  local seed_addr remaining
  while (( SECONDS < deadline )); do
    # Re-resolve each iteration: at the moment add_node calls this for
    # i=1 the local asty server may not yet be listening on :7060, so a
    # one-shot live_seed_ip expansion would lock in an empty seed_addr
    # and the URL would stay malformed for the whole budget. Re-resolving
    # picks the seed up as soon as the listener answers /health.
    # `|| true` on the substitution: without it, set -e propagates
    # live_seed_ip's "no seed yet" return 1 through the assignment and
    # aborts the script on the very first iteration (assignment's exit
    # status IS the rightmost command-sub's status when there's no
    # `local` keyword on the line).
    seed_addr="${explicit:-$(live_seed_ip || true)}"
    if [[ -n "$seed_addr" ]]; then
      remaining=$((deadline - SECONDS))
      # Event-driven wait: subscribe to the dashboard SSE stream
      # (GET /dashboard/v1/ with Accept: text/event-stream — the server
      # PUSHES one snapshot event per streamHub tick) and exit on the
      # first '"stabilized":true'. No client-side polling of /metrics:
      # the server tells us, we don't ask. curl --max-time caps the
      # SSE read at the remaining budget so a server that never
      # stabilizes can't block past the wall-clock deadline; on connect
      # failure (listener still binding) curl returns immediately and
      # the outer loop reconnects on the next iteration.
      # Process-substitution form (NOT `curl | grep -m1 -q`): with
      # `set -o pipefail` the early-exit grep would SIGPIPE curl and
      # pipefail propagates curl's non-zero exit, swallowing the match.
      if grep -m1 -q '"stabilized":true' < <(curl -fsSN --max-time "$remaining" \
           -H "Accept: text/event-stream" \
           "http://${seed_addr}:7060/dashboard/v1/" 2>/dev/null); then
        info "  ✓ cluster normalized (stabilized=1)"
        return 0
      fi
    fi
    # Short retry while the listener is still binding (event-source for
    # "server is up" is a successful connection — there is no out-of-
    # band signal to subscribe to before then).
    sleep 1
  done
  warn "cluster did not reach stabilized=1 within 90s — continuing anyway"
  return 1
}

# live_seed_ip echoes a live cluster node's IP to seed a join against /
# announce to / query. Prefers node 1 (the canonical bootstrap) when
# alive, else the first /health-reachable node — so `add` and the wait
# gates work even after node 1 has been killed (e.g. a degradation run
# left another node as the lone survivor). Empty + nonzero exit if nothing
# is alive.
live_seed_ip() {
  local i addr
  # Iterate over existing per-node pidfiles instead of a hardcoded
  # index range. After many add/degrade cycles, node indices can climb
  # past 100 — `seq 1 16` would miss every live survivor and `add`
  # would bail with "no live cluster node to seed from".
  for i in $(node_indices); do
    addr="127.0.0.$i"
    curl -fsS --max-time 1 "http://${addr}:7060/health" >/dev/null 2>&1 && { printf '%s' "$addr"; return 0; }
  done
  return 1
}

# announce_peer_to_seed runs `asty -mode admin add-peer` against a LIVE
# cluster node's agent (the seed, passed in as $2 — NOT assumed to be
# node 1, which a degradation run may have killed). In prod the same CLI
# is invoked over SSH and reads the client IP from $SSH_CLIENT (set by
# sshd). In dev we bypass SSH and synthesise SSH_CLIENT ourselves so the
# CLI takes the same code path. The CLI then connects to the seed's NATS
# (<seed_ip>:4222) with the agent's own credentials and publishes
# CmdAddPeer to the seed's command subject; the seed records the IP and
# SIGHUP/cold-restarts its nats-server so :6222 opens (or now lists the
# extra route). A solo standalone survivor cold-restarts into clustered
# here — the 1→N re-grow, mirror of the N→1 collapse.
announce_peer_to_seed() {
  local i=$1
  local seed_ip=$2
  local addr="127.0.0.$i"
  local seed_id="dev-node-${seed_ip##*.}"
  local config_file="$SCRIPT_DIR/config.asty"
  log "announcing node $i ($addr) to seed $seed_id ($seed_ip)..."
  # SSH_CLIENT format: "<client-ip> <client-port> <server-port>".
  SSH_CLIENT="$addr 0 0" \
    A_NODE_ID="$seed_id" A_NODE_IP="$seed_ip" \
    "$BIN_DIR/asty" -mode admin -config "$config_file" add-peer \
    || warn "announce failed; node $i may take longer to join"
  # Tiny pause for the seed to finish the cold-restart triggered by the
  # standalone→clustered flip. NATS itself retries the inbound route,
  # so we don't have to be perfectly synchronous; this just shortens
  # the visible "Waiting for routing to be established" window.
  sleep 1
}

# start_node brings up one server + agent pair with the per-node env
# (NODE_ID/IP/HOST/UI/gateway address, fake disk type). Shared by
# start_asty (initial fan-out) and add_node (live cluster growth).
#
# NATS peer discovery: i=1 boots standalone (A_NATS_SEED unset, $2 empty).
# i>=2 joins through the live seed IP passed as $2 (resolved by the caller
# via live_seed_ip — NOT hardcoded to node 1) — from there on the agent
# watches cluster KV for membership and rewrites cluster.routes via SIGHUP
# on every change. No DNS lookup is involved.
start_node() {
  local i=$1
  local nats_seed="${2:-}"
  local addr="127.0.0.$i"
  local node_host="n${i}.${HOSTS_NAME}"
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

  # Per-node datacenter so the proximity-aware scheduler has something
  # non-trivial to work with — without this every node ends up in dc1
  # (the YAML default) and DC-diversity / proximity matrix degrade to
  # no-ops. dc$i is just a label; the matrix's latency entries (or
  # lack thereof) decide actual placement preference.
  local node_dc="dc$i"

  # Server runs WITHOUT sudo — no privileged ports, drop-root is a
  # no-op (resolveDropTarget sees euid != 0). Dashboard listens on
  # the per-node loopback alias so multiple servers don't race for
  # the same socket.
  A_NODE_ID="dev-node-$i" A_NODE_IP="$addr" A_NODE_HOST="$node_host" \
    A_DATACENTER="$node_dc" \
    A_NATS_SEED="$nats_seed" \
    A_DASHBOARD_HOST="$dashboard_host" A_PROMETHEUS_HOST="$dashboard_host" \
    A_WORK_DIR="$DATA_BASE/work" \
    "$BIN_DIR/asty" -mode server -config "$config_file" >> "$server_log" 2>&1 &
  local server_pid=$!

  # Agent runs under sudo so it can bind :80 for the gateway and then
  # drop to `asty` (if the user exists on this dev box). On macOS dev
  # boxes without an `asty` user the drop is a no-op and the agent
  # stays as root for the session — start.sh-only convenience, see
  # asty/internal/agent/privileges.go.
  sudo -E A_NODE_ID="dev-node-$i" A_NODE_IP="$addr" A_NODE_HOST="$node_host" \
    A_DATACENTER="$node_dc" \
    A_NATS_SEED="$nats_seed" \
    A_WORK_DIR="$DATA_BASE/work" \
    A_GATEWAY_HOST="$gateway_host" \
    A_DISK_TYPE="$disk_type" \
    "$BIN_DIR/asty" -mode agent -config "$config_file" >> "$agent_log" 2>&1 &
  local agent_pid=$!

  printf '%s\n%s\n' "$server_pid" "$agent_pid" > "${PID_FILE_TMPL}-$i"

  info "Node $i: id=dev-node-$i | ip=$addr | host=$node_host | dc=$node_dc | seed=${nats_seed:-<none>} | disk=${A_DISK_TOTAL}M ${disk_type} | swap=${A_SWAP_TOTAL}M | server PID=$server_pid | agent PID=$agent_pid"
  info "  logs: $server_log | $agent_log"
}

# add_node brings up ONE node at the next free index and returns only
# once the cluster has normalized with it. The single bring-up path,
# shared by the N-node cold start (start_asty loops it) and the live
# `add` subcommand — no duplicated logic. Index 1 bootstraps standalone;
# every later index joins through the live seed resolved by live_seed_ip
# (node 1 when alive, else any survivor), and existing agents pick up the
# new peer KV-side (WatchNodes) and SIGHUP their nats-server to add the route.
add_node() {
  ensure_paths_writable
  # When invoked via sudo, ensure DATA_BASE + per-node log files are
  # writable by the calling user so a follow-up plain `add` doesn't
  # hit "Permission denied" on /tmp/asty-dev/nodeN or
  # /tmp/asty-dev-{server,agent}-N.log.
  if [[ -n "${SUDO_USER:-}" && "${SUDO_USER}" != "root" && -d "$DATA_BASE" ]]; then
    chown -R "${SUDO_USER}":staff "$DATA_BASE" 2>/dev/null || true
    chown "${SUDO_USER}":staff /tmp/asty-dev-*.log 2>/dev/null || true
  fi
  # Next free index = max existing + 1, from the pidfiles. On a clean
  # start node_indices is empty → i=1 (standalone bootstrap); on a live
  # cluster it's max+1 (joins the seed). One path for both the N-node
  # cold start (start_asty loops this) and the live `add` — no dup.
  local max_i=0 idx
  for idx in $(node_indices); do
    [[ $idx -gt $max_i ]] && max_i=$idx
  done
  local i=$((max_i + 1))
  local addr="127.0.0.$i"
  local seed_ip=""

  log "adding node $i (id=dev-node-$i, ip=$addr)..."

  # Publish A-records (existing + new) before booting so the SPA can
  # address it by name immediately.
  sync_hosts $(node_indices) "$i"
  # Node 1 bootstraps standalone; every later node announces its IP to a
  # LIVE seed first (resolved here, not assumed to be node 1) so the seed's
  # nats-server opens the route — a solo survivor cold-restarts to clustered.
  if (( i >= 2 )); then
    seed_ip=$(live_seed_ip) || { warn "no live cluster node to seed node $i from — is the cluster up? aborting add"; return 1; }
    announce_peer_to_seed "$i" "$seed_ip"
  fi
  start_node "$i" "$seed_ip"
  # Two gates, in order: the node registers in KV (joined + first
  # heartbeat), THEN the whole cluster normalizes (replicas re-replicated
  # and current, stabilized=1). Returning only after BOTH is what keeps a
  # loop of add_node — or a live `add` — from outrunning NATS settling.
  wait_node_registered "$i" "$seed_ip"
  wait_cluster_stable "$seed_ip"

  info "node $i added; cluster normalized."
}

# kill_node shrinks a running cluster by tearing down one node and
# dropping its A-records. Without args, removes the highest-numbered
# node (symmetric with add_node). With an explicit index, removes that
# one. Removing the last node prompts for an explicit 'yes' — the
# cluster is fully dismantled in that case and data dirs go with it.
#
# The departing agent's graceful-shutdown path drops its node.<id>
# entry from the asty-cluster KV. Surviving agents see the KV delete
# through WatchNodes and SIGHUP their nats-server to drop the route
# (cold restart only on the 2→1 step, where JS goes standalone).
kill_node() {
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
    die "usage: $0 kill [N]  (N ≥ 1)"
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

  # 4) Rebuild the /etc/hosts block from the survivors so the SPA
  #    round-robin set and the per-node names match reality. Asty
  #    itself shrinks via the cluster-KV WatchNodes signal — the
  #    departing agent's graceful path already deleted node.<id>.
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

  # `sudo kill -0` (not unprivileged): agent runs under sudo, so its
  # PID is owned by root; an unprivileged kill -0 from this script's
  # user gets EPERM and falsely reports the process dead. Single sudo
  # kill -0 works for both root-owned (agent) and user-owned (server)
  # PIDs.
  local pidfile pid
  for pidfile in "${PID_FILE_TMPL}-"*; do
    [[ -f "$pidfile" ]] || continue
    while IFS= read -r pid; do
      [[ -n "$pid" ]] || continue
      if ! sudo kill -0 "$pid" 2>/dev/null; then
        die "Asty process (PID=$pid, from $pidfile) died. Check logs in /tmp/asty-dev-*.log"
      fi
    done < "$pidfile"
  done

  # Dev-only: make everything under $DATA_BASE world-readable so Claude
  # can inspect nats.conf, JetStream store, and pidfiles without sudo.
  # See umask comment at top of file. Failure is non-fatal — at worst
  # operator still needs sudo for inspection.
  sudo -n chmod -R a+rX "$DATA_BASE" 2>/dev/null || true
  sudo -n chmod a+r /tmp/asty-dev-*.log 2>/dev/null || true

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
  info "Dashboard:  http://asty.test:7060/dashboard/v1  (SPA target; /etc/hosts round-robin across nodes)"
  info "Per-node:   http://n<i>.asty.test:7060/dashboard/v1  (frontend balancer can pin a specific node)"
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
# Hard N→1 degradation — SIGKILL the current leader on every step
# =============================================================================
# degrade_cluster shrinks the running cluster down to one node by repeatedly
# yanking whoever is the JS-meta / leader-election leader RIGHT NOW. This is
# the hardest schedule: every kill targets the node carrying the most state,
# so the cluster has to elect a new leader, evict the dead peer, and rebalance
# replicas before the next kill lands.
#
# Survivor-side recovery is the cluster's responsibility — reapDeadPeers
# (server/deadpeers.go) + stream-placement repair (server/streamplacement.go).
# This script only kicks; it does not nurse the cluster between kicks.

# degradeConvergeBudget — hard wall-clock cap on the SSE wait after one
# SIGKILL. 10 min covers the worst case where the JS meta leader was the
# killed node, the asty-leader bucket's stream RAFT also lost quorum,
# AND the canonical 30 s leader TTL has to expire before a new candidate
# can claim. Each leader-kill carries: ≤30 s TTL expiry + ≤22.5 s
# candidate-tick (cut to one event-RTT by the wake-on-delete watcher,
# but still bounded by RAFT propagation) + stream reap + replica
# rebalance. Under deep degrade (12+ consecutive leader kills) NATS's
# own catchup work can stretch a single step well past 5 min; 600 s
# leaves headroom for that without masking real stalls.
degradeConvergeBudget=600

degrade_seed() {
  local i addr
  for i in $(node_indices); do
    addr="127.0.0.$i"
    if curl -fsS --max-time 1 "http://${addr}:7060/health" >/dev/null 2>&1; then
      printf '%s\n' "$addr"
      return 0
    fi
  done
  return 1
}

degrade_snapshot() {
  local seed="$1"
  curl -fsS --max-time 3 -H "Authorization: Bearer ${A_TOKEN:-}" \
    "http://${seed}:7060/dashboard/v1/" 2>/dev/null
}

degrade_stab() {
  local seed="$1"
  curl -fsS --max-time 2 "http://${seed}:7060/metrics" 2>/dev/null \
    | awk '/^asty_cluster_stabilized / {print $2}'
}

degrade_yank_node() {
  # Hard unplug. Skip the dashboard CmdShutdown, skip SIGTERM grace —
  # this MUST be indistinguishable from a power yank, so the cluster
  # recovers from JSZ/RAFT signals alone (reapDeadPeers + stream-
  # placement repair on the survivor side), not from anything the
  # dying node cooperated with. Returns non-zero only on a sudo cache
  # miss; missing host processes are no-ops.
  local idx="$1"
  local pidfile="${PID_FILE_TMPL}-$idx"
  local nats_conf="$DATA_BASE/work/dev-node-$idx/nats.conf"
  local addr="127.0.0.$idx"

  if ! sudo -n true 2>/dev/null; then
    warn "[$idx] sudo cache empty — yank cannot proceed"
    return 1
  fi

  log "yanking node $idx (id=dev-node-$idx, ip=$addr) — hard kill, no graceful path"

  if [[ -f "$pidfile" ]]; then
    local server_pid agent_pid
    server_pid=$(sed -n '1p' "$pidfile")
    agent_pid=$(sed -n '2p' "$pidfile")
    if [[ -n "$server_pid" ]]; then
      if kill -9 "$server_pid" 2>/dev/null; then
        info "✓ server PID $server_pid SIGKILL"
      else
        info "  server PID $server_pid already gone"
      fi
    fi
    if [[ -n "$agent_pid" ]]; then
      if sudo -n pkill -9 -P "$agent_pid" 2>/dev/null; then
        info "✓ agent children of $agent_pid SIGKILL"
      fi
      sleep 0.2
      if ! sudo -n kill -0 "$agent_pid" 2>/dev/null; then
        info "✓ agent wrapper PID $agent_pid gone (via child SIGKILL)"
      elif sudo -n kill -9 "$agent_pid" 2>/dev/null; then
        info "✓ agent wrapper PID $agent_pid SIGKILL"
      else
        warn "agent wrapper PID $agent_pid: kill failed"
      fi
    fi
  fi
  if sudo -n pkill -9 -f "nats-server -c $nats_conf" 2>/dev/null; then
    info "✓ nats-server (conf $nats_conf) SIGKILL"
  fi

  # Drop pidfile + per-node state. Pidfile removal drops the node
  # from node_indices so degrade's main loop sees the new live count.
  rm -f "$pidfile"
  sudo rm -rf "$DATA_BASE/work/dev-node-$idx" "$DATA_BASE/jetstream/dev-node-$idx" "$DATA_BASE/node$idx"

  # Rebuild /etc/hosts so the SPA round-robin set matches the survivors.
  sync_hosts $(node_indices)

  # Loopback alias on macOS — tear down so a future add reusing this
  # index re-binds cleanly. Failure is non-fatal.
  if [[ "$(uname)" == "Darwin" && "$idx" != "1" ]]; then
    sudo -n ifconfig lo0 -alias "$addr" 2>/dev/null && info "✓ loopback alias $addr removed" || true
  fi

  info "node $idx yanked. Survivor-side recovery (reapDeadPeers + stream re-placement) runs on the cluster."
}

degrade_wait_converged() {
  local prev_leader="$1" budget="$2"
  local deadline=$((SECONDS + budget))
  local seed snap leader stab
  while (( SECONDS < deadline )); do
    seed=$(degrade_seed) || { sleep 1; continue; }
    snap=$(degrade_snapshot "$seed") || { sleep 1; continue; }
    leader=$(printf '%s' "$snap" | grep -oE '"leader":"[^"]*"' | head -1 | cut -d'"' -f4)
    stab=$(degrade_stab "$seed")
    if [[ -n "$leader" && "$leader" != "$prev_leader" && "$stab" == "1" ]]; then
      printf '%s|%s\n' "$seed" "$leader"
      return 0
    fi
    sleep 2
  done
  return 1
}

degrade_cluster() {
  # Disable pipefail+errexit locally: curl timeouts (rc=28) inside the
  # many polling pipelines below otherwise propagate up and kill the
  # script mid-iteration, even though the next retry would succeed.
  # Each step has explicit return-1 paths for genuine failures.
  set +eo pipefail
  log "hard N→1 degradation: SIGKILL the current leader, wait full reconvergence, repeat"

  # Mirror EVERYTHING the rest of this script prints to a log file —
  # both stdout and stderr — so the operator doesn't need to copy-paste.
  DEGRADE_LOG="/tmp/asty-dev-degrade.log"
  : > "$DEGRADE_LOG"
  log "degrade output (stdout + stderr) mirrored to $DEGRADE_LOG"
  exec > >(tee -a "$DEGRADE_LOG") 2>&1

  if ! sudo -v 2>/dev/null; then
    sudo -v || die "degrade needs cached sudo to SIGKILL agents — run 'sudo -v' first"
  fi

  local step=0 seed snap leader idx live stab out wait_rc new_leader
  while true; do
    step=$((step + 1))
    sudo -n -v 2>/dev/null || warn "[$step] sudo cache lapsed — kill_node will fail; run 'sudo -v' in the terminal that owns this degrade run"

    live=$(node_indices | wc -l | tr -d ' ')
    if (( live <= 1 )); then
      log "[$step] one node remains under operator control — degradation complete"
      break
    fi

    seed=$(degrade_seed) || { warn "[$step] no dashboard answering — aborting"; return 1; }
    snap=$(degrade_snapshot "$seed")
    leader=$(printf '%s' "$snap" | grep -oE '"leader":"[^"]*"' | head -1 | cut -d'"' -f4)
    stab=$(degrade_stab "$seed")
    if [[ -z "$leader" ]]; then
      warn "[$step] no leader reported via $seed — aborting"
      return 1
    fi
    log "[$step] live=$live via $seed leader=$leader stab=$stab"

    # leader id format: dev-node-<i>
    idx="${leader##*-}"
    info "[$step] SIGKILL leader $leader (idx=$idx) — node off the wire"
    ( degrade_yank_node "$idx" ) || warn "[$step] degrade_yank_node returned non-zero — pressing on"

    info "[$step] waiting on SSE for reconvergence (new leader + stabilized=true), budget ${degradeConvergeBudget}s"
    out=$(degrade_wait_converged "$leader" "$degradeConvergeBudget") || true
    wait_rc=$?
    if [[ -n "$out" ]]; then
      info "  [$step] wait_converged returned rc=$wait_rc out='$out'"
      new_leader="${out##*|}"
      info "[$step] converged via ${out%%|*} — new leader=$new_leader"
    else
      info "  [$step] wait_converged returned rc=$wait_rc out=''"
      warn "[$step] cluster did NOT reconverge within ${degradeConvergeBudget}s — survivor-side recovery is stuck"
      warn "[$step] aborting run; inspect /tmp/asty-dev-server-*.log for replica/peer errors"
      return 1
    fi
  done

  seed=$(degrade_seed 2>/dev/null) || { warn "no node alive at end of run"; return 0; }
  # Wait for the survivor's KV view to reflect alone-state (nodes_total=1).
  # Without this the final snapshot can show stale ghosts from before the
  # natssolo collapse, falsely suggesting degrade didn't finish.
  local final_deadline=$((SECONDS + 60)) snap_total
  while (( SECONDS < final_deadline )); do
    snap_total=$(degrade_snapshot "$seed" | grep -oE '"nodes_total":[0-9]+' | head -1 | cut -d: -f2)
    [[ "$snap_total" == "1" ]] && break
    sleep 2
  done
  info "final state via $seed:"
  degrade_snapshot "$seed" | head -1 || true
  log "✓ degradation complete (survivor remains)"
}

# =============================================================================
# Stop
# =============================================================================
stop_all() {
  log "stopping Asty..."

  # Asty processes by per-node PID files (sudo — agents run as root).
  # 3s SIGTERM grace mirrors kill_node: agent runs deregister +
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
  sweep_orphan_nodes
  if ! compgen -G "${PID_FILE_TMPL}-*" > /dev/null; then
    die "no running cluster found (no ${PID_FILE_TMPL}-*). Start one with: $0 [N]"
  fi
  add_node
  exit 0
fi

if [[ "$CMD" == "kill" ]]; then
  kill_node "${2:-}"
  exit 0
fi

if [[ "$CMD" == "degrade" ]]; then
  check_deps
  sweep_orphan_nodes
  if ! compgen -G "${PID_FILE_TMPL}-*" > /dev/null; then
    die "no running cluster found (no ${PID_FILE_TMPL}-*). Start one with: $0 [N]"
  fi
  degrade_cluster
  exit 0
fi

if ! [[ "$CMD" =~ ^[0-9]+$ ]] || [[ "$CMD" -lt 1 ]]; then
  die "usage: $0 [NODES|stop|add|kill [N]|degrade]  (NODES ≥ 1, default 1)"
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
