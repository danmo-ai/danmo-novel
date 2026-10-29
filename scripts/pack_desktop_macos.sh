#!/usr/bin/env bash
# macOS Tauri desktop build — Cargo -> out/desktop/cargo, bundles -> out/desktop/bundle
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=out_paths.sh
source "$SCRIPT_DIR/out_paths.sh"

APP_NAME="${DQ_APP_NAME:-danmo-novel}"
export CARGO_TARGET_DIR="${CARGO_TARGET_DIR:-$DQ_DESKTOP_CARGO}"
dq_ensure_out_layout

if [[ "$(uname -s)" != Darwin ]]; then
  echo "pack-macos-desktop must run on macOS" >&2
  exit 1
fi

cd "$DQ_ROOT/desktop"
if [[ ! -d node_modules ]]; then
  npm install
fi

export VITE_API_BASE_URL="http://127.0.0.1:${DQ_BACKEND_PORT:-7804}"

if [[ -z "${TAURI_SIGNING_PRIVATE_KEY:-}" && -z "${TAURI_SIGNING_PRIVATE_KEY_PATH:-}" && -f "$DQ_ROOT/desktop/src-tauri/keys/updater.key" ]]; then
  export TAURI_SIGNING_PRIVATE_KEY_PATH="$DQ_ROOT/desktop/src-tauri/keys/updater.key"
  export TAURI_SIGNING_PRIVATE_KEY_PASSWORD="${TAURI_SIGNING_PRIVATE_KEY_PASSWORD:-}"
fi

if [[ -z "${TAURI_SIGNING_PRIVATE_KEY:-}" && -n "${TAURI_SIGNING_PRIVATE_KEY_PATH:-}" && -f "${TAURI_SIGNING_PRIVATE_KEY_PATH}" ]]; then
  TAURI_SIGNING_PRIVATE_KEY="$(tr -d '\r\n' < "$TAURI_SIGNING_PRIVATE_KEY_PATH")"
  export TAURI_SIGNING_PRIVATE_KEY
fi
unset TAURI_SIGNING_PRIVATE_KEY_PATH

has_tauri_signing_key() {
  [[ -n "${TAURI_SIGNING_PRIVATE_KEY:-}" ]]
}

echo "==> Building backend sidecar..."
"$SCRIPT_DIR/build_sidecar.sh"

echo "==> Tauri build ($APP_NAME) -> $CARGO_TARGET_DIR"
if has_tauri_signing_key; then
  npm run tauri build -- -b app
else
  echo "WARNING: no Tauri signing key — building without updater artifacts"
  npm run tauri build -- -b app --config '{"bundle":{"createUpdaterArtifacts":false}}'
fi

BUNDLE_SRC=""
for candidate in \
  "$CARGO_TARGET_DIR/release/bundle" \
  "$CARGO_TARGET_DIR/aarch64-apple-darwin/release/bundle" \
  "$DQ_ROOT/desktop/src-tauri/target/release/bundle" \
  "$DQ_ROOT/desktop/src-tauri/target/aarch64-apple-darwin/release/bundle"; do
  if [[ -d "$candidate" ]]; then
    BUNDLE_SRC="$candidate"
    break
  fi
done

if [[ -z "$BUNDLE_SRC" ]]; then
  echo "Tauri bundle not found under $CARGO_TARGET_DIR" >&2
  exit 1
fi

