#!/usr/bin/env bash
set -euo pipefail

MODE="run"
PROBE_EMP_NO="${PROBE_EMP_NO:-}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-120}"
POLL_SECONDS="${POLL_SECONDS:-2}"
KEEP_PROBE="false"

MYSQL_CONTAINER="${MYSQL_CONTAINER:-db-stream-mysql}"
CLICKHOUSE_CONTAINER="${CLICKHOUSE_CONTAINER:-db-stream-clickhouse}"
SOURCE_CONNECT_URL="${SOURCE_CONNECT_URL:-http://localhost:8083}"
SINK_CONNECT_URL="${SINK_CONNECT_URL:-http://localhost:8084}"
SOURCE_CONNECTOR="${SOURCE_CONNECTOR:-mysql-employee-connector}"
SINK_CONNECTOR="${SINK_CONNECTOR:-mysql-sink}"

MYSQL_DATABASE="${MYSQL_DATABASE:-employee}"
MYSQL_USER="${MYSQL_USER:-db_stream}"
MYSQL_PASSWORD="${MYSQL_PASSWORD:-db_stream_password}"

CLICKHOUSE_DATABASE="${CLICKHOUSE_DATABASE:-mysql_employee_mirror}"
CLICKHOUSE_TABLE="${CLICKHOUSE_TABLE:-employee}"

PROBE_FIRST_NAME="${PROBE_FIRST_NAME:-CDCHEALTH}"
PROBE_LAST_NAME="${PROBE_LAST_NAME:-PROBE}"
PROBE_GENDER="${PROBE_GENDER:-M}"
PROBE_BIRTH_DATE="${PROBE_BIRTH_DATE:-1990-01-01}"
PROBE_HIRE_DATE="${PROBE_HIRE_DATE:-2020-01-01}"

STATE_FILE="${STATE_FILE:-/tmp/db-stream-healthcheck/mysql_clickhouse_probe.env}"
PROBE_INSERTED="false"

usage() {
  cat <<'USAGE'
Usage:
  scripts/mysql_clickhouse_healthcheck.sh [run|scaffold|verify|cleanup] [options]

Modes:
  run         Scaffold -> verify ClickHouse ingestion -> unscaffold (default)
  scaffold    Insert probe row into MySQL and persist probe state
  verify      Verify probe row exists in ClickHouse (requires --probe-emp-no or prior scaffold state)
  cleanup     Remove probe row from MySQL and ClickHouse (requires --probe-emp-no or prior scaffold state)

Options:
  --probe-emp-no <int>      Probe employee number (auto-generated if omitted for run/scaffold)
  --timeout-seconds <int>   Max seconds for wait loops (default: 120)
  --poll-seconds <int>      Poll interval in seconds (default: 2)
  --keep-probe              Keep probe data after run (skip cleanup)
  --help                    Show this help

Environment overrides:
  MYSQL_CONTAINER, CLICKHOUSE_CONTAINER
  SOURCE_CONNECT_URL, SINK_CONNECT_URL
  SOURCE_CONNECTOR, SINK_CONNECTOR
  MYSQL_DATABASE, MYSQL_USER, MYSQL_PASSWORD
  CLICKHOUSE_DATABASE, CLICKHOUSE_TABLE
  STATE_FILE
USAGE
}

log() {
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"
}

warn() {
  printf '[%s] WARN: %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >&2
}

die() {
  printf '[%s] ERROR: %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >&2
  exit 1
}

ensure_command() {
  command -v "$1" >/dev/null 2>&1 || die "Required command not found: $1"
}

ensure_positive_int() {
  local value="$1"
  local label="$2"
  [[ "$value" =~ ^[0-9]+$ ]] || die "$label must be a positive integer"
}

container_is_running() {
  local container="$1"
  local running
  running="$(docker inspect -f '{{.State.Running}}' "$container" 2>/dev/null || true)"
  [[ "$running" == "true" ]]
}

