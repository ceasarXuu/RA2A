#!/bin/sh
set -eu

fail() {
  printf 'RA2A install error: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: ./install.sh [--pin ABC123] [--node-id ID] [--name NAME] [--codex PATH]
       ./install.sh --uninstall

Builds RA2A from this checkout and installs a restarting user daemon.
If --pin is omitted, a long-lived six-character PIN is generated and printed.
EOF
}

shell_quote() {
  escaped=$(printf '%s' "$1" | sed "s/'/'\\\\''/g")
  printf "'%s'" "$escaped"
}

xml_escape() {
  printf '%s' "$1" | sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g'
}

PIN=
NODE_ID=$(hostname 2>/dev/null || printf 'ra2a-node')
NODE_NAME=
CODEX_PATH=
UNINSTALL=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --pin) [ "$#" -ge 2 ] || fail '--pin requires a value'; PIN=$2; shift 2 ;;
    --node-id) [ "$#" -ge 2 ] || fail '--node-id requires a value'; NODE_ID=$2; shift 2 ;;
    --name) [ "$#" -ge 2 ] || fail '--name requires a value'; NODE_NAME=$2; shift 2 ;;
    --codex) [ "$#" -ge 2 ] || fail '--codex requires a value'; CODEX_PATH=$2; shift 2 ;;
    --uninstall) UNINSTALL=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown option: $1" ;;
  esac
done

[ -n "$NODE_NAME" ] || NODE_NAME=$NODE_ID

OS_NAME=$(uname -s)
BIN_DIR=$HOME/.local/bin
BIN_PATH=$BIN_DIR/ra2a
CONFIG_DIR=$HOME/.config/ra2a
RUNNER_PATH=$CONFIG_DIR/run.sh
LOG_DIR=$CONFIG_DIR/logs

uninstall_ra2a() {
	MCP_CODEX=$CODEX_PATH
	if [ -z "$MCP_CODEX" ] && command -v codex >/dev/null 2>&1; then
		MCP_CODEX=$(command -v codex)
	elif [ -z "$MCP_CODEX" ] && [ -x /Applications/ChatGPT.app/Contents/Resources/codex ]; then
		MCP_CODEX=/Applications/ChatGPT.app/Contents/Resources/codex
	fi
	if [ -n "$MCP_CODEX" ] && [ -x "$MCP_CODEX" ]; then
		"$MCP_CODEX" mcp remove ra2a >/dev/null 2>&1 || true
	fi
  case "$OS_NAME" in
    Darwin)
      DOMAIN=gui/$(id -u)
      PLIST=$HOME/Library/LaunchAgents/com.ra2a.daemon.plist
      launchctl bootout "$DOMAIN/com.ra2a.daemon" >/dev/null 2>&1 || true
      rm -f "$PLIST"
      ;;
    Linux)
      UNIT=$HOME/.config/systemd/user/ra2a.service
      systemctl --user disable --now ra2a.service >/dev/null 2>&1 || true
      rm -f "$UNIT"
      systemctl --user daemon-reload >/dev/null 2>&1 || true
      ;;
    *) fail "unsupported operating system: $OS_NAME" ;;
  esac
  rm -f "$BIN_PATH" "$RUNNER_PATH"
  printf 'RA2A uninstalled for current user\n'
}

if [ "$UNINSTALL" -eq 1 ]; then
  uninstall_ra2a
  exit 0
fi

case "$OS_NAME" in Darwin|Linux) ;; *) fail "unsupported operating system: $OS_NAME" ;; esac

if [ -z "$PIN" ]; then
  PIN=$(od -An -N4 -tu4 /dev/urandom | awk '{printf "%06X", $1 % 16777216}')
fi
case "$PIN" in
  ??????) ;;
  *) fail 'PIN must be exactly 6 characters' ;;
esac
case "$PIN" in *[!A-Za-z0-9]*) fail 'PIN must contain only letters and digits' ;; esac
[ -n "$NODE_ID" ] || fail 'node ID cannot be empty'
[ -n "$NODE_NAME" ] || fail 'node name cannot be empty'

