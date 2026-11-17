import SwiftUI

/// The catalog + prose split used by the expanded reading pose.
///
/// This is a `NavigationSplitView` rather than the newer `ArrangementView`.
/// `ArrangementView` is iOS 27.1+, which is *newer than the SDK shipped with
/// Xcode 26* — and a type the SDK does not declare is a compile error, not a
/// runtime availability problem, so `@available` cannot make it build. CI has no
/// Xcode 27, so using it would mean the iOS build can never go green there.
///
/// `NavigationSplitView` is the system container this app already uses for
/// shelf → book navigation (see `docs/native-mobile.md`), available from iOS 16,
/// and keeps the bars system-owned. The one Duo-specific gain that is lost is
/// `ArrangementView`'s hinge-aware rebalancing; the catalog rail and prose
/// minimum below still keep the prose column readable, and the reader still
/// falls back to a single pane when the offered width cannot hold both.
struct ArrangementPane: View {
    let bookTitle: String
    let catalog: AnyView
    let reader: AnyView
    @Binding var selection: String?

    /// Catalog rail. A navigation split gives the sidebar `ideal` and lets it
    /// stay put, so this is the rail width rather than a proportional half.
    private static let sidebarIdeal: CGFloat = 300
    private static let sidebarMin: CGFloat = 240
    private static let sidebarMax: CGFloat = 360

    var body: some View {
        NavigationSplitView {
            catalog
                .navigationSplitViewColumnWidth(
                    min: Self.sidebarMin, ideal: Self.sidebarIdeal, max: Self.sidebarMax
                )
        } detail: {
            reader
        }
        .navigationTitle(bookTitle)
        .navigationBarTitleDisplayMode(.inline)
    }
}