rm -rf "$DQ_DESKTOP_BUNDLE"/*
mkdir -p "$DQ_DESKTOP_BUNDLE"
cp -R "$BUNDLE_SRC"/* "$DQ_DESKTOP_BUNDLE/"

APP_BUNDLE=$(find "$DQ_DESKTOP_BUNDLE" -name "*.app" -maxdepth 2 -type d 2>/dev/null | head -1)
if [[ -n "$APP_BUNDLE" ]]; then
  SIDECAR_BIN="$DQ_ROOT/desktop/src-tauri/bin/danmo-novel-backend-$(rustc -vV | sed -n 's|host: ||p')"
  if [[ -f "$SIDECAR_BIN" ]]; then
    cp "$SIDECAR_BIN" "$APP_BUNDLE/Contents/MacOS/"
    echo "==> Injected sidecar: $(basename "$SIDECAR_BIN") -> $APP_BUNDLE/Contents/MacOS/"
    rm -f "$APP_BUNDLE/Contents/MacOS/danmo-novel-backend"
    codesign --force --deep --sign - "$APP_BUNDLE" 2>/dev/null && echo "==> Re-signed .app bundle" || echo "WARNING: codesign failed"
    xattr -cr "$APP_BUNDLE" 2>/dev/null && echo "==> Removed quarantine attributes" || true
  else
    echo "WARNING: sidecar binary not found at $SIDECAR_BIN"
  fi
fi

if [[ -n "$APP_BUNDLE" && -d "$APP_BUNDLE" ]] && has_tauri_signing_key; then
  UPDATER_DIR="$DQ_DESKTOP_BUNDLE/macos"
  mkdir -p "$UPDATER_DIR"
  APP_BASENAME="$(basename "$APP_BUNDLE")"
  APP_VERSION=$(plutil -extract CFBundleShortVersionString raw "$APP_BUNDLE/Contents/Info.plist" 2>/dev/null || echo "0.0.0")
  ARCH=$(uname -m)
  case "$ARCH" in
    arm64) ARCH_LABEL="aarch64" ;;
    x86_64) ARCH_LABEL="x86_64" ;;
    *) ARCH_LABEL="$ARCH" ;;
  esac
  TAR_NAME="Danmo.Novel_${APP_VERSION}_${ARCH_LABEL}.app.tar.gz"
  TAR_PATH="$UPDATER_DIR/$TAR_NAME"
  echo "==> Creating updater archive: $TAR_NAME"
  (
    cd "$(dirname "$APP_BUNDLE")"
    tar -czf "$TAR_PATH" "$APP_BASENAME"
  )
  echo "==> Signing updater archive..."
  npx tauri signer sign "$TAR_PATH" -p "${TAURI_SIGNING_PRIVATE_KEY_PASSWORD:-}"
  find "$UPDATER_DIR" -maxdepth 1 -name '*.app.tar.gz' ! -name "$TAR_NAME" -delete 2>/dev/null || true
  find "$UPDATER_DIR" -maxdepth 1 -name '*.app.tar.gz.sig' ! -name "${TAR_NAME}.sig" -delete 2>/dev/null || true
  echo "==> Updater artifacts: $TAR_PATH (+ .sig)"
elif ! has_tauri_signing_key; then
  echo "WARNING: no Tauri signing key — skipping updater archive resign"
fi

DMG_STAGING="$DQ_DESKTOP_BUNDLE/_dmg_staging"
rm -rf "$DMG_STAGING"
mkdir -p "$DMG_STAGING"

if [[ -n "$APP_BUNDLE" ]] && [[ -d "$APP_BUNDLE" ]]; then
  cp -R "$APP_BUNDLE" "$DMG_STAGING/"
fi

cat > "$DMG_STAGING/阅读说明.txt" << 'README_EOF'
📦 Danmo Novel 安装说明 (macOS)

由于本应用未使用 Apple 开发者签名，首次打开需要特殊操作。

安装步骤：
  1. 将 Danmo Novel.app 拖入「应用程序」文件夹
  2. 在 Finder 中 右键（Control+点击）app → 选择「打开」
  3. 弹窗中点击「打开」确认

注意：必须使用右键 → 打开，直接双击会被 macOS 拦截。
      右键打开一次后，后续即可正常双击启动。

如果右键打开仍然被拦截：
  前往「系统设置 → 隐私与安全性」
  点击「仍要打开」按钮

终端修复（备选）：
  xattr -cr /Applications/Danmo\ Novel.app
README_EOF

ln -s /Applications "$DMG_STAGING/Applications"

if [[ -d "$DMG_STAGING/Danmo Novel.app" ]]; then
  DMG_DIR="$DQ_DESKTOP_BUNDLE/dmg"
  rm -rf "$DMG_DIR"
  mkdir -p "$DMG_DIR"
  APP_VERSION=$(plutil -extract CFBundleShortVersionString raw "$DMG_STAGING/Danmo Novel.app/Contents/Info.plist" 2>/dev/null || echo "0.0.0")
  ARCH=$(uname -m)
  DMG_NAME="Danmo.Novel_${APP_VERSION}_${ARCH}.dmg"
  echo "==> Creating DMG: $DMG_NAME"
  hdiutil create -volname "Danmo Novel" -srcfolder "$DMG_STAGING" -ov -format UDZO "$DMG_DIR/$DMG_NAME" 2>/dev/null && echo "==> DMG created" || echo "WARNING: DMG creation failed"
fi

rm -rf "$DMG_STAGING"

echo "==> Desktop bundle -> $DQ_DESKTOP_BUNDLE"
