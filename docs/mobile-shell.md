# Mobile shell (iOS / Android)

The phone app is **not** the desktop UI under a breakpoint. It is a second Vue
entry with its own navigation, sharing the same business logic.

## Two entries, one codebase

| | Desktop / browser | Mobile |
|---|---|---|
| HTML | `frontend/index.html` | `frontend/mobile.html` |
| Entry | `src/main.ts` → `App.vue` | `src/main.mobile.ts` → `src/mobile/AppMobile.vue` |
| Router | `src/router/index.ts` (flat pages) | `src/mobile/router.ts` (hierarchy) |
| Build | `npm run build` → `out/frontend/dist` | `npm run build:mobile` → `out/frontend/mobile` |
| Styles | `src/styles.css` (tokens + desktop) | `src/mobile/styles.css` (tokens only) |
| Updater | tauri updater + `latest.json` | none (`VITE_SHELL=mobile`) |

Fullscreen navigation on the phone:

```
/                          shelf
/books/:id                 unit list (grouped by volume)
/books/:id/units/:unitId   细纲 / 正文 / 审校  ← segments, not tabs
/books/:id/bible           doc list → detail
/books/:id/setup           pitch workspace
/runs, /settings           shared views
```

## Where logic lives

Anything both shells must agree on is a composable, never a view:

- `composables/useBookWorkspace.ts` — book load, unit grouping, primary action,
  candidate accept/reject, review retries, chapter editing, run polling.
- `composables/useReview.ts` — per-unit finding groups, selection, chapter
  filter, archive.
- `composables/useBible.ts` — doc list, markdown/state/locked-term parsing,
  identity timeline.

Presentational components are reused as-is by both shells: `UnitContract`,
`UnitProse`, `UnitFlowActions`, `ChapterDiff`, `PlanCandidateDiff`,
`RunConfirmDialog` (already fullscreen below 720px), `RelationGraph`,
`ProjectSetup`, `SettingsView`, `RunsView`.

## Rules to keep the two shells from drifting

1. **No business logic in a mobile view.** Requests go through `api/client`,
   state through the store or a composable.
2. **No `@media` in `src/mobile/`.** Layout is fixed; hierarchy comes from the
   router. If a screen needs two layouts, it is two routes.
3. **New panel ⇒ composable first, templates second.** If a feature can only be
   written once, it belongs in the shared layer.
4. `vue-tsc` runs over both entries (`npm run typecheck`), so a break in either
   shell fails the build.

## Build

```bash
make pack-ios        # Go core → xcframework, mobile UI, xcodebuild
make pack-android    # Go core → aar, mobile UI, gradle
npm run dev:mobile   # UI only, needs a backend on :7804
```

`scripts/build_go_mobile.sh` installs `gomobile`/`gobind` at the version pinned
in `go.mod` and runs `gomobile init` — without it `bind` fails with
`gobind was not found`. The Android target preflights `javac`; a missing JDK is
reported before the Go build instead of after it.

`scripts/pack_mobile.sh ios` runs `tauri ios init` and then
`scripts/ios_hygiene.sh`, which replays the App Store requirements into the
generated project (`gen/apple` is gitignored, so the patch cannot be committed):

- `LSApplicationCategoryType`
- `UIRequiresFullScreen=false` (iPad multitasking)
- `ITSAppUsesNonExemptEncryption=false` (no export-compliance prompt)
- `PrivacyInfo.xcprivacy` (no data collection, no tracking)
- `UIApplicationSupportsIndirectInputEvents`

Without `APPLE_SIGNING_IDENTITY` the iOS build passes `--no-sign` **and targets
the simulator**: a device archive needs a development team (`xcodebuild` fails
with "Arch specified by Xcode was invalid" when none is set), while the
simulator build still proves the whole chain — Go core, app assets, plugin IPC:

```bash
xcrun simctl install booted "desktop/src-tauri/gen/apple/build/arm64-sim/Danmo Novel.app"
xcrun simctl launch booted com.danmo.novel
```

On first launch the app writes `Library/Application Support/com.danmo.novel/novel.db`
inside its container, which is the proof that the in-process Go core came up.
Set `APPLE_SIGNING_IDENTITY` (and a team) to get a signed `.ipa` for devices.

## App Store notes

- The app is fully offline for reading and editing; only generation calls the
  user's own model endpoint. Submissions should say "local engine, data stays on
  device" — the WebView itself is not a rejection reason, an empty shell is.
- First launch offers a sample book that needs no model call, because a
  reviewer has no API key and an empty shelf with disabled buttons is the
  classic 4.2 failure. The book itself is bundled in the Go binary
  (`core/service/books/demo/demo-fog-harbor/`) and served by
  `POST /api/books/demo`: it is a **complete** small book — 总纲, 设定, 卷纲,
  细纲, and one unit of prose — and `books/demo_test.go` asserts it still passes
  the same `ValidateStructuredOutlineFile` checks a real plan candidate must
  pass. Re-clicking the entry never overwrites author edits.
- No self-update on mobile: `hasUpdater()` in `src/utils/desktop.ts` is false
  when `VITE_SHELL=mobile`, and `capabilities/mobile.json` has no updater
  permission.
- Fonts are system fonts — nothing is fetched from Google Fonts at runtime.
