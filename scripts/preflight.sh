#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
git --version
go version
node --version
npm --version
docker compose version
test -f .env || { echo 'Missing server .env; follow docs/DEPLOYMENT.md' >&2; exit 1; }
docker compose config --quiet
