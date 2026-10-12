#!/bin/sh
# Intended only for an inspected Ubuntu host without an existing Docker engine.
set -eu
test "$(id -u)" = 0 || { echo 'Run with sudo' >&2; exit 1; }
. /etc/os-release
test "$ID" = ubuntu || { echo 'Only Ubuntu is supported by this script' >&2; exit 1; }
case "$VERSION_ID" in
  22.04|24.04|26.04) ;;
  *) echo 'Inspect Docker OS support before continuing' >&2; exit 1 ;;
esac
if command -v docker >/dev/null 2>&1; then
  docker version
  docker compose version
  echo 'Existing Docker installation retained'
  exit 0
fi
for package in docker.io docker-compose docker-compose-v2 podman-docker containerd runc; do
  if dpkg-query -W -f='${Status}' "$package" 2>/dev/null | grep -q 'install ok installed'; then
    echo "Existing $package requires manual review before Docker installation" >&2
    exit 1
  fi
done
apt-get update
apt-get install -y ca-certificates curl git
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
cat > /etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: ${UBUNTU_CODENAME:-$VERSION_CODENAME}
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF
apt-get update
apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
systemctl enable --now docker
docker run --rm hello-world
docker compose version
