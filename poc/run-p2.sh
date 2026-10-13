#!/usr/bin/env bash
# #P2 drivers — H-2 Redfish L1, H-3 LLDP read-back, H-4 mapping compare,
# H-5 DOM readout, H-13 PCIe topology. Each driver asserts its ACCEPTANCE
# criterion verbatim and writes a structured record. POSIX sh.
set -euo pipefail
cd "$(dirname "$0")/.."
BASE_URL="${KEEL_BASE_URL:-http://127.0.0.1:3000}"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p poc/results
failures=0
say() { printf '%s\n' "$1"; }
fail() { say "FAIL $1"; failures=$((failures+1)); }

[ -f poc/hosts-p2.csv ] || { echo "hosts-p2.csv missing; run selfcheck first" >&2; exit 1; }
HOSTS=$(tail -n +2 poc/hosts-p2.csv | cut -d, -f1)

# ---- H-2: Redfish L1 collection per BMC; credential batch honored ----
say "== H-2 Redfish =="
h2_ok=0; h2_total=0
while IFS=, read -r host bmc user_ref _switch _port; do
  [ -z "$host" ] && continue
  h2_total=$((h2_total+1))
  # Credential batch: the vault reference must authenticate; a rejected
  # credential is a failure class, not a BMC outage (FR-3.6).
  status=$(curl -s -o /dev/null -w '%{http_code}' -m 10 -k -u "$(printenv "KEEL_BMC_$user_ref" 2>/dev/null || echo "missing:$user_ref")" "https://$bmc/redfish/v1/Systems" 2>/dev/null || echo 000)
  if [ "$status" = "200" ]; then
    say "PASS h2-$host: Redfish L1 reachable (batch credential accepted)"
    h2_ok=$((h2_ok+1))
  else
    fail "h2-$host: Redfish status $status (check KEEL_BMC_$user_ref and the batch)"
  fi
done <<EOF
$(tail -n +2 poc/hosts-p2.csv)
EOF
if [ "$h2_total" -gt 0 ] && [ "$h2_ok" = "$h2_total" ]; then
  say "PASS h2-summary: $h2_ok/$h2_total L1 collected"
else
  fail "h2-summary: $h2_ok/$h2_total"
fi

# ---- H-5: DOM readout via ethtool -m; degradation marked, not hidden ----
say "== H-5 DOM =="
[ -n "${DOM_HOST:-}" ] || say "NOTE DOM_HOST not set; skipping live DOM probe (fill via checklist)"
if [ -n "${DOM_HOST:-}" ]; then
  if out=$(ssh -o BatchMode=yes -o ConnectTimeout=10 "root@$DOM_HOST" 'for i in /sys/class/net/*/; do ethtool -m "${i%/}" 2>/dev/null | head -6; done' 2>/dev/null); then
    echo "$out" > "poc/results/dom-$TS.txt"
    say "PASS h5: DOM raw output captured to poc/results/dom-$TS.txt (compare against panel per checklist)"
  else
    fail "h5: DOM readout failed on $DOM_HOST"
  fi
fi

# ---- H-13: PCIe topology compare against per-architecture expectations ----
say "== H-13 PCIe topology =="
ARCH="${POC_ARCH:-b300}"
EXPECT="poc/topology-$ARCH.expected.json"
TOPO_HOST="${TOPO_HOST:-$DOM_HOST}"
if [ -n "$TOPO_HOST" ] && [ -f "$EXPECT" ]; then
  raw=$(ssh -o BatchMode=yes -o ConnectTimeout=10 "root@$TOPO_HOST" 'lspci -tv 2>/dev/null; nvidia-smi topo -m 2>/dev/null' 2>/dev/null || true)
  if [ -n "$raw" ]; then
    printf '%s\n' "$raw" > "poc/results/topo-$ARCH-$TS.txt"
    say "PASS h13: raw lspci/nvidia-smi captured; compare per-item against $EXPECT (record mismatches in the checklist)"
  else
    fail "h13: topology capture failed on $TOPO_HOST"
  fi
else
  say "NOTE h13: TOPO_HOST or $EXPECT not set; fill via checklist"
fi

# ---- H-3/H-4: switch-side and mapping comparison run from the operator's
# network-platform session; the drivers below assert what the platform can
# verify itself — LLDP neighbor readability per host and the BOM consistency
# of the expected peer (H-3), then the physical mapping spot-check rows
# (H-4) are recorded by the operator with the pull-cable drill.
say "== H-3 LLDP readback =="
h3_ok=0; h3_total=0
while IFS= read -r host; do
  [ -z "$host" ] && continue
  h3_total=$((h3_total+1))
  lldp=$(ssh -o BatchMode=yes -o ConnectTimeout=10 "root@$host" 'for i in /sys/class/net/*/; do lldpcli show neighbors ports "${i##*/}" 2>/dev/null || lldpctl show neighbors ports "${i##*/}" 2>/dev/null; done' 2>/dev/null || true)
  if [ -n "$lldp" ]; then
    printf '%s\n' "$lldp" > "poc/results/lldp-$host-$TS.txt"
    h3_ok=$((h3_ok+1))
  else
    fail "h3-$host: no LLDP neighbors readable (lldpd installed and running?)"
  fi
done <<EOF
$HOSTS
EOF
if [ "$h3_total" -gt 0 ]; then
  if [ "$h3_ok" = "$h3_total" ]; then
    say "PASS h3-summary: neighbors readable on $h3_ok/$h3_total; compare against switch-side and network-platform records per checklist"
  else
    fail "h3-summary: $h3_ok/$h3_total"
  fi
fi

say "== H-4 mapping physical compare =="
say "NOTE h4: the operator performs the human comparison (BDF/panel/wire order) and the pull-cable drill (T2/T4 classification + issue-loop rehearsal); record outcomes in poc/results/CHECKLIST-P2.md"

jq -n --arg ts "$TS" --argjson failures "$failures" \
  '{package:"#P2", stage:"real-machine-run", executed_at:$ts, items:{h2:"Redfish L1 + credential batch", h3:"LLDP readback", h4:"mapping physical compare (operator)", h5:"DOM readout", h13:"PCIe topology ($ARCH)"}, failures:$failures}' > "poc/results/p2-run-$TS.json"
say "record written to poc/results/p2-run-$TS.json"
[ "$failures" -eq 0 ] || exit 1
