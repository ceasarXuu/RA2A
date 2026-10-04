#!/bin/sh
set -eu

fail() { printf 'RA2A install error: %s\n' "$*" >&2; exit 1; }
if [ "${1:-}" = --uninstall ]; then
  BIN_DIR=$HOME/.local/bin
  BIN_PATH=$BIN_DIR/ra2a
  if [ -x "$BIN_PATH" ]; then
    if [ -f "${PI_CODING_AGENT_DIR:-$HOME/.pi/agent}/extensions/ra2a.mjs" ]; then "$BIN_PATH" pi-unregister || fail 'could not unregister Pi extension'; fi
    "$BIN_PATH" opencode-mcp-unregister || fail 'could not unregister OpenCode MCP'
    "$BIN_PATH" opencode-server-cleanup || fail 'could not stop the shared OpenCode server'
  fi
  if command -v codex >/dev/null 2>&1; then codex mcp remove ra2a >/dev/null 2>&1 || true; fi
  if [ -f "$BIN_DIR/.ra2a-codex-wrapper" ]; then
    if [ -L "$BIN_DIR/codex.bin" ]; then
      rm -f "$BIN_DIR/codex" "$BIN_DIR/codex.bin"
    elif [ -e "$BIN_DIR/codex.bin" ]; then
      mv -f "$BIN_DIR/codex.bin" "$BIN_DIR/codex"
    else
      rm -f "$BIN_DIR/codex"
    fi
    rm -f "$BIN_DIR/.ra2a-codex-wrapper"
  fi
  if [ -f "$BIN_DIR/.ra2a-opencode-wrapper" ]; then
    if [ -e "$BIN_DIR/opencode.real" ] || [ -L "$BIN_DIR/opencode.real" ]; then
      mv -f "$BIN_DIR/opencode.real" "$BIN_DIR/opencode"
    else
      rm -f "$BIN_DIR/opencode"
    fi
    rm -f "$BIN_DIR/.ra2a-opencode-wrapper"
  fi
  case "$(uname -s)" in
    Darwin)
      launchctl bootout "gui/$(id -u)/com.ra2a.daemon" >/dev/null 2>&1 || true
      rm -f "$HOME/Library/LaunchAgents/com.ra2a.daemon.plist" ;;
    Linux)
      systemctl --user disable --now ra2a.service >/dev/null 2>&1 || true
      rm -f "$HOME/.config/systemd/user/ra2a.service"
      systemctl --user daemon-reload >/dev/null 2>&1 || true ;;
    *) fail 'unsupported operating system' ;;
  esac
  rm -f "$BIN_PATH"
  printf 'RA2A uninstalled; native harness commands restored\n'
  exit 0
fi
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

BIN_DIR=$HOME/.local/bin
BIN_PATH=$BIN_DIR/ra2a
CODEX_WRAPPER_PATH=$BIN_DIR/codex
CODEX_MARKER=$BIN_DIR/.ra2a-codex-wrapper
OC_WRAPPER_PATH=$BIN_DIR/opencode
OC_MARKER=$BIN_DIR/.ra2a-opencode-wrapper
CODEX_NATIVE=
if command -v codex >/dev/null 2>&1; then CODEX_NATIVE=$(command -v codex); fi
if [ -z "$CODEX_NATIVE" ] && [ -x "$CODEX_WRAPPER_PATH" ]; then CODEX_NATIVE=$CODEX_WRAPPER_PATH; fi
if [ -f "$CODEX_MARKER" ]; then
  if [ -e "$BIN_DIR/codex.bin" ]; then
    CODEX_NATIVE=$BIN_DIR/codex.bin
  else
    CODEX_NATIVE=
    old_ifs=$IFS; IFS=:
    for directory in $PATH; do
      [ -n "$directory" ] || directory=.
      if [ "$directory/codex" != "$CODEX_WRAPPER_PATH" ] && [ -x "$directory/codex" ]; then
        CODEX_NATIVE=$directory/codex
        break
      fi
    done
    IFS=$old_ifs
    if [ -n "$CODEX_NATIVE" ]; then
      if [ -L "$BIN_DIR/codex.bin" ]; then mv "$BIN_DIR/codex.bin" "$BIN_DIR/codex.bin.stale-$(date +%s)"; fi
      ln -s "$CODEX_NATIVE" "$BIN_DIR/codex.bin"
      CODEX_NATIVE=$BIN_DIR/codex.bin
    else
      mv "$CODEX_WRAPPER_PATH" "$CODEX_WRAPPER_PATH.retired-$(date +%s)"
      mv "$CODEX_MARKER" "$CODEX_MARKER.retired-$(date +%s)"
    fi
  fi
