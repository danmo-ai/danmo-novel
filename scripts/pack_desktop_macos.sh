#!/usr/bin/env bash
# macOS Tauri desktop build — Cargo -> out/desktop/cargo, bundles -> out/desktop/bundle
#
# 签名/公证：
#   - 设了 APPLE_SIGNING_IDENTITY（且证书已在 keychain 里）→ Developer ID 签名；
#     CI 里由 scripts/ci_import_apple_cert.sh 先导入证书。
#   - 没设 → ad-hoc 签名，产物只能自己机器用。
#   - 有公证凭据（APPLE_ID / APPLE_PASSWORD / APPLE_TEAM_ID）时，本脚本另外公证
#     自制的 DMG（Tauri 只公证它自己打的那个包，这个 DMG 是后做的）。
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

SIGN_IDENTITY="${APPLE_SIGNING_IDENTITY:-}"
SIGNED=0
NOTARIZED=0
if [[ -n "$SIGN_IDENTITY" ]]; then
  SIGNED=1
  echo "==> Signing identity: $SIGN_IDENTITY"
  if [[ -n "${APPLE_ID:-}" && -n "${APPLE_PASSWORD:-}" && -n "${APPLE_TEAM_ID:-}" ]]; then
    NOTARIZED=1
  else
    echo "==> No APPLE_ID/APPLE_PASSWORD/APPLE_TEAM_ID — signed but NOT notarized"
  fi
else
  echo "==> No APPLE_SIGNING_IDENTITY — ad-hoc signature only (not distributable)"
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

SIDECAR_NAME="danmo-novel-backend-$(rustc -vV | sed -n 's|host: ||p')"
SIDECAR_BIN="$DQ_ROOT/desktop/src-tauri/bin/$SIDECAR_NAME"

# sidecar 必须在 Tauri 把它拷进 .app **之前**签好。事后再改 bundle 里的任何二进制
# （老脚本的 cp + codesign --deep --sign - 就是）都会让 Developer ID 签名和公证失效，
# 用户端表现为「app 已损坏」。
if [[ "$SIGNED" == 1 ]]; then
  [[ -f "$SIDECAR_BIN" ]] || { echo "sidecar not found: $SIDECAR_BIN" >&2; exit 1; }
  codesign --force --options runtime --timestamp --sign "$SIGN_IDENTITY" "$SIDECAR_BIN"
  echo "==> Signed sidecar: $SIDECAR_NAME"
fi

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

# sidecar 由 bundle.externalBin 配好，Tauri 会把 bin/danmo-novel-backend-<triple>
# 拷进来并去掉三元组后缀（Contents/MacOS/danmo-novel-backend）。这里只校验存在性：
# 缺了说明 externalBin 没匹配上，必须在打包阶段停住，而不是发一个起不来后端的包。
if [[ -n "$APP_BUNDLE" ]]; then
  sidecars="$(find "$APP_BUNDLE/Contents/MacOS" -maxdepth 1 -name 'danmo-novel-backend*' 2>/dev/null || true)"
  if [[ -z "$sidecars" ]]; then
    echo "ERROR: $APP_BUNDLE 里没有 sidecar（检查 bundle.externalBin 与 bin/ 里的文件名）" >&2
    ls -la "$APP_BUNDLE/Contents/MacOS" >&2 || true
    exit 1
  fi
fi

if [[ "$SIGNED" == 1 && -n "$APP_BUNDLE" ]]; then
  # 签名坏了就别上传：用户看到的会是「已损坏，无法打开」，而 CI 是绿的。
  echo "==> Verifying code signature..."
  codesign --verify --deep --strict --verbose=2 "$APP_BUNDLE"
  # 注意别写成 `codesign -dv | grep -q`：grep -q 命中即退出，codesign 收到 SIGPIPE，
  # pipefail 会把这条管道判成失败，于是签名正常也会进错误分支。
  sig_report="$(codesign -dv --verbose=2 "$APP_BUNDLE" 2>&1 || true)"
  if [[ "$sig_report" != *"Authority=Developer ID Application"* ]]; then
    echo "ERROR: $APP_BUNDLE 不是 Developer ID 签名（签名身份没生效）" >&2
    printf '%s\n' "$sig_report" >&2
    exit 1
  fi
  echo "==> Signature OK"
