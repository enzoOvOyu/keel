#!/usr/bin/env bash
# PoC execution package #P2 — H-2/H-3/H-4/H-5/H-13 real-machine drivers.
#
# Per IMPLEMENTATION-PLAN §5.5: self-check → per-item drivers → structured
# records. The engineering owner executes in the PoC environment (8 BMC
# machines + 2 switches + B300/GB300 samples); execution backfill is the GA
# gate, not a stage gate. POSIX sh only (no bashisms).
set -euo pipefail
cd "$(dirname "$0")/.."

BASE_URL="${KEEL_BASE_URL:-http://127.0.0.1:3000}"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p poc/results
failures=0
report=""
say() { report="$report$1\n"; printf '%s\n' "$1"; }
fail() { say "FAIL $1"; failures=$((failures+1)); }

say "== #P2 selfcheck started $TS =="

# 1. Platform reachable.
if body=$(curl -fsS -m 5 "$BASE_URL/api/v1" 2>/dev/null); then
  say "PASS platform-reachable"
else
  fail "platform-reachable: set KEEL_BASE_URL"
fi

# 2. Tools for every item.
for tool in jq curl ssh ethtool lspci; do
  if command -v "$tool" >/dev/null 2>&1; then
    say "PASS tool-$tool"
  else
    fail "tool-$tool missing (H-2 needs curl; H-3 ssh; H-5 ethtool; H-13 lspci)"
  fi
done

# 3. Host inventory file declares the PoC machines.
if [ -f poc/hosts-p2.csv ]; then
  rows=$(tail -n +2 poc/hosts-p2.csv | cut -d, -f1 | sort -u | wc -l)
  if [ "$rows" -ge 8 ] && [ "$rows" -le 10 ]; then
    say "PASS hosts-p2.csv: $rows machines (window 8-10)"
  else
    fail "hosts-p2.csv: $rows machines outside 8-10"
  fi
else
  fail "hosts-p2.csv missing (columns: hostname,bmc_ip,bmc_user_ref,switch,switch_port)"
fi

# 4. B300 and GB300 topology expectations exist for H-13.
for arch in b300 gb300; do
  if [ -f "poc/topology-$arch.expected.json" ]; then
    say "PASS topology-$arch expectations present"
  else
    fail "topology-$arch.expected.json missing (H-13 needs independent expectations per architecture)"
  fi
done

# 5. BMC credential batch reference exists (H-2 credential batches).
if [ -n "${KEEL_BMC_BATCH:-}" ]; then
  say "PASS bmc-batch reference set"
else
  say "NOTE KEEL_BMC_BATCH not set; H-2 needs the vault batch reference before run-h2.sh"
fi

jq -n --arg report "$report" --argjson failures "$failures" --arg ts "$TS" \
  '{package:"#P2", stage:"selfcheck", executed_at:$ts, base_url:$ENV.KEEL_BASE_URL, failures:$failures, report:$report}' > poc/results/p2-selfcheck.json
say "----"
if [ "$failures" -gt 0 ]; then
  say "SELF-CHECK FAILED ($failures); see poc/ENVIRONMENT-P2.md"
  exit 1
fi
say "SELF-CHECK PASSED; drivers: sh poc/run-h2.sh (Redfish) / run-h3.sh (LLDP) / run-h4.sh (mapping) / run-h5.sh (DOM) / run-h13.sh (PCIe)"
