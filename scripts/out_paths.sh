# Unified build output paths for danmo-novel
if [[ -n "${_DQ_OUT_PATHS_LOADED:-}" ]]; then
  return 0 2>/dev/null || true
fi
_DQ_OUT_PATHS_LOADED=1

_DQ_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export DQ_ROOT="$(cd "$_DQ_SCRIPT_DIR/.." && pwd)"
export DQ_OUT="$DQ_ROOT/out"
export DQ_FRONTEND_DIST="$DQ_OUT/frontend/dist"
export DQ_SERVER_DIR="$DQ_OUT/server"
export DQ_DESKTOP_BUNDLE="$DQ_OUT/desktop/bundle"
export DQ_DESKTOP_CARGO="$DQ_OUT/desktop/cargo"
export DQ_RUN_DIR="$DQ_OUT/run"
export DQ_PROJECT="${DQ_PROJECT:-danmo-novel}"
export DQ_BACKEND_PORT="${DQ_BACKEND_PORT:-7804}"
export DQ_FRONTEND_PORT="${DQ_FRONTEND_PORT:-5804}"

dq_ensure_out_layout() {
  mkdir -p "$DQ_FRONTEND_DIST" "$DQ_SERVER_DIR" "$DQ_DESKTOP_BUNDLE" "$DQ_DESKTOP_CARGO" "$DQ_RUN_DIR"
}

dq_free_listen_port() {
  local port="$1"
  local pids
  pids="$(lsof -nP -t -iTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)"
  [[ -n "$pids" ]] || return 0
  echo "==> Port $port already in use; stopping so ${DQ_PROJECT} can bind"
  echo "$pids" | xargs kill -TERM 2>/dev/null || true
  sleep 0.4
  pids="$(lsof -nP -t -iTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)"
  if [[ -n "$pids" ]]; then
    echo "$pids" | xargs kill -KILL 2>/dev/null || true
  fi
}

dq_wait_http() {
  local url="$1"
  local log="$2"
  for _ in $(seq 1 30); do
    if curl -sf -m 1 "$url" >/dev/null 2>&1; then return 0; fi
    sleep 0.2
  done
  echo "timed out waiting for $url" >&2
  [[ -n "$log" && -f "$log" ]] && tail -n 20 "$log" >&2
  return 1
}
