#!/bin/sh
# steam-achievements-cli installer for Linux and macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/tomikav/steam-achievements-cli-/main/install.sh | sh
#
# Downloads the latest release, verifies its SHA-256 checksum and installs the
# binary to ~/.local/bin (override with INSTALL_DIR). No sudo needed.
#
# On Linux the Steam client already owns the `steam` command, so the binary
# is installed as `steam-ach` there unless BIN_NAME is set.
#
# Optional: STEAM_CLI_VERSION=v1.1.0 to pin a version.
set -eu

repo="tomikav/steam-achievements-cli-"
install_dir="${INSTALL_DIR:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin) ;;
  *) echo "Unsupported OS: $os. On Windows use install.ps1." >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
esac

if [ -z "${BIN_NAME:-}" ]; then
  if [ "$os" = "linux" ]; then BIN_NAME=steam-ach; else BIN_NAME=steam; fi
fi

if [ -n "${STEAM_CLI_VERSION:-}" ]; then
  base="https://github.com/$repo/releases/download/$STEAM_CLI_VERSION"
else
  base="https://github.com/$repo/releases/latest/download"
fi
asset="steam-achievements-cli_${os}_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $asset"
curl -fsSL "$base/$asset" -o "$tmp/$asset"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

expected=$(grep " $asset\$" "$tmp/checksums.txt" | awk '{print $1}')
if [ -z "$expected" ]; then echo "checksums.txt has no entry for $asset" >&2; exit 1; fi
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$asset" | awk '{print $1}')
else
  actual=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
fi
if [ "$expected" != "$actual" ]; then echo "Checksum mismatch for $asset. Aborting." >&2; exit 1; fi
echo "Checksum verified"

tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$install_dir"
install -m 0755 "$tmp/steam" "$install_dir/$BIN_NAME"

echo
"$install_dir/$BIN_NAME" version
echo "Installed to $install_dir/$BIN_NAME"

case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) echo "Add $install_dir to your PATH, for example:"
     echo "  echo 'export PATH=\"$install_dir:\$PATH\"' >> ~/.profile" ;;
esac

echo
echo "Next steps:"
echo "  $BIN_NAME config set key YOUR_API_KEY     (https://steamcommunity.com/dev/apikey)"
echo "  $BIN_NAME config set id  YOUR_STEAMID64"
echo "  $BIN_NAME doctor"
