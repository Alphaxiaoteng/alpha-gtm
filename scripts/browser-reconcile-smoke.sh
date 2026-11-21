#!/usr/bin/env sh
# T-E04: browser Reconcile / runtime-lease smoke
#
# What this covers (no real fingerprint-chromium required):
#   1) Empty-DB migrate creates browser_runtime_instances + profile_leases
#   2) PortAllocator + LeaseManager unit tests (ports/leases Reconcile depends on)
#   3) Security gates: UI/CDP bind localhost; no cookie/password log patterns in hot paths
#
# Full kill -9 browser PID path is integration (T-A02); hooks:
#   internal/browser/supervisor.go
#     - reconcileAll()       — heartbeatLoop every 5s: dead PID → drop instance/lease/runtime
#     - cleanStaleInstances()— Supervisor start: DELETE FROM browser_runtime_instances
#     - removeRuntimeInstance / leases.Release / logEvent("crashed", ...)
#   go test ./internal/browser/ -run 'TestPortAllocator|TestLeaseManager|TestSupervisor'
#
# Usage:
#   ./scripts/browser-reconcile-smoke.sh
#   CI: same command from repo root
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT_DIR"

echo "==> [1/4] go test internal/errors (error codes)"
go test ./internal/errors/... -count=1

echo "==> [2/4] go test internal/db (migrate + runtime tables)"
go test ./internal/db/... -count=1 -run 'TestOpenAndMigrate|TestMigrationIdempotent|TestCoreTablesExist|TestRuntimeTablesExist'

echo "==> [3/4] go test internal/browser (ports / leases / supervisor smoke)"
go test ./internal/browser/... -count=1 -run 'TestPortAllocator|TestLeaseManager|TestSupervisor'

echo "==> [4/4] security grep gates (T-E03)"
# UI must bind 127.0.0.1 only
if ! grep -n 'fmt.Sprintf("127.0.0.1:%d", s.cfg.UIPort)' internal/ui/server.go >/dev/null 2>&1; then
  echo "FAIL: UI ListenAndServe must bind 127.0.0.1" >&2
  exit 1
fi

# CDP remote-debugging-address must be 127.0.0.1 (never 0.0.0.0)
if grep -n 'remote-debugging-address=0.0.0.0' internal/browser/*.go >/dev/null 2>&1; then
  echo "FAIL: CDP must not bind 0.0.0.0" >&2
  exit 1
fi
if ! grep -n 'remote-debugging-address=127.0.0.1' internal/browser/browser.go internal/browser/supervisor.go >/dev/null 2>&1; then
  echo "FAIL: missing --remote-debugging-address=127.0.0.1" >&2
  exit 1
fi

# Cookie / proxy password must not be printed via fmt/log in browser/cli hot paths.
# Allow identifiers (cookie_encrypted, password-store) but block Printf/Println of secrets.
leak_cookie=$(grep -RIn --include='*.go' -E 'Printf\(.*[Cc]ookie|Println\(.*[Cc]ookie' \
  internal/browser internal/cli 2>/dev/null | grep -viE 'cookie_encrypted|cookie_import|GetCookies|//' || true)
if [ -n "$leak_cookie" ]; then
  echo "FAIL: cookie value may leak into logs:" >&2
  echo "$leak_cookie" >&2
  exit 1
fi

leak_pass=$(grep -RIn --include='*.go' -E 'Printf\(.*[Pp]assword|Println\(.*[Pp]assword|proxy.?password' \
  internal/browser internal/cli 2>/dev/null | grep -viE 'password-store|Keychain|//' || true)
if [ -n "$leak_pass" ]; then
  echo "FAIL: password may leak into logs:" >&2
  echo "$leak_pass" >&2
  exit 1
fi

echo "OK: browser-reconcile-smoke passed"
echo "Note: live PID kill-9 Reconcile is covered by T-A02 integration when browser binary is present."
