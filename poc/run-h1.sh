#!/usr/bin/env bash
# PoC execution package #P1 — H-1 real-machine driver (executed by the
# engineering owner in the PoC environment; NOT run by CI).
#
# Prerequisites proven by selfcheck.sh. This script walks the H-1 judging
# criteria from ACCEPTANCE.md verbatim:
#   1) HTTP-first boot succeeds on all machines
#   2) hostname matches the BOM
#   3) SSH trust auto-establishes on 100% of machines
#   4) host key fingerprints recorded
# Every step either passes an assertion or writes a FAIL entry; the run
# record lands in poc/results/h1-run-<ts>.json and doubles as the L4 evidence
# once the observation checklist is filled in.
set -euo pipefail
cd "$(dirname "$0")/.."

BASE_URL="${KEEL_BASE_URL:-http://127.0.0.1:3000}"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="poc/results/h1-run-$TS.json"
mkdir -p poc/results
failures=0
report=""
say() { report="$report$1\n"; printf '%s\n' "$1"; }
fail() { say "FAIL $1"; failures=$((failures+1)); }

[ -f poc/bom-h1.csv ] || { echo "bom-h1.csv missing; run selfcheck.sh first" >&2; exit 1; }
[ -f keys/keel-ctrl ] || { echo "keel-ctrl key missing; run selfcheck.sh first" >&2; exit 1; }

say "== H-1 run started $TS =="

# Step 1: import the BOM through the platform's two-phase pipeline (FR-2.1).
# The operator provides an admin API token via KEEL_TOKEN (isolated PoC run).
if [ -z "${KEEL_TOKEN:-}" ]; then
  say "NOTE KEEL_TOKEN not set; import the BOM through the UI (Assets → 导入 BOM) with file version h1-$TS and continue this script afterwards."
else
  say "STEP bom-import: file_version h1-$TS"
  # Login-free scripted import is deliberately not part of this package: the
  # two-phase preview must be human-confirmed (FR-2.1 冲突以预览→确认两段式提交).
  say "NOTE scripted import is intentionally not automated; use the UI preview flow, then re-run this script."
fi

# Step 2: wait for provisioning callbacks and read back the platform state.
# H-1 assertion 1: every machine in the BOM reached provisioned state.
say "STEP trust-matrix: reading machine trust states from the platform"
# The platform exposes machine state via the project assets API; the operator
# supplies PROJECT_ID. When absent, fall back to per-host SSH checks.
if [ -n "${PROJECT_ID:-}" ] && [ -n "${KEEL_TOKEN:-}" ]; then
  assets=$(curl -fsS -m 10 -H "Authorization: Bearer $KEEL_TOKEN" \
    "$BASE_URL/api/v1/projects/$PROJECT_ID/assets?page=1&page_size=50" 2>/dev/null || echo '')
  if [ -n "$assets" ]; then
    total=$(printf '%s' "$assets" | jq '.items | length')
    established=$(printf '%s' "$assets" | jq '[.items[] | select(.ssh_trust=="established")] | length')
    provisioned=$(printf '%s' "$assets" | jq '[.items[] | select(.state=="provisioned")] | length')
    [ "$total" -gt 0 ] && say "PASS assets-visible: $total machines listed" || fail "assets-visible: empty list"
    if [ "$established" = "$total" ] && [ "$total" -gt 0 ]; then
      say "PASS h1-trust-rate: $established/$total established (criterion: 100%)"
    else
      fail "h1-trust-rate: $established/$total established (criterion: 100%)"
    fi
    [ "$provisioned" = "$total" ] && say "PASS h1-provisioned: $provisioned/$total" || fail "h1-provisioned: $provisioned/$total"
  else
    fail "assets-visible: API unreachable or unauthorized"
  fi
else
  say "NOTE PROJECT_ID/KEEL_TOKEN not set; skipping API readback. Falling back to direct SSH probes."
fi

# Step 3: direct verification per machine — hostname + key-based login +
# fingerprint recorded. H-1 assertions 2, 3 and 4.
expected_hosts=$(tail -n +2 poc/bom-h1.csv | cut -d, -f2)  # column 2 = hostname (SN,hostname,model_name,...)
trust_ok=0; trust_total=0
while IFS= read -r host; do
  [ -z "$host" ] && continue
  trust_total=$((trust_total+1))
  if ! ssh -i keys/keel-ctrl -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new \
       "root@$host" true 2>/dev/null; then
    fail "h1-ssh-trust: key login to root@$host failed"
    continue
  fi
  got_host=$(ssh -i keys/keel-ctrl -o BatchMode=yes -o ConnectTimeout=10 root@$host hostname 2>/dev/null || true)
  if [ "$got_host" = "$host" ]; then
    say "PASS h1-hostname: $host matches BOM"
  else
    fail "h1-hostname: $host reports '${got_host:-<unreachable>}'"
  fi
  fp=$(ssh-keyscan -t ed25519 "$host" 2>/dev/null | ssh-keygen -lf - 2>/dev/null | awk '{print $2}' || true)
  if [ -n "$fp" ]; then
    say "PASS h1-fingerprint: $host $fp"
  else
    fail "h1-fingerprint: keyscan failed for $host"
  fi
  trust_ok=$((trust_ok+1))
done <<EOF
$expected_hosts
EOF

if [ "$trust_total" -gt 0 ]; then
  if [ "$trust_ok" = "$trust_total" ]; then
    say "PASS h1-trust-rate-ssh: $trust_ok/$trust_total key logins succeeded (criterion: 100%)"
  else
    fail "h1-trust-rate-ssh: $trust_ok/$trust_total key logins (criterion: 100%)"
  fi
fi

say "== H-1 run finished: $failures failure(s) =="
jq -n --arg report "$report" --argjson failures "$failures" --arg ts "$TS" \
  '{package:"#P1", item:"H-1", stage:"real-machine-run", executed_at:$ts,
    base_url:$ENV.KEEL_BASE_URL, project_id:$ENV.PROJECT_ID, failures:$failures,
    criteria:{boot:"HTTP-first boot completed for all machines (operator observation)",
              hostname:"hostname matches BOM", trust:"auto-establishment rate 100%",
              fingerprint:"host key fingerprint recorded"},
    report:$report}' > "$OUT"
say "record written to $OUT — attach it to ACCEPTANCE H-1 together with the operator checklist (poc/results/CHECKLIST.md)"
[ "$failures" -eq 0 ] || exit 1
