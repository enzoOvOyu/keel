#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
sudo apt-get update
sudo apt-get install -y build-essential ripgrep protobuf-compiler pkg-config curl git python3 tmux
sudo sh scripts/bootstrap-node.sh
sh scripts/bootstrap-go.sh
npm ci
sh scripts/preflight.sh
