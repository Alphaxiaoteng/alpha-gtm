#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DATA_DIR="${ALPHA_NEXUS_DATA_DIR:-${HOME}/.alpha-nexus}"
DB_PATH="${ALPHA_NEXUS_DB_PATH:-$DATA_DIR/app.db}"

mkdir -p "$DATA_DIR"

# Apply all numbered migrations in order (empty DB bootstrap parity with db.Migrate).
for f in "$ROOT_DIR"/migrations/*.sql; do
  [ -f "$f" ] || continue
  sqlite3 "$DB_PATH" < "$f"
done

printf '%s\n' "$DB_PATH"
