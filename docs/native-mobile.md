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
| Storage | iCloud Documents + Application Support | App-private files |

iOS layout files, so a Duo change lands in the right place:

| File | Owns |
|---|---|
| `Sources/Features/ShelfView.swift` | The only top-level navigator: `NavigationSplitView` at ≥ 820pt, `NavigationStack` below it, `ShelfRoute`, cross-pose hand-off, `columnVisibility` |
| `Sources/Features/BookDetailView.swift` | Book summary, volume grouping, unit list; no `NavigationStack` of its own |
| `Sources/Features/ReaderSplitView.swift` | Expanded reading pose: unit catalog + prose via `ArrangementView` (iOS 27.1+ only) |
| `Sources/Features/ReaderView.swift` | Prose rendering, chapter paging toolbar; used both standalone and as the arrangement's secondary pane |
| `Sources/Core/NovelAPI.swift` | `URLSession` over the in-process core; `?view=list` projection for shelf and unit lists |

Android is still single-column everywhere. When the iOS expanded layout is
settled, the same split lands in `ShelfScreen.kt` / `BookScreen.kt`.

The first native vertical slice includes the shelf, new-book sheet, bundled
sample book, unit list, prose reader, provider settings, and run history.
Candidate diffs/acceptance, generation confirmation, structured setup, prose
editing, and review workflows remain to be ported; there is no WebView
fallback.

## iCloud sharing (iOS + macOS)

Books and model settings are shared between iPhone and the macOS desktop app
through the app's iCloud Documents container. The database is not shared.

| Item | iOS | macOS |
|---|---|---|
| Shared root | ubiquity container `iCloud.com.danmo.novel`, `Documents/` | `~/Library/Mobile Documents/iCloud~com~danmo~novel/Documents` |
| Books | `Documents/library/novel/<book-id>/` | `Documents/library/novel/<book-id>/` |
| Model settings | `Documents/model-settings.json` | `Documents/model-settings.json` |
| Database / runs | app sandbox (`Application Support/DanmoNovel`) | `~/.danmo-novel/novel.db` |

An iCloud container is not stored under `CloudDocs`: it is its own directory
named after the container identifier. `iCloud.com.danmo.novel` materializes as
`iCloud~com~danmo~novel` (older Xcode used `<TEAMID>~iCloud~com~danmo~novel`),
and both names are accepted.

The choice is made by system, once, inside the core
(`runtimepaths.DefaultLibraryRoot`): on macOS the library is the container, on
every other system it is `~/.danmo-novel/library`. There is no switch and no
host-specific path, so the packaged app, `make dev`, `make dev-desktop` and a
bare `go run ./cmd/danmo-novel` read the same books and the same
`model-settings.json` on one Mac. A scratch home (`NOVEL_HOME` pointing
somewhere other than `~/.danmo-novel`, which is what tests and the embedded core
do) stays local, so automated tests never touch a developer's iCloud library.

The author can override the directory (Settings → 书籍目录, `PUT
/api/settings/library-root`). Only the override is persisted
(`customLibraryRoot`); the stored `libraryRoot` is recomputed on every load, so
macOS starts using iCloud as soon as the container exists instead of pinning a
path recorded earlier. Confirming a change **moves** every book directory and
every knowledge article to the new root — `books.MoveLibrary` — and then
rebuilds the registry, so the shelf follows without a restart. A target that
already holds a book with the same id is refused (409) rather than merged. The
move is idempotent and never overwrites: an interrupted move finishes on the
next request or launch.

The same move runs once at startup when the platform default changed, which is
how books in `~/.danmo-novel/library` end up in iCloud the first time the
container is visible. `libraryRoot` is per device and is never part of the
shared settings snapshot.

The macOS app has no iCloud entitlement and cannot create the container. It
uses the container directory only once iCloud has materialized it — in practice
after the iOS app has opened its library once on the same account. Until then
the app keeps its local library rather than writing into a directory that would
never sync; Settings shows iCloud as not enabled.

