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

# 版本号：CI 用 tag 注入；本地给了 RELEASE_VERSION 就用它，并在构建后还原，
# 免得把"这次发布的版本"写死进被 git 跟踪的配置文件。App Store Connect 看到的
# CFBundleShortVersionString 就是这里写的值。
TAURI_CONF="$ROOT_DIR/desktop/src-tauri/tauri.conf.json"
TAURI_CONF_BAK=""
if [[ -n "${RELEASE_VERSION:-}" ]]; then
  case "$RELEASE_VERSION" in
    v*) VERSION="${RELEASE_VERSION#v}" ;;
    *) VERSION="$RELEASE_VERSION" ;;
  esac
  TAURI_CONF_BAK="$(mktemp)"
  cp "$TAURI_CONF" "$TAURI_CONF_BAK"
  python3 - "$TAURI_CONF" "$VERSION" <<'PY'
import json, sys
path, version = sys.argv[1], sys.argv[2]
conf = json.load(open(path))
conf['version'] = version
json.dump(conf, open(path, 'w'), indent=2, ensure_ascii=False)
open(path, 'a').write('\n')
PY
  trap '[[ -n "$TAURI_CONF_BAK" ]] && cp "$TAURI_CONF_BAK" "$TAURI_CONF" && rm -f "$TAURI_CONF_BAK"' EXIT
  echo "==> Version: ${VERSION} (构建后还原 tauri.conf.json)"
fi

case "$PLATFORM" in
  ios)
    # xcodegen 会把 project.yml 里的 ${FORCE_COLOR} 当成自己的变量在**生成时**替换掉。
    # 只要环境里带着 FORCE_COLOR=0（Cursor/部分终端会设），生成的 Build Rust Code
    # 脚本就会多出一个 `0` 参数，tauri 把它当作架构名，报
    # "Arch specified by Xcode was invalid. 0 isn't a known arch"。
    # 这个变量只影响彩色输出，直接清掉让它在 Xcode 里运行时展开。
    unset FORCE_COLOR
    # init 幂等；必须在 build 之前，否则工程刚生成就被覆盖，补丁丢失。
    npm run tauri -- ios init
    "$ROOT_DIR/scripts/ios_hygiene.sh"
    # 有 team 就签：没有签名身份时设备归档必然失败（xcodebuild 报
    # "Arch specified by Xcode was invalid"），所以显式改出模拟器包。
    IOS_SIGN_ARGS=(--ci)
    if [[ -n "${APPLE_TEAM_ID:-}" ]]; then
      # 导出方式决定产物：debugging=可装真机，release-testing=TestFlight，
      # app-store-connect=App Store。
      IOS_SIGN_ARGS+=(--export-method "${IOS_EXPORT_METHOD:-debugging}")
      echo "==> 签名构建 · team=$APPLE_TEAM_ID · export=${IOS_EXPORT_METHOD:-debugging}"
    else
      echo "==> 没有 APPLE_TEAM_ID：构建模拟器包（--no-sign，不产出 .ipa）"
      IOS_SIGN_ARGS+=(--no-sign --target aarch64-sim)
    fi
    npm run tauri -- ios build "${IOS_SIGN_ARGS[@]}" --config src-tauri/tauri.${PLATFORM}.conf.json
    ;;
  android) npm run tauri -- android build --ci --config src-tauri/tauri.${PLATFORM}.conf.json ;;
esac

echo "==> Mobile bundle -> $MOBILE_DIST"
