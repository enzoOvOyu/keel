#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
config_file="${KEEL_CONFIG_FILE:-${XDG_CONFIG_HOME:-$HOME/.config}/keel/development.env}"
if [ -f "$config_file" ]; then
  set -a
  . "$config_file"
  set +a
fi
export KEEL_WORKER_ID=${KEEL_WORKER_ID:-keel-dev-worker}
exec go run ./cmd/keel-worker
