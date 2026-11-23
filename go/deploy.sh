#!/usr/bin/env bash
#
# Cross-compiles PiPulse on this machine and installs it on a Raspberry Pi as
# a systemd service. Only Go is needed locally; the Pi needs nothing extra.
#
#   ./deploy.sh                          # pihole@pihole.local
#   ./deploy.sh pi@192.168.1.130         # another Pi
#   PI_TARGET=pi@raspberrypi.local ./deploy.sh

set -euo pipefail

usage() {
  echo "Usage: $0 [user@raspberry-pi-host]"
  echo "       PI_TARGET=user@raspberry-pi-host $0"
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if (( $# > 1 )); then
  usage >&2
  exit 2
fi

cd "$(dirname "$0")"

TARGET="${1:-${PI_TARGET:-pihole@pihole.local}}"
VERSION="$(git describe --always --dirty 2>/dev/null || echo dev)"

echo "🔎 Detecting ${TARGET} architecture..."
REMOTE_ARCH="$(ssh "$TARGET" uname -m)"

case "$REMOTE_ARCH" in
  aarch64|arm64) GOARCH=arm64; GOARM="" ;;
  armv7l)        GOARCH=arm;   GOARM=7 ;;
  armv6l)        GOARCH=arm;   GOARM=6 ;;
  *)
    echo "Unsupported Raspberry Pi architecture: ${REMOTE_ARCH}" >&2
    exit 1
    ;;
esac

BINARY="dist/pipulse-linux-${GOARCH}${GOARM:+v${GOARM}}"
echo "🏗️ Building ${BINARY} (${VERSION})..."
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" GOARM="$GOARM" \
  go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "$BINARY" .

echo "📤 Copying the binary and service unit..."
REMOTE_TMP="$(ssh "$TARGET" mktemp -d)"
scp -q "$BINARY" "${TARGET}:${REMOTE_TMP}/pipulse"
scp -q deploy/pipulse.service "${TARGET}:${REMOTE_TMP}/pipulse.service"

echo "🚀 Installing and restarting PiPulse (sudo may ask for your password)..."
ssh -t "$TARGET" "set -e
  sudo install -m 0755 '${REMOTE_TMP}/pipulse' /usr/local/bin/pipulse
  sudo install -m 0644 '${REMOTE_TMP}/pipulse.service' /etc/systemd/system/pipulse.service
  rm -rf '${REMOTE_TMP}'
  sudo systemctl daemon-reload
  sudo systemctl enable --quiet pipulse
  sudo systemctl restart pipulse
  sleep 2
  systemctl --no-pager --lines=5 status pipulse"

HOST="${TARGET#*@}"
PORT="$(ssh "$TARGET" "grep -s '^PORT=' /etc/default/pipulse | cut -d= -f2" || true)"
echo "✅ PiPulse is running: http://${HOST}:${PORT:-3000}"
