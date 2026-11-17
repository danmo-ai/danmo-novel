import SwiftUI

/// The expanded-width reading layout: unit catalog on the left, prose on the
/// right, bookshelf column collapsed.
///
/// This is the Duo pattern Apple describes for a main-detail relationship: the
/// system split arrangement keeps both panes visible and rebalances them when
/// the device is partially folded. It is *not* a hand-built `HStack` — the
/// arrangement owns the split so reserved regions (hinge, camera) stay clear.
///
/// The arrangement degrades by *dropping* the secondary pane when the bounds
/// cannot hold both, which here would leave a catalog whose rows only change
/// selection — a dead end with no way into the prose. So this view measures
/// itself and falls back to the single-pane reader rather than trust the
/// arrangement to always fit two panes.
///
/// Compact width never reaches this view; it pushes a plain `ReaderView`.
struct ReaderSplitView: View {
    @Environment(LibraryStore.self) private var library
    let bookID: String
    let unit: UnitInfo

    @State private var book: BookDetail?
    @State private var currentUnitID: String?
    @State private var error: String?
    /// Width actually offered to this view. It is the detail column's width, not
    /// the window's, so it shrinks when the bookshelf sidebar is revealed.
    @State private var width: CGFloat = 0

    /// Catalog rail (240) + a readable prose column (~420) + separator slack.
    /// Below this the arrangement cannot show both panes.
    private static let twoPaneMinWidth: CGFloat = 700
    /// Optimistic until the first measurement: showing both panes for one layout
    /// pass is harmless, whereas starting single-pane would discard and refetch
    /// the chapter on every entry.
    private var showsTwoPanes: Bool { width == 0 || width >= Self.twoPaneMinWidth }

    init(bookID: String, unit: UnitInfo) {
        self.bookID = bookID
        self.unit = unit
        _currentUnitID = State(initialValue: unit.unitId)
    }

    private var selectedUnitID: String { currentUnitID ?? unit.unitId }

    private var currentUnit: UnitInfo {
        book?.units.first { $0.unitId == selectedUnitID } ?? unit
    }

    /// Every unit, grouped by volume — not just the ones with prose. Hiding the
    /// rest made the catalogue look as though the book had lost its units; they
    /// are part of the book's shape even when the prose is not written yet.
    private var volumeGroups: [(volume: Int, units: [UnitInfo])] {
        guard let book else { return [] }
        return Dictionary(grouping: book.units, by: \.volume)
            .map { (volume: $0.key, units: $0.value.sorted { $0.unitId < $1.unitId }) }
            .sorted { $0.volume < $1.volume }
    }

    /// The navigation bar spans both panes and sits above the catalog, which
    /// lists this book's units, so it must name the book. The open unit is
    /// identified by the highlighted catalog row and by the prose header.
    /// Reading straight from the store avoids a title flash while `book` loads.
    private var bookTitle: String {
        library.books.first { $0.id == bookID }?.title
            ?? book?.summary.title
            ?? ""
    }

    var body: some View {
        Group {
            if showsTwoPanes {
                twoPane
            } else {
                // Same reading surface as compact width: the prose is always
                // reachable, and the unit list is one back step away.
                ReaderView(bookID: bookID, unit: currentUnit, showsNavigationTitle: false)
            }
        }
        .onGeometryChange(for: CGFloat.self) { proxy in
            proxy.size.width
        } action: { offered in
            width = offered
        }
        .navigationTitle(bookTitle)
        .navigationBarTitleDisplayMode(.inline)
        .task(id: bookID) {
            guard book?.summary.id != bookID else { return }
            do { book = try await library.book(id: bookID) }
            catch { self.error = error.localizedDescription }
        }
    }

    private var twoPane: some View {
        // System split container (iOS 16+), not the iOS 27.1-only
        // `ArrangementView`: the SDK bundled with Xcode 26 has no such type, so
        // that spelling cannot compile on CI at all. See `ArrangementPane`.
        ArrangementPane(
            bookTitle: bookTitle,
            catalog: AnyView(catalog),
            reader: AnyView(readerPane),
            selection: $currentUnitID
        )
    }

    /// The prose column of the two-pane reading pose.
    private var readerPane: some View {
        ReaderView(bookID: bookID, unit: currentUnit, showsNavigationTitle: false)
            // A new unit must rebuild reader state (chapters, paragraphs,
            // selected chapter) instead of mutating the previous unit's.
            .id(currentUnit.unitId)
    }

    @ViewBuilder
    private var catalog: some View {
        if book != nil {
            List(selection: $currentUnitID) {
                ForEach(volumeGroups, id: \.volume) { group in
                    Section(Copy.volumeTitle(group.volume)) {
                        ForEach(group.units) { item in
                            if item.hasProse {
                                CatalogRow(unit: item, isCurrent: item.unitId == selectedUnitID)
                                    .tag(item.unitId)
                                    .accessibilityIdentifier("catalog.row.\(item.unitId)")
                            } else {
                                // A selection list only makes a row selectable
                                // when it has a tag, so leaving the tag off is
                                // what keeps an unwritten unit from loading a
                                // missing prose file.
                                CatalogRow(unit: item, isCurrent: false)
                                    .foregroundStyle(.secondary)
                                    .accessibilityHint(Copy.readerUnwrittenHint)
                                    .accessibilityIdentifier("catalog.row.\(item.unitId)")
                            }
                        }
                    }
                }
            }
            .listStyle(.sidebar)
        } else if let error {
            ContentUnavailableView(
                Copy.bookUnavailable,
                systemImage: "book.closed",
                description: Text(error)
            )
        } else {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }
}

private struct CatalogRow: View {
    let unit: UnitInfo
    let isCurrent: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(unit.title.isEmpty ? Copy.unitUnnamed : unit.title)
                .font(.subheadline.weight(isCurrent ? .semibold : .regular))
                .lineLimit(2)
            HStack(spacing: 6) {
                Text(unit.unitId)
                    .font(.caption2.monospaced())
                Text(unit.statusTitle)
                    .font(.caption2)
                    .foregroundStyle(unit.hasProse
                        ? (unit.isFinished ? Color.green : Color.orange)
                        : Color.secondary)
            }
            .foregroundStyle(.secondary)
        }
        .padding(.vertical, 2)
    }
}
