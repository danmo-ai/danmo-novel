import SwiftUI

/// One push destination inside the shelf's navigator. Keeping the book ID with
/// the unit lets the reader be pushed from the shelf stack without a nested
/// NavigationStack inside the book detail.
struct ReaderRoute: Hashable {
    let bookID: String
    let unit: UnitInfo
}

/// A destination in the shelf's stacks (compact and split detail share it).
enum ShelfRoute: Hashable {
    case book(String)
    case unit(bookID: String, unitID: String)
    case reader(ReaderRoute)
}

struct ShelfView: View {
    @Environment(LocalCoreService.self) private var core
    @Environment(LibraryStore.self) private var library
    @Environment(WorkspaceRegistry.self) private var registry

    @State private var showingNewBook = false
    @State private var isSeeding = false
    @State private var selectedBookID: String?
    @State private var compactPath: [ShelfRoute] = []
    @State private var detailPath: [ShelfRoute] = []
    /// Live mirror of whichever path is on screen. `NavigationStack(path:)`
    /// writes its binding back to [] when the stack leaves the hierarchy, so
    /// reading `detailPath`/`compactPath` inside the *reverse* hand-off can see
    /// an already-cleared value. These refs are updated from `onChange` while
    /// the stack is still presented and are never cleared by SwiftUI.
    @State private var liveDetail: [ShelfRoute] = []
    @State private var liveCompact: [ShelfRoute] = []
    /// Collapses the bookshelf column while someone is reading, so the expanded
    /// layout becomes "unit catalog | prose" instead of "bookshelf | prose".
    @State private var columnVisibility: NavigationSplitViewVisibility = .doubleColumn
    /// Width of the space this view was actually given. Apple's Duo guidance is
    /// explicit: decide by comparing the width and height of the available
    /// space, never by size class or device. The inner display is regular width
    /// even in portrait (669pt), and splitting there squeezed the reading column
    /// to roughly twelve characters a line.
    @State private var availableWidth: CGFloat = 0

    /// A split needs a usable reading column, not just "regular" width:
    /// 300pt sidebar + 500pt detail + divider. The Duo inner display only
    /// reaches this in landscape (951pt); inner portrait (669pt) and the outer
    /// display (466pt) stay single-column.
    private static let splitMinWidth: CGFloat = 820
    private var usesSplit: Bool { availableWidth >= Self.splitMinWidth }

    var body: some View {
        content
            // Measure the offered size instead of guessing from the device or
            // size class. `onGeometryChange` reports layout passes reliably,
            // unlike a preference that can miss the first one.
            .onGeometryChange(for: CGFloat.self) { proxy in
                proxy.size.width
            } action: { width in
                availableWidth = width
            }
            .sheet(isPresented: $showingNewBook) {
                NewBookView { draft in
                    try await library.createBook(draft)
                } onCreated: { book in
                    openBook(book.summary.id)
                }
            }
            .onChange(of: library.books.map(\.id)) { _, ids in
                guard usesSplit else { return }
                if let selectedBookID, ids.contains(selectedBookID) { return }
                selectedBookID = ids.first
                detailPath = []
            }
            .onChange(of: selectedBookID) { old, new in
                // Sidebar selection replaces the detail pane; a reader pushed
                // for the previous book must not stay on screen for the new one.
                guard usesSplit, old != new else { return }
                detailPath = []
            }
            .onChange(of: usesSplit) { _, split in
                handOff(split: split)
            }
            .onChange(of: detailPath) { _, path in
                if usesSplit { liveDetail = path }
                // Reading takes over the whole width: shelf column away, catalog
                // and prose side by side.
                columnVisibility = path.isEmpty ? .doubleColumn : .detailOnly
            }
            .onChange(of: compactPath) { _, path in
                if !usesSplit { liveCompact = path }
            }
    }

    @ViewBuilder
    private var content: some View {
        if usesSplit {
            // The system split container, not a hand-built HStack: on Duo the
            // toolbar and tab bar rotate into a vertical rail for the detail
            // column only, and that only happens inside a real split view.
            NavigationSplitView(columnVisibility: $columnVisibility) {
                sidebar
            } detail: {
                detailStack
            }
        } else {
            compactStack
        }
    }