if [ -z "$CODEX_PATH" ]; then
  if command -v codex >/dev/null 2>&1; then
    CODEX_PATH=$(command -v codex)
  elif [ "$OS_NAME" = Darwin ] && [ -x /Applications/ChatGPT.app/Contents/Resources/codex ]; then
    CODEX_PATH=/Applications/ChatGPT.app/Contents/Resources/codex
  else
    fail 'Codex executable not found; pass --codex /absolute/path/to/codex'
  fi
fi
case "$CODEX_PATH" in /*) ;; *) CODEX_PATH=$(command -v "$CODEX_PATH" 2>/dev/null || true) ;; esac
[ -n "$CODEX_PATH" ] && [ -x "$CODEX_PATH" ] || fail 'Codex executable is not executable'
command -v go >/dev/null 2>&1 || fail 'Go 1.24 or newer is required to build from source'

SCRIPT_DIR=$(CDPATH= cd "$(dirname "$0")" && pwd)
BUILD_DIR=$(mktemp -d "${TMPDIR:-/tmp}/ra2a-install.XXXXXX")
trap 'rm -rf "$BUILD_DIR"' EXIT HUP INT TERM
(cd "$SCRIPT_DIR" && go build -trimpath -ldflags '-s -w' -o "$BUILD_DIR/ra2a" ./cmd/ra2a)

mkdir -p "$BIN_DIR" "$CONFIG_DIR" "$LOG_DIR"
cp "$BUILD_DIR/ra2a" "$BIN_PATH.new"
chmod 755 "$BIN_PATH.new"
mv -f "$BIN_PATH.new" "$BIN_PATH"

"$CODEX_PATH" mcp remove ra2a >/dev/null 2>&1 || true
"$CODEX_PATH" mcp add ra2a -- "$BIN_PATH" mcp

{
  printf '#!/bin/sh\nexec '
  shell_quote "$BIN_PATH"
  printf ' serve --pin '
  shell_quote "$PIN"
  printf ' --id '
  shell_quote "$NODE_ID"
  printf ' --name '
  shell_quote "$NODE_NAME"
  printf ' --codex '
  shell_quote "$CODEX_PATH"
  printf '\n'
} >"$RUNNER_PATH"
chmod 700 "$RUNNER_PATH"

if [ "$OS_NAME" = Darwin ]; then
  SERVICE_DIR=$HOME/Library/LaunchAgents
  SERVICE_PATH=$SERVICE_DIR/com.ra2a.daemon.plist
  mkdir -p "$SERVICE_DIR"
  RUNNER_XML=$(xml_escape "$RUNNER_PATH")
  LOG_XML=$(xml_escape "$LOG_DIR")
  cat >"$SERVICE_PATH" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>com.ra2a.daemon</string>
<key>ProgramArguments</key><array><string>/bin/sh</string><string>$RUNNER_XML</string></array>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>StandardOutPath</key><string>$LOG_XML/ra2a.log</string>
<key>StandardErrorPath</key><string>$LOG_XML/ra2a.err.log</string>
</dict></plist>
EOF
  DOMAIN=gui/$(id -u)
  launchctl bootout "$DOMAIN/com.ra2a.daemon" >/dev/null 2>&1 || true
  launchctl bootstrap "$DOMAIN" "$SERVICE_PATH"
  launchctl kickstart -k "$DOMAIN/com.ra2a.daemon"
  launchctl print "$DOMAIN/com.ra2a.daemon" >/dev/null
else
  command -v systemctl >/dev/null 2>&1 || fail 'systemd user services are required on Linux'
  SERVICE_DIR=$HOME/.config/systemd/user
  SERVICE_PATH=$SERVICE_DIR/ra2a.service
  mkdir -p "$SERVICE_DIR"
  cat >"$SERVICE_PATH" <<EOF
[Unit]
Description=RA2A LAN agent daemon
After=network-online.target

[Service]
ExecStart=/bin/sh "$RUNNER_PATH"
Restart=always
RestartSec=2

[Install]
WantedBy=default.target
EOF
  systemctl --user daemon-reload
  systemctl --user enable --now ra2a.service
  systemctl --user is-active --quiet ra2a.service
fi

printf 'RA2A installed\n'
printf 'binary: %s\n' "$BIN_PATH"
printf 'node: %s\n' "$NODE_ID"
printf 'PIN: %s\n' "$PIN"
printf 'status: running\n'
