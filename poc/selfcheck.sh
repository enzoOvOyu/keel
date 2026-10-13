#!/usr/bin/env bash
# PoC execution package #P1 — H-1: PXE boot + automatic SSH trust (ACCEPTANCE L4).
#
# Self-check stage: verifies the control-plane prerequisites on the machine
# running this script (the keel side). Real-machine steps (PXE boot the 10
# B300 nodes, watch trust establish) are driven by run-h1.sh; this script
# only proves the platform is ready and refuses loudly when anything is
# missing. Output: poc/p1-selfcheck.json + human observation checklist.
set -euo pipefail
cd "$(dirname "$0")/.."

BASE_URL="${KEEL_BASE_URL:-http://127.0.0.1:3000}"
OUT="poc/results/p1-selfcheck.json"
mkdir -p poc/results
failures=0
report=""

say() { report="$report$1\n"; printf '%s\n' "$1"; }

# 1. Platform reachable.
if body=$(curl -fsS -m 5 "$BASE_URL/api/v1" 2>/dev/null); then
  say "PASS platform-reachable: $(printf '%s' "$body" | head -c 120)"
else
  say "FAIL platform-reachable: cannot GET $BASE_URL/api/v1 (set KEEL_BASE_URL)"; failures=$((failures+1))
fi

# 2. Prerequisite binaries for the executor.
for tool in ssh-keygen ssh ssh-keyscan jq curl; do
  if command -v "$tool" >/dev/null 2>&1; then
    say "PASS tool-$tool: $(command -v "$tool")"
  else
    say "FAIL tool-$tool: not installed"; failures=$((failures+1))
  fi
done

# 3. keel-ctrl key pair present (FR-1.2 control-plane key custody).
if [ -f keys/keel-ctrl ] && [ -f keys/keel-ctrl.pub ]; then
  say "PASS keel-ctrl keypair present: $(ssh-keygen -lf keys/keel-ctrl.pub 2>/dev/null || echo fingerprint-unavailable)"
else
  say "FAIL keel-ctrl keypair missing: expected keys/keel-ctrl and keys/keel-ctrl.pub (generate: ssh-keygen -t ed25519 -f keys/keel-ctrl -N '')"; failures=$((failures+1))
fi

# 4. BOM sample present and machine count in the H-1 window (10 B300).
if [ -f poc/bom-h1.csv ]; then
  rows=$(tail -n +2 poc/bom-h1.csv | cut -d, -f1 | sort -u | wc -l)
  if [ "$rows" -ge 8 ] && [ "$rows" -le 12 ]; then
    say "PASS bom-h1.csv: $rows distinct machines (H-1 window 8-12, target 10)"
  else
    say "FAIL bom-h1.csv: $rows machines outside the 8-12 window"; failures=$((failures+1))
  fi
else
  say "FAIL bom-h1.csv missing (see poc/ENVIRONMENT.md §BOM)"; failures=$((failures+1))
fi

# 5. IPMI/HTTP boot services reachable if declared.
if [ -n "${PXE_HTTP_BASE:-}" ]; then
  if curl -fsS -m 5 "$PXE_HTTP_BASE" >/dev/null 2>&1; then
    say "PASS pxe-http-base reachable: $PXE_HTTP_BASE"
  else
    say "FAIL pxe-http-base unreachable: $PXE_HTTP_BASE"; failures=$((failures+1))
  fi
else
  say "NOTE pxe-http-base not set; set PXE_HTTP_BASE before run-h1.sh"
fi

jq -n --arg report "$report" --argjson failures "$failures" \
  '{package:"#P1", item:"H-1", stage:"selfcheck", executed_at:(now|todateiso8601),
    base_url:$ENV.KEEL_BASE_URL, failures:$failures, report:$report}' > "$OUT"
say "----"
if [ "$failures" -gt 0 ]; then
  say "SELF-CHECK FAILED with $failures failure(s); see poc/ENVIRONMENT.md for the prerequisites"
  exit 1
fi
say "SELF-CHECK PASSED; record written to $OUT; next: KEEL_BASE_URL=... sh poc/run-h1.sh"
