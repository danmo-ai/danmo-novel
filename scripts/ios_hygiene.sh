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

# 签名：项目由 xcodegen 生成，DESIGN_TEAM 只能每次重建后写入，不能写在被 gitignore
# 的工程文件里。team 优先取 APPLE_TEAM_ID，其次从本机安装的描述文件里推断。
TEAM="${APPLE_TEAM_ID:-}"
if [[ -z "$TEAM" ]]; then
  TEAM="$(security cms -D -i "$HOME/Library/Developer/Xcode/UserData/Provisioning Profiles/"*.mobileprovision 2>/dev/null \
    | plutil -extract TeamIdentifier.0 raw - 2>/dev/null | head -1 || true)"
fi
if [[ -z "$TEAM" ]]; then
  echo "WARNING: 没有 APPLE_TEAM_ID，也没有本机描述文件可推断 team —— 只能出未签名包" >&2
else
  echo "==> Signing team: $TEAM"
  python3 - "$PROJECT_YML" "$TEAM" <<'PY'
import sys
path, team = sys.argv[1], sys.argv[2]
src = open(path).read()
if "DEVELOPMENT_TEAM:" in src:
    # 换 team 时覆盖旧值，避免沿用上一次的团队。
    import re
    src = re.sub(r'(\n\s*DEVELOPMENT_TEAM:\s*)\S+', r'\g<1>' + team, src, count=1)
else:
    marker = "      base:\n"
    inject = (
        f"        DEVELOPMENT_TEAM: {team}\n"
        "        CODE_SIGN_STYLE: Automatic\n"
    )
    if marker not in src:
        raise SystemExit("project.yml 缺少 settings.base，无法写入签名配置")
    src = src.replace(marker, marker + inject, 1)
open(path, "w").write(src)
print("signing settings applied")
PY
fi

echo "==> iOS hygiene OK"
