#!/bin/sh
set -eu

fail() { printf 'RA2A install error: %s\n' "$*" >&2; exit 1; }
usage() {
  cat <<'EOF'
Usage: ./install.sh
       ./install.sh --pin ABC123 --node-id ID [--name NAME] [--codex PATH]
       ./install.sh --codex-wrapper
       ./install.sh --uninstall

Without setup options, installs the command only. Run ra2a to finish setup.
With a PIN, performs an Agent-friendly non-interactive setup.
--codex-wrapper installs the codex launcher that proxies plain TUI sessions
when RA2A is available and otherwise passes through the native codex.
EOF
}

PIN=
NODE_ID=$(hostname 2>/dev/null || printf 'ra2a-node')
NODE_NAME=
CODEX_PATH=
WRAPPER=0
SETUP=0
UNINSTALL=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --pin) [ "$#" -ge 2 ] || fail '--pin requires a value'; PIN=$2; SETUP=1; shift 2 ;;
    --node-id) [ "$#" -ge 2 ] || fail '--node-id requires a value'; NODE_ID=$2; SETUP=1; shift 2 ;;
    --name) [ "$#" -ge 2 ] || fail '--name requires a value'; NODE_NAME=$2; SETUP=1; shift 2 ;;
    --codex) [ "$#" -ge 2 ] || fail '--codex requires a value'; CODEX_PATH=$2; SETUP=1; shift 2 ;;
    --codex-wrapper) WRAPPER=1; shift ;;
    --uninstall) UNINSTALL=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown option: $1" ;;
  esac
done

OS_NAME=$(uname -s)
BIN_DIR=$HOME/.local/bin
BIN_PATH=$BIN_DIR/ra2a
WRAPPER_PATH=$BIN_DIR/codex
WRAPPER_MARKER=$BIN_DIR/.ra2a-codex-wrapper

if [ "$UNINSTALL" -eq 1 ]; then
  MCP_CODEX=$CODEX_PATH
  if [ -z "$MCP_CODEX" ] && command -v codex >/dev/null 2>&1; then MCP_CODEX=$(command -v codex); fi
  # Run the MCP cleanup before removing a wrapper so `mcp` still passes through.
  if [ -n "$MCP_CODEX" ] && [ -x "$MCP_CODEX" ]; then "$MCP_CODEX" mcp remove ra2a >/dev/null 2>&1 || true; fi
  if [ -f "$WRAPPER_MARKER" ]; then
    rm -f "$BIN_DIR/codex" "$WRAPPER_MARKER"
    printf 'RA2A codex wrapper removed; the native codex command is restored.\n'
  fi
  case "$OS_NAME" in
    Darwin)
      DOMAIN=gui/$(id -u)
      launchctl bootout "$DOMAIN/com.ra2a.daemon" >/dev/null 2>&1 || true
      rm -f "$HOME/Library/LaunchAgents/com.ra2a.daemon.plist"
      ;;
    Linux)
      systemctl --user disable --now ra2a.service >/dev/null 2>&1 || true
      rm -f "$HOME/.config/systemd/user/ra2a.service"
      systemctl --user daemon-reload >/dev/null 2>&1 || true
      ;;
    *) fail "unsupported operating system: $OS_NAME" ;;
  esac
  rm -f "$BIN_PATH"
  printf 'RA2A uninstalled for current user\n'
  exit 0
fi

case "$OS_NAME" in Darwin|Linux) ;; *) fail "unsupported operating system: $OS_NAME" ;; esac
command -v go >/dev/null 2>&1 || fail 'Go 1.24 or newer is required to build from source'
SCRIPT_DIR=$(CDPATH= cd "$(dirname "$0")" && pwd)
BUILD_DIR=$(mktemp -d "${TMPDIR:-/tmp}/ra2a-install.XXXXXX")
trap 'rm -rf "$BUILD_DIR"' EXIT HUP INT TERM
(cd "$SCRIPT_DIR" && go build -trimpath -ldflags '-s -w' -o "$BUILD_DIR/ra2a" ./cmd/ra2a)
mkdir -p "$BIN_DIR"
cp "$BUILD_DIR/ra2a" "$BIN_PATH.new"
chmod 755 "$BIN_PATH.new"
mv -f "$BIN_PATH.new" "$BIN_PATH"

if [ "$WRAPPER" -eq 1 ]; then
  if [ -e "$WRAPPER_PATH" ] && [ ! -f "$WRAPPER_MARKER" ]; then
    fail "codex already exists at $WRAPPER_PATH without the RA2A marker; refusing to overwrite it"
  fi
  (cd "$SCRIPT_DIR" && go build -trimpath -ldflags '-s -w' -o "$BUILD_DIR/codex-wrapper" ./cmd/codex-wrapper)
  cp "$BUILD_DIR/codex-wrapper" "$WRAPPER_PATH.new"
  chmod 755 "$WRAPPER_PATH.new"
  mv -f "$WRAPPER_PATH.new" "$WRAPPER_PATH"
  : > "$WRAPPER_MARKER"
  printf 'RA2A codex wrapper installed (plain codex TUI sessions are proxied when RA2A is available)\n'
fi

printf 'RA2A command installed\n'
printf 'binary: %s\n' "$BIN_PATH"
if [ "$SETUP" -eq 0 ]; then
  printf 'Run ra2a to finish setup.\n'
  exit 0
fi

case "$PIN" in ??????) ;; *) fail 'PIN must be exactly 6 characters' ;; esac
case "$PIN" in *[!A-Za-z0-9]*) fail 'PIN must contain only letters and digits' ;; esac
[ -n "$NODE_NAME" ] || NODE_NAME=$NODE_ID
if [ -z "$CODEX_PATH" ]; then
  if command -v codex >/dev/null 2>&1; then
    CODEX_PATH=$(command -v codex)
  elif [ "$OS_NAME" = Darwin ] && [ -x /Applications/ChatGPT.app/Contents/Resources/codex ]; then
    CODEX_PATH=/Applications/ChatGPT.app/Contents/Resources/codex
  else
    fail 'Codex executable not found; pass --codex /absolute/path/to/codex'
  fi
fi
"$BIN_PATH" setup --pin "$PIN" --node-id "$NODE_ID" --name "$NODE_NAME" --codex "$CODEX_PATH"