The iOS side gets the container because Xcode's automatic signing provisions it:
with the entitlements file present and a team configured, `xcodebuild
-allowProvisioningUpdates` enables iCloud Documents on the App ID
(`com.danmo.novel`), creates `iCloud.com.danmo.novel`, and issues a profile
carrying `com.apple.developer.icloud-container-identifiers` /
`ubiquity-container-identifiers`. Nothing needs to be clicked in the developer
portal. Verify a build with:

```bash
security cms -D -i <app>/embedded.mobileprovision | rg icloud
```

`LocalCoreService` resolves the ubiquity container before starting the Go core,
because an iOS app sandbox cannot see `~/Library/Mobile Documents`. It hands both
paths over through `DanmonovelSetPaths`, not `setenv`: Go snapshots the process
environment when its runtime starts, so a libc `setenv` from Swift afterwards is
invisible to `os.Getenv` and the core silently falls back to its own sandbox.
That binding call is the one place a host supplies the path. The Go side has no
other platform branches: it scans the library on every start
(`books.ScanLibrary`), reconciles the SQLite registry
(`books.ReconcileRegistry`), and merges `model-settings.json`
(`core/service/settingsync`).

Before the core starts, the whole shared root is resolved for download. iCloud
stores an undownloaded file as a hidden `<name>.icloud` entry, so the real path
does not exist and `FileManager.enumerator` — which skips hidden files — never
sees it. Two failures follow from that, and both were observed on device:
`model-settings.json` looked absent, so the device pushed its empty provider list
over the Mac's; and books on the other device looked missing. The app now walks
the directory with hidden entries included, maps each placeholder back to its
real URL, and requests the download (bounded at 20 s). As a second guard,
`settingsync.Push` refuses to write when a snapshot exists but cannot be read, so
an undownloaded file can never be overwritten — that mistake cost the Mac's
provider list once during development.

What syncs:

- Provider definitions (name, protocol, base URL, dialect, model list,
  enabled flags) and the per-stage model assignments, model parameters and
  review settings. API keys never sync: the snapshot type has no key field, and
  a pull keeps whatever key the receiving device already stored.
- Deletions travel as tombstones so an offline device cannot resurrect a
  provider the user removed elsewhere.
- Settings merge per provider on `updatedAt`; the settings block is taken whole
  from whichever device changed it most recently.

What does not sync:

- `novel.db` (runs, candidates, review history). Two devices must not write one
  SQLite file; accepting a candidate on one device changes the book files, which
  the other device sees after reopening the book.
- Prose/outline files edited on both devices at the same time: last writer wins
  and iCloud may leave a conflict copy. There is no merge.

iOS materializes placeholder files before starting the core (bounded ~15 s
wait); the Go core does not use `NSFileCoordinator`. If iCloud is unavailable —
signed out, entitlement missing, unsigned simulator build — the app falls back
to its local sandbox and Settings says so. Android remains local-only.

Store/platform prerequisites: enable iCloud Documents on the `com.danmo.novel`
App ID, create the `iCloud.com.danmo.novel` container, and regenerate the
provisioning profile; otherwise device archives sign but resolve no container at
runtime.

## iPhone Duo

Apple's guidance comes from:

