#!/usr/bin/env sh
# Install Alpha-Nexus MCP registration for Claude Code / OpenCode.
# Modeled after https://github.com/xueyc1f/turbopush-mcp install.sh
set -eu

BIN="${ALPHA_NEXUS_BIN:-}"
if [ -z "$BIN" ]; then
  if command -v alpha-nexus >/dev/null 2>&1; then
    BIN="$(command -v alpha-nexus)"
  elif [ -x "$HOME/.local/bin/alpha-nexus" ]; then
    BIN="$HOME/.local/bin/alpha-nexus"
  else
    echo "alpha-nexus not found on PATH. Build first: go build -o ~/.local/bin/alpha-nexus ./cmd/alpha-nexus-egolite" >&2
    exit 1
  fi
fi

echo "Using binary: $BIN"

if [ "${SKIP_MCP_ADD:-0}" != "1" ] && command -v claude >/dev/null 2>&1; then
  # remove old entry if present, then add
  claude mcp remove alpha-nexus 2>/dev/null || true
  claude mcp add alpha-nexus -- "$BIN" agent mcp-serve
  echo "Registered with Claude Code: alpha-nexus"
fi

if [ "${SKIP_MCP_ADD:-0}" != "1" ] && command -v opencode >/dev/null 2>&1; then
  opencode mcp add alpha-nexus -- "$BIN" agent mcp-serve 2>/dev/null || \
    echo "Note: run manually: opencode mcp add alpha-nexus -- $BIN agent mcp-serve"
fi

# write client snippet next to data dir
DATA_DIR="${ALPHA_NEXUS_DATA_DIR:-$HOME/.alpha-nexus}"
mkdir -p "$DATA_DIR/agent"
cat > "$DATA_DIR/agent/mcp-client.json" <<EOF
{
  "mcpServers": {
    "alpha-nexus": {
      "command": "$BIN",
      "args": ["agent", "mcp-serve"],
      "env": {
        "ALPHA_NEXUS_DATA_DIR": "$DATA_DIR"
      }
    }
  }
}
EOF
echo "Wrote $DATA_DIR/agent/mcp-client.json"
echo "Done. Restart Claude Code / OpenCode to load MCP tools."
