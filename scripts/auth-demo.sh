#!/usr/bin/env bash
# Minimal auth demo: bootstrap user → login → call protected API.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
BIN="${BIN:-./dist/alpha-nexus}"
DATA_DIR="${DATA_DIR:-$(mktemp -d -t an-auth-demo-XXXXXX)}"
PORT="${PORT:-17322}"

mkdir -p dist
go build -o "$BIN" ./cmd/alpha-nexus-egolite/

echo "==> data-dir: $DATA_DIR"
"$BIN" --data-dir "$DATA_DIR" init >/dev/null
"$BIN" --data-dir "$DATA_DIR" auth bootstrap --username demo --password demo123
"$BIN" --data-dir "$DATA_DIR" auth status

lsof -ti:"$PORT" | xargs kill -9 2>/dev/null || true
"$BIN" --data-dir "$DATA_DIR" ui serve --port "$PORT" >/tmp/an-auth-demo-ui.log 2>&1 &
PID=$!
trap 'kill $PID 2>/dev/null || true' EXIT

for i in $(seq 1 30); do
  curl -sf "http://127.0.0.1:$PORT/api/auth/status" >/dev/null && break
  sleep 0.3
done

echo "==> status (public)"
curl -sf "http://127.0.0.1:$PORT/api/auth/status"
echo

echo "==> overview without token (expect 401)"
code=$(curl -s -o /tmp/an-auth-no-tok.json -w "%{http_code}" "http://127.0.0.1:$PORT/api/overview")
echo "HTTP $code $(cat /tmp/an-auth-no-tok.json)"
test "$code" = "401"

echo "==> login"
TOK=$(curl -sf -X POST "http://127.0.0.1:$PORT/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"demo","password":"demo123"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')
echo "token_len=${#TOK}"

echo "==> overview with token"
curl -sf "http://127.0.0.1:$PORT/api/overview" -H "Authorization: Bearer $TOK" | python3 -c 'import sys,json;d=json.load(sys.stdin);print("platforms",d.get("platforms"),"ok")'

echo "==> me"
curl -sf "http://127.0.0.1:$PORT/api/auth/me" -H "Authorization: Bearer $TOK"
echo
echo "AUTH_DEMO_OK"
echo "UI: http://127.0.0.1:$PORT  (login demo / demo123)"
echo "DATA_DIR=$DATA_DIR"
# keep server up a few seconds for manual open if KEEP=1
if [ "${KEEP:-}" = "1" ]; then
  trap - EXIT
  echo "Server left running pid=$PID port=$PORT"
fi
