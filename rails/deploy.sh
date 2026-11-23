#!/usr/bin/env bash

set -euo pipefail

usage() {
  echo "Usage: $0 [raspberry-pi-host]"
  echo "       PI_HOST=raspberry-pi.local $0"
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

PI_USER="${PI_USER:-pi}"
PI_HOST="${1:-${PI_HOST:-192.168.1.130}}"
PI_DIR="${PI_DIR:-/home/pi/pi-pulse}"
PLATFORM="${PLATFORM:-}"
IMAGE_NAME="${IMAGE_NAME:-pi-pulse}"
TAR_FILE="${IMAGE_NAME}.tar"
ENV_FILE=""

cleanup() {
  rm -f "$TAR_FILE"
  if [[ -n "$ENV_FILE" ]]; then
    rm -f "$ENV_FILE"
  fi
}
trap cleanup EXIT

echo "🔎 Detecting ${PI_USER}@${PI_HOST} architecture..."
REMOTE_ARCH="$(ssh "${PI_USER}@${PI_HOST}" uname -m)"

if ! ssh "${PI_USER}@${PI_HOST}" "command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1"; then
  echo "Docker Engine with the Compose plugin is required on ${PI_HOST}." >&2
  echo "Install Docker on the target Raspberry Pi, then rerun this deployment." >&2
  exit 1
fi

if [[ -z "$PLATFORM" ]]; then
  case "$REMOTE_ARCH" in
    aarch64|arm64) PLATFORM="linux/arm64" ;;
    armv7l) PLATFORM="linux/arm/v7" ;;
    armv6l) PLATFORM="linux/arm/v5" ;;
    *)
      echo "Unsupported Raspberry Pi architecture: ${REMOTE_ARCH}" >&2
      exit 1
      ;;
  esac
fi

echo "🏗️ Building PiPulse for ${PLATFORM}..."
docker buildx build \
  --platform "$PLATFORM" \
  --provenance=false \
  --tag "${IMAGE_NAME}:latest" \
  --output "type=docker,dest=${TAR_FILE}" \
  .

echo "🔧 Preparing ${PI_USER}@${PI_HOST}:${PI_DIR}..."
ssh "${PI_USER}@${PI_HOST}" "mkdir -p '${PI_DIR}/storage'"

if ! ssh "${PI_USER}@${PI_HOST}" "test -f '${PI_DIR}/.env.production'"; then
  ENV_FILE="$(mktemp)"
  SECRET_KEY_BASE="$(openssl rand -hex 64)"
  cat > "$ENV_FILE" <<EOF
RAILS_ENV=production
RAILS_LOG_LEVEL=info
RAILS_MAX_THREADS=2
PORT=3000
METRIC_INTERVAL=15
SECRET_KEY_BASE=${SECRET_KEY_BASE}
FORCE_SSL=false
EOF
  scp -v "$ENV_FILE" "${PI_USER}@${PI_HOST}:${PI_DIR}/.env.production"
  ssh "${PI_USER}@${PI_HOST}" "chmod 600 '${PI_DIR}/.env.production'"
fi

echo "📤 Copying image and deployment configuration..."
scp -v "$TAR_FILE" compose.yaml "${PI_USER}@${PI_HOST}:${PI_DIR}/"

echo "🔄 Loading image and starting PiPulse..."
ssh -t "${PI_USER}@${PI_HOST}" "cd '${PI_DIR}' && \
  echo '📦 [remote] Importing the Docker image while the current version stays online...' && \
  sudo docker load -i '${TAR_FILE}' && \
  echo '🧹 [remote] Removing the transferred image archive...' && \
  rm '${TAR_FILE}' && \
  echo '🛑 [remote] Stopping PiPulse for the database update...' && \
  sudo docker compose stop web collector && \
  echo '🗄️ [remote] Preparing the production database...' && \
  if ! sudo docker run --rm \
    --security-opt seccomp=unconfined \
    --env-file .env.production \
    -v '${PI_DIR}/storage:/rails/storage' \
    '${IMAGE_NAME}:latest' bin/rails db:prepare; then \
    echo '❌ [remote] Database preparation failed; restarting the previous containers.' >&2; \
    sudo docker compose start web collector; \
    exit 1; \
  fi && \
  echo '🚀 [remote] Recreating PiPulse containers with the new image...' && \
  sudo docker compose up -d --force-recreate --remove-orphans && \
  echo '🔎 [remote] Container status:' && \
  sudo docker compose ps"

echo "✅ PiPulse deployment started: http://${PI_HOST}:3000"
