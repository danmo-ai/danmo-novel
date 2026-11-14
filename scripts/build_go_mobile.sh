#!/usr/bin/env bash
# Build the Go core for gomobile (xcframework / aar). The native iOS and
# Android apps link these artifacts directly from out/mobile.
#
# gomobile needs two tools beyond the Go compiler: `gomobile` itself and
# `gobind`. Both come from golang.org/x/mobile, which is pinned in go.mod so a
# bind on CI and on a laptop produce the same binding. `gomobile init` is what
# actually installs gobind and the Android/Apple support packages; without it
# bind fails with "gobind was not found".
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$ROOT_DIR/out/mobile"
GOBIN_DIR="$(go env GOPATH)/bin"
GOMOBILE="$GOBIN_DIR/gomobile"
TARGETS="${1:-ios,android}"
export DEVELOPER_DIR="${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}"
export CGO_ENABLED=1
# A JDK with javac is required by gobind. Search standard macOS JDK locations
# before asking for a Homebrew JDK (an IDE's JRE alone is not sufficient).
if ! javac -version >/dev/null 2>&1; then
  for candidate in /Library/Java/JavaVirtualMachines/*/Contents/Home "$HOME/Library/Java/JavaVirtualMachines"/*/Contents/Home /Applications/Android\ Studio.app/Contents/jbr/Contents/Home; do
    if [[ -x "$candidate/bin/javac" ]]; then export JAVA_HOME="$candidate"; break; fi
  done
fi
if [[ -n "${JAVA_HOME:-}" ]]; then export PATH="$JAVA_HOME/bin:$PATH"; fi

# Android paths differ between a laptop (Homebrew) and CI (ANDROID_HOME preset
# by setup-android, which only exists at runtime). Resolve both instead of
# hardcoding one: an empty ANDROID_NDK_HOME makes gobind pick a stale toolchain
# and fail with an unrelated clang error.
if [[ -z "${ANDROID_HOME:-}" ]] && command -v brew >/dev/null 2>&1; then
  ANDROID_HOME="$(brew --prefix 2>/dev/null)/share/android-commandlinetools"
fi
if [[ -z "${ANDROID_NDK_HOME:-}" && -n "${ANDROID_HOME:-}" && -d "$ANDROID_HOME/ndk" ]]; then
  ANDROID_NDK_HOME="$(ls -d "$ANDROID_HOME"/ndk/* 2>/dev/null | sort -V | tail -1 || true)"
fi
if [[ -z "${ANDROID_NDK_HOME:-}" ]] && command -v brew >/dev/null 2>&1; then
  ANDROID_NDK_HOME="$(brew --prefix 2>/dev/null)/share/android-ndk"
fi
export ANDROID_HOME="${ANDROID_HOME:-}"
export ANDROID_NDK_HOME="${ANDROID_NDK_HOME:-}"

mkdir -p "$OUT_DIR" "$GOBIN_DIR"
cd "$ROOT_DIR"

# Install gomobile at the version go.mod pins (not @latest): the generated
# binding is part of the build contract, so a silent minor bump can change it.
echo "==> Installing gomobile from go.mod"
GOBIN="$GOBIN_DIR" go install golang.org/x/mobile/cmd/gomobile
GOBIN="$GOBIN_DIR" go install golang.org/x/mobile/cmd/gobind

# `gomobile init` is a no-op on a warm cache and installs gobind + the platform
# support packages on a cold one. It must run before the first bind.
echo "==> gomobile init"
"$GOMOBILE" init

case ",$TARGETS," in
  *,ios,*)
    # gomobile writes intermediate frameworks here; a stale one from another
    # Go or Xcode version fails the link with a confusing SDK error.
    rm -rf "$OUT_DIR/DanmoNovel.xcframework"
    "$GOMOBILE" bind -v -target=ios -o "$OUT_DIR/DanmoNovel.xcframework" ./mobile
    ;;
esac
case ",$TARGETS," in
  *,android,*)
    # gobind shells out to javac to compile the Java wrapper. Without a JDK the
    # failure surfaces as a bare "exit status 1" after the whole Go build, which
    # reads like a code error; stop early with the actual cause instead.
    if ! javac -version >/dev/null 2>&1; then
      echo "ERROR: 需要 JDK 才能绑定 Android（javac 不可用）。" >&2
      echo "       macOS: brew install openjdk@21 && export JAVA_HOME=\$(/usr/libexec/java_home -v 21)" >&2
      exit 1
    fi
    rm -f "$OUT_DIR/danmo-novel.aar"
    "$GOMOBILE" bind -v -target=android -androidapi 23 -o "$OUT_DIR/danmo-novel.aar" ./mobile
    ;;
esac

printf 'Mobile core artifacts:\n'
ls -ld "$OUT_DIR"/DanmoNovel.xcframework "$OUT_DIR"/danmo-novel.aar 2>/dev/null || true
