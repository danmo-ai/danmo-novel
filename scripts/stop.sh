#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/out_paths.sh"
source "$SCRIPT_DIR/dev_process.sh"
if [[ -d "$DQ_RUN_DIR" ]]; then
  for f in "$DQ_RUN_DIR"/*.pid; do
    [[ -f "$f" ]] || continue
    pid="$(cat "$f" 2>/dev/null || true)"
    [[ -n "$pid" ]] && kill -TERM "$pid" 2>/dev/null || true
  done
fi
# best effort env-marked processes
pids="$(ps axeww 2>/dev/null | grep -F "DQ_DEV=${DQ_PROJECT}" | grep -F "DQ_DEV_ROOT=${DQ_ROOT}" | awk '{print $1}' || true)"
[[ -n "$pids" ]] && echo "$pids" | xargs kill -TERM 2>/dev/null || true
echo "stopped ${DQ_PROJECT}"
