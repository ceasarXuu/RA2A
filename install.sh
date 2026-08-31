#!/bin/sh
set -eu

fail() { printf 'RA2A install error: %s\n' "$*" >&2; exit 1; }
usage() {
  cat <<'EOF'
Usage: ./install.sh
       ./install.sh --pin ABC123 --node-id ID [--name NAME] [--codex PATH]
       ./install.sh --uninstall

Without setup options, installs the command only. Run ra2a to finish setup.
With a PIN, performs an Agent-friendly non-interactive setup.
EOF
}

PIN=
NODE_ID=$(hostname 2>/dev/null || printf 'ra2a-node')
NODE_NAME=
CODEX_PATH=
SETUP=0
UNINSTALL=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --pin) [ "$#" -ge 2 ] || fail '--pin requires a value'; PIN=$2; SETUP=1; shift 2 ;;
    --node-id) [ "$#" -ge 2 ] || fail '--node-id requires a value'; NODE_ID=$2; SETUP=1; shift 2 ;;
    --name) [ "$#" -ge 2 ] || fail '--name requires a value'; NODE_NAME=$2; SETUP=1; shift 2 ;;
    --codex) [ "$#" -ge 2 ] || fail '--codex requires a value'; CODEX_PATH=$2; SETUP=1; shift 2 ;;
    --uninstall) UNINSTALL=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown option: $1" ;;
  esac
done

OS_NAME=$(uname -s)
BIN_DIR=$HOME/.local/bin
BIN_PATH=$BIN_DIR/ra2a

if [ "$UNINSTALL" -eq 1 ]; then
  MCP_CODEX=$CODEX_PATH
  if [ -z "$MCP_CODEX" ] && command -v codex >/dev/null 2>&1; then MCP_CODEX=$(command -v codex); fi
  if [ -n "$MCP_CODEX" ]; then "$MCP_CODEX" mcp remove ra2a >/dev/null 2>&1 || true; fi
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
