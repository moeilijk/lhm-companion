#!/bin/sh

set -eu

BINARY="${BINARY:-lhm-companion}"
BIN_DIR="${BIN_DIR:-/usr/local/bin}"
UNIT_DIR="${UNIT_DIR:-/etc/systemd/system}"
STATE_DIR="${STATE_DIR:-/var/lib/lhm-companion}"
SYSTEMCTL="${SYSTEMCTL:-systemctl}"
METADATA_FILE="${STATE_DIR}/install.env"

requested_binary="$BINARY"
requested_bin_dir="$BIN_DIR"
requested_unit_dir="$UNIT_DIR"
requested_state_dir="$STATE_DIR"

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "Error: required command not found: $1" >&2
		exit 127
	}
}

require_file() {
	path="$1"
	label="$2"
	if [ ! -f "$path" ]; then
		echo "Error: expected $label at $path" >&2
		exit 1
	fi
}

hash_file() {
	set -- $(sha256sum "$1")
	printf '%s\n' "$1"
}

if [ "$(id -u)" -ne 0 ]; then
	echo "Error: run this script as root, for example via 'sudo sh'." >&2
	exit 1
fi

need_cmd rm
need_cmd grep
need_cmd cut
need_cmd sha256sum
need_cmd "$SYSTEMCTL"

require_file "$METADATA_FILE" "install metadata"

# shellcheck disable=SC1090
. "$METADATA_FILE"

expected_binary_path="${requested_bin_dir}/${requested_binary}"
expected_unit_path="${requested_unit_dir}/${requested_binary}.service"
expected_metadata_path="${requested_state_dir}/install.env"

if [ "${BINARY:-}" != "$requested_binary" ]; then
	echo "Error: metadata binary name mismatch: ${BINARY:-unset}" >&2
	exit 1
fi
if [ "$METADATA_FILE" != "$expected_metadata_path" ]; then
	echo "Error: metadata path mismatch: $METADATA_FILE" >&2
	exit 1
fi
if [ "${BINARY_PATH:-}" != "$expected_binary_path" ]; then
	echo "Error: metadata binary path mismatch: ${BINARY_PATH:-unset}" >&2
	exit 1
fi
if [ "${UNIT_PATH:-}" != "$expected_unit_path" ]; then
	echo "Error: metadata unit path mismatch: ${UNIT_PATH:-unset}" >&2
	exit 1
fi
if [ "${SERVICE_NAME:-}" != "${BINARY}.service" ]; then
	echo "Error: metadata service name mismatch: ${SERVICE_NAME:-unset}" >&2
	exit 1
fi

require_file "$BINARY_PATH" "installed binary"
require_file "$UNIT_PATH" "installed unit"

unit_execstart=$(grep -m1 -E '^ExecStart=' "$UNIT_PATH" | cut -d= -f2- || true)
if [ "$unit_execstart" != "${UNIT_EXECSTART:-}" ]; then
	echo "Error: unit ExecStart mismatch: $unit_execstart" >&2
	exit 1
fi

binary_sha=$(hash_file "$BINARY_PATH")
unit_sha=$(hash_file "$UNIT_PATH")

if [ "$binary_sha" != "${BINARY_SHA256:-}" ]; then
	echo "Error: binary checksum mismatch; refusing to remove $BINARY_PATH" >&2
	exit 1
fi
if [ "$unit_sha" != "${UNIT_SHA256:-}" ]; then
	echo "Error: unit checksum mismatch; refusing to remove $UNIT_PATH" >&2
	exit 1
fi

"$SYSTEMCTL" disable --now "$SERVICE_NAME" >/dev/null 2>&1 || true
rm -f "$BINARY_PATH" "$UNIT_PATH" "$METADATA_FILE"
rmdir "$STATE_DIR" >/dev/null 2>&1 || true
"$SYSTEMCTL" daemon-reload
"$SYSTEMCTL" reset-failed "$SERVICE_NAME" >/dev/null 2>&1 || true

echo "Removed $BINARY_PATH"
echo "Removed $UNIT_PATH"
echo "Removed $METADATA_FILE"
echo "Disabled and stopped $SERVICE_NAME"
