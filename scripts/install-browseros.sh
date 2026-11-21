#!/usr/bin/env bash
# Install BrowserOS (https://github.com/browseros-ai/BrowserOS) and wire Alpha-Nexus config.
set -euo pipefail

DMG_URL="${BROWSEROS_DMG_URL:-https://files.browseros.com/download/BrowserOS.dmg}"
TMP="${TMPDIR:-/tmp}/browseros-install-$$"
mkdir -p "$TMP" "$HOME/Applications" "$HOME/.alpha-nexus"
cd "$TMP"

echo "==> Downloading BrowserOS.dmg from $DMG_URL"
curl -fL --retry 5 --retry-delay 3 --connect-timeout 30 --max-time 1800 \
  -C - -o BrowserOS.dmg "$DMG_URL"
ls -lh BrowserOS.dmg

echo "==> Mounting"
OUT=$(hdiutil attach -nobrowse -readonly BrowserOS.dmg)
MOUNT=$(echo "$OUT" | grep -o '/Volumes/[^ ]*' | head -1)
APP=$(find "$MOUNT" -maxdepth 2 -name "*.app" | head -1)
test -n "$APP"
echo "    found $APP"

echo "==> Installing to ~/Applications and /Applications (if writable)"
NAME=$(basename "$APP")
ditto "$APP" "$HOME/Applications/$NAME"
xattr -dr com.apple.quarantine "$HOME/Applications/$NAME" 2>/dev/null || true
if [ -w /Applications ]; then
  ditto "$APP" "/Applications/$NAME"
  xattr -dr com.apple.quarantine "/Applications/$NAME" 2>/dev/null || true
fi
hdiutil detach "$MOUNT" -quiet || hdiutil detach "$MOUNT" || true

BIN=""
for c in \
  "/Applications/BrowserOS.app/Contents/MacOS/BrowserOS" \
  "$HOME/Applications/BrowserOS.app/Contents/MacOS/BrowserOS"; do
  if [ -x "$c" ]; then BIN="$c"; break; fi
done
if [ -z "$BIN" ]; then
  echo "ERROR: BrowserOS binary not found after install" >&2
  exit 1
fi
echo "==> Binary: $BIN"

# CDP smoke
PORT=15176
PROF="$TMP/cdp-profile"
mkdir -p "$PROF"
"$BIN" --remote-debugging-port=$PORT --remote-debugging-address=127.0.0.1 \
  --user-data-dir="$PROF" --no-first-run --no-default-browser-check about:blank \
  >/dev/null 2>&1 &
BPID=$!
ok=0
for _ in $(seq 1 40); do
  if curl -sf --max-time 1 "http://127.0.0.1:$PORT/json/version" >/dev/null; then ok=1; break; fi
  sleep 0.5
done
kill "$BPID" 2>/dev/null || true
pkill -f "user-data-dir=$PROF" 2>/dev/null || true
if [ "$ok" != 1 ]; then
  echo "WARN: CDP smoke failed (binary installed; check Gatekeeper)" >&2
else
  echo "==> CDP smoke OK on 127.0.0.1:$PORT"
fi

# Merge into ~/.alpha-nexus/config.json
CFG="$HOME/.alpha-nexus/config.json"
python3 - "$BIN" "$CFG" <<'PY'
import json, os, sys
bin_path, cfg_path = sys.argv[1], sys.argv[2]
cfg = {}
if os.path.exists(cfg_path):
    try:
        cfg = json.load(open(cfg_path))
    except Exception:
        cfg = {}
cfg["browseros_path"] = bin_path
cfg.setdefault("browser_engine", "fingerprint-chromium")
os.makedirs(os.path.dirname(cfg_path), exist_ok=True)
json.dump(cfg, open(cfg_path, "w"), indent=2)
open(cfg_path, "a").write("\n")
print("==> Wrote", cfg_path)
print(json.dumps(cfg, indent=2))
PY

echo ""
echo "Done. Use with Alpha-Nexus:"
echo "  alpha-nexus tools call browser.start --args '{\"account_id\":1,\"engine\":\"browseros\"}'"
echo "  # or set browser_profiles.browser_type = browseros"
echo "Docs: docs/BROWSER_ENGINES.md"
rm -rf "$TMP"
