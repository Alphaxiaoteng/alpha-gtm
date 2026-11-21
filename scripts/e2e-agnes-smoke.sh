#!/usr/bin/env bash
set -euo pipefail

if [[ "${ALPHA_NEXUS_REAL_AGNES_SMOKE:-}" != "1" ]]; then
  echo "Refusing real Agnes calls. Re-run with ALPHA_NEXUS_REAL_AGNES_SMOKE=1 after explicit approval." >&2
  exit 2
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
exec "$SCRIPT_DIR/e2e-run.sh" --config playwright.agnes.config.ts "$@"
