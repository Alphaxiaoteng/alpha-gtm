#!/usr/bin/env bash
# Build the Alpha-Nexus desktop shell (Tauri 2) end to end:
#   1. build ui-web
#   2. build the Go control plane binary
#   3. copy the binary into src-tauri/resources/
#   4. cargo tauri build
#
# Usage:
#   ./desktop/build-desktop.sh                       # host target
#   ./desktop/build-desktop.sh aarch64-apple-darwin  # explicit Rust triple
#   ./desktop/build-desktop.sh universal-apple-darwin
#
# This script writes only inside desktop/ (plus ui-web/dist, which Vite owns).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DESKTOP="$ROOT/desktop"
TAURI_DIR="$DESKTOP/src-tauri"
RES_DIR="$TAURI_DIR/resources"
UI_BUILD_DIR="$DESKTOP/build/ui-web-dist"
BIN_NAME="alpha-nexus-egolite"
TARGET="${1:-}"

VERSION="$(sed -n 's/^version = "\([^"]*\)".*/\1/p' "$TAURI_DIR/Cargo.toml" | head -1)"
BUILD_DATE="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
GIT_COMMIT="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
LDFLAGS="-s -w -X 'alpha-nexus-egolite/internal/cli.Version=${VERSION:-0.0.0}' -X 'alpha-nexus-egolite/internal/cli.BuildDate=${BUILD_DATE}' -X 'alpha-nexus-egolite/internal/cli.GitCommit=${GIT_COMMIT}'"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[!]\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31m[x]\033[0m %s\n' "$*" >&2; exit 1; }

# --- prerequisites -----------------------------------------------------------
command -v go >/dev/null 2>&1 || die "go not found in PATH"
command -v npm >/dev/null 2>&1 || die "npm not found in PATH"
command -v cargo >/dev/null 2>&1 || die "cargo not found in PATH (install rustup)"

if ! cargo tauri --version >/dev/null 2>&1; then
  warn "The Tauri CLI (cargo-tauri 2.x) is not installed, so packaging cannot run."
  cat >&2 <<'EOF'

Install it once, then re-run this script:

    cargo install tauri-cli --version "^2" --locked

(Only the packaging step needs it; `cargo check` / `cargo build` in
desktop/src-tauri work without the CLI.)
EOF
  exit 1
fi

# --- 1. ui-web ---------------------------------------------------------------
log "Building ui-web (Vite) -> ${UI_BUILD_DIR#"$ROOT"/}"
if [ ! -d "$ROOT/ui-web/node_modules" ]; then
  log "ui-web/node_modules missing; running npm ci"
  ( cd "$ROOT/ui-web" && npm ci )
fi
( cd "$ROOT/ui-web" && npm run build -- --outDir "$UI_BUILD_DIR" --emptyOutDir )

# The Go binary embeds internal/ui/dist (//go:embed all:dist). That directory is
# tracked in git and this script must not rewrite it, so verify freshness and
# tell the operator how to refresh it when it differs.
if [ -f "$ROOT/internal/ui/dist/index.html" ] && [ -f "$UI_BUILD_DIR/index.html" ]; then
  if ! cmp -s "$ROOT/internal/ui/dist/index.html" "$UI_BUILD_DIR/index.html"; then
    warn "internal/ui/dist/index.html differs from the freshly built ui-web/dist."
    warn "The packaged Go binary will serve the committed (older) UI. To refresh it:"
    warn "    (cd ui-web && npm run build) && rm -rf internal/ui/dist && cp -r ui-web/dist internal/ui/dist"
  fi
fi

# --- 2. Go control plane -----------------------------------------------------
goos="$(go env GOOS)"
goarch="$(go env GOARCH)"
case "$TARGET" in
  *windows*)                     goos=windows; goarch=amd64 ;;
  aarch64-apple-darwin)          goos=darwin;  goarch=arm64 ;;
  x86_64-apple-darwin)           goos=darwin;  goarch=amd64 ;;
  universal-apple-darwin)        goos=darwin;  goarch=universal ;;
  *-unknown-linux-gnu)           goos=linux;   goarch=amd64 ;;
  aarch64-unknown-linux-gnu)     goos=linux;   goarch=arm64 ;;
  "" ) ;;
  *) warn "Unknown Rust triple '$TARGET'; building the sidecar for host ${goos}/${goarch}" ;;
esac

build_sidecar() { # $1=GOOS $2=GOARCH $3=output
  log "go build ${1}/${2} -> ${3#"$ROOT"/}"
  ( cd "$ROOT" && GOOS="$1" GOARCH="$2" CGO_ENABLED=0 \
      go build -ldflags="$LDFLAGS" -trimpath -o "$3" ./cmd/alpha-nexus-egolite )
}

ext=""
[ "$goos" = "windows" ] && ext=".exe"
out="$RES_DIR/${BIN_NAME}${ext}"

if [ "$goarch" = "universal" ]; then
  build_sidecar darwin arm64 "$RES_DIR/${BIN_NAME}-arm64"
  build_sidecar darwin amd64 "$RES_DIR/${BIN_NAME}-amd64"
  log "lipo -create -> ${out#"$ROOT"/}"
  lipo -create -output "$out" "$RES_DIR/${BIN_NAME}-arm64" "$RES_DIR/${BIN_NAME}-amd64"
  rm -f "$RES_DIR/${BIN_NAME}-arm64" "$RES_DIR/${BIN_NAME}-amd64"
else
  build_sidecar "$goos" "$goarch" "$out"
fi
chmod +x "$out" 2>/dev/null || true

if [ "$goos" = "windows" ]; then
  warn "Cross-compiling the Go sidecar for Windows currently fails (syscall.Flock /"
  warn "syscall.Setpgid / syscall.Kill are Unix-only). Build on Windows instead."
fi

# --- 3. packaging ------------------------------------------------------------
if [ -n "$TARGET" ]; then
  tauri_args=(build --target "$TARGET")
else
  tauri_args=(build)
fi

log "cargo tauri ${tauri_args[*]} (in desktop/src-tauri)"
( cd "$TAURI_DIR" && cargo tauri "${tauri_args[@]}" )

log "Bundles:"
find "$TAURI_DIR/target" -maxdepth 4 -type d \( -name bundle -o -name dmg -o -name macos -o -name nsis -o -name msi \) 2>/dev/null | sed "s|$ROOT/|  |"
