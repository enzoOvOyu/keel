#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
test -f .env || { echo 'Create .env from .env.example first' >&2; exit 1; }
test -z "$(git status --porcelain)" || { echo 'Commit or stash changes before deploying' >&2; exit 1; }
mkdir .deploy-lock 2>/dev/null || { echo 'Deployment already in progress; check .deploy-lock' >&2; exit 1; }
env_tmp=
trap 'if [ -n "$env_tmp" ]; then rm -f "$env_tmp"; fi; rmdir .deploy-lock' EXIT
revision=$(git rev-parse HEAD)
image="keel:$revision"
docker compose config --quiet
docker build --build-arg "APP_VERSION=$revision" -t "$image" .
KEEL_IMAGE="$image" docker compose up -d --no-build --wait --wait-timeout 90 api
env_tmp=$(mktemp .env.deploy.XXXXXX)
awk '!/^KEEL_IMAGE=/' .env > "$env_tmp"
printf '\nKEEL_IMAGE=%s\n' "$image" >> "$env_tmp"
chmod 600 "$env_tmp"
mv "$env_tmp" .env
env_tmp=
printf 'Healthy deployment: %s\n' "$revision"
