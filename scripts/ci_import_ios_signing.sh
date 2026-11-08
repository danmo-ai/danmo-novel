#!/usr/bin/env bash
# 把 iOS 签名材料装进 runner：p12 → 临时 keychain，.mobileprovision → Xcode 描述目录。
#
# 用法（GitHub Actions 的 macOS job）：
#   env:
#     IOS_CERTIFICATE: ${{ secrets.IOS_CERTIFICATE }}
#     IOS_CERTIFICATE_PASSWORD: ${{ secrets.IOS_CERTIFICATE_PASSWORD }}
#     IOS_PROVISIONING_PROFILE: ${{ secrets.IOS_PROVISIONING_PROFILE }}
#     APPLE_TEAM_ID: ${{ secrets.APPLE_TEAM_ID }}
#   run: scripts/ci_import_ios_signing.sh
#
# 没设 IOS_CERTIFICATE 时直接跳过，pack_mobile.sh 会改出模拟器包 —— 明确"未签名"
# 比在 xcodebuild 里报 requires a development team 好排查。
#
# 注意 APPLE_CERTIFICATE 不能复用：那是 macOS 的 Developer ID 证书，iOS 设备归档
# 需要的是 Apple Development（装真机）或 Apple Distribution（TestFlight / App Store）。
set -euo pipefail

if [[ -z "${IOS_CERTIFICATE:-}" ]]; then
  echo "No IOS_CERTIFICATE secret — iOS job will build an unsigned simulator bundle"
  exit 0
fi

fail() { echo "::error::$*" >&2; exit 1; }

[[ -n "${IOS_CERTIFICATE_PASSWORD:-}" ]] || fail "IOS_CERTIFICATE 已设置，但缺 IOS_CERTIFICATE_PASSWORD"
[[ -n "${APPLE_TEAM_ID:-}" ]] || fail "IOS_CERTIFICATE 已设置，但缺 APPLE_TEAM_ID"
[[ -n "${RUNNER_TEMP:-}" ]] || fail "这个脚本只在 GitHub Actions 的 macOS runner 上用（缺 RUNNER_TEMP）"

KEYCHAIN="$RUNNER_TEMP/ios-signing.keychain-db"
P12="$RUNNER_TEMP/ios-signing.p12"
KEYCHAIN_PW="$(openssl rand -hex 24)"

if ! printf '%s' "$IOS_CERTIFICATE" | base64 --decode > "$P12" 2>/dev/null; then
  fail "IOS_CERTIFICATE 不是合法 base64（通常是复制时被截断）"
fi
chmod 600 "$P12"

# DER 编码的 p12 一定以 0x30 开头；导出成 .cer/.pem 是常见错误。
if [[ "$(od -An -tx1 -N1 "$P12" | tr -d ' \n')" != "30" ]]; then
  rm -f "$P12"
  fail "IOS_CERTIFICATE 解码后不是 PKCS#12 —— 导出时要选带私钥的 .p12"
fi

security create-keychain -p "$KEYCHAIN_PW" "$KEYCHAIN"
security set-keychain-settings -lut 21600 "$KEYCHAIN"
security unlock-keychain -p "$KEYCHAIN_PW" "$KEYCHAIN"

if ! security import "$P12" -k "$KEYCHAIN" -P "$IOS_CERTIFICATE_PASSWORD" \
  -T /usr/bin/codesign -T /usr/bin/security >/dev/null 2>&1; then
  rm -f "$P12"
  security delete-keychain "$KEYCHAIN" 2>/dev/null || true
  fail "导入 p12 失败：IOS_CERTIFICATE_PASSWORD 不对，或文件不是带私钥的 .p12"
fi
security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$KEYCHAIN_PW" "$KEYCHAIN" >/dev/null
rm -f "$P12"

security list-keychains -d user -s "$KEYCHAIN" $(security list-keychains -d user | tr -d '"')
security default-keychain -s "$KEYCHAIN"

identities="$(security find-identity -p codesigning "$KEYCHAIN" 2>&1 || true)"
echo "$identities" | sed 's/^/  /'
case "$identities" in
  *"Apple Development"*|*"Apple Distribution"*|*"iPhone Developer"*|*"iPhone Distribution"*) ;;
  *) printf '%s\n' "$identities" >&2
     fail "导入后没有 iOS 签名身份（需要 Apple Development / Apple Distribution）" ;;
esac

# 描述文件必须落在 Xcode 找得到的位置，否则自动签名会报 No profiles found。
if [[ -n "${IOS_PROVISIONING_PROFILE:-}" ]]; then
  PROFILES_DIR="$HOME/Library/Developer/Xcode/UserData/Provisioning Profiles"
  mkdir -p "$PROFILES_DIR"
  PROFILE_TMP="$RUNNER_TEMP/profile.mobileprovision"
  printf '%s' "$IOS_PROVISIONING_PROFILE" | base64 --decode > "$PROFILE_TMP" 2>/dev/null \
    || fail "IOS_PROVISIONING_PROFILE 不是合法 base64"
  # 用 UUID 命名，和 Xcode 自己的规则一致。
  UUID="$(security cms -D -i "$PROFILE_TMP" 2>/dev/null | plutil -extract UUID raw - 2>/dev/null || true)"
  [[ -n "$UUID" ]] || fail "IOS_PROVISIONING_PROFILE 解不出 UUID（不是 .mobileprovision？）"
  cp "$PROFILE_TMP" "$PROFILES_DIR/$UUID.mobileprovision"
  rm -f "$PROFILE_TMP"
  echo "Provisioning profile installed: $UUID"
else
  echo "::warning::没有 IOS_PROVISIONING_PROFILE —— 依赖 Xcode 自动签名，CI 上大概率拿不到描述文件"
fi

if [[ -n "${GITHUB_ENV:-}" ]]; then
  echo "APPLE_TEAM_ID=$APPLE_TEAM_ID" >> "$GITHUB_ENV"
fi

echo "iOS signing ready (team $APPLE_TEAM_ID)"
