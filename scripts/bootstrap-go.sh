#!/bin/sh
set -eu
keel_go_version=1.27.2
keel_go_sha=ecbadb99091a3f46e31f5f934b068b1864eafa7995211b39eaddf76996045fe5
[ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || { echo 'This host bootstrap supports Linux amd64 only; agent builds support amd64/arm64.' >&2; exit 1; }
keel_go_dest=/opt/keel-tools/go-$keel_go_version
if [ ! -x "$keel_go_dest/bin/go" ]; then
  keel_go_tmp=$(mktemp -d)
  trap 'rm -rf "$keel_go_tmp"' EXIT
  curl -fL --retry 3 "https://go.dev/dl/go$keel_go_version.linux-amd64.tar.gz" -o "$keel_go_tmp/go.tgz"
  printf '%s  %s\n' "$keel_go_sha" "$keel_go_tmp/go.tgz" | sha256sum -c -
  sudo install -d "$keel_go_dest"
  sudo tar -xzf "$keel_go_tmp/go.tgz" -C "$keel_go_dest" --strip-components=1
fi
sudo ln -sfn "$keel_go_dest/bin/go" /usr/local/bin/go
sudo ln -sfn "$keel_go_dest/bin/gofmt" /usr/local/bin/gofmt
go version
