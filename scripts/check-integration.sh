#!/bin/sh
# Reproducible real PostgreSQL + HTTP + browser checks; disposable data only.
set -eu
cd "$(dirname "$0")/.."
test -z "$(ss -H -ltn 'sport = :3102')" || { echo 'Port3102 is busy; stop your isolated API before running the suite.' >&2; exit 1; }
test_lock=/tmp/keel-integration.lock
mkdir "$test_lock" 2>/dev/null || { echo 'Another Keel integration suite is active; inspect its lock.' >&2; exit 1; }
umask 077
test_work=$(mktemp -d /tmp/keel-integration.XXXXXX)
test_api_pid=
test_worker_pid=
cleanup() {
  if [ -n "$test_worker_pid" ]; then kill "$test_worker_pid" 2>/dev/null || true; wait "$test_worker_pid" 2>/dev/null || true; fi
  if [ -n "$test_api_pid" ]; then kill "$test_api_pid" 2>/dev/null || true; wait "$test_api_pid" 2>/dev/null || true; fi
  rm -f "$test_work/browser-credentials.json"
  rmdir "$test_lock"
}
trap cleanup EXIT
python3 scripts/test-environment.py start
set -a
. /opt/keel-tools/state/keel-test-db.env
set +a
export KEEL_TEST_EXECUTION_DATABASE_URL
KEEL_TEST_EXECUTION_DATABASE_URL=$(python3 -c 'import os; print(os.environ["KEEL_TEST_DATABASE_URL"].replace("/keel_test_auth?", "/keel_test_execution?"))')
make check
docker exec keel-auth-test-postgres psql -X -U keel_test -d keel_test_http -v ON_ERROR_STOP=1 -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;' >/dev/null
go build -o "$test_work/keel-api" ./cmd/keel-api
go build -o "$test_work/keel-admin" ./cmd/keel-admin
go build -o "$test_work/keel-worker" ./cmd/keel-worker
export KEEL_DATABASE_URL KEEL_TEST_DATABASE_URL KEEL_ADMIN_BINARY KEEL_AUTH_BROWSER_CREDENTIALS_FILE
KEEL_DATABASE_URL=$(python3 -c 'import os; print(os.environ["KEEL_TEST_DATABASE_URL"].replace("/keel_test_auth?", "/keel_test_http?"))')
KEEL_TEST_DATABASE_URL=$KEEL_DATABASE_URL
KEEL_ADMIN_BINARY="$test_work/keel-admin"
KEEL_AUTH_BROWSER_CREDENTIALS_FILE="$test_work/browser-credentials.json"
export KEEL_ENV=development KEEL_INSECURE_DEV_COOKIE=true
export KEEL_PUBLIC_ORIGINS=http://127.0.0.1:3102,http://localhost:3102
export KEEL_LISTEN_ADDR=127.0.0.1:3102 KEEL_AUTH_TEST_BASE_URL=http://127.0.0.1:3102
"$KEEL_ADMIN_BINARY" migrate
"$test_work/keel-api" >"$test_work/api.log" 2>&1 &
test_api_pid=$!
curl --retry 20 --retry-delay 1 --retry-connrefused --fail --silent http://127.0.0.1:3102/healthz >/dev/null
python3 scripts/check-auth-integration.py
python3 scripts/check-control-integration.py
export KEEL_WORKER_ID=keel-integration-worker
export KEEL_WORKER_CONCURRENCY=${KEEL_WORKER_CONCURRENCY:-8}
export KEEL_SIM_REPORT_FILE="$test_work/simulation-report.json"
"$test_work/keel-worker" >"$test_work/worker.log" 2>&1 &
test_worker_pid=$!
python3 scripts/check-execution-integration.py
python3 scripts/check-model-integration.py
python3 scripts/check-model-import.py
python3 scripts/check-bom-import.py
python3 scripts/check-trust.py
python3 scripts/check-collections.py
python3 scripts/check-gates.py
python3 scripts/check-lldp.py
python3 scripts/check-issues.py
python3 scripts/check-dashboard.py
python3 scripts/check-circuit.py
python3 scripts/check-rerun.py
python3 scripts/check-mon.py
python3 scripts/check-media-integration.py
python3 scripts/check-param-integration.py
export KEEL_PLAYWRIGHT_MODULE=${KEEL_PLAYWRIGHT_MODULE:-/tmp/keel-browser-check/node_modules/playwright/index.mjs}
test -f "$KEEL_PLAYWRIGHT_MODULE" || { echo 'Install the pinned Playwright package/browser from docs/DEPLOYMENT.md first.' >&2; exit 1; }
node scripts/check-auth-browser.mjs
node scripts/check-control-browser.mjs
node scripts/check-media-browser.mjs
node scripts/check-model-import-browser.mjs
node scripts/check-bom-browser.mjs
node scripts/check-dashboard-browser.mjs
KEEL_BASE_URL=http://127.0.0.1:3102 node scripts/check-browser.mjs
echo "Integration suite passed; non-secret process logs are in $test_work"
echo "Simulation capacity report: $test_work/simulation-report.json"
echo 'Disposable PostgreSQL is preserved for inspection; remove it with python3 scripts/test-environment.py stop.'