connector_running() {
  local connect_url="$1"
  local connector_name="$2"
  local status_body

  status_body="$(curl -fsS "${connect_url}/connectors/${connector_name}/status" 2>/dev/null || true)"
  [[ -n "$status_body" ]] || return 1

  if command -v jq >/dev/null 2>&1; then
    local connector_state task_total task_running
    connector_state="$(printf '%s' "$status_body" | jq -r '.connector.state // empty')"
    task_total="$(printf '%s' "$status_body" | jq -r '[.tasks[]?] | length')"
    task_running="$(printf '%s' "$status_body" | jq -r '[.tasks[]? | select(.state == "RUNNING")] | length')"
    [[ "$connector_state" == "RUNNING" && "$task_total" -gt 0 && "$task_running" -eq "$task_total" ]]
    return
  fi

  local compact
  compact="$(printf '%s' "$status_body" | tr -d '[:space:]')"
  [[ "$compact" == *'"connector":{"state":"RUNNING"'* ]] && [[ "$compact" == *'"tasks":[{"id":0,"state":"RUNNING"'* ]]
}

wait_for_connector() {
  local connect_url="$1"
  local connector_name="$2"
  local label="$3"
  local start now

  start="$(date +%s)"
  while true; do
    if connector_running "$connect_url" "$connector_name"; then
      return 0
    fi

    now="$(date +%s)"
    if (( now - start >= TIMEOUT_SECONDS )); then
      die "Timed out waiting for ${label} connector to be RUNNING: ${connector_name}"
    fi

    sleep "$POLL_SECONDS"
  done
}

run_mysql_query() {
  local query="$1"
  docker exec "$MYSQL_CONTAINER" mysql "-u${MYSQL_USER}" "-p${MYSQL_PASSWORD}" -N -e "$query"
}

run_clickhouse_query() {
  local query="$1"
  docker exec "$CLICKHOUSE_CONTAINER" clickhouse-client -q "$query"
}

write_state() {
  mkdir -p "$(dirname "$STATE_FILE")"
  cat > "$STATE_FILE" <<STATE
PROBE_EMP_NO=${PROBE_EMP_NO}
MYSQL_DATABASE=${MYSQL_DATABASE}
CLICKHOUSE_DATABASE=${CLICKHOUSE_DATABASE}
CLICKHOUSE_TABLE=${CLICKHOUSE_TABLE}
STATE
}

load_state_if_exists() {
  if [[ -z "$PROBE_EMP_NO" && -f "$STATE_FILE" ]]; then
    # shellcheck disable=SC1090
    source "$STATE_FILE"
    PROBE_EMP_NO="${PROBE_EMP_NO:-}"
  fi
}

clear_state() {
  rm -f "$STATE_FILE"
}

generate_probe_emp_no() {
  local epoch pid
  epoch="$(date +%s)"
  pid="$$"
  printf '%d' $(( 900000 + ((epoch + pid) % 90000) ))
}

preflight() {
  ensure_command docker
  ensure_command curl

  ensure_positive_int "$TIMEOUT_SECONDS" "timeout-seconds"
  ensure_positive_int "$POLL_SECONDS" "poll-seconds"

  container_is_running "$MYSQL_CONTAINER" || die "Container is not running: $MYSQL_CONTAINER"
  container_is_running "$CLICKHOUSE_CONTAINER" || die "Container is not running: $CLICKHOUSE_CONTAINER"
}

wait_for_connectors() {
  log "Waiting for source connector: ${SOURCE_CONNECTOR}"
  wait_for_connector "$SOURCE_CONNECT_URL" "$SOURCE_CONNECTOR" "source"

  log "Waiting for sink connector: ${SINK_CONNECTOR}"
  wait_for_connector "$SINK_CONNECT_URL" "$SINK_CONNECTOR" "sink"
}

prepare_clickhouse() {
  run_clickhouse_query "CREATE DATABASE IF NOT EXISTS ${CLICKHOUSE_DATABASE}"
}

scaffold_probe() {
  if [[ -z "$PROBE_EMP_NO" ]]; then
    PROBE_EMP_NO="$(generate_probe_emp_no)"
  fi

  ensure_positive_int "$PROBE_EMP_NO" "probe-emp-no"
  prepare_clickhouse

  log "Cleaning stale probe row for emp_no=${PROBE_EMP_NO}"
  run_mysql_query "DELETE FROM ${MYSQL_DATABASE}.employee WHERE emp_no = ${PROBE_EMP_NO}" || true
  run_clickhouse_query "ALTER TABLE ${CLICKHOUSE_DATABASE}.${CLICKHOUSE_TABLE} DELETE WHERE emp_no = ${PROBE_EMP_NO}" || true

  log "Scaffolding probe row in MySQL emp_no=${PROBE_EMP_NO}"
  run_mysql_query "INSERT INTO ${MYSQL_DATABASE}.employee (emp_no, birth_date, first_name, last_name, gender, hire_date) VALUES (${PROBE_EMP_NO}, '${PROBE_BIRTH_DATE}', '${PROBE_FIRST_NAME}', '${PROBE_LAST_NAME}', '${PROBE_GENDER}', '${PROBE_HIRE_DATE}')"

  PROBE_INSERTED="true"
  write_state
  log "Scaffold complete. Probe state: ${STATE_FILE}"
}

