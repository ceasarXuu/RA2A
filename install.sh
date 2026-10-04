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
Supported harnesses are detected automatically. Codex CLI and OpenCode launchers
are installed when their native commands exist; no wrapper options are needed.
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
    --codex-wrapper|--opencode-wrapper) shift ;; # accepted for older callers; detection is automatic
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
OC_WRAPPER_PATH=$BIN_DIR/opencode
OC_WRAPPER_MARKER=$BIN_DIR/.ra2a-opencode-wrapper

if [ "$UNINSTALL" -eq 1 ]; then
  if [ -x "$BIN_PATH" ]; then
    if [ -f "${PI_CODING_AGENT_DIR:-$HOME/.pi/agent}/extensions/ra2a.mjs" ]; then "$BIN_PATH" pi-unregister || fail 'could not unregister Pi extension'; fi
    "$BIN_PATH" opencode-mcp-unregister || fail 'could not unregister OpenCode MCP'
    "$BIN_PATH" opencode-server-cleanup || fail 'could not stop the shared OpenCode server'
  fi
  MCP_CODEX=$CODEX_PATH
  if [ -z "$MCP_CODEX" ] && command -v codex >/dev/null 2>&1; then MCP_CODEX=$(command -v codex); fi
  # Run the MCP cleanup before removing a wrapper so `mcp` still passes through.
  if [ -n "$MCP_CODEX" ] && [ -x "$MCP_CODEX" ]; then "$MCP_CODEX" mcp remove ra2a >/dev/null 2>&1 || true; fi
  if [ -f "$WRAPPER_MARKER" ]; then
    if [ -L "$BIN_DIR/codex.bin" ]; then
      rm -f "$BIN_DIR/codex" "$BIN_DIR/codex.bin"
    elif [ -e "$BIN_DIR/codex.bin" ]; then
      mv -f "$BIN_DIR/codex.bin" "$WRAPPER_PATH"
    else
      rm -f "$WRAPPER_PATH"
    fi
    rm -f "$WRAPPER_MARKER"
    printf 'RA2A codex wrapper removed; the native codex command is restored.\n'
  fi
  if [ -f "$OC_WRAPPER_MARKER" ]; then
    if [ -e "$BIN_DIR/opencode.real" ] || [ -L "$BIN_DIR/opencode.real" ]; then
      mv -f "$BIN_DIR/opencode.real" "$OC_WRAPPER_PATH"
    else
      rm -f "$OC_WRAPPER_PATH"
    fi
    rm -f "$OC_WRAPPER_MARKER"
    printf 'RA2A opencode wrapper removed; the native opencode command is restored.\n'
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
CODEX_NATIVE=$CODEX_PATH
if [ -n "$CODEX_NATIVE" ] && [ ! -x "$CODEX_NATIVE" ]; then fail "Codex executable is not runnable: $CODEX_NATIVE"; fi
if [ -z "$CODEX_NATIVE" ] && command -v codex >/dev/null 2>&1; then CODEX_NATIVE=$(command -v codex); fi
if [ -z "$CODEX_NATIVE" ] && [ -x "$WRAPPER_PATH" ]; then CODEX_NATIVE=$WRAPPER_PATH; fi
if [ -f "$WRAPPER_MARKER" ]; then
  if [ -e "$BIN_DIR/codex.bin" ]; then
    CODEX_NATIVE=$BIN_DIR/codex.bin
  else
    CODEX_NATIVE=
    old_ifs=$IFS; IFS=:
    for directory in $PATH; do
      [ -n "$directory" ] || directory=.
      if [ "$directory/codex" != "$WRAPPER_PATH" ] && [ -x "$directory/codex" ]; then
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
      mv "$WRAPPER_PATH" "$WRAPPER_PATH.retired-$(date +%s)"
      mv "$WRAPPER_MARKER" "$WRAPPER_MARKER.retired-$(date +%s)"
    fi
  fi
fi
OC_NATIVE=
if command -v opencode >/dev/null 2>&1; then OC_NATIVE=$(command -v opencode); fi
if [ -z "$OC_NATIVE" ] && [ -x "$OC_WRAPPER_PATH" ]; then OC_NATIVE=$OC_WRAPPER_PATH; fi
if [ -f "$OC_WRAPPER_MARKER" ]; then
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
      mv "$OC_WRAPPER_MARKER" "$OC_WRAPPER_MARKER.retired-$(date +%s)"
    fi
  fi
fi
CODEX_FOR_CONFIG=$CODEX_NATIVE
if [ -z "$CODEX_FOR_CONFIG" ] && [ "$OS_NAME" = Darwin ] && [ -x /Applications/ChatGPT.app/Contents/Resources/codex ]; then
  CODEX_FOR_CONFIG=/Applications/ChatGPT.app/Contents/Resources/codex
