#!/bin/sh
# vhsweb installer — downloads the latest release binary for your OS/arch.
#
#   curl -fsSL https://raw.githubusercontent.com/steadyspacecorp/vhsweb/main/install.sh | sh
#
# Overrides (env vars):
#   VERSION=v0.2.0      install a specific tag instead of the latest
#   BIN_DIR=/usr/local/bin   install location (default: ~/.local/bin)
set -eu

OWNER="steadyspacecorp"
REPO="vhsweb"
BIN="vhsweb"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"

err() { echo "install.sh: $*" >&2; exit 1; }

# --- detect platform (matches GoReleaser's GOOS/GOARCH archive names) ---
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin|linux) ;;
  *) err "unsupported OS: $os (build from source instead)" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) err "unsupported architecture: $arch" ;;
esac

command -v curl >/dev/null 2>&1 || err "curl is required"
command -v tar  >/dev/null 2>&1 || err "tar is required"

# --- resolve version ---
version="${VERSION:-}"
if [ -z "$version" ]; then
  api="https://api.github.com/repos/$OWNER/$REPO/releases/latest"
  body=$(curl -fsSL "$api" 2>/dev/null) \
    || err "no published releases found for $OWNER/$REPO yet (the repo may be private, or no version has been tagged). Once a release exists, re-run this; to pin one now: VERSION=vX.Y.Z"
  version=$(printf '%s' "$body" | grep '"tag_name":' | head -n1 | sed 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/')
  [ -n "$version" ] || err "could not parse a release tag from $api"
fi
# GoReleaser archive names use the version without the leading "v".
ver_no_v=$(echo "$version" | sed 's/^v//')

asset="${BIN}_${ver_no_v}_${os}_${arch}.tar.gz"
url="https://github.com/$OWNER/$REPO/releases/download/$version/$asset"

echo "Downloading $BIN $version ($os/$arch)…"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$url" -o "$tmp/$asset" || err "download failed: $url"
tar -xzf "$tmp/$asset" -C "$tmp" || err "extract failed"

mkdir -p "$BIN_DIR"
install -m 0755 "$tmp/$BIN" "$BIN_DIR/$BIN" 2>/dev/null \
  || { cp "$tmp/$BIN" "$BIN_DIR/$BIN" && chmod 0755 "$BIN_DIR/$BIN"; }

echo "Installed $BIN to $BIN_DIR/$BIN"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "Note: $BIN_DIR is not on your PATH. Add it, e.g.:"
     echo "  echo 'export PATH=\"$BIN_DIR:\$PATH\"' >> ~/.zshrc" ;;
esac

echo
echo "Next steps:"
echo "  - Install ffmpeg if you haven't:  brew install ffmpeg   (or your package manager)"
echo "  - Download the browser once:      $BIN install"
