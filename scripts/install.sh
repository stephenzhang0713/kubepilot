#!/bin/sh
set -eu

repo='stephenzhang0713/kubepilot'
case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) echo 'KubePilot supports macOS and Linux.' >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo 'Unsupported CPU architecture.' >&2; exit 1 ;;
esac

command -v curl >/dev/null 2>&1 || { echo 'curl is required.' >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  checksum=sha256sum
elif command -v shasum >/dev/null 2>&1; then
  checksum='shasum -a 256'
else
  echo 'sha256sum or shasum is required.' >&2
  exit 1
fi

asset="kubepilot_${os}_${arch}"
base="https://github.com/${repo}/releases/latest/download"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
curl -fsSL "$base/$asset" -o "$tmp/$asset"
curl -fsSL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS"
expected=$(awk -v name="$asset" '$2 == name { print $1; count++ } END { if (count != 1) exit 1 }' "$tmp/SHA256SUMS") || { echo 'Release checksum is missing or ambiguous.' >&2; exit 1; }
actual=$($checksum "$tmp/$asset")
actual=${actual%% *}
if [ "$actual" != "$expected" ]; then
  echo 'Downloaded file failed checksum verification.' >&2
  exit 1
fi
install_dir=${KUBEPILOT_INSTALL_DIR:-"$HOME/.local/bin"}
mkdir -p "$install_dir"
chmod 755 "$tmp/$asset"
mv -f "$tmp/$asset" "$install_dir/kubepilot"
printf 'Installed KubePilot to %s/kubepilot\n' "$install_dir"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) printf 'Add %s to your PATH to run kubepilot.\n' "$install_dir" ;;
esac