elif [[ -n "$APP_BUNDLE" ]]; then
  # 本地无证书：保持原有 ad-hoc 重签，让机器上能跑（分发不可用）。
  codesign --force --deep --sign - "$APP_BUNDLE" 2>/dev/null && echo "==> Re-signed .app bundle (ad-hoc)" || echo "WARNING: codesign failed"
  xattr -cr "$APP_BUNDLE" 2>/dev/null && echo "==> Removed quarantine attributes" || true
fi

notarize() {
  local path="$1"
  if [[ "$NOTARIZED" != 1 ]]; then
    if [[ "$SIGNED" == 1 ]]; then
      echo "WARNING: 有签名身份但缺公证凭据，跳过公证：$(basename "$path")" >&2
    fi
    return 0
  fi
  echo "==> Notarizing $(basename "$path") (may take several minutes)"
  xcrun notarytool submit "$path" \
    --apple-id "$APPLE_ID" --password "$APPLE_PASSWORD" --team-id "$APPLE_TEAM_ID" --wait
  xcrun stapler staple "$path"
  xcrun stapler validate "$path"
}

# 更新包是直接从 .app 打的，所以 .app 必须先有公证票据：Tauri 在 bundling 阶段会公证
# 并 staple，这里确认一次；没票据的 app 离线首次启动仍会被 Gatekeeper 拦。
if [[ "$NOTARIZED" == 1 && -n "$APP_BUNDLE" ]]; then
  if xcrun stapler validate "$APP_BUNDLE" >/dev/null 2>&1; then
    echo "==> App bundle already notarized (ticket stapled)"
  else
    echo "WARNING: .app 没有公证票据，尝试 staple（Tauri 没做公证？）" >&2
    xcrun stapler staple "$APP_BUNDLE" || echo "WARNING: staple 失败，app 未公证" >&2
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

# 说明文件只在 ad-hoc 构建里有意义：签名过的包双击即可打开，再教用户右键反而让人
# 以为这包有问题。只有真的公证过才敢写"已通过 Apple 公证"。
if [[ "$NOTARIZED" == 1 ]]; then
  cat > "$DMG_STAGING/阅读说明.txt" << 'README_NOTARIZED_EOF'
📦 Danmo Novel 安装说明 (macOS)

  1. 将 Danmo Novel.app 拖入「应用程序」文件夹
  2. 双击打开即可

本应用已使用 Developer ID 签名，并通过 Apple 公证。
README_NOTARIZED_EOF
elif [[ "$SIGNED" == 1 ]]; then
  cat > "$DMG_STAGING/阅读说明.txt" << 'README_SIGNED_EOF'
📦 Danmo Novel 安装说明 (macOS)

  1. 将 Danmo Novel.app 拖入「应用程序」文件夹
  2. 双击打开即可

本应用已使用 Developer ID 签名，但未经过 Apple 公证。
首次打开若被拦截，请到「系统设置 → 隐私与安全性」点「仍要打开」。
README_SIGNED_EOF
else
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
fi

ln -s /Applications "$DMG_STAGING/Applications"

DMG_PATH=""
if [[ -d "$DMG_STAGING/Danmo Novel.app" ]]; then
  DMG_DIR="$DQ_DESKTOP_BUNDLE/dmg"
  rm -rf "$DMG_DIR"
  mkdir -p "$DMG_DIR"
  APP_VERSION=$(plutil -extract CFBundleShortVersionString raw "$DMG_STAGING/Danmo Novel.app/Contents/Info.plist" 2>/dev/null || echo "0.0.0")
  ARCH=$(uname -m)
  DMG_NAME="Danmo.Novel_${APP_VERSION}_${ARCH}.dmg"
  DMG_PATH="$DMG_DIR/$DMG_NAME"
  echo "==> Creating DMG: $DMG_NAME"
  hdiutil create -volname "Danmo Novel" -srcfolder "$DMG_STAGING" -ov -format UDZO "$DMG_PATH" 2>/dev/null \
    && echo "==> DMG created" || echo "WARNING: DMG creation failed"
fi

rm -rf "$DMG_STAGING"

# Tauri 公证的是它自己在 bundle 里打的 dmg，而上传的是上面这个自制的 dmg，所以它
# 得单独过一遍 notarytool，否则用户打开这个 dmg 仍会被 Gatekeeper 拦。
if [[ -n "$DMG_PATH" && -f "$DMG_PATH" ]]; then
  notarize "$DMG_PATH"
fi

echo "==> Desktop bundle -> $DQ_DESKTOP_BUNDLE"
