#!/usr/bin/env bash
# Run one safe periodic commit + push cycle for the current checkout.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

branch="$(git symbolic-ref --quiet --short HEAD 2>/dev/null || true)"
if [[ -z "$branch" ]]; then
  echo "scheduled-auto-commit: detached HEAD; skipped"
  exit 0
fi
if [[ "$branch" == "main" || "$branch" == "master" ]]; then
  echo "scheduled-auto-commit: protected branch $branch; skipped"
  exit 0
fi

git_dir="$(git rev-parse --git-dir)"
for state in MERGE_HEAD rebase-merge rebase-apply CHERRY_PICK_HEAD REVERT_HEAD; do
  if [[ -e "$git_dir/$state" ]]; then
    echo "scheduled-auto-commit: repository operation $state is active; skipped"
    exit 0
  fi
done

lock_dir="$git_dir/alpha-nexus-auto-commit.lock"
if ! mkdir "$lock_dir" 2>/dev/null; then
  echo "scheduled-auto-commit: another cycle is running; skipped"
  exit 0
fi
trap 'rmdir "$lock_dir" 2>/dev/null || true' EXIT

"$ROOT/scripts/auto-commit.sh" --push \
  -m "chore(auto): periodic workspace snapshot" \
  --body "Created by the local Alpha-Nexus EgoLite launchd job on branch $branch."
