#!/bin/sh
set -eu
test "$(id -u)" = 0 || { echo 'Run with sudo' >&2; exit 1; }
if command -v node >/dev/null 2>&1; then
  node --version
  node -e 'if (process.versions.node.split(".")[0] !== "22") process.exit(1)'
  npm --version
  exit 0
fi
case "$(uname -m)" in
  x86_64) arch=x64 ;;
  aarch64) arch=arm64 ;;
  *) echo 'Unsupported architecture' >&2; exit 1 ;;
esac
task_tmp=$(mktemp -d)
trap 'rm -rf "$task_tmp"' EXIT
cd "$task_tmp"
curl -fsSLO https://nodejs.org/dist/latest-v22.x/SHASUMS256.txt
archive=$(awk -v suffix="linux-$arch.tar.xz" '$2 ~ suffix "$" {print $2}' SHASUMS256.txt)
test -n "$archive"
curl -fsSLO "https://nodejs.org/dist/latest-v22.x/$archive"
awk -v file="$archive" '$2 == file {print}' SHASUMS256.txt | sha256sum -c -
install -d /opt/keel-tools/node
tar -xJf "$archive" -C /opt/keel-tools/node --strip-components=1
for tool in node npm npx; do
  test ! -e "/usr/local/bin/$tool" || { echo "Existing $tool requires review" >&2; exit 1; }
  ln -s "/opt/keel-tools/node/bin/$tool" "/usr/local/bin/$tool"
done
node --version
npm --version
