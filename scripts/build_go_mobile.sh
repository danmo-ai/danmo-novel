#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$ROOT_DIR/out/mobile"
GOMOBILE="$(go env GOPATH)/bin/gomobile"
TARGETS="${1:-ios,android}"
export DEVELOPER_DIR="${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}"
export ANDROID_HOME="${ANDROID_HOME:-$(brew --prefix)/share/android-commandlinetools}"
export ANDROID_NDK_HOME="${ANDROID_NDK_HOME:-$(brew --prefix)/share/android-ndk}"
export CGO_ENABLED=1

mkdir -p "$OUT_DIR"
cd "$ROOT_DIR"
# gobind is a build-time tool; keeping it in go.mod makes bind reproducible.
go mod tidy

case ",$TARGETS," in
  *,ios,*)
    "$GOMOBILE" bind -v -target=ios -o "$OUT_DIR/DanmoNovel.xcframework" ./mobile
    mkdir -p "$ROOT_DIR/desktop/plugins/localcore/ios/Frameworks"
    cp -R "$OUT_DIR/DanmoNovel.xcframework" "$ROOT_DIR/desktop/plugins/localcore/ios/Frameworks/"
    ;;
esac
case ",$TARGETS," in
  *,android,*)
    "$GOMOBILE" bind -v -target=android -androidapi 23 -o "$OUT_DIR/danmo-novel.aar" ./mobile
    mkdir -p "$ROOT_DIR/desktop/plugins/localcore/android/libs"
    cp "$OUT_DIR/danmo-novel.aar" "$ROOT_DIR/desktop/plugins/localcore/android/libs/"
    ;;
esac

printf 'Mobile core artifacts:\n'
ls -ld "$OUT_DIR"/DanmoNovel.xcframework "$OUT_DIR"/danmo-novel.aar 2>/dev/null || true
