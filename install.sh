#!/bin/sh

set -eu

REPO="${REPO:-moeilijk/lhm-companion}"
BINARY="${BINARY:-lhm-companion}"
VERSION="${VERSION:-latest}"
BIN_DIR="${BIN_DIR:-/usr/local/bin}"
UNIT_DIR="${UNIT_DIR:-/etc/systemd/system}"
SYSTEMCTL="${SYSTEMCTL:-systemctl}"
BASE_URL="${BASE_URL:-}"

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "Error: required command not found: $1" >&2
		exit 127
	}
}

download() {
	target="$1"
	url="$2"
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL -o "$target" "$url"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$target" "$url"
	else
		echo "Error: curl or wget is required." >&2
		exit 127
	fi
}

if [ "$(id -u)" -ne 0 ]; then
	echo "Error: run this script as root, for example via 'sudo sh'." >&2
	exit 1
fi

need_cmd tar
need_cmd sha256sum
need_cmd install
need_cmd "$SYSTEMCTL"

os="$(uname -s)"
arch="$(uname -m)"

if [ "$os" != "Linux" ]; then
	echo "Error: this installer only supports Linux." >&2
	exit 1
fi

if [ "$arch" != "x86_64" ] && [ "$arch" != "amd64" ]; then
	echo "Error: this installer only supports x86_64/amd64." >&2
	exit 1
fi

asset_base="${BINARY}_linux_amd64.tar.gz"

if [ -n "$BASE_URL" ]; then
	base_url="$BASE_URL"
else
	case "$VERSION" in
	latest)
		base_url="https://github.com/${REPO}/releases/latest/download"
		;;
	v*)
		base_url="https://github.com/${REPO}/releases/download/${VERSION}"
		;;
	*)
		echo "Error: VERSION must be 'latest' or a tag like 'v0.1.0'." >&2
		exit 1
		;;
	esac
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT INT TERM

archive="$tmpdir/$asset_base"
checksum="$archive.sha256"

echo "Downloading $BINARY $VERSION from $REPO..."
download "$archive" "$base_url/$asset_base"
download "$checksum" "$base_url/$asset_base.sha256"

(cd "$tmpdir" && sha256sum -c "$(basename "$checksum")")
tar -C "$tmpdir" --no-same-owner --strip-components=1 -xzf "$archive"

install -Dm755 "$tmpdir/$BINARY" "$BIN_DIR/$BINARY"
install -Dm644 "$tmpdir/$BINARY.service" "$UNIT_DIR/$BINARY.service"
"$SYSTEMCTL" daemon-reload
"$SYSTEMCTL" enable "$BINARY.service"
if "$SYSTEMCTL" is-active --quiet "$BINARY.service"; then
	"$SYSTEMCTL" restart "$BINARY.service"
	echo "Restarted $BINARY.service"
else
	"$SYSTEMCTL" start "$BINARY.service"
	echo "Started $BINARY.service"
fi

echo "Installed $BINARY to $BIN_DIR/$BINARY"
echo "Installed systemd unit to $UNIT_DIR/$BINARY.service"
echo "Enabled $BINARY.service"
