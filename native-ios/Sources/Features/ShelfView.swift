import SwiftUI

/// One push destination inside the shelf's navigator. Keeping the book ID with
/// the unit lets the reader be pushed from the shelf stack without a nested
/// NavigationStack inside the book detail.
struct ReaderRoute: Hashable {
    let bookID: String
    let unit: UnitInfo
}

/// A destination in the single-column (compact) shelf stack.
enum ShelfRoute: Hashable {
    case book(String)
    case reader(ReaderRoute)
}

struct ShelfView: View {
    @Environment(LocalCoreService.self) private var core
    @Environment(LibraryStore.self) private var library

    @State private var showingNewBook = false
    @State private var isSeeding = false
    @State private var selectedBookID: String?
    @State private var compactPath: [ShelfRoute] = []
    @State private var detailPath: [ShelfRoute] = []
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
                // Reading takes over the whole width: shelf column away, catalog
                // and prose side by side.
                columnVisibility = path.isEmpty ? .doubleColumn : .detailOnly
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
                ProgressView("正在打开书架…")
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
                            .accessibilityHint("打开单元和正文")
                            .accessibilityIdentifier("book.row.\(book.id)")
                    }
                }
                .listStyle(.sidebar)
                .refreshable { await library.loadBooks() }
            }
        }
        .navigationTitle("我的书架")
        .toolbar { newBookToolbar }
    }

    private var detailStack: some View {
        NavigationStack(path: $detailPath) {
            detailRoot
                .navigationDestination(for: ShelfRoute.self) { route in
                    switch route {
                    case .book(let id):
                        BookDetailView(bookID: id)
                    case .reader(let route):
                        // Expanded width gets the catalog + prose arrangement;
                        // the compact stack pushes a single-page reader instead.
                        if #available(iOS 27.1, *) {
                            ReaderSplitView(bookID: route.bookID, unit: route.unit)
                        } else {
                            ReaderView(bookID: route.bookID, unit: route.unit)
                        }
                    }
                }
        }
    }

    @ViewBuilder
    private var detailRoot: some View {
        if let selectedBookID {
            BookDetailView(bookID: selectedBookID)
        } else {
            ContentUnavailableView(
                "选择一本书",
                systemImage: "books.vertical",
                description: Text("在左侧选择书稿，查看单元并继续阅读。")
            )
        }
    }

    // MARK: - Compact

    private var compactStack: some View {
        NavigationStack(path: $compactPath) {
            Group {
                if library.isLoading && library.books.isEmpty {
                    ProgressView("正在打开书架…")
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
                            .accessibilityHint("打开单元和正文")
                            .accessibilityIdentifier("book.row.\(book.id)")
                        }
                    }
                    .listStyle(.plain)
                    .refreshable { await library.loadBooks() }
                }
            }
            .navigationTitle("我的书架")
            .toolbar { newBookToolbar }
            .navigationDestination(for: ShelfRoute.self) { route in
                switch route {
                case .book(let id):
                    BookDetailView(bookID: id)
                case .reader(let route):
                    // Compact width is one screen: a plain single-page reader.
                    ReaderView(bookID: route.bookID, unit: route.unit)
                }
            }
        }
    }

    // MARK: - Shared

    @ToolbarContentBuilder
    private var newBookToolbar: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) {
            Button { showingNewBook = true } label: {
                Label("新建书籍", systemImage: "plus")
            }
            .disabled(!core.isReady)
        }
    }

    private var emptyShelf: some View {
        ContentUnavailableView {
            Label("开始你的第一本书", systemImage: "books.vertical")
        } description: {
            Text("书稿保存在本机。你可以先读一本完整示例，或从自己的灵感开始。")
        } actions: {
            Button {
                Task { await seedDemo() }
            } label: {
                if isSeeding { ProgressView() } else { Text("看看示例书") }
            }
            .buttonStyle(.borderedProminent)
            .disabled(isSeeding || !core.isReady)
            Button("新建一本书") { showingNewBook = true }
                .disabled(!core.isReady)
        }
    }

    /// Carries the open book and reading position across the split boundary so
    /// rotating the Duo never drops the reader — the compact stack and the
    /// detail stack are different containers and cannot share a path.
    private func handOff(split: Bool) {
        if split {
            if case .reader(let route)? = compactPath.last {
                selectedBookID = route.bookID
                detailPath = [.reader(route)]
            } else if case .book(let id)? = compactPath.last {
                selectedBookID = id
                detailPath = []
            } else if selectedBookID == nil {
                selectedBookID = library.books.first?.id
                detailPath = []
            }
            compactPath = []
            columnVisibility = detailPath.isEmpty ? .doubleColumn : .detailOnly
        } else {
            var path: [ShelfRoute] = []
            if let bookID = selectedBookID {
                path.append(.book(bookID))
                if let last = detailPath.last { path.append(last) }
            }
            compactPath = path
            detailPath = []
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
                    Label("待审", systemImage: "checkmark.circle")
                        .font(.caption)
                        .foregroundStyle(.orange)
                }
            }
            HStack {
                Text(book.genre.isEmpty ? "未分类" : book.genre)
                Text("·")
                Text("\(book.unitCount) 个单元")
                Spacer()
                Text("\(book.reviewedCount) 篇定稿")
            }
            .font(.subheadline)
            .foregroundStyle(.secondary)
            if book.unitCount > 0 {
                ProgressView(value: book.progress)
                    .accessibilityLabel("定稿进度")
                    .accessibilityValue("\(book.reviewedCount)/\(book.unitCount)")
            }
        }
        .padding(.vertical, 8)
    }
}

extension BookDetail: Identifiable {
    var id: String { summary.id }
}
