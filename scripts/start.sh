#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/out_paths.sh"
source "$SCRIPT_DIR/dev_process.sh"

APP_NAME="${DQ_APP_NAME:-danmo-novel}"
BACKEND_PORT="${DQ_BACKEND_PORT}"
FRONTEND_PORT="${DQ_FRONTEND_PORT}"

dq_ensure_out_layout
"$SCRIPT_DIR/stop.sh" 2>/dev/null || true
dq_free_listen_port "$BACKEND_PORT"
dq_free_listen_port "$FRONTEND_PORT"

echo "==> Starting $APP_NAME (dev)"
echo "    Backend : http://127.0.0.1:${BACKEND_PORT}"
echo "    Frontend: http://127.0.0.1:${FRONTEND_PORT}/"

cd "$DQ_ROOT/frontend"
if [[ ! -d node_modules ]] || [[ package-lock.json -nt node_modules ]]; then
  echo "==> Installing frontend deps..."
  npm install --no-audit --no-fund
  touch node_modules
fi

DEV_BACKEND_BIN="$DQ_RUN_DIR/backend-bin"
echo "==> Building dev backend"
(cd "$DQ_ROOT" && go build -o "$DEV_BACKEND_BIN" ./cmd/danmo-novel)

export DQ_DEV_ENV=$'DQ_HTTP_HOST=127.0.0.1'$'\nDQ_BACKEND_PORT='"${BACKEND_PORT}"
dq_dev_start backend "$DQ_ROOT" "$DEV_BACKEND_BIN"
unset DQ_DEV_ENV

export DQ_DEV_ENV=$'DQ_BACKEND_PORT='"${BACKEND_PORT}"$'\nDQ_FRONTEND_PORT='"${FRONTEND_PORT}"
dq_dev_start frontend "$DQ_ROOT/frontend" npm run dev
unset DQ_DEV_ENV

dq_wait_http "http://127.0.0.1:${BACKEND_PORT}/api/system/health" "$DQ_RUN_DIR/backend.log"
dq_wait_http "http://127.0.0.1:${FRONTEND_PORT}/" "$DQ_RUN_DIR/frontend.log"
echo "==> Open: http://127.0.0.1:${FRONTEND_PORT}/"
