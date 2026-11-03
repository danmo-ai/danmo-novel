#!/usr/bin/env bash
# 写入发布用的 GitHub repository secrets：p12 路径当参数，其余信息按提示填。
#
#   scripts/gh_set_release_secrets.sh ~/certs/developer-id.p12
#   scripts/gh_set_release_secrets.sh ~/certs/developer-id.p12 -R owner/repo
#
# 值都从 stdin 喂给 gh（不进 ps、不回显），p12 与 updater.key 转成单行 base64。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

P12=""
REPO=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -R|--repo) REPO="${2:-}"; shift 2 ;;
    -h|--help) sed -n '2,7p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*) echo "未知参数：$1" >&2; exit 1 ;;
    *) P12="$1"; shift ;;
  esac
done

[[ -n "$P12" ]] || { echo "用法：$0 <p12 路径> [-R owner/repo]" >&2; exit 1; }
P12="${P12/#\~/$HOME}"
[[ -f "$P12" ]] || { echo "找不到 ${P12}" >&2; exit 1; }

command -v gh >/dev/null || { echo "找不到 gh：brew install gh" >&2; exit 1; }
gh auth status >/dev/null 2>&1 || { echo "先跑 gh auth login" >&2; exit 1; }

ask() {
  local def="${2:-}" v=""
  if [[ -n "$def" ]]; then printf '%s [%s]: ' "$1" "$def" >&2; else printf '%s: ' "$1" >&2; fi
  read -r v || true
  printf '%s' "${v:-$def}"
}
ask_secret() { printf '%s' "$1" >&2; local v=""; read -rs v || true; printf '\n' >&2; printf '%s' "$v"; }

TMP="$(mktemp -d "${TMPDIR:-/tmp}/ghsecrets.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

put() { # put <名称> <文件>
  printf '  → %s ... ' "$1" >&2
  if gh secret set "$1" --repo "$REPO" < "$2" 2>/dev/null; then echo "ok" >&2; else echo "失败" >&2; fi
}

# 默认取 origin 的 OWNER/REPO。
if [[ -z "$REPO" ]]; then
  REPO="$(git -C "$ROOT" remote get-url origin 2>/dev/null \
    | sed -e 's#^git@[^:]*:##' -e 's#^https\{0,1\}://[^/]*/##' -e 's#\.git$##')"
fi

echo
REPO="$(ask "仓库 OWNER/REPO" "$REPO")"
echo "目标：$REPO"

# ---- p12：必须转单行 base64，GitHub secret 不接受换行 ----
echo
echo "[1/2] macOS 签名证书"
base64 < "$P12" | tr -d '\n' > "$TMP/p12"
put APPLE_CERTIFICATE "$TMP/p12"

printf '%s' "$(ask_secret "p12 导出密码：")" > "$TMP/p12pw"
put APPLE_CERTIFICATE_PASSWORD "$TMP/p12pw"

printf '%s' "$(ask "签名身份（如 Developer ID Application: Foo (TEAMID)）" "")" > "$TMP/id"
put APPLE_SIGNING_IDENTITY "$TMP/id"

# ---- 公证：Apple ID + App 专用密码 + Team ID ----
echo
echo "[2/2] 公证"
printf '%s' "$(ask "Apple ID" "")" > "$TMP/appleid"
put APPLE_ID "$TMP/appleid"

printf '%s' "$(ask_secret "App 专用密码（abcd-efgh-ijkl-mnop）：")" > "$TMP/applepw"
put APPLE_PASSWORD "$TMP/applepw"

printf '%s' "$(ask "Team ID（10 位）" "")" > "$TMP/team"
put APPLE_TEAM_ID "$TMP/team"

# ---- Tauri 更新包签名：release.yml 真正读的两个 ----
echo
echo "[附加] Tauri 更新包签名"
key="$(ask "updater.key 路径（回车跳过）" "")"
key="${key/#\~/$HOME}"
if [[ -f "$key" ]]; then
  tr -d '\r' < "$key" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/^"//' -e 's/"$//' > "$TMP/key"
  put TAURI_SIGNING_PRIVATE_KEY "$TMP/key"
  pw="$(ask_secret "密钥密码（没设就直接回车）：")"
  if [[ -n "$pw" ]]; then
    printf '%s' "$pw" > "$TMP/pw"
    put TAURI_SIGNING_PRIVATE_KEY_PASSWORD "$TMP/pw"
  else
    echo "  → TAURI_SIGNING_PRIVATE_KEY_PASSWORD 跳过（空密码）" >&2
  fi
else
  echo "  跳过" >&2
fi

echo
gh secret list --repo "$REPO" 2>/dev/null | sed 's/^/  /'
echo
echo "触发一次发布：git tag v0.1.0 && git push origin v0.1.0"
