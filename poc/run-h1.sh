#!/bin/sh
# Offline H-1 evidence validation only. Never connects to or modifies hardware.
set -eu
cd "$(dirname "$0")/.."
if [ "$#" -ne 1 ]; then
  echo 'usage: sh poc/run-h1.sh /private/poc/evidence.json' >&2
  echo 'Collect the evidence described in poc/ENVIRONMENT.md; no H-1 PASS is granted by this script.' >&2
  exit 2
fi
exec python3 poc/h1/validate.py "$1"
