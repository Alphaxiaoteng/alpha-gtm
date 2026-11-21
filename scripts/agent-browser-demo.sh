#!/usr/bin/env bash
# Demo: start Chromium with CDP, run BrowserAgent with URL goal (no model service required).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
BIN="${BIN:-./dist/alpha-nexus}"
DATA_DIR="${DATA_DIR:-$(mktemp -d -t an-browser-agent-XXXXXX)}"
PORT="${CDP_PORT:-15155}"

mkdir -p dist
go build -o "$BIN" ./cmd/alpha-nexus-egolite/

echo "==> data-dir $DATA_DIR"
"$BIN" --data-dir "$DATA_DIR" init >/dev/null
"$BIN" --data-dir "$DATA_DIR" account add --platform xhs --name agent-demo --group demo-brand
ACC=$("$BIN" --data-dir "$DATA_DIR" account list | awk 'NR==2{print $1}')
echo "account=$ACC"

# Prefer system Chromium/Chrome for CDP smoke if fingerprint binary missing
BROWSER="${BROWSER_BIN:-}"
if [ -z "$BROWSER" ]; then
  for c in \
    "/Applications/Chromium.app/Contents/MacOS/Chromium" \
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
  do
    if [ -x "$c" ]; then BROWSER="$c"; break; fi
  done
fi

if [ -z "$BROWSER" ]; then
  echo "SKIP live browser: no Chromium/Chrome found; unit path only"
  "$BIN" --data-dir "$DATA_DIR" agent browser --cdp-url "http://127.0.0.1:1" --goal "https://example.com" --max-steps 1 && exit 1 || true
  echo "AGENT_BROWSER_DEMO_PARTIAL_OK (no local browser binary)"
  exit 0
fi

USER_DATA="$DATA_DIR/chrome-smoke"
mkdir -p "$USER_DATA"
# free port
lsof -ti:"$PORT" | xargs kill -9 2>/dev/null || true
"$BROWSER" --remote-debugging-port="$PORT" --user-data-dir="$USER_DATA" \
  --no-first-run --disable-default-apps about:blank >/tmp/an-chrome-smoke.log 2>&1 &
CHROME_PID=$!
trap 'kill $CHROME_PID 2>/dev/null || true' EXIT

# wait CDP
for i in $(seq 1 40); do
  if curl -sf "http://127.0.0.1:$PORT/json/version" >/dev/null; then
    echo "CDP ready :$PORT"
    break
  fi
  sleep 0.25
done

echo "==> agent browser navigate example.com"
"$BIN" --data-dir "$DATA_DIR" agent browser \
  --cdp-url "http://127.0.0.1:$PORT" \
  --goal "https://example.com" \
  --max-steps 3 | tee /tmp/an-agent-browser-out.json

grep -q '"agent": "BrowserAgent"' /tmp/an-agent-browser-out.json
echo "AGENT_BROWSER_DEMO_OK"
