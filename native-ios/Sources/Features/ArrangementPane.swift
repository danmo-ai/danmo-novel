import SwiftUI

/// The catalog + prose split used by the expanded reading pose.
///
/// `ArrangementView` and its `splitArrangement*` modifiers are iOS 27.1+, which
/// is *newer than the SDK bundled with Xcode 26*. `@available` is a runtime gate:
/// it silences misuse detection but cannot compile a type that the SDK does not
/// declare, so a build on Xcode 26 fails with "cannot find 'ArrangementView' in
/// scope". A compile-time SDK check is the only thing that works, and
/// `compiler(>=6.2)` is the guard that tracks the toolchain rather than the
/// deployment target — the deployment target stays 17.0 either way.
///
/// Both branches must expose the same `init`, because `ReaderSplitView` calls
/// this unconditionally; the older toolchain simply renders no arrangement.
struct ArrangementPane: View {
    let catalog: AnyView
    let reader: AnyView

    var body: some View {
        #if compiler(>=6.4)
        if #available(iOS 27.1, *) {
            ArrangementSplit(catalog: catalog, reader: reader)
        } else {
            reader
        }
        #else
        // The SDK shipped with Xcode 26 (Swift 6.2) does not declare
        // `ArrangementView` at all, so referencing it — even behind
        // `#available` — fails to compile. Older toolchains get the reader.
        reader
        #endif
    }
}

/// The `ArrangementView` call itself, isolated so the type reference only
/// exists inside an `@available` scope.
///
/// Availability is a *runtime* gate, so on a toolchain whose SDK has no
/// `ArrangementView` (Xcode 26 ships iOS 26.5) this file would still fail to
/// compile. `ReaderSplitView` is therefore guarded by `#if compiler(>=6.2)` at
/// its use site — see `ShelfView`, which falls back to the single-pane reader
/// and never instantiates this type on an older SDK.
@available(iOS 27.1, *)
private struct ArrangementSplit: View {
    let catalog: AnyView
    let reader: AnyView

    var body: some View {
        ArrangementView {
            // A catalog is a fixed rail, not a proportional half: without this
            // the split hands it half the width and the prose column drops to a
            // phone-sized measure.
            catalog
                .splitArrangementLayoutSize(minWidth: 240, idealWidth: 300, maxWidth: 360)
                .splitArrangementFixedLayoutSize(horizontal: true)
        } secondary: {
            // Declare the prose column's minimum too, so the arrangement prefers
            // keeping both panes when they do fit.
            reader
                .splitArrangementLayoutSize(minWidth: 420)
        }
        .arrangementViewStyle(.split.axes(.horizontal))
    }
}