#!/usr/bin/env bash
# deployments/envs/dev/start.sh
#
# Запуск и остановка dev-окружения Asty.
#
# Использование:
#   ./start.sh          — 1 нода (Asty server + agent)
#   ./start.sh 3        — 3 ноды (1 server + 3 agents)
#   ./start.sh stop     — остановить всё

set -euo pipefail

# =============================================================================
# Пути
# =============================================================================
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../../.." && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"
VARS_FILE="$SCRIPT_DIR/dev.vars"
BIN_DIR="$ROOT_DIR/bin"
DATA_BASE="/tmp/asty-dev"
PID_FILE="/tmp/asty-dev-pids"
NATS_CONF_RENDERED="/tmp/asty-dev-nats.conf"

# =============================================================================
# Вывод
# =============================================================================
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; CYAN='\033[0;36m'; NC='\033[0m'
log()  { echo -e "${GREEN}▶${NC} $*"; }
info() { echo -e "${CYAN}  $*${NC}"; }
warn() { echo -e "${YELLOW}⚠${NC} $*"; }
die()  { echo -e "${RED}✗ $*${NC}" >&2; exit 1; }

# =============================================================================
# Проверка зависимостей
# =============================================================================
check_deps() {
  local missing=()
  for cmd in docker go; do
    command -v "$cmd" &>/dev/null || missing+=("$cmd")
  done
  [[ ${#missing[@]} -eq 0 ]] || die "Не найдены: ${missing[*]}. Установите и повторите."
}

# =============================================================================
# Рендер NATS-конфига
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
# Инфраструктура (NATS + PostgreSQL)
# =============================================================================
start_infra() {
  local nodes=$1
  log "Запуск инфраструктуры: $nodes нод NATS + PostgreSQL..."
  render_nats_conf "$nodes"
  export DEV_NATS_CONF="$NATS_CONF_RENDERED"

  if [[ $nodes -gt 1 ]]; then
    export NATS_CLIENT_PORTS="4222-4322:4222"
    export NATS_HTTP_PORTS="8222-8322:8222"
  else
    unset NATS_CLIENT_PORTS NATS_HTTP_PORTS
  fi

  docker compose -f "$COMPOSE_FILE" down --remove-orphans 2>/dev/null || true
  docker compose -f "$COMPOSE_FILE" up -d --scale nats="$nodes"
  info "NATS мониторинг: http://localhost:8222"
}

# =============================================================================
# Ожидание готовности NATS (JetStream)
# =============================================================================
wait_nats() {
  local nodes=$1
  log "Ожидание готовности NATS (JetStream)..."
  local max_wait=60
  local elapsed=0

  if [[ $nodes -gt 1 ]]; then
    until docker compose -f "$COMPOSE_FILE" exec -T nats \
          wget -q -O - --timeout=2 "http://localhost:8222/jsz" 2>/dev/null \
          | grep -Eq '"leader":[[:space:]]*"[^"]'; do
      sleep 1
      elapsed=$((elapsed + 1))
      [[ $elapsed -lt $max_wait ]] || die "NATS meta-leader не выбран за ${max_wait}s"
    done
  fi

  until docker compose -f "$COMPOSE_FILE" exec -T --index 1 nats \
        wget -q -O /dev/null --timeout=2 "http://localhost:8222/healthz?js-server-only=true" \
        &>/dev/null; do
    sleep 1
    elapsed=$((elapsed + 1))
    [[ $elapsed -lt $max_wait ]] || die "NATS dev-nats-1 не готов за ${max_wait}s"
  done

  info "NATS готов (${elapsed}s)"
}

stop_infra() {
  log "Остановка инфраструктуры..."
  docker compose -f "$COMPOSE_FILE" down
}

# =============================================================================
# Сборка бинарников
# =============================================================================
build_binaries() {
  log "Сборка бинарников → $BIN_DIR ..."
  mkdir -p "$BIN_DIR"
  cd "$ROOT_DIR"

  shopt -s nullglob
  for dir in cmd/*/; do
    svc="${dir%/}"; svc="${svc#cmd/}"
    go build -o "$BIN_DIR/$svc" "./cmd/$svc"
    info "✓ $svc"
  done
}

# =============================================================================
# Loopback-алиасы для multi-node кластера (macOS)
# =============================================================================
setup_loopback_aliases() {
  local nodes=$1
  [[ "$(uname -s)" == "Darwin" ]] || return 0
  [[ $nodes -gt 1 ]] || return 0

  log "Настройка loopback-алиасов 127.0.0.2..127.0.0.$nodes (потребуется sudo)..."
  for ((i=2; i<=nodes; i++)); do
    sudo ifconfig lo0 -alias "127.0.0.$i" 2>/dev/null || true
    sudo ifconfig lo0 alias "127.0.0.$i" up
    info "алиас 127.0.0.$i"
  done
}

teardown_loopback_aliases() {
  [[ "$(uname -s)" == "Darwin" ]] || return 0
  local aliases
  aliases=$(ifconfig lo0 2>/dev/null | awk '/inet 127\.0\.0\.[0-9]+ / && $2!="127.0.0.1" {print $2}')
  [[ -n "$aliases" ]] || return 0
  log "Удаление loopback-алиасов (потребуется sudo)..."
  for addr in $aliases; do
    sudo ifconfig lo0 -alias "$addr" 2>/dev/null && info "убран $addr" || true
  done
}

# =============================================================================
# Asty: 1 server + N agents
# =============================================================================
start_asty() {
  local nodes=$1
  log "Запуск Asty: 1 server + $nodes agents..."

  # Загружаем dev.vars и экспортируем все переменные
  while IFS='=' read -r key value; do
    # Skip comments and empty lines
    [[ -z "$key" || "$key" =~ ^[[:space:]]*# ]] && continue
    # Strip leading/trailing whitespace
    key=$(echo "$key" | xargs)
    value=$(echo "$value" | xargs)
    # Export variable
    export "$key=$value"
  done < <(grep -v '^\s*#' "$VARS_FILE" | grep -v '^\s*$')

  # Определяем host-порт NATS
  local nats_host_port
  nats_host_port=$(docker port dev-nats-1 4222/tcp 2>/dev/null | awk -F: '/0\.0\.0\.0:/ {print $NF; exit}')
  [[ -n "$nats_host_port" ]] || die "не удалось определить host-порт NATS"
  info "NATS host-порт: $nats_host_port"

  # Экспортируем переменные для Asty
  export A_NATS_HOST="127.0.0.1"
  export A_NATS_PORT="$nats_host_port"
  export A_NATS_USER="${A_NATS_USER:-}"
  export A_NATS_PASSWORD="${A_NATS_PASSWORD:-}"
  export A_DOMAIN="${A_DOMAIN:-dev.local}"
  export A_TOKEN="${A_TOKEN:-dev-token-secret}"
  export A_LOG_LEVEL="${A_LOG_LEVEL:-debug}"
  export A_DATACENTER="${A_DATACENTER:-dc1}"
  export A_MIN_COPIES="${A_MIN_COPIES:-2}"
  export A_TARGET_CPU="${A_TARGET_CPU:-75}"
  export A_TARGET_MEMORY="${A_TARGET_MEMORY:-75}"
  export A_TRAFFIC_RPS_THRESHOLD="${A_TRAFFIC_RPS_THRESHOLD:-5}"
  export A_UI_ADDR="${A_UI_ADDR:-127.0.0.1:4747}"
  export A_WORK_DIR="${A_WORK_DIR:-$DATA_BASE/work}"
  export A_SERVICE_DIR="${A_SERVICE_DIR:-${SCRIPT_DIR}}"

  # Запускаем 1 server
  local server_log="/tmp/asty-dev-server.log"
  mkdir -p "$DATA_BASE/server"

  export A_NODE_ID="server-1"
  "$BIN_DIR/asty" -mode server >> "$server_log" 2>&1 &
  local server_pid=$!
  echo "$server_pid" >> "$PID_FILE"
  info "Server: PID=$server_pid | Логи: $server_log"

  # Запускаем N agents (каждый с уникальным node ID и IP)
  for ((i=1; i<=nodes; i++)); do
    local agent_log="/tmp/asty-dev-node-$i.log"
    local addr="127.0.0.$i"
    mkdir -p "$DATA_BASE/node$i"

    export A_NODE_ID="dev-node-$i"
    export A_NODE_IP="$addr"
    "$BIN_DIR/asty" -mode agent >> "$agent_log" 2>&1 &
    local agent_pid=$!
    echo "$agent_pid" >> "$PID_FILE"
    info "Node $i: id=dev-node-$i | ip=$addr | PID=$agent_pid | Логи: $agent_log"
  done
}

# =============================================================================
# Ожидание готовности Asty
# =============================================================================
wait_asty() {
  log "Ожидание готовности Asty..."
  local max_wait=30
  local elapsed=0

  # Проверяем что процессы живы (простая проверка)
  sleep 2

  while IFS= read -r pid; do
    if ! kill -0 "$pid" 2>/dev/null; then
      die "Процесс Asty (PID=$pid) упал. Проверьте логи в /tmp/asty-dev-*.log"
    fi
  done < "$PID_FILE"

  info "Asty запущен"
}

# =============================================================================
# Очистка осиротевших процессов (агрессивно убивает ВСЁ связанное с asty)
# =============================================================================
cleanup_orphans() {
  local killed=0
  # Убиваем asty (и через полный путь, и через ./bin/, и через любое упоминание)
  pkill -9 -f "$BIN_DIR/asty" 2>/dev/null && killed=1 || true
  pkill -9 -f "./bin/asty" 2>/dev/null && killed=1 || true
  pkill -9 -f "bin/asty" 2>/dev/null && killed=1 || true
  pkill -9 -f "/asty" 2>/dev/null && killed=1 || true
  pkill -9 asty 2>/dev/null && killed=1 || true

  # Убиваем сервисы (агрессивно)
  for svc in gateway xauth xhttp xws; do
    pkill -9 -f "$BIN_DIR/$svc" 2>/dev/null && killed=1 || true
    pkill -9 -f "./bin/$svc" 2>/dev/null && killed=1 || true
    pkill -9 -f "bin/$svc" 2>/dev/null && killed=1 || true
    pkill -9 "$svc" 2>/dev/null && killed=1 || true
  done

  # Убиваем UI dev server
  pkill -9 -f "vite.*up.mt/asty/ui" 2>/dev/null && killed=1 || true
  pkill -9 -f "vite.*asty" 2>/dev/null && killed=1 || true

  # Убиваем процессы на известных портах (последняя мера)
  for port in 4222 4747 8222; do
    lsof -ti:"$port" 2>/dev/null | xargs -r kill -9 2>/dev/null && killed=1 || true
  done

  [[ $killed -eq 1 ]] && info "✓ осиротевшие процессы убраны" || true
}

# =============================================================================
# Статус
# =============================================================================
print_status() {
  echo ""
  echo -e "${GREEN}═══════════════════════════════════════${NC}"
  echo -e "${GREEN}  Asty dev-окружение запущено${NC}"
  echo -e "${GREEN}═══════════════════════════════════════${NC}"
  echo ""
  info "Asty UI:    http://localhost:4747"
  info "NATS:       http://localhost:8222"
  info "PostgreSQL: localhost:5432"
  info ""
  info "Логи сервера: tail -f /tmp/asty-dev-server.log"
  info "Логи ноды 1:  tail -f /tmp/asty-dev-node-1.log"
  echo ""
  info "Остановить: $SCRIPT_DIR/start.sh stop"
}

# =============================================================================
# Остановка
# =============================================================================
stop_all() {
  log "Остановка сервисов и ПОЛНАЯ ОЧИСТКА ресурсов..."

  # Asty processes
  if [[ -f "$PID_FILE" ]]; then
    while IFS= read -r pid; do
      kill -9 "$pid" 2>/dev/null && info "✓ PID $pid завершён (SIGKILL)" || true
    done < "$PID_FILE"
    rm -f "$PID_FILE"
  fi

  # Осиротевшие процессы (агрессивная очистка)
  cleanup_orphans

  # Дополнительная агрессивная очистка всех asty-процессов (защита от "грязных рук")
  log "Агрессивная очистка всех Asty/NATS процессов..."
  pkill -9 -f "asty" 2>/dev/null && info "✓ все asty процессы убиты" || true
  pkill -9 -f "gateway" 2>/dev/null && info "✓ все gateway процессы убиты" || true
  pkill -9 -f "xauth" 2>/dev/null && info "✓ все xauth процессы убиты" || true
  pkill -9 -f "xhttp" 2>/dev/null && info "✓ все xhttp процессы убиты" || true
  pkill -9 -f "xws" 2>/dev/null && info "✓ все xws процессы убиты" || true
  pkill -9 -f "vite.*up.mt/asty" 2>/dev/null && info "✓ UI dev server убит" || true

  # Освобождение портов (на случай зависших процессов)
  log "Освобождение портов..."
  for port in 3000 4222 4747 6222 8222 8080 8081 8082 8083 8084 8085; do
    lsof -ti:"$port" 2>/dev/null | xargs -r kill -9 2>/dev/null && info "✓ порт $port освобождён" || true
  done

  # Docker инфраструктура
  stop_infra

  # Временные данные
  log "Удаление временных данных..."
  rm -rf "$DATA_BASE"
  rm -rf /var/lib/asty 2>/dev/null || true
  rm -rf /tmp/asty* 2>/dev/null || true
  rm -f "$NATS_CONF_RENDERED"
  rm -f /tmp/asty-dev-*.log 2>/dev/null || true
  info "✓ временные файлы удалены"

  # Loopback-алиасы (macOS)
  teardown_loopback_aliases

  log "✓ ВСЁ ОСТАНОВЛЕНО И ВЫЧИЩЕНО"
}

# =============================================================================
# Точка входа
# =============================================================================
CMD="${1:-1}"

if [[ "$CMD" == "stop" ]]; then
  stop_all
  exit 0
fi

if ! [[ "$CMD" =~ ^[0-9]+$ ]] || [[ "$CMD" -lt 1 ]]; then
  die "Использование: $0 [NODES|stop]  (NODES ≥ 1, по умолчанию 1)"
fi

NODES="$CMD"

check_deps
cleanup_orphans

if [[ -f "$PID_FILE" ]]; then
  warn "Обнаружено запущенное окружение. Останавливаю перед повторным запуском..."
  stop_all
fi

start_infra "$NODES"
build_binaries

setup_loopback_aliases "$NODES"
wait_nats "$NODES"

start_asty "$NODES"
wait_asty

print_status
