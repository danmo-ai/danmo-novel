#!/usr/bin/env bash
# Dev: Go API + Tauri Desktop (Vite HMR via beforeDevCommand)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=out_paths.sh
source "$SCRIPT_DIR/out_paths.sh"
# shellcheck source=dev_process.sh
source "$SCRIPT_DIR/dev_process.sh"

APP_NAME="${DQ_APP_NAME:-danmo-novel}"
BACKEND_PORT="${DQ_BACKEND_PORT}"

dq_ensure_out_layout
"$SCRIPT_DIR/stop.sh" 2>/dev/null || true

echo "==> Starting $APP_NAME (dev-desktop) [${DQ_PROJECT}]"
echo "    Backend : http://127.0.0.1:${BACKEND_PORT}"
echo "    Desktop : Tauri webview (Vite HMR on :${DQ_FRONTEND_PORT})"

cd "$DQ_ROOT/frontend"
if [[ ! -d node_modules ]] || [[ package-lock.json -nt node_modules ]]; then
  echo "==> Installing frontend deps (npm install --prefer-offline)..."
  npm install --prefer-offline --no-audit --no-fund
  touch node_modules
fi

OS="$(uname -s)"
ARCH="$(uname -m)"
case "$OS-$ARCH" in
  Darwin-arm64) TRIPLE="aarch64-apple-darwin" ;;
  Darwin-x86_64) TRIPLE="x86_64-apple-darwin" ;;
  Linux-x86_64) TRIPLE="x86_64-unknown-linux-gnu" ;;
  MINGW*-x86_64|MSYS*-x86_64|CYGWIN*-x86_64) TRIPLE="x86_64-pc-windows-msvc" ;;
  *)
    echo "Unsupported platform: $OS-$ARCH" >&2
    exit 1
    ;;
esac
SIDECAR_NAME="danmo-novel-backend-$TRIPLE"
if [[ "$TRIPLE" == *"-pc-windows-msvc" ]]; then
  SIDECAR_NAME="$SIDECAR_NAME.exe"
fi
SIDECAR_PATH="$DQ_ROOT/desktop/src-tauri/bin/$SIDECAR_NAME"
DEV_BACKEND_BIN="$DQ_RUN_DIR/backend-bin"
echo "==> Building dev backend -> $DEV_BACKEND_BIN"
(cd "$DQ_ROOT" && go build -o "$DEV_BACKEND_BIN" ./cmd/danmo-novel)
mkdir -p "$(dirname "$SIDECAR_PATH")"
cp -f "$DEV_BACKEND_BIN" "$SIDECAR_PATH"
if [[ -d "$HOME/.danmo-novel/bin" ]]; then
  cp -f "$DEV_BACKEND_BIN" "$HOME/.danmo-novel/bin/danmo-novel-backend"
fi
echo "==> Sidecar binary: $SIDECAR_PATH"
echo ""

# Tell Rust shell to leave :7804 alone — script owns the backend in dev.
export NOVEL_EXTERNAL_BACKEND=1

if [[ "${SKIP_BACKEND:-0}" == "1" ]]; then
  echo "==> SKIP_BACKEND=1: using external backend on port ${BACKEND_PORT}"
  echo ""
else
  export DQ_DEV_ENV=$'DQ_BACKEND_PORT='"${BACKEND_PORT}"$'\nDANMO_NOVEL_PORT='"${BACKEND_PORT}"
  dq_dev_start backend "$DQ_ROOT" "$DEV_BACKEND_BIN"
  unset DQ_DEV_ENV

  echo "==> Backend PID: $(cat "$DQ_RUN_DIR/backend.pid")"
  echo "    Logs: $DQ_RUN_DIR/backend.log"
  echo ""
fi

echo "==> Starting Tauri dev..."
echo "    Press Ctrl+C to stop"
echo ""

cleanup() {
  echo ""
  echo "==> Stopping dev processes..."
  "$SCRIPT_DIR/stop.sh" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

cd "$DQ_ROOT/desktop"
if [[ ! -d node_modules ]]; then
  npm install
fi
npm run tauri dev
