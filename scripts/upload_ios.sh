#!/usr/bin/env bash
# 把 .ipa 上传到 App Store Connect（走 TestFlight / App Store 审核）。
#
# 用法：
#   scripts/upload_ios.sh <path/to/app.ipa>
#
# 凭据二选一（环境变量）：
#   App Store Connect API Key（推荐，可在 CI 用）：
#     ASC_KEY_ID      Key ID（App Store Connect → 用户与访问 → 集成 → 密钥）
#     ASC_ISSUER_ID   Issuer ID（同页顶部）
#     ASC_KEY_PATH    AuthKey_XXXX.p8 所在目录，默认 ~/.appstoreconnect/private_keys
#   Apple ID + App 专用密码：
#     APPLE_ID / APPLE_PASSWORD / APPLE_TEAM_ID
#
# 上传前还有两件必须人工完成的事，脚本不能替你做：
#   1. App Store Connect 里先建好这个 app 记录（Bundle ID com.danmo.novel）；
#   2. 填好「App 信息」与截图。上传成功只代表二进制进了处理队列。
set -euo pipefail

IPA="${1:-}"
if [[ -z "$IPA" || ! -f "$IPA" ]]; then
  echo "Usage: upload_ios.sh <path/to/app.ipa>" >&2
  exit 2
fi

if [[ -n "${ASC_KEY_ID:-}" && -n "${ASC_ISSUER_ID:-}" ]]; then
  KEY_DIR="${ASC_KEY_PATH:-$HOME/.appstoreconnect/private_keys}"
  KEY_FILE="$KEY_DIR/AuthKey_${ASC_KEY_ID}.p8"
  [[ -f "$KEY_FILE" ]] || { echo "找不到 API Key：$KEY_FILE" >&2; exit 1; }
  echo "==> 用 App Store Connect API Key 上传（Key ${ASC_KEY_ID}）"
  xcrun altool --upload-app --type ios --file "$IPA" \
    --apiKey "$ASC_KEY_ID" --apiIssuer "$ASC_ISSUER_ID"
elif [[ -n "${APPLE_ID:-}" && -n "${APPLE_PASSWORD:-}" ]]; then
  echo "==> 用 Apple ID 上传（${APPLE_ID}）"
  xcrun altool --upload-app --type ios --file "$IPA" \
    --username "$APPLE_ID" --password "$APPLE_PASSWORD" \
    ${APPLE_TEAM_ID:+--asc-provider "$APPLE_TEAM_ID"}
else
  cat >&2 <<'MSG'
没有上传凭据。两种做法选一个：

1) 打开 Xcode → Window → Organizer → 选中最新的 Danmo Novel 归档 →
   Distribute App → App Store Connect → Upload。不需要配置任何凭据。

2) 设好 ASC_KEY_ID / ASC_ISSUER_ID（并把 AuthKey_<id>.p8 放到
   ~/.appstoreconnect/private_keys/），再重跑本脚本，之后就能在 CI 里自动化。
MSG
  exit 1
fi

echo "==> 上传完成。处理结果：App Store Connect → 你的 App → TestFlight。"
echo "    二进制处理通常 5–15 分钟；首包被拒常见原因是缺少 App 隐私清单或截图。"
