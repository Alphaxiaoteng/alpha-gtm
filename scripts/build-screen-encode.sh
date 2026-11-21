#!/bin/bash
# Build the optional Rust screen filter and install it next to the app binary.
# The Go capture path works without it; with it, polled frames shrink ~10x.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CRATE="$ROOT/rust/screen-encode"
TARGET="${ALPHA_BIN_DIR:-$HOME/.alpha-nexus-egolite/bin}"

if ! command -v cargo >/dev/null 2>&1; then
    echo "cargo not found; the app will use full-size frames (no Rust filter)." >&2
    exit 0
fi

echo "Building screen-encode (release)..."
(cd "$CRATE" && cargo build --release)

mkdir -p "$TARGET"
install -m 0755 "$CRATE/target/release/screen-encode" "$TARGET/screen-encode"
echo "Installed: $TARGET/screen-encode"
"$TARGET/screen-encode" --help >/dev/null 2>&1 || true
