#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
config_file="${KEEL_CONFIG_FILE:-${XDG_CONFIG_HOME:-$HOME/.config}/keel/development.env}"
if [ -f "$config_file" ]; then
  set -a
  . "$config_file"
  set +a
fi
export KEEL_LISTEN_ADDR=127.0.0.1:3001
exec go run ./cmd/keel-api
