#!/usr/bin/env sh
# AgentPush CLI smoke: init → account → target → content → media → publish dry-run
# → agent → queue → metrics → export (temp data-dir, no browser required).
#
# Usage (from repo root):
#   ./scripts/agentpush-smoke.sh
# CI: same command; exits 0 on success.
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT_DIR"

echo "==> AgentPush smoke (repo: $ROOT_DIR)"

# Build binary into a temp path so we do not clobber local ./alpha-nexus
BIN="$(mktemp -t alpha-nexus-smoke-XXXXXX)"
# mktemp creates a file; go build will overwrite. On some systems keep .bin suffix.
rm -f "$BIN"
BIN="${BIN}.bin"
cleanup() {
  rm -f "$BIN"
  if [ -n "${DATA_DIR:-}" ] && [ -d "$DATA_DIR" ]; then
    rm -rf "$DATA_DIR"
  fi
}
trap cleanup EXIT

echo "==> [1/12] go build"
go build -o "$BIN" ./cmd/alpha-nexus-egolite

DATA_DIR="$(mktemp -d -t alpha-nexus-smoke-data-XXXXXX)"
AN() {
  "$BIN" --data-dir "$DATA_DIR" "$@"
}

echo "==> [2/12] init ($DATA_DIR)"
AN init

echo "==> [3/12] account add (youtube / xhs / facebook)"
# Parse "ID:       N" from account add output
parse_id() {
  # stdin: command output → first "ID:" field (account add)
  sed -n 's/.*ID:[[:space:]]*\([0-9][0-9]*\).*/\1/p' | head -n1
}

YT_OUT=$(AN account add --platform youtube --name smoke-yt 2>&1)
XHS_OUT=$(AN account add --platform xhs --name smoke-xhs 2>&1)
FB_OUT=$(AN account add --platform facebook --name smoke-fb 2>&1)

YT_ACC=$(printf '%s\n' "$YT_OUT" | parse_id)
XHS_ACC=$(printf '%s\n' "$XHS_OUT" | parse_id)
FB_ACC=$(printf '%s\n' "$FB_OUT" | parse_id)

if [ -z "$YT_ACC" ] || [ -z "$XHS_ACC" ] || [ -z "$FB_ACC" ]; then
  echo "FAIL: could not parse account IDs" >&2
  echo "yt=$YT_OUT" >&2
  echo "xhs=$XHS_OUT" >&2
  echo "fb=$FB_OUT" >&2
  exit 1
fi
echo "    accounts: yt=$YT_ACC xhs=$XHS_ACC fb=$FB_ACC"

echo "==> [4/12] target add"
YT_T_OUT=$(AN target add --account-id "$YT_ACC" --platform youtube --type youtube_channel --name my-ch 2>&1)
XHS_T_OUT=$(AN target add --account-id "$XHS_ACC" --platform xhs --type xhs_account --name my-xhs 2>&1)
FB_T_OUT=$(AN target add --account-id "$FB_ACC" --platform facebook --type facebook_page --name My Page 2>&1)

# Target created: #N
parse_target() {
  sed -n 's/.*Target created: #\([0-9][0-9]*\).*/\1/p' | head -n1
}
YT_T=$(printf '%s\n' "$YT_T_OUT" | parse_target)
XHS_T=$(printf '%s\n' "$XHS_T_OUT" | parse_target)
FB_T=$(printf '%s\n' "$FB_T_OUT" | parse_target)

if [ -z "$YT_T" ] || [ -z "$XHS_T" ] || [ -z "$FB_T" ]; then
  echo "FAIL: could not parse target IDs" >&2
  exit 1
fi
echo "    targets: yt=$YT_T xhs=$XHS_T fb=$FB_T"

echo "==> [5/12] target list"
LIST_OUT=$(AN target list)
printf '%s\n' "$LIST_OUT" | grep -q youtube || { echo "FAIL: target list missing youtube" >&2; exit 1; }
printf '%s\n' "$LIST_OUT" | grep -q xhs || { echo "FAIL: target list missing xhs" >&2; exit 1; }
printf '%s\n' "$LIST_OUT" | grep -q facebook || { echo "FAIL: target list missing facebook" >&2; exit 1; }
AN target list --platform youtube >/dev/null

echo "==> [6/12] content create"
CT_OUT=$(AN content create --type graph_text --title "Smoke title" --caption "Smoke caption for AgentPush dry-run" 2>&1)
CONTENT_ID=$(printf '%s\n' "$CT_OUT" | sed -n 's/.*Content created: #\([0-9][0-9]*\).*/\1/p' | head -n1)
if [ -z "$CONTENT_ID" ]; then
  echo "FAIL: could not parse content id from: $CT_OUT" >&2
  exit 1
fi
echo "    content=$CONTENT_ID"

echo "==> [7/12] media import README"
MEDIA_OUT=$(AN media import --path "$ROOT_DIR/README.md" 2>&1)
printf '%s\n' "$MEDIA_OUT" | grep -q 'media_library:' || {
  echo "FAIL: media import did not upsert media_library" >&2
  echo "$MEDIA_OUT" >&2
  exit 1
}

echo "==> [8/12] publish dry-run (youtube / xhs / facebook)"
for pair in "youtube:$YT_T" "xhs:$XHS_T" "facebook:$FB_T"; do
  plat=${pair%%:*}
  tid=${pair##*:}
  echo "    publish --platform $plat --target $tid --dry-run"
  OUT=$(AN publish --platform "$plat" --content "$CONTENT_ID" --target "$tid" --dry-run 2>&1)
  printf '%s\n' "$OUT" | grep -Eiq 'dry_run_ok|dry-run ok|Result: dry_run_ok' || {
    echo "FAIL: dry-run for $plat did not report dry_run_ok" >&2
    echo "$OUT" >&2
    exit 1
  }
done

echo "==> [9/12] agent adapt"
ADAPT_OUT=$(AN agent adapt --platform xhs --text "Hello AgentPush smoke caption" 2>&1)
# Offline/template path should still return JSON-ish result without error
printf '%s\n' "$ADAPT_OUT" | grep -Eq '"status"|"ok"|adapted|caption|output|text|message|steps' || {
  # Accept any non-empty JSON object
  printf '%s\n' "$ADAPT_OUT" | grep -q '{' || {
    echo "FAIL: agent adapt unexpected output: $ADAPT_OUT" >&2
    exit 1
  }
}

echo "==> [10/12] queue enqueue"
AN queue enqueue --kind publish --payload "{\"content_id\":$CONTENT_ID}" >/dev/null
AN queue list >/dev/null

echo "==> [11/12] metrics collect"
# dry-run creates publish_records; pick first record id
REC_LINE=$(AN records list | awk 'NR>1 && $1 ~ /^[0-9]+$/ {print $1; exit}')
if [ -z "$REC_LINE" ]; then
  echo "FAIL: no publish records after dry-run; cannot metrics collect" >&2
  AN records list >&2 || true
  exit 1
fi
AN metrics collect --record "$REC_LINE" --views 10 --likes 2 >/dev/null
AN metrics list >/dev/null

echo "==> [12/12] export data"
EXPORT_DIR="$DATA_DIR/export-out"
AN export data --out "$EXPORT_DIR"
test -f "$EXPORT_DIR/export_summary.json" || {
  echo "FAIL: missing export_summary.json" >&2
  exit 1
}

echo "OK: agentpush-smoke passed"
echo "    data-dir was $DATA_DIR (cleaned on exit)"
