#!/usr/bin/env bash
# Package the iOS or Android app: Go core → mobile Vue bundle → Tauri build.
#
# 移动端用的是独立的 Vue 入口（frontend/mobile.html），产物在 out/frontend/mobile。
# 桌面不再共用同一个 dist：桌面包里不该带移动页面，移动包里也不该带桌面 Tab 布局。
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PLATFORM="${1:-ios}"
cd "$ROOT_DIR"

case "$PLATFORM" in
  ios|android) ;;
  *) echo 'Usage: pack_mobile.sh ios|android' >&2; exit 2 ;;
esac

./scripts/build_go_mobile.sh "$PLATFORM"

echo "==> Building mobile UI bundle"
cd "$ROOT_DIR/frontend"
# 依赖没装就直接 install：CI 与本地都只调这一个脚本，别让调用方记两步。
[[ -d node_modules ]] || npm install --no-audit --no-fund
npm run build:mobile

MOBILE_DIST="$ROOT_DIR/out/frontend/mobile"
[[ -f "$MOBILE_DIST/index.html" ]] || { echo "ERROR: $MOBILE_DIST/index.html 不存在" >&2; exit 1; }

# 兼容旧的 /web 落点（pack_mobile 曾把前端复制到这里供本地检视）。
rm -rf "$ROOT_DIR/web"
mkdir -p "$ROOT_DIR/web"
cp -R "$MOBILE_DIST/." "$ROOT_DIR/web/"

cd "$ROOT_DIR/desktop" && npm install --no-audit --no-fund

# Tauri 把 frontendDist 编进原生库；生成目录里的 assets 副本用于平台侧打包与人工核对。
if [[ "$PLATFORM" == "ios" ]]; then
  mkdir -p src-tauri/gen/apple/assets
  cp -R "$MOBILE_DIST/." src-tauri/gen/apple/assets/
else
  mkdir -p src-tauri/gen/android/app/src/main/assets
  cp -R "$MOBILE_DIST/." src-tauri/gen/android/app/src/main/assets/
fi

case "$PLATFORM" in
  ios)
    # init 幂等；必须在 build 之前，否则工程刚生成就被覆盖，补丁丢失。
    npm run tauri -- ios init
    "$ROOT_DIR/scripts/ios_hygiene.sh"
    # 没有签名身份时明确走 --no-sign：这样本地/CI 都能产出可用的产物用来验证，
    # 而不是在 xcodebuild 里报 "requires a development team" 后失败。
    #
    # 设备归档（.ipa）必须有开发团队：xcodebuild 在无团队时 arch 解析失败
    # （"Arch specified by Xcode was invalid"）。所以无签名时改出模拟器包，
    # 用它验证 UI 与本地内核，而不是留一个永远失败的 CI 步骤。
    IOS_SIGN_ARGS=(--ci)
    if [[ -z "${APPLE_SIGNING_IDENTITY:-}" ]]; then
      echo "==> 没有 APPLE_SIGNING_IDENTITY：构建模拟器包（--no-sign，不产出 .ipa）"
      IOS_SIGN_ARGS+=(--no-sign --target aarch64-sim)
    else
      echo "==> 签名身份: $APPLE_SIGNING_IDENTITY"
    fi
    npm run tauri -- ios build "${IOS_SIGN_ARGS[@]}" --config src-tauri/tauri.${PLATFORM}.conf.json
    ;;
  android) npm run tauri -- android build --ci --config src-tauri/tauri.${PLATFORM}.conf.json ;;
esac

echo "==> Mobile bundle -> $MOBILE_DIST"
