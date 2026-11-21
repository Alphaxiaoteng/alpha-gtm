#!/usr/bin/env bash
# auto-commit.sh — stage tracked project files and create an informative commit.
#
# Usage:
#   ./scripts/auto-commit.sh                    # auto message from diff
#   ./scripts/auto-commit.sh -m "feat: ..."     # explicit subject (+ auto body)
#   ./scripts/auto-commit.sh --dry-run          # print message only
#   ./scripts/auto-commit.sh --push             # commit then push current branch
#
# Safety:
#   - never force-push
#   - never add secrets, runtime data, or gitignored paths
#   - no-op (exit 0) when worktree is clean
#   - commit-msg hook still validates the final message
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SUBJECT=""
DRY_RUN=0
DO_PUSH=0
EXTRA_BODY=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -m|--message)
      SUBJECT="${2:-}"
      shift 2
      ;;
    --body)
      EXTRA_BODY="${2:-}"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --push)
      DO_PUSH=1
      shift
      ;;
    -h|--help)
      sed -n '2,20p' "$0"
      exit 0
      ;;
    *)
      echo "unknown arg: $1" >&2
      exit 2
      ;;
  esac
done

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "auto-commit: not a git repository" >&2
  exit 1
fi

# Ensure hooks are installed so commit-msg validation always runs.
if [[ "$(git config --local --get core.hooksPath 2>/dev/null || true)" != "scripts/git-hooks" ]]; then
  if [[ -x "$ROOT/scripts/install-git-hooks.sh" ]]; then
    "$ROOT/scripts/install-git-hooks.sh" >/dev/null
  fi
fi

# Stage everything except ignored paths. Never touch .git internals.
git add -A

# Explicitly unstage known local-only trees if they ever slip past gitignore.
for path in .e2e-data .alpha-nexus-run .alpha-nexus release dist; do
  git reset -q -- "$path" 2>/dev/null || true
done

if git diff --cached --quiet && git diff --quiet; then
  # Also handle untracked that were not staged (should be none after add -A of non-ignored).
  if [[ -z "$(git status --porcelain)" ]]; then
    echo "auto-commit: clean worktree — nothing to commit"
    exit 0
  fi
fi

if git diff --cached --quiet; then
  echo "auto-commit: nothing staged after filters" >&2
  git status --short
  exit 1
fi

stat_summary="$(git diff --cached --stat | tail -n 20)"
name_status="$(git diff --cached --name-status | head -n 80)"
file_count="$(git diff --cached --name-only | wc -l | tr -d ' ')"
changed_paths="$(git diff --cached --name-only)"

# Infer conventional type/scope from paths.
type="chore"
scope=""
if printf '%s\n' "$changed_paths" | grep -qE '^internal/(publish|platforms|publishguard)/'; then
  type="feat"; scope="publish"
elif printf '%s\n' "$changed_paths" | grep -qE '^internal/(tools|agents|mcp|fleet)/'; then
  type="feat"; scope="tools"
elif printf '%s\n' "$changed_paths" | grep -qE '^internal/studio/|^ui-web/src/components/Studio'; then
  type="feat"; scope="studio"
elif printf '%s\n' "$changed_paths" | grep -qE '^internal/browser/'; then
  type="feat"; scope="browser"
elif printf '%s\n' "$changed_paths" | grep -qE '^ui-web/|^internal/ui/'; then
  type="feat"; scope="ui"
elif printf '%s\n' "$changed_paths" | grep -qE '^electron/'; then
  type="feat"; scope="electron"
elif printf '%s\n' "$changed_paths" | grep -qE '^docs/'; then
  type="docs"; scope="docs"
elif printf '%s\n' "$changed_paths" | grep -qE '^scripts/git-hooks/|^scripts/auto-commit|^scripts/install-git-hooks'; then
  type="chore"; scope="hooks"
elif printf '%s\n' "$changed_paths" | grep -qE '_test\.go$|\.spec\.ts$'; then
  type="test"; scope="tests"
elif printf '%s\n' "$changed_paths" | grep -qE '^migrations/'; then
  type="feat"; scope="db"
fi

top_dirs="$(printf '%s\n' "$changed_paths" | awk -F/ 'NF{print $1}' | sort | uniq -c | sort -nr | head -n 5 | awk '{print $2}' | paste -sd',' -)"

if [[ -z "$SUBJECT" ]]; then
  if [[ -n "$scope" ]]; then
    SUBJECT="${type}(${scope}): auto-commit ${file_count} files (${top_dirs})"
  else
    SUBJECT="${type}: auto-commit ${file_count} files (${top_dirs})"
  fi
fi

# Keep subject within hook limit.
if [[ ${#SUBJECT} -gt 100 ]]; then
  SUBJECT="${SUBJECT:0:97}..."
fi

BODY="$(cat <<EOF
Summary
- files: ${file_count}
- top paths: ${top_dirs}

Changed (name-status, capped):
${name_status}

Diffstat:
${stat_summary}
EOF
)"

if [[ -n "$EXTRA_BODY" ]]; then
  BODY="${EXTRA_BODY}

${BODY}"
fi

MESSAGE="${SUBJECT}

${BODY}"

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "----- commit message (dry-run) -----"
  printf '%s\n' "$MESSAGE"
  echo "----- end -----"
  exit 0
fi

# Use a temp file so multi-line body and commit-msg hook both work.
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
printf '%s\n' "$MESSAGE" >"$tmp"

if ! git commit -F "$tmp"; then
  echo "auto-commit: git commit failed (see commit-msg hook output)" >&2
  exit 1
fi

echo "auto-commit: created $(git rev-parse --short HEAD) — $SUBJECT"

if [[ "$DO_PUSH" -eq 1 ]]; then
  branch="$(git rev-parse --abbrev-ref HEAD)"
  echo "auto-commit: pushing $branch to origin (no force)"
  git push -u origin "$branch"
fi
