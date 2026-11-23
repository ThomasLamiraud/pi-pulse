#!/usr/bin/env bash
#
# Cross-compiles the home gate on this machine and installs it on a Raspberry
# Pi as a systemd service, together with config.yaml.
#
#   ./deploy.sh                          # pi@homemonitor.local
#   ./deploy.sh pi@192.168.1.80
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

TARGET="${1:-${PI_TARGET:-pi@homemonitor.local}}"
VERSION="$(git describe --always --dirty 2>/dev/null || echo dev)"

echo "🧪 Checking config.yaml..."
go run . -config config.yaml -check

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

BINARY="dist/gate-linux-${GOARCH}${GOARM:+v${GOARM}}"
echo "🏗️ Building ${BINARY} (${VERSION})..."
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" GOARM="$GOARM" \
  go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "$BINARY" .

echo "📤 Copying the binary, service unit and config..."
REMOTE_TMP="$(ssh "$TARGET" mktemp -d)"
scp -q "$BINARY" "${TARGET}:${REMOTE_TMP}/gate"
scp -q deploy/gate.service config.yaml "${TARGET}:${REMOTE_TMP}/"

echo "🚀 Installing and restarting the gate (sudo may ask for your password)..."
ssh -t "$TARGET" "set -e
  sudo install -m 0755 '${REMOTE_TMP}/gate' /usr/local/bin/gate
  sudo install -m 0644 '${REMOTE_TMP}/gate.service' /etc/systemd/system/gate.service
  sudo install -d -m 0755 /etc/gate
  sudo install -m 0644 '${REMOTE_TMP}/config.yaml' /etc/gate/config.yaml
  rm -rf '${REMOTE_TMP}'
  sudo systemctl daemon-reload
  sudo systemctl enable --quiet gate
  sudo systemctl restart gate
  sleep 2
  systemctl --no-pager --lines=5 status gate"

echo "✅ The gate is running: http://${TARGET#*@}/"