fi
if [ -n "$CODEX_NATIVE" ]; then printf 'detected harness: Codex CLI (%s)\n' "$CODEX_NATIVE"; fi
if [ -n "$OC_NATIVE" ]; then printf 'detected harness: OpenCode (%s)\n' "$OC_NATIVE"; fi
if [ -n "$CODEX_FOR_CONFIG" ] && [ -z "$CODEX_NATIVE" ]; then printf 'detected harness: Codex App (%s)\n' "$CODEX_FOR_CONFIG"; fi
if [ -z "$CODEX_FOR_CONFIG" ] && [ -z "$OC_NATIVE" ]; then printf 'no supported harness detected; RA2A command only will be installed\n'; fi
BUILD_DIR=$(mktemp -d "${TMPDIR:-/tmp}/ra2a-install.XXXXXX")
trap 'rm -rf "$BUILD_DIR"' EXIT HUP INT TERM
(cd "$SCRIPT_DIR" && go build -trimpath -ldflags '-s -w' -o "$BUILD_DIR/ra2a" ./cmd/ra2a)
if [ -n "$CODEX_NATIVE" ]; then
  (cd "$SCRIPT_DIR" && go build -trimpath -ldflags '-s -w' -o "$BUILD_DIR/codex-wrapper" ./cmd/codex-wrapper)
fi
if [ -n "$OC_NATIVE" ]; then
  (cd "$SCRIPT_DIR" && go build -trimpath -ldflags '-s -w' -o "$BUILD_DIR/oc-wrapper" ./cmd/oc-wrapper)
fi
mkdir -p "$BIN_DIR"
cp "$BUILD_DIR/ra2a" "$BIN_PATH.new"
chmod 755 "$BIN_PATH.new"
mv -f "$BIN_PATH.new" "$BIN_PATH"

if [ -n "$CODEX_NATIVE" ]; then
  cp "$BUILD_DIR/codex-wrapper" "$WRAPPER_PATH.new"
  chmod 755 "$WRAPPER_PATH.new"
  if [ ! -f "$WRAPPER_MARKER" ]; then
    [ ! -e "$BIN_DIR/codex.bin" ] && [ ! -L "$BIN_DIR/codex.bin" ] || fail "codex.bin already exists at $BIN_DIR/codex.bin; refusing to overwrite it"
    if [ -e "$WRAPPER_PATH" ] || [ -L "$WRAPPER_PATH" ]; then
      mv -f "$WRAPPER_PATH" "$BIN_DIR/codex.bin"
    else
      ln -s "$CODEX_NATIVE" "$BIN_DIR/codex.bin"
    fi
  fi
  mv -f "$WRAPPER_PATH.new" "$WRAPPER_PATH"
  : > "$WRAPPER_MARKER"
  printf 'RA2A codex wrapper installed (plain codex TUI sessions are proxied when RA2A is available)\n'
fi

if [ -n "$OC_NATIVE" ]; then
  cp "$BUILD_DIR/oc-wrapper" "$OC_WRAPPER_PATH.new"
  chmod 755 "$OC_WRAPPER_PATH.new"
  if [ ! -f "$OC_WRAPPER_MARKER" ] && { [ -e "$OC_WRAPPER_PATH" ] || [ -L "$OC_WRAPPER_PATH" ]; }; then
    if [ -e "$BIN_DIR/opencode.real" ] || [ -L "$BIN_DIR/opencode.real" ]; then
      fail "opencode.real already exists at $BIN_DIR/opencode.real; refusing to overwrite it"
    fi
    mv -f "$OC_WRAPPER_PATH" "$BIN_DIR/opencode.real"
  fi
  mv -f "$OC_WRAPPER_PATH.new" "$OC_WRAPPER_PATH"
  : > "$OC_WRAPPER_MARKER"
  printf 'RA2A opencode wrapper installed (interactive opencode now attaches automatically);\n'
  printf 'non-interactive subcommands pass through to native opencode.\n'
fi

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) printf 'RA2A launcher directory is not on PATH: add %s before other harness binaries when opening a new terminal.\n' "$BIN_DIR" ;;
esac

printf 'RA2A command installed\n'
printf 'binary: %s\n' "$BIN_PATH"
if [ "$SETUP" -eq 0 ]; then
  if [ -f "$HOME/.config/ra2a/config.json" ]; then
    "$BIN_PATH" restart
    printf 'RA2A service restarted with detected harnesses.\n'
    exit 0
  fi
  printf 'Run ra2a to finish setup.\n'
  exit 0
fi

case "$PIN" in ??????) ;; *) fail 'PIN must be exactly 6 characters' ;; esac
case "$PIN" in *[!A-Za-z0-9]*) fail 'PIN must contain only letters and digits' ;; esac
[ -n "$NODE_NAME" ] || NODE_NAME=$NODE_ID
if [ -z "$CODEX_FOR_CONFIG" ] && [ -z "$OC_NATIVE" ] && ! command -v pi >/dev/null 2>&1; then
  fail 'no supported harness found; install Codex, OpenCode or Pi before setup'
fi
set -- setup --pin "$PIN" --node-id "$NODE_ID" --name "$NODE_NAME"
if [ -n "$CODEX_FOR_CONFIG" ]; then set -- "$@" --codex "$CODEX_FOR_CONFIG"; fi
if [ -n "$OC_NATIVE" ]; then set -- "$@" --opencode "$OC_NATIVE"; fi
"$BIN_PATH" "$@"