    // MARK: - Split

    private var sidebar: some View {
        Group {
            if library.isLoading && library.books.isEmpty {
                ProgressView(Copy.shelfLoading)
            } else if library.books.isEmpty {
                emptyShelf
            } else {
                // A split-view sidebar is a *selection* list, not a navigation
                // list: `List(selection:)` gives the system's row highlight and
                // the system draws no disclosure chevron here (Mail, Notes and
                // Files behave the same). `.sidebar` style is what makes taps
                // select on iOS outside of edit mode.
                List(selection: $selectedBookID) {
                    ForEach(library.books) { book in
                        BookRow(book: book)
                            .tag(book.id)
                            .accessibilityHint(Copy.shelfOpenBookHint)
                            .accessibilityIdentifier("book.row.\(book.id)")
                    }
                }
                .listStyle(.sidebar)
                .refreshable { await library.loadBooks() }
            }
        }
        .navigationTitle(Copy.shelfTitle)
        .toolbar { newBookToolbar }
    }

    private var detailStack: some View {
        NavigationStack(path: $detailPath) {
            detailRoot
                .navigationDestination(for: ShelfRoute.self) { route in
                    destination(route, path: $detailPath)
                }
        }
    }

    /// One destination builder for both stacks: the typed path is shared, so a
    /// route pushed in one pose must resolve identically in the other.
    @ViewBuilder
    private func destination(_ route: ShelfRoute, path: Binding<[ShelfRoute]>) -> some View {
        switch route {
        case .book(let id):
            BookDetailView(
                bookID: id,
                push: { path.wrappedValue.append($0) },
                popToBook: { if !path.wrappedValue.isEmpty { path.wrappedValue.removeLast() } }
            )
        case .unit(let bookID, let unitID):
            let workspace = registry.workspace(bookID: bookID)
            UnitWorkView(
                workspace: workspace,
                unitID: unitID,
                openTasks: {
                    // Confirmation lives on the book's task page: pop back to
                    // the book and select that tab.
                    workspace.tab = .tasks
                    if !path.wrappedValue.isEmpty { path.wrappedValue.removeLast() }
                }
            )
        case .reader(let route):
            // Expanded width gets the catalog + prose split; the compact stack
            // pushes a single-page reader instead. The split is a
            // NavigationSplitView (iOS 16+), so no availability gate is needed.
            if usesSplit {
                ReaderSplitView(bookID: route.bookID, unit: route.unit)
            } else {
                ReaderView(bookID: route.bookID, unit: route.unit)
            }
        }
    }

    @ViewBuilder
    private var detailRoot: some View {
        if let selectedBookID {
            BookDetailView(
                bookID: selectedBookID,
                push: { detailPath.append($0) },
                popToBook: { detailPath = [] }
            )
        } else {
            ContentUnavailableView(
                Copy.shelfPickBookTitle,
                systemImage: "books.vertical",
                description: Text(Copy.shelfPickBookBody)
            )
        }
    }

    // MARK: - Compact

    private var compactStack: some View {
        NavigationStack(path: $compactPath) {
            Group {
                if library.isLoading && library.books.isEmpty {
                    ProgressView(Copy.shelfLoading)
                } else if library.books.isEmpty {
                    emptyShelf
                } else {
                    // Compact width pushes one screen, so a NavigationLink lets
                    // the system draw and manage the standard chevron and back
                    // stack.
                    List {
                        ForEach(library.books) { book in
                            NavigationLink(value: ShelfRoute.book(book.id)) {
                                BookRow(book: book)
                            }
                            .accessibilityHint(Copy.shelfOpenBookHint)
                            .accessibilityIdentifier("book.row.\(book.id)")
                        }
                    }
                    .listStyle(.plain)
                    .refreshable { await library.loadBooks() }
                }
            }
            .navigationTitle(Copy.shelfTitle)
            .toolbar { newBookToolbar }
            .navigationDestination(for: ShelfRoute.self) { route in
                destination(route, path: $compactPath)
            }
        }
    }

