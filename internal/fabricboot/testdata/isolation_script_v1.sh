#!/bin/bash
# CUMULUS-AUTOPROVISIONING
set -euo pipefail
shopt -s nullglob
pending_before="$(nv config diff applied pending -o commands)"
if [[ -n "${pending_before//[[:space:]]/}" ]]; then
  echo 'ZTP isolation failed: unrelated pending NVUE configuration exists' >&2
  exit 1
fi
ports=(/sys/class/net/swp*)
if ((${#ports[@]} == 0)); then
  echo 'ZTP isolation failed: no front-panel ports detected' >&2
  exit 1
fi
for path in "${ports[@]}"; do
  iface="${path##*/}"
  if [[ ! "$iface" =~ ^swp[0-9]+(s[0-9]+)?$ ]]; then
    echo 'ZTP isolation failed: unexpected front-panel name' >&2
    exit 1
  fi
  nv set interface "$iface" link state down
done
pending_after="$(nv config diff applied pending -o commands)"
while IFS= read -r command; do
  [[ -z "$command" ]] && continue
  if [[ "$command" =~ ^nv[[:space:]]set[[:space:]]interface[[:space:]](swp[0-9]+(s[0-9]+)?)[[:space:]]link[[:space:]]state[[:space:]]down$ ]]; then
    [[ -e "/sys/class/net/${BASH_REMATCH[1]}" ]] || exit 1
  else
    echo 'ZTP isolation failed: unexpected pending NVUE command' >&2
    exit 1
  fi
done <<< "$pending_after"
nv config apply --assume-yes
exit 0
