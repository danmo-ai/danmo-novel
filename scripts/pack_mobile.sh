#!/usr/bin/env bash
# Native iOS / Android packaging. The Vue frontend and Tauri remain desktop-only.
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PLATFORM="${1:-ios}"
CONFIGURATION="${CONFIGURATION:-Debug}"
# RELEASE_VERSION is optional. Under `set -u` a bare ${RELEASE_VERSION#v}
# aborts the script when the variable is unset, which is what the Android CI
# job hit: the iOS job happened to pass it, the Android job did not.
VERSION="${RELEASE_VERSION:-}"
VERSION="${VERSION:-0.1.0}"
cd "$ROOT_DIR"
case "$PLATFORM" in ios|android) ;; *) echo 'Usage: pack_mobile.sh ios|android' >&2; exit 2 ;; esac

./scripts/build_go_mobile.sh "$PLATFORM"
mkdir -p out/mobile/native

if [[ "$PLATFORM" == "ios" ]]; then
  command -v xcodegen >/dev/null || { echo 'ERROR: brew install xcodegen' >&2; exit 1; }
  cd native-ios
  RELEASE_VERSION="$VERSION" APPLE_TEAM_ID="${APPLE_TEAM_ID:-}" xcodegen generate
  # Branch on a real signing identity, not on APPLE_TEAM_ID. The team id is a
  # repository secret that exists even when no certificate was imported, and
  # archiving without an identity fails with "No profiles were found". A local
  # machine with the team id but no cert hits the same wall.
  HAS_SIGNING_IDENTITY=0
  if [[ -n "${APPLE_TEAM_ID:-}" ]] && security find-identity -p codesigning -v 2>/dev/null \
      | grep -qE 'Apple (Development|Distribution)|iPhone (Developer|Distribution)'; then
    HAS_SIGNING_IDENTITY=1
  fi
  if [[ "$HAS_SIGNING_IDENTITY" == "0" ]]; then
    if [[ -n "${APPLE_TEAM_ID:-}" ]]; then
      echo "==> APPLE_TEAM_ID=$APPLE_TEAM_ID 但钥匙串里没有 iOS 签名身份：构建未签名模拟器包"
    else
      echo '==> 没有 APPLE_TEAM_ID：构建未签名模拟器包'
    fi
    xcodebuild -project DanmoNovel.xcodeproj -scheme DanmoNovel \
      -configuration Debug -destination 'generic/platform=iOS Simulator' \
      CODE_SIGNING_ALLOWED=NO build
    echo 'Native iOS simulator build succeeded.'
  else
    echo "==> 原生 iOS 归档 · team=$APPLE_TEAM_ID"
    ARCHIVE="$ROOT_DIR/out/mobile/native/DanmoNovel.xcarchive"
    xcodebuild -project DanmoNovel.xcodeproj -scheme DanmoNovel \
      -configuration Release -destination 'generic/platform=iOS' \
      -archivePath "$ARCHIVE" archive
    METHOD="${IOS_EXPORT_METHOD:-release-testing}"
    EXPORT_PLIST="$ROOT_DIR/out/mobile/native/ExportOptions.plist"
    /usr/libexec/PlistBuddy -c 'Clear dict' -c "Add :method string $METHOD" \
      -c 'Add :signingStyle string automatic' -c "Add :teamID string $APPLE_TEAM_ID" \
      "$EXPORT_PLIST"
    xcodebuild -exportArchive -archivePath "$ARCHIVE" \
      -exportPath "$ROOT_DIR/out/mobile/native" -exportOptionsPlist "$EXPORT_PLIST"
    echo "Native iOS IPA: $ROOT_DIR/out/mobile/native"
  fi
else
  if [[ -z "${JAVA_HOME:-}" ]]; then
    for candidate in /Library/Java/JavaVirtualMachines/*/Contents/Home "$HOME/Library/Java/JavaVirtualMachines"/*/Contents/Home; do
      if [[ -x "$candidate/bin/javac" ]]; then export JAVA_HOME="$candidate"; break; fi
    done
  fi
  export JAVA_HOME="${JAVA_HOME:-$(/usr/libexec/java_home -v 21 2>/dev/null || true)}"
  if [[ -z "${JAVA_HOME:-}" || ! -x "$JAVA_HOME/bin/javac" ]]; then
    echo 'ERROR: Android 原生构建需要完整 JDK 21（含 javac）。' >&2
    exit 1
  fi
  export PATH="$JAVA_HOME/bin:$PATH"
  if [[ -z "${ANDROID_HOME:-}" ]] && command -v brew >/dev/null 2>&1; then
    export ANDROID_HOME="$(brew --prefix)/share/android-commandlinetools"
  fi
  cd native-android
  if [[ -x gradlew ]]; then
    ./gradlew :app:assembleDebug :app:testDebugUnitTest :app:lintDebug
  else
    gradle :app:assembleDebug :app:testDebugUnitTest :app:lintDebug
  fi
  echo 'Native Android APK: native-android/app/build/outputs/apk/debug/app-debug.apk'
fi