    // MARK: - Shared

    @ToolbarContentBuilder
    private var newBookToolbar: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) {
            Button { showingNewBook = true } label: {
                Label(Copy.shelfNewBook, systemImage: "plus")
            }
            .disabled(!core.isReady)
        }
    }

    private var emptyShelf: some View {
        ContentUnavailableView {
            Label(Copy.shelfEmptyTitle, systemImage: "books.vertical")
        } description: {
            Text(Copy.shelfEmptyBody)
        } actions: {
            Button {
                Task { await seedDemo() }
            } label: {
                if isSeeding { ProgressView() } else { Text(Copy.shelfOpenDemo) }
            }
            .buttonStyle(.borderedProminent)
            .disabled(isSeeding || !core.isReady)
            Button(Copy.shelfNewBook) { showingNewBook = true }
                .disabled(!core.isReady)
        }
    }

    /// Carries the open book and reading position across the split boundary so
    /// rotating the Duo never drops the reader — the compact stack and the
    /// detail stack are different containers and cannot share a path.
    ///
    /// Reads the live mirrors, not the bindings: when a stack leaves the
    /// hierarchy SwiftUI writes its `path` binding back to [], so the reverse
    /// hand-off would otherwise see an empty source and drop the reader.
    private func handOff(split: Bool) {
        if split {
            let source = liveCompact
            if case .reader(let route)? = source.last {
                selectedBookID = route.bookID
                detailPath = [.unit(bookID: route.bookID, unitID: route.unit.unitId), .reader(route)]
            } else if case .unit(let bookID, let unitID)? = source.last {
                selectedBookID = bookID
                detailPath = [.unit(bookID: bookID, unitID: unitID)]
            } else if case .book(let id)? = source.last {
                selectedBookID = id
                detailPath = []
            } else if selectedBookID == nil {
                selectedBookID = library.books.first?.id
                detailPath = []
            }
            liveDetail = detailPath
            compactPath = []
            liveCompact = []
            columnVisibility = detailPath.isEmpty ? .doubleColumn : .detailOnly
        } else {
            // Carry the *whole* detail path, not just its last route: a reader
            // pushed on top of unit work must keep its back stack, or rotating
            // back strands the reader with no way to the unit list.
            var path: [ShelfRoute] = []
            if let bookID = selectedBookID {
                path.append(.book(bookID))
                path.append(contentsOf: liveDetail)
            }
            compactPath = path
            liveCompact = path
            detailPath = []
            liveDetail = []
            columnVisibility = .doubleColumn
        }
    }

    private func seedDemo() async {
        isSeeding = true
        defer { isSeeding = false }
        do {
            let book = try await library.createDemoBook()
            openBook(book.summary.id)
        } catch { library.error = error.localizedDescription }
    }

    private func openBook(_ id: String) {
        selectedBookID = id
        if usesSplit {
            detailPath = []
        } else {
            compactPath = [.book(id)]
        }
    }
}

private struct BookRow: View {
    let book: BookSummary

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline) {
                Text(book.title)
                    .font(.headline)
                    .lineLimit(2)
                Spacer()
                if book.openCandidates > 0 {
                    Label(Copy.candidates(book.openCandidates), systemImage: "checkmark.circle")
                        .font(.caption)
                        .foregroundStyle(.orange)
                }
            }
            HStack {
                Text(book.genre.isEmpty ? Copy.genreUnknown : book.genre)
                Text("·")
                Text(Copy.unitCount(book.unitCount))
                Spacer()
                Text(Copy.reviewedCount(book.reviewedCount))
            }
            .font(.subheadline)
            .foregroundStyle(.secondary)
            if book.unitCount > 0 {
                ProgressView(value: book.progress)
                    .accessibilityLabel(Copy.reviewProgressLabel)
                    .accessibilityValue(Copy.reviewProgress(reviewed: book.reviewedCount, units: book.unitCount))
            }
        }
        .padding(.vertical, 8)
    }
}

extension BookDetail: Identifiable {
    var id: String { summary.id }
}
