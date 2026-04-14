#!/bin/sh

set -eu

BINARY="${BINARY:-lhm-companion}"
BIN_DIR="${BIN_DIR:-/usr/local/bin}"
UNIT_DIR="${UNIT_DIR:-/etc/systemd/system}"
SYSTEMCTL="${SYSTEMCTL:-systemctl}"

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "Error: required command not found: $1" >&2
		exit 127
	}
}

if [ "$(id -u)" -ne 0 ]; then
	echo "Error: run this script as root, for example via 'sudo sh'." >&2
	exit 1
fi

need_cmd rm
need_cmd "$SYSTEMCTL"

"$SYSTEMCTL" disable --now "$BINARY.service" >/dev/null 2>&1 || true
rm -f "$BIN_DIR/$BINARY" "$UNIT_DIR/$BINARY.service"
"$SYSTEMCTL" daemon-reload
"$SYSTEMCTL" reset-failed "$BINARY.service" >/dev/null 2>&1 || true

echo "Removed $BIN_DIR/$BINARY"
echo "Removed $UNIT_DIR/$BINARY.service"
echo "Disabled and stopped $BINARY.service if it existed"