- [Three steps to make your app shine on iPhone Duo](https://developer.apple.com/iphone-duo/prepare/)
  (the resizability checklist)
- Tech Talks: [Prepare your app for iPhone Duo](https://developer.apple.com/videos/play/tech-talks/111461/)
  (SDK tiers, safe areas), [Raise the bar with iPhone Duo](https://developer.apple.com/videos/play/tech-talks/111462/)
  (vertical bars, item ordering, overflow),
  [Strike a pose with adaptive layouts](https://developer.apple.com/videos/play/tech-talks/111463/)
  (reserved regions, displacement, arrangements),
  [Leverage multiple displays and scenes](https://developer.apple.com/videos/play/tech-talks/111464/)
- [Designing for iPhone Duo](https://developer.apple.com/design/human-interface-guidelines/designing-for-iphone-duo)
  and *Design for iPhone Duo* (poses, controls at the edge, Split View)

This section is the checklist we hold ourselves to.

### Pre-flight before changing mobile layout

1. Decide from **offered width**, never from orientation, size class, idiom or a
   known device. `onGeometryChange { $0.size.width }`.
2. Pick the container: `NavigationSplitView` for navigation, `ArrangementView`
   for plain panes, single column otherwise. Never `HStack` + `Divider`.
3. Keep bars system-owned; give every toolbar item a symbol **and** a title.
4. Verify in **both** poses. A test that passes in landscape says nothing about
   the compact stack (that is how the typed-path bug shipped green).
5. Confirm with frame numbers, not screenshots — a stale capture looks exactly
   like a broken layout.

### SDK tier decides how much of the display we get

| Built with | What the app gets on Duo |
|---|---|
| iOS 26 SDK or earlier | Window centred on the inner display with empty space around it; outer display shows content left of the status bar and camera |
| iOS 27 SDK | Fills most of the inner display, still avoids the status-bar area on the right edge |
| **iOS 27.1 SDK or later** | Uses the full display; **toolbars and tab bars lay out vertically below the status bar** |

We build with the iOS 27.1+ SDK (currently 27.2), so the vertical-bar layout is
the one we must design for — not an edge case.

### Hard rules

These are the patterns Apple flags. The `app-resizability` skill from
`xcrun agent skills export` can also scan a project for them.

1. **Never size from the main screen.** No `UIScreen.main`; size from the view
   or window, re-read on every change, take scale from
   `@Environment(\.displayScale)`. Our iOS sources use none of these APIs.
2. **Never branch layout on orientation.** `UIDevice.current.orientation`,
   `statusBarOrientation`, `interfaceOrientation` and the scene geometry
   equivalent all describe the device, not the space the app was given. Compare
   width and height of the available area instead.
3. **Never treat size class or idiom as a device.** A Duo app can have any
   combination of size classes, and regular width is not "iPad". In particular
   the inner display is *regular width in portrait* (669pt) — splitting there
   leaves no readable column. Decide by the width and height of the space you
   were given (we compare the offered width against a minimum that fits both
   columns); never by `userInterfaceIdiom` or known device dimensions.
4. **Safe areas are asymmetric and can move.** A vertical bar sits on one edge,
   so leading and trailing insets differ. The bar can move to the left in
   landscape and Split View. Read each edge independently; never derive one edge
   from the other, never cache an inset, never compute one from a width.
5. **Only interactive content must stay inside the safe area.** Backgrounds and
   reading text may run to the edge — that is what buys back vertical space.
6. **Use system bars; do not draw our own.** System bars adapt axis, spacing,
   material and overflow automatically. A hand-built bar (a `safeAreaInset`
   bottom strip, a custom `UIToolbar`) both adds spacing the platform says not
   to add and consumes the vertical space the side-rail design is meant to
   reclaim.
7. **Do not duplicate standard affordances or redraw system chrome.** The system
   supplies back; adding a second back control creates two paths with different
   targets. Likewise a disclosure chevron: `NavigationLink` gets the system one
   for free, and a sidebar row must have none at all — a hand-drawn
   `chevron.forward` has its own size, colour and inset, so it never matches.
8. **Support every orientation and stay resizable.** `UIRequiresFullScreen` must
   be absent or false; all four interface orientations declared. We declare all
   four for both iPhone and iPad.
9. **Never hand-build a multi-column container.** An `HStack { NavigationStack …
   Divider … NavigationStack }` looks like a split but is not one: the system
   does not treat it as a split, so the detail column never gets its vertical
   bar, the fold is not respected, and column widths are whatever we typed. Use
   `NavigationSplitView` for main-detail navigation and `ArrangementView` for a
   plain two-pane layout. This app shipped the `HStack` version first and the
   detail column silently lost its vertical toolbar on Duo.

### How this app applies them

| Area | Implementation |
|---|---|
| Shell | `TabView` + system tab bar; no custom bottom navigation. No custom `tint` either — the system accent is used so controls follow the user's tint choice and Increase Contrast |
| Book shelf | System `List`. Regular width uses `List(selection:)` with `.listStyle(.sidebar)` — the system draws the selection highlight and, correctly for a sidebar, **no disclosure chevron**. Compact width uses `NavigationLink`, where the system draws and owns the chevron and back stack |
| Book → reader | The shelf owns the only top-level `NavigationStack`; a single `ShelfRoute` enum (`book` / `reader`) is registered on both the compact stack and the split's detail stack, so the back sequence is always reader → unit list → shelf and a typed path can never drop a push |
| Route type | `ShelfRoute` is shared by both stacks deliberately. A typed `NavigationStack(path:)` array only accepts its own element type, so a unit row pushing a bare `ReaderRoute` silently did nothing in compact width — the row just highlighted |
| Wide layout | Width-based, not size-class-based: the split appears only when the offered width is ≥ 820pt, which is the minimum that fits a 300pt sidebar, a 500pt detail column and the divider. The system then resolves the sidebar to 320pt. On Duo that means the inner display **in landscape** (951pt window, 867pt usable after the 84pt vertical-bar safe area); inner portrait (669pt) and the outer display (466pt) read single-column. Size class alone was wrong — the inner display is regular width even in portrait, which squeezed the reading column to ~12 characters |
| Expanded reading | With a unit open at that width the bookshelf column collapses (`.detailOnly`) and the detail becomes unit catalog + prose via the system `ArrangementView` with `.split.axes(.horizontal)` — not a hand-built `HStack`. The catalog is pinned as a fixed rail (`splitArrangementLayoutSize` + `splitArrangementFixedLayoutSize`, 240–360pt) so it never takes half the width; the prose pane declares a 420pt minimum. Below iOS 27.1, or whenever the detail column is narrower than 700pt (e.g. the bookshelf is revealed mid-read), it degrades to the single-pane reader instead of letting the arrangement drop the prose pane |
| Shared title | The two panes share one navigation bar and it sits above the catalog, so its title is the **book**, never the open unit: a unit name there reads as the list's header and mislabels a catalogue of many units. The open unit is identified by the highlighted row plus the prose header, and only the standalone compact reader titles itself with the unit (`.navigationTitle` bubbles up from the pane, so the embedded reader passes `showsNavigationTitle: false`) |
| Catalog contents | The catalog lists **every** unit grouped by volume, not only those with prose: hiding unwritten units made a full book look truncated. Unwritten ones are dimmed, carry a "正文尚未写作" hint, and get no `.tag` — in a selection `List` the tag is what makes a row selectable, so omitting it is what stops a tap from loading a missing prose file |
| Reader | System toolbar holds chapter paging (`chevron.up`/`chevron.down`/`list.number`) and reading settings as symbol + title items, so iOS can move them to the vertical rail |
| Text | Paragraphs are split once per chapter, not per frame; one selection scope per chapter |
| Camera / status bar | They sit on the right edge of the outer display; the system bar avoids them, and our content never draws its own edge padding to compensate |

Why symbol + title matters: on a vertical bar the item's height is flexible and
its width is fixed, which suits symbols. Text-only items stay on the horizontal
axis, so mixing them into the same group splits the bar across two axes. Always
supply a title as well — the system uses it in overflow menus.

### Choosing the expanded container

Three containers look interchangeable and are not. Pick by intent, not by what
fits:

| Intent | Container | Why |
|---|---|---|
| Navigate a hierarchy (list → detail → deeper) | `NavigationSplitView` | Owns the sidebar reveal control, collapse behaviour and the system back stack. Only the detail column gets a vertical bar |
| Two panes of equal standing, no navigation semantics ("kind of like a split view, but I don't need the expanding and collapsing") | `ArrangementView` + `.arrangementViewStyle(.split)` | Splits the given bounds; rebalances around the fold; no navigation state to fight |
| Foreground controls over background content (reader controls over prose) | `ArrangementView` + `.overlay` | Content may be partially obscured by design; read `overlayArrangementZIndex` to switch between collapsed and expanded representations |

Rules that come with `ArrangementView`:

- **Do not nest `NavigationSplitView` inside it** — arrangements provide no
  navigation infrastructure.
- **Do not put it inside `List` / `ScrollView`** — those containers own scrolling
  and the arrangement needs the full bounds.
- `.split` splits horizontally when the view is wider than it is tall, otherwise
  vertically. Lock the axis with `.split.axes(.horizontal)`; if the primary axis
  is locked and unavailable, the arrangement shows only the primary view.
- The default split is proportional. A rail that should stay narrow needs
  `.splitArrangementLayoutSize(minWidth:idealWidth:maxWidth:)` plus
  `.splitArrangementFixedLayoutSize(horizontal: true)`, or it takes half the
  width and leaves a phone-sized measure.
- **Degradation drops the secondary pane.** When the bounds cannot hold both, an
  arrangement shows only the primary. So the primary must never be the only way
  to reach the payload: a `List(selection:)` catalog whose selection renders in
  the secondary becomes a dead end — rows highlight and nothing opens. Measure
  the container (`onGeometryChange`) and fall back to a single-pane view below
  the width that fits both panes. This app shipped that bug: revealing the
  bookshelf narrowed the detail column to ~630pt and the catalogue silently
  swallowed the prose.
- `ArrangementView` and its modifiers are **iOS 27.1+**. Our deployment target is
  17.0, so every use is behind `if #available(iOS 27.1, *)` with a single-pane
  fallback.

### Reserved regions (fold and cameras)

The fold is a *division* region and an active FaceTime camera is an *occlusion*
region. Neither is in the safe area, so insets do not tell you where they are.
`reservedRegions(kind:options:layoutDirectionBehavior:)` is **iOS 27.1+** and
lives on `GeometryProxy`:

```swift
.onGeometryChange(for: [CGRect].self) { proxy in
    proxy.reservedRegions(kind: .division).map(\.frame)
} action: { frames in
    // empty when flat: the fold's region has zero width when inactive
}
```

Each `ReservedRegion` also exposes `margins` (keep-out inset) and `isActive`,
so a query that includes inactive regions can distinguish "flat" from "folded".
Availability: `@available(anyAppleOS 27.1, *)`.

- `kind: .division` — splits the inner display into usable regions when
  partially folded.
- `kind: .occlusion` — the FaceTime camera; a smaller frame inside the bounds.
- `options: .includeInactive` returns inactive regions too. A zero-width
  division region is still a useful hint: prefer even column counts when one
  exists, so folding does not leave an orphan column.

We do not query reserved regions yet: every layout we ship is a system container
or a single column, and those already handle the fold. Query them only if we add
a custom grid or a two-page reading spread.

### Measured facts on the Duo simulator

Screenshots are 3× (2853×2007 landscape). Actual numbers from
`XCUIApplication().windows.firstMatch.frame` and element frames, so they can be
asserted instead of eyeballed:

| Quantity | Value |
|---|---|
| Inner display, landscape | 951 × 669 pt (867pt usable after the 84pt vertical-bar safe area) |
| Inner display, portrait | 669 × 951 pt |
| Outer display | 466pt wide |
| Split threshold | offered width ≥ 820pt |
| Bookshelf column | 320pt in the split |
| Unit list / detail band | x 340 → 867pt |
| Unit catalog rail (reading) | ~309pt on Duo; prose pane starts at x 309. Configured as a fixed 240–360pt rail, so this is the resolved value, not the range |
| Prose pane | x 309 → 867pt; text 345 → 843pt (24pt padding), so the measure is ~500pt |
| Vertical toolbar items | x 884, width 38, y 129 / 189 / 241 / 293 / 345 (back, prev, next, chapter menu, settings) |
| Split-view sidebar reveal | system "Show Sidebar" at x 24 in the navigation bar, not ours |

If a screenshot shows a single column in landscape, confirm with the frame
numbers before changing layout code — a stale capture of a previous build looks
exactly like a broken split.

### Device poses

Duo is used closed, fully open, and partially folded.

- **Closed (outer display)** — wider and shorter than a traditional iPhone. This
  is exactly why controls move to the side.
- **Fully open (inner display)** — room for a list and its selection side by
  side, which is what the regular-width split is for. Browsing is shelf | unit
  list; reading swaps to unit catalog | prose with the shelf column collapsed,
  and the system offers its own sidebar reveal control instead of a custom back
  button.
- **Partially folded** — the centre curves around the hinge, so content near the
  middle is harder to read and tap. Keep interactive content off the fold.
  A reading app can present a two-page spread, one page per side; a sheet or
  dialog should sit on one side rather than centred on the hinge. For layouts
  that need to know where the fold is rather than just the insets, the
  *Strike a pose* talk covers reserved regions; we do not use them yet.
- **In Split View** — the app can be narrow again, and the bar follows the outer
  edge: right when the app is on the right, left when it is on the left.

Multi-window: Xcode's generated scene manifest leaves
`UIApplicationSupportsMultipleScenes` as `true`, so on iPad we technically get
multiple windows and share the app-level stores across them. We have not
designed a multi-window UX; Split View on Duo is a different case (another app
beside ours), and it does not create a second scene for us.

### Overflow

The reader toolbar carries four items. On a narrow pose, or when the keyboard
or Picture-in-Picture competes for space, iOS compresses the bar and moves items
into the overflow menu. That is the platform behaviour and we do not fight it;
we only make sure every item has both an icon and a title so it stays legible in
overflow.

### Control colours come from the system palette

Surfaces and text use semantic colours only: `systemGroupedBackground`
(#F2F2F7), `systemBackground` (#FFFFFF), `systemGray4` separators (#D1D1D6),
`.primary` / `.secondary` labels. Measured on a Duo screenshot, these are the
actual values, so the page background and rows are stock.

The accent must also be stock. A hardcoded `.tint` (this app briefly used
`.indigo`, rendering #574BE9) overrides every control, link, progress bar and
selected tab, and ignores the tint the user picked under Settings →
Accessibility. If a brand colour is ever wanted, define it in an `AccentColor`
asset so it still adapts to dark mode and Increase Contrast — do not call
`.tint` with a literal colour.

State colours are fine as accents on their own (`.orange` for a pending review,
`.red` for a failure) but should stay secondary to text. Never use colour as the
only signal.

### Test matrix

| Pose | Automated | Manual |
|---|---|---|
| Duo inner display, portrait (669×951, single column) | UI test on the Duo simulator | — |
| Duo inner display, landscape (951×669, split) | Frame assertions: window 951×669, sidebar 320pt, detail starts at x 340 | — |
| Expanded reading pose (catalog + prose) | `catalog.row.*` exists, `book.row.*` gone, `阅读设置` hittable, navigation bar identifier is the **book** title | Visual check |
| Catalog unit switch | Tapping another readable row swaps the prose chapter while the bar keeps the book title; unwritten rows have no tag, so tapping them changes nothing | Visual check |
| Sidebar revealed while reading | UI test (`testReadingPoseKeepsProseReachable`): the detail column narrows, the arrangement can no longer hold two panes, and prose must remain reachable rather than stranding the user on a selection-only catalog | — |
| Rotation landscape ↔ portrait | UI test (`testReaderSurvivesRotation`), including reader position | — |
| Reader back → unit list | UI test (`testReaderBackReturnsToUnitList`) | — |
| Compact width, unit tap → reader | UI test — guards the typed-path regression where a mismatched `NavigationStack(path:)` element type silently dropped the push | — |
| Outer display, compact width | UI test on iPhone 17 simulator | Check on device |
| Partially folded (book / table / tent) | — | Required on device |
| Split View, bar switching edges | — | Required on device |
| Keyboard up (new-book sheet) | — | Required on device |

Two behaviours that only show up in a specific pose, so they need a test in
*both* poses:

- **Pushing the reader.** Landscape exercises the split's detail stack; portrait
  exercises the compact stack. Passing in one pose says nothing about the other
   — this is exactly how the typed-path bug shipped green.
- **Back.** In landscape the control comes from the split view; in portrait it
  is the navigation stack's. Assert the destination, not the affordance.

Automated coverage runs on the iPhone Duo simulator, which Apple calls the best
way to test the full native experience, plus a compact-width iPhone for the
narrow column. Passing those is not a guarantee of App Store approval.

### Store assets

Declaring Duo support pays off on the product page: submitting dedicated
screenshots in App Store Connect shows a Duo badge, and a featuring nomination
can call out support for all poses.

### App icon

`make app-icon` (`scripts/make_app_icon.py`) is the **only** source of the icon.
It renders one 1024pt master and derives every platform asset, so the three apps
cannot drift apart:

| Output | Used by |
|---|---|
| `native-ios/Resources/Assets.xcassets/AppIcon.appiconset` | Single 1024 universal icon; `ASSETCATALOG_COMPILER_APPICON_NAME` is set in `project.yml` |
| `native-android/app/src/main/res/mipmap-*` | Legacy densities plus an adaptive icon (`mipmap-anydpi-v26`) with a `monochrome` layer for Android 13 themed icons |
| `desktop/src-tauri/icons` | Tauri bundle: PNG set, `.ico`, `.icns` |
| `out/app-icon-preview.png` | Contact sheet at 256/180/120/80/40/20pt for judging legibility (ignored by git) |

Design rules, inherited from DanQing Studio so the two apps read as one family:

- **Ground is full-bleed ink** `#3A1620`. No baked rounded corners and no
  padding: iOS and Android apply their own mask, and an inset art rect shows up
  as a badge on the home screen.
- **The three pigments carry meaning**, not decoration: vermilion `#C93756` is
  the cinnabar spine (丹), azurite `#2A7A82` is the ink on the page (石青), ochre
  `#C8AC80` is the cut edge of the paper block (赭石).
- **The mark spans ~59% of the canvas**, matching DanQing's icon, which keeps
  the pair visually the same weight when installed side by side.
- **Two rules of text, not five.** Detail below ~40pt turns to mush; the icon is
  judged on the contact sheet, not at 1024.
- Render at 4× and downsample once — that is the whole anti-aliasing strategy,
  so no vector toolkit or design tool is required to change the art.


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
- API keys remain in the app sandbox and are never written to the shared
  snapshot; API responses never return plaintext provider keys. Books and the
  non-secret model settings live in the user's own iCloud Documents container
  (see "iCloud sharing" above); the database and run history stay in the app
  sandbox.
- The native UI uses system navigation, keyboard handling, Dynamic Type
  compatible text, safe areas, and platform back gestures.
- iOS includes a privacy manifest, supports iPad multitasking, and declares
  the productivity category. No self-updater or remote UI is bundled.
- The bundled sample book gives reviewers a complete reading path without
  requiring a model key.
- Store acceptance still requires real-device accessibility, lifecycle,
  keyboard, interrupted-run, import/export, and complete workflow testing.
