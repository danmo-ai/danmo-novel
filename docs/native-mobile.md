# Native mobile apps (iOS / Android)

The shipping phone UI is native: **SwiftUI** on iOS and **Kotlin / Jetpack
Compose** on Android. Vue is desktop-only; neither native app embeds a WebView
or the Vue bundle.

Both apps call the same Go REST handlers as desktop. `mobile.Start` initializes
the local database and book library in the application sandbox. Requests use
`danmo://local/api/...` through `URLProtocol` / an OkHttp interceptor, which
executes the handler in-process. No loopback TCP port or unauthenticated local
server is exposed.

## Native projects

| | iOS | Android |
|---|---|---|
| Project | `native-ios/project.yml` | `native-android/` |
| UI | SwiftUI, iOS 17+ | Compose / Material 3, Android 8+ |
| Navigation | `TabView`, `NavigationStack` | Navigation Compose, predictive system back |
| Local core | `DanmoNovel.xcframework` | `danmo-novel.aar` |
| REST transport | `URLSession` + `LocalCoreURLProtocol` | OkHttp + local interceptor |
| Storage | Application Support | App-private files |

The first native vertical slice includes the shelf, new-book sheet, bundled
sample book, unit list, prose reader, provider settings, and run history.
Candidate diffs/acceptance, generation confirmation, structured setup, prose
editing, and review workflows remain to be ported; there is no WebView
fallback. Desktop/mobile libraries are separate until explicit import/export
is implemented. During development, the mobile workspace only recognizes its
current `NOVEL_HOME/library/novel` registry; stale absolute directories are
not migrated or implicitly adopted.

## iPhone Duo

Build with iOS 27.1+ (currently 27.2). The SwiftUI layout responds to size
class and available space, never device model or orientation. Wide scenes show
books and details side by side; narrower scenes keep the native push
navigation. Each book has a dedicated navigation stack, preserving the unit →
reader transition when the scene moves between displays.

The iPhone Duo simulator runs the complete REST/model tests, sample-book UI,
reader controls, and portrait/landscape rotation. Manual fold/unfold poses and
physical-device safe areas still need final device acceptance; passing these
simulator checks is not a guarantee of App Store approval.

## Build

```bash
make mobile-core
make pack-ios
make pack-android
```

Without `APPLE_TEAM_ID`, the iOS command builds an unsigned simulator app.
With a team, it archives for iOS devices and exports an IPA. Select the
export with `IOS_EXPORT_METHOD=debugging`, `release-testing` (TestFlight), or
`app-store-connect`. `RELEASE_VERSION` sets the app version.

Android produces a debug APK and runs JVM tests and lint. The build requires
JDK 21 (Temurin or another full JDK with `javac`) and Android SDK 36. A signed release AAB still
requires the release keystore configuration; do not commit credentials.

CI builds these native projects directly. It installs no Rust/Tauri and builds
no mobile frontend bundle; the only shared artifact is the gomobile core in
`out/mobile`.

## Privacy and store requirements

- The Go REST handler is called in-process; no TCP listener is opened.
- Books, configuration, and keys remain in the app sandbox. API responses
  never return plaintext provider keys.
- The native UI uses system navigation, keyboard handling, Dynamic Type
  compatible text, safe areas, and platform back gestures.
- iOS includes a privacy manifest, supports iPad multitasking, and declares
  the productivity category. No self-updater or remote UI is bundled.
- The bundled sample book gives reviewers a complete reading path without
  requiring a model key.
- Store acceptance still requires real-device accessibility, lifecycle,
  keyboard, interrupted-run, import/export, and complete workflow testing.
