#!/usr/bin/env bash
# install-git-hooks.sh — point this repo at versioned hooks under scripts/git-hooks.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

HOOK_DIR="scripts/git-hooks"
if [[ ! -d "$HOOK_DIR" ]]; then
  echo "install-git-hooks: missing $HOOK_DIR" >&2
  exit 1
fi

chmod +x "$HOOK_DIR"/* 2>/dev/null || true
chmod +x scripts/auto-commit.sh 2>/dev/null || true

git config --local core.hooksPath "$HOOK_DIR"
echo "install-git-hooks: core.hooksPath=$(git config --local --get core.hooksPath)"
echo "install-git-hooks: active hooks:"
ls -1 "$HOOK_DIR"
echo
echo "Use ./scripts/auto-commit.sh to stage + commit with an informative message."
echo "Use ./scripts/auto-commit.sh --push to commit and push the current branch."
