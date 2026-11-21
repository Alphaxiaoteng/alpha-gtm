#!/usr/bin/env bash
# Build Go binary if needed, install e2e deps, run Playwright.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "==> ensure Go binary"
mkdir -p dist
# Keep E2E independent from the universal release binary produced by build.sh.
go build -o ./dist/alpha-nexus-e2e ./cmd/alpha-nexus-egolite/

echo "==> ensure e2e deps"
cd e2e
if [[ ! -d node_modules/@playwright/test ]]; then
  pnpm install
  pnpm exec playwright install chromium
fi

echo "==> playwright test"
E2E_NODE="${ALPHA_NEXUS_E2E_NODE:-$(command -v node)}"
if ! "$E2E_NODE" "$ROOT/ui-web/node_modules/vite/bin/vite.js" --version >/dev/null 2>&1; then
  CODEX_NODE="$HOME/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node"
  if [[ -x "$CODEX_NODE" ]] && "$CODEX_NODE" "$ROOT/ui-web/node_modules/vite/bin/vite.js" --version >/dev/null 2>&1; then
    E2E_NODE="$CODEX_NODE"
  else
    echo "当前 Node 无法加载 Vite 原生模块；请设置 ALPHA_NEXUS_E2E_NODE 指向兼容的 Node。" >&2
    exit 1
  fi
fi
export ALPHA_NEXUS_E2E_NODE="$E2E_NODE"
if [[ -z "${ALPHA_NEXUS_E2E_API_PORT:-}" ]]; then
  ALPHA_NEXUS_E2E_API_PORT="$("$E2E_NODE" -e 'const net=require("node:net");const s=net.createServer();s.listen(0,"127.0.0.1",()=>{process.stdout.write(String(s.address().port));s.close()})')"
  export ALPHA_NEXUS_E2E_API_PORT
fi
if [[ -z "${ALPHA_NEXUS_E2E_WEB_PORT:-}" ]]; then
  ALPHA_NEXUS_E2E_WEB_PORT="$("$E2E_NODE" -e 'const net=require("node:net");const s=net.createServer();s.listen(0,"127.0.0.1",()=>{process.stdout.write(String(s.address().port));s.close()})')"
  export ALPHA_NEXUS_E2E_WEB_PORT
fi
"$E2E_NODE" ./node_modules/@playwright/test/cli.js test "$@"
