#!/bin/sh
set -eu
printf 'OS: '
uname -srm
id
for tool in git docker curl; do
  command -v "$tool" >/dev/null || { echo "Missing: $tool" >&2; exit 1; }
done
git --version
docker version
docker compose version
docker compose up --help | grep -q -- '--wait' || {
  echo 'Docker Compose with --wait support is required' >&2
  exit 1
}
df -h .
