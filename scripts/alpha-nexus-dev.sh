#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DATA_DIR="${ALPHA_NEXUS_DATA_DIR:-${HOME}/.alpha-nexus}"
DB_PATH="${ALPHA_NEXUS_DB_PATH:-$DATA_DIR/app.db}"

usage() {
  cat <<'USAGE'
alpha-nexus-dev commands:
  init
  platform list
  db path

Environment:
  ALPHA_NEXUS_DATA_DIR=/path/to/data
  ALPHA_NEXUS_DB_PATH=/path/to/app.db
USAGE
}

ensure_db() {
  mkdir -p "$DATA_DIR"
  sqlite3 "$DB_PATH" < "$ROOT_DIR/migrations/001_init.sql"
}

cmd="${1:-}"
sub="${2:-}"

case "$cmd $sub" in
  "init ")
    ensure_db
    printf '%s\n' "$DB_PATH"
    ;;
  "platform list")
    ensure_db
    sqlite3 -header -column "$DB_PATH" "SELECT code,name,enabled,login_url FROM platforms ORDER BY sort;"
    ;;
  "db path")
    printf '%s\n' "$DB_PATH"
    ;;
  *)
    usage
    exit 1
    ;;
esac
