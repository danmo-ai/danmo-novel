#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PLATFORM="${1:-ios}"
cd "$ROOT_DIR"
./scripts/build_go_mobile.sh "$PLATFORM"
cd "$ROOT_DIR/frontend" && npm run build
rm -rf "$ROOT_DIR/web"
mkdir -p "$ROOT_DIR/web"
cp -R "$ROOT_DIR/out/frontend/dist/" "$ROOT_DIR/web/"
cd "$ROOT_DIR/desktop" && npm install --no-audit --no-fund
# Tauri embeds frontendDist into the native library; these generated app asset
# directories are also populated for platform-specific packaging and inspection.
if [[ "$PLATFORM" == "ios" ]]; then
  mkdir -p src-tauri/gen/apple/assets
  cp -R ../out/frontend/dist/ src-tauri/gen/apple/assets/
else
  mkdir -p src-tauri/gen/android/app/src/main/assets
  cp -R ../out/frontend/dist/ src-tauri/gen/android/app/src/main/assets/
fi
case "$PLATFORM" in
  ios) npm run tauri -- ios build --ci --config src-tauri/tauri.${PLATFORM}.conf.json ;;
  android) npm run tauri -- android build --ci --config src-tauri/tauri.${PLATFORM}.conf.json ;;
  *) echo 'Usage: pack_mobile.sh ios|android' >&2; exit 2 ;;
esac
