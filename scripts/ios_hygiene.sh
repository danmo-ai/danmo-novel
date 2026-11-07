#!/usr/bin/env bash
# iOS 工程项补丁：让 App Store 提交不需要手工改 Xcode。
#
# 为什么是"打补丁"而不是改配置：gen/apple 由 `tauri ios init` 生成，被 gitignore，
# 直接手改会在下一次 init 时丢失。所以这里每次 init 之后重放一遍，幂等。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
APP_DIR="$ROOT_DIR/desktop/src-tauri/gen/apple"
PLIST="$APP_DIR/danmo-novel-desktop_iOS/Info.plist"
PROJECT_YML="$APP_DIR/project.yml"

if [[ ! -f "$PLIST" ]]; then
  echo "ERROR: 找不到 $PLIST —— 先跑 tauri ios init" >&2
  exit 1
fi

echo "==> Patching iOS Info.plist"
pb() { /usr/libexec/PlistBuddy -c "$1" "$PLIST" >/dev/null 2>&1; }

# 分类必填：没有 LSApplicationCategoryType 的应用会被拒。
pb "Add :LSApplicationCategoryType string public.app-category.productivity" || \
  pb "Set :LSApplicationCategoryType public.app-category.productivity"

# iPad 要能进分屏/多任务：UIRequiresFullScreen 必须不存在或为 false。
pb "Delete :UIRequiresFullScreen" || true
pb "Add :UIRequiresFullScreen bool false" || true

# 只用 HTTPS 调用用户自己的模型 API，不属于"非豁免加密"，否则每次上传都要手填
# 出口合规说明。
pb "Add :ITSAppUsesNonExemptEncryption bool false" || \
  pb "Set :ITSAppUsesNonExemptEncryption false"

# 外接键盘/指针：iPad 上编辑正文时才有正确的光标与快捷键行为。
pb "Add :UIApplicationSupportsIndirectInputEvents bool true" || true

# 中文正文依赖系统字体，不需要额外字体声明；不声明 UIAppFonts 反而避免审核问
# "为什么包内字体没有授权文件"。

# 隐私清单：本应用不收集数据、不追踪、不访问需要理由的 API。
MANIFEST="$APP_DIR/danmo-novel-desktop_iOS/PrivacyInfo.xcprivacy"
echo "==> Writing PrivacyInfo.xcprivacy"
cat > "$MANIFEST" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>NSPrivacyTracking</key>
	<false/>
	<key>NSPrivacyTrackingDomains</key>
	<array/>
	<key>NSPrivacyCollectedDataTypes</key>
	<array/>
	<key>NSPrivacyAccessedAPITypes</key>
	<array/>
</dict>
</plist>
PLIST

if [[ -f "$PROJECT_YML" ]] && ! grep -q "PrivacyInfo.xcprivacy" "$PROJECT_YML"; then
  echo "==> Registering PrivacyInfo.xcprivacy as a resource"
  python3 - "$PROJECT_YML" <<'PY'
import re, sys
path = sys.argv[1]
src = open(path).read()
# 放在 sources 列表末尾，显式声明为 resource：xcprivacy 若被当成源码编译会报错。
marker = "    info:\n"
entry = "      - path: danmo-novel-desktop_iOS/PrivacyInfo.xcprivacy\n        buildPhase: resources\n"
if marker in src and "PrivacyInfo.xcprivacy" not in src:
    src = src.replace(marker, entry + marker, 1)
    open(path, "w").write(src)
    print("registered")
else:
    print("skipped")
PY
fi

echo "==> iOS hygiene OK"
