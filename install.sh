#!/bin/sh

set -eu

REPO="${REPO:-moeilijk/lhm-companion}"
BINARY="${BINARY:-lhm-companion}"
VERSION="${VERSION:-latest}"
BIN_DIR="${BIN_DIR:-/usr/local/bin}"
UNIT_DIR="${UNIT_DIR:-/etc/systemd/system}"
STATE_DIR="${STATE_DIR:-/var/lib/lhm-companion}"
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
need_cmd mkdir
need_cmd sed
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

unit_tmp="${tmpdir}/${BINARY}.installed.service"
sed "s|^ExecStart=.*$|ExecStart=${BIN_DIR}/${BINARY}|" "$tmpdir/$BINARY.service" > "$unit_tmp"

install -Dm755 "$tmpdir/$BINARY" "$BIN_DIR/$BINARY"
install -Dm644 "$unit_tmp" "$UNIT_DIR/$BINARY.service"
mkdir -p "$STATE_DIR"
set -- $(sha256sum "$tmpdir/$BINARY")
binary_sha="$1"
set -- $(sha256sum "$unit_tmp")
unit_sha="$1"
cat > "$STATE_DIR/install.env" <<EOF
REPO='${REPO}'
VERSION='${VERSION}'
BINARY='${BINARY}'
SERVICE_NAME='${BINARY}.service'
BINARY_PATH='${BIN_DIR}/${BINARY}'
UNIT_PATH='${UNIT_DIR}/${BINARY}.service'
STATE_DIR='${STATE_DIR}'
UNIT_EXECSTART='${BIN_DIR}/${BINARY}'
BINARY_SHA256='${binary_sha}'
UNIT_SHA256='${unit_sha}'
EOF
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
echo "Wrote install metadata to $STATE_DIR/install.env"
echo "Enabled $BINARY.service"
