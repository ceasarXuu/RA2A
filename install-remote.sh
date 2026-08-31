#!/bin/sh
set -eu

fail() { printf 'RA2A install error: %s\n' "$*" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || fail 'curl is required'

RELEASE_ROOT=${RA2A_RELEASE_ROOT:-https://github.com/ceasarXuu/RA2A/releases}
VERSION=${RA2A_VERSION:-}
if [ -z "$VERSION" ]; then
  LATEST_URL=$(curl -fsSIL -o /dev/null -w '%{url_effective}' "$RELEASE_ROOT/latest")
  VERSION=${LATEST_URL##*/}
fi
case "$VERSION" in v[0-9]*) ;; *) fail "invalid release version: $VERSION" ;; esac

case "$(uname -s)" in
  Darwin) OS=darwin; CHECKSUM=shasum ;;
  Linux) OS=linux; CHECKSUM=sha256sum ;;
  *) fail "unsupported operating system: $(uname -s)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac
command -v "$CHECKSUM" >/dev/null 2>&1 || fail "$CHECKSUM is required"

ASSET=ra2a-$VERSION-$OS-$ARCH
DOWNLOAD_ROOT=$RELEASE_ROOT/download/$VERSION
TEMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/ra2a-release.XXXXXX")
trap 'rm -rf "$TEMP_DIR"' EXIT HUP INT TERM
curl -fsSL "$DOWNLOAD_ROOT/$ASSET" -o "$TEMP_DIR/$ASSET"
curl -fsSL "$DOWNLOAD_ROOT/$ASSET.sha256" -o "$TEMP_DIR/$ASSET.sha256"
EXPECTED=$(awk 'NR == 1 {print $1}' "$TEMP_DIR/$ASSET.sha256")
if [ "$OS" = darwin ]; then
  ACTUAL=$(shasum -a 256 "$TEMP_DIR/$ASSET" | awk '{print $1}')
else
  ACTUAL=$(sha256sum "$TEMP_DIR/$ASSET" | awk '{print $1}')
fi
[ -n "$EXPECTED" ] && [ "$EXPECTED" = "$ACTUAL" ] || fail 'release checksum verification failed'

BIN_DIR=$HOME/.local/bin
BIN_PATH=$BIN_DIR/ra2a
mkdir -p "$BIN_DIR"
cp "$TEMP_DIR/$ASSET" "$BIN_PATH.new"
chmod 755 "$BIN_PATH.new"
mv -f "$BIN_PATH.new" "$BIN_PATH"
printf 'RA2A %s installed\n' "$VERSION"
printf 'binary: %s\n' "$BIN_PATH"

if [ "$#" -gt 0 ]; then
  "$BIN_PATH" setup "$@"
elif [ -f "$HOME/.config/ra2a/config.json" ]; then
  "$BIN_PATH" restart
else
  printf 'Run %s to finish setup.\n' "$BIN_PATH"
fi