fi
OC_NATIVE=
if command -v opencode >/dev/null 2>&1; then OC_NATIVE=$(command -v opencode); fi
if [ -z "$OC_NATIVE" ] && [ -x "$OC_WRAPPER_PATH" ]; then OC_NATIVE=$OC_WRAPPER_PATH; fi
if [ -f "$OC_MARKER" ]; then
  if [ -x "$BIN_DIR/opencode.real" ]; then
    OC_NATIVE=$BIN_DIR/opencode.real
  else
    OC_NATIVE=
    old_ifs=$IFS; IFS=:
    for directory in $PATH; do
      [ -n "$directory" ] || directory=.
      if [ "$directory/opencode" != "$OC_WRAPPER_PATH" ] && [ -x "$directory/opencode" ]; then
        OC_NATIVE=$directory/opencode
        break
      fi
    done
    IFS=$old_ifs
    if [ -z "$OC_NATIVE" ]; then
      mv "$OC_WRAPPER_PATH" "$OC_WRAPPER_PATH.retired-$(date +%s)"
      mv "$OC_MARKER" "$OC_MARKER.retired-$(date +%s)"
    fi
  fi
fi
if [ -n "$CODEX_NATIVE" ]; then printf 'detected harness: Codex CLI (%s)\n' "$CODEX_NATIVE"; fi
if [ -n "$OC_NATIVE" ]; then printf 'detected harness: OpenCode (%s)\n' "$OC_NATIVE"; fi

ASSET=ra2a-$VERSION-$OS-$ARCH
DOWNLOAD_ROOT=$RELEASE_ROOT/download/$VERSION
TEMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/ra2a-release.XXXXXX")
trap 'rm -rf "$TEMP_DIR"' EXIT HUP INT TERM
download_verified() {
  asset=$1
  curl -fsSL "$DOWNLOAD_ROOT/$asset" -o "$TEMP_DIR/$asset"
  curl -fsSL "$DOWNLOAD_ROOT/$asset.sha256" -o "$TEMP_DIR/$asset.sha256"
  expected=$(awk 'NR == 1 {print $1}' "$TEMP_DIR/$asset.sha256")
  if [ "$OS" = darwin ]; then
    actual=$(shasum -a 256 "$TEMP_DIR/$asset" | awk '{print $1}')
  else
    actual=$(sha256sum "$TEMP_DIR/$asset" | awk '{print $1}')
  fi
  [ -n "$expected" ] && [ "$expected" = "$actual" ] || fail "release checksum verification failed: $asset"
}
download_verified "$ASSET"
if [ -n "$CODEX_NATIVE" ]; then
  CODEX_ASSET=codex-wrapper-$VERSION-$OS-$ARCH
  download_verified "$CODEX_ASSET"
fi
if [ -n "$OC_NATIVE" ]; then
  OC_ASSET=opencode-wrapper-$VERSION-$OS-$ARCH
  download_verified "$OC_ASSET"
fi
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) printf 'RA2A launcher directory is not on PATH: add %s before other harness binaries when opening a new terminal.\n' "$BIN_DIR" ;;
esac

mkdir -p "$BIN_DIR"
cp "$TEMP_DIR/$ASSET" "$BIN_PATH.new"
chmod 755 "$BIN_PATH.new"
mv -f "$BIN_PATH.new" "$BIN_PATH"

if [ -n "$CODEX_NATIVE" ]; then
  cp "$TEMP_DIR/$CODEX_ASSET" "$CODEX_WRAPPER_PATH.new"
  chmod 755 "$CODEX_WRAPPER_PATH.new"
  if [ ! -f "$CODEX_MARKER" ]; then
    [ ! -e "$BIN_DIR/codex.bin" ] && [ ! -L "$BIN_DIR/codex.bin" ] || fail 'codex.bin already exists; refusing to overwrite it'
    if [ -e "$CODEX_WRAPPER_PATH" ] || [ -L "$CODEX_WRAPPER_PATH" ]; then
      mv -f "$CODEX_WRAPPER_PATH" "$BIN_DIR/codex.bin"
    else
      ln -s "$CODEX_NATIVE" "$BIN_DIR/codex.bin"
    fi
  fi
  mv -f "$CODEX_WRAPPER_PATH.new" "$CODEX_WRAPPER_PATH"
  : > "$CODEX_MARKER"
  printf 'Codex CLI launcher automatically installed\n'
fi
if [ -n "$OC_NATIVE" ]; then
  cp "$TEMP_DIR/$OC_ASSET" "$OC_WRAPPER_PATH.new"
  chmod 755 "$OC_WRAPPER_PATH.new"
  if [ ! -f "$OC_MARKER" ] && { [ -e "$OC_WRAPPER_PATH" ] || [ -L "$OC_WRAPPER_PATH" ]; }; then
    [ ! -e "$BIN_DIR/opencode.real" ] && [ ! -L "$BIN_DIR/opencode.real" ] || fail 'opencode.real already exists; refusing to overwrite it'
    mv -f "$OC_WRAPPER_PATH" "$BIN_DIR/opencode.real"
  fi
  mv -f "$OC_WRAPPER_PATH.new" "$OC_WRAPPER_PATH"
  : > "$OC_MARKER"
  printf 'OpenCode launcher automatically installed (ordinary TUI attaches to RA2A)\n'
fi
printf 'RA2A %s installed\n' "$VERSION"
printf 'binary: %s\n' "$BIN_PATH"

if [ "$#" -gt 0 ]; then
  "$BIN_PATH" setup "$@"
elif [ -f "$HOME/.config/ra2a/config.json" ]; then
  "$BIN_PATH" restart
else
  printf 'Run %s to finish setup.\n' "$BIN_PATH"
fi
