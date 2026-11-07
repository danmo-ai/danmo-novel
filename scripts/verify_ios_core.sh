#!/usr/bin/env bash
# Verify the real generated XCFramework inside a booted iOS simulator.
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DEVICE="${1:-BDB66022-3010-4886-80BB-750E8DFAF625}"
export DEVELOPER_DIR="${DEVELOPER_DIR:-/Applications/Xcode.app/Contents/Developer}"
cd "$ROOT_DIR"
./scripts/build_go_mobile.sh ios
mkdir -p out/mobile
cp scripts/CoreSmoke.swift out/mobile/CoreSmoke.swift
xcrun --sdk iphonesimulator swiftc \
  -target arm64-apple-ios15.0-simulator \
  -F out/mobile/DanmoNovel.xcframework/ios-arm64_x86_64-simulator \
  out/mobile/CoreSmoke.swift -o out/mobile/CoreSmoke -framework DanmoNovel
xcrun simctl spawn "$DEVICE" "$ROOT_DIR/out/mobile/CoreSmoke"