verify_probe() {
  [[ -n "$PROBE_EMP_NO" ]] || die "probe-emp-no is required for verify (or run scaffold first)"

  log "Verifying ClickHouse ingestion for emp_no=${PROBE_EMP_NO}"
  local start now row_count
  start="$(date +%s)"

  while true; do
    row_count="$(run_clickhouse_query "SELECT count() FROM ${CLICKHOUSE_DATABASE}.${CLICKHOUSE_TABLE} WHERE emp_no = ${PROBE_EMP_NO}" 2>/dev/null || echo 0)"
    if [[ "$row_count" =~ ^[0-9]+$ ]] && (( row_count > 0 )); then
      log "PASS: probe row present in ClickHouse (count=${row_count})"
      return 0
    fi

    now="$(date +%s)"
    if (( now - start >= TIMEOUT_SECONDS )); then
      die "Timed out waiting for probe row in ClickHouse for emp_no=${PROBE_EMP_NO}"
    fi

    sleep "$POLL_SECONDS"
  done
}

cleanup_probe() {
  [[ -n "$PROBE_EMP_NO" ]] || die "probe-emp-no is required for cleanup (or run scaffold first)"

  log "Unscaffolding probe row for emp_no=${PROBE_EMP_NO}"
  run_mysql_query "DELETE FROM ${MYSQL_DATABASE}.employee WHERE emp_no = ${PROBE_EMP_NO}" || warn "MySQL cleanup failed"
  run_clickhouse_query "ALTER TABLE ${CLICKHOUSE_DATABASE}.${CLICKHOUSE_TABLE} DELETE WHERE emp_no = ${PROBE_EMP_NO}" || warn "ClickHouse cleanup failed"

  PROBE_INSERTED="false"
  clear_state
  log "Cleanup complete"
}

cleanup_on_exit() {
  if [[ "$MODE" == "run" && "$KEEP_PROBE" != "true" && "$PROBE_INSERTED" == "true" ]]; then
    warn "Run interrupted. Attempting automatic cleanup for emp_no=${PROBE_EMP_NO}"
    cleanup_probe || warn "Automatic cleanup failed; run cleanup manually"
  fi
}

parse_args() {
  if [[ $# -gt 0 ]]; then
    case "$1" in
      run|scaffold|verify|cleanup)
        MODE="$1"
        shift
        ;;
    esac
  fi

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --probe-emp-no)
        PROBE_EMP_NO="$2"
        shift 2
        ;;
      --timeout-seconds)
        TIMEOUT_SECONDS="$2"
        shift 2
        ;;
      --poll-seconds)
        POLL_SECONDS="$2"
        shift 2
        ;;
      --keep-probe)
        KEEP_PROBE="true"
        shift
        ;;
      --help|-h)
        usage
        exit 0
        ;;
      *)
        die "Unknown argument: $1"
        ;;
    esac
  done
}

main() {
  parse_args "$@"
  preflight
  load_state_if_exists

  case "$MODE" in
    scaffold)
      wait_for_connectors
      scaffold_probe
      ;;
    verify)
      wait_for_connectors
      verify_probe
      ;;
    cleanup)
      cleanup_probe
      ;;
    run)
      trap cleanup_on_exit EXIT
      wait_for_connectors
      scaffold_probe
      verify_probe

      if [[ "$KEEP_PROBE" == "true" ]]; then
        log "--keep-probe set. Skipping cleanup. Run cleanup mode later."
      else
        cleanup_probe
      fi

      log "END-TO-END HEALTHCHECK PASS"
      ;;
    *)
      die "Unsupported mode: ${MODE}"
      ;;
  esac
}

main "$@"
