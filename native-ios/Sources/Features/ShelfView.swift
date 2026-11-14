import SwiftUI

/// One push destination inside the shelf's navigator. Keeping the book ID with
/// the unit lets the reader be pushed from the shelf stack without a nested
/// NavigationStack inside the book detail.
struct ReaderRoute: Hashable {
    let bookID: String
    let unit: UnitInfo
}

struct ShelfView: View {
    @Environment(LocalCoreService.self) private var core
    @Environment(LibraryStore.self) private var library
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass

    @State private var showingNewBook = false
    @State private var isSeeding = false
    @State private var selectedBookID: String?
    @State private var compactPath = NavigationPath()
    @State private var detailPath = NavigationPath()

    private var isRegular: Bool { horizontalSizeClass == .regular }

    var body: some View {
        columns
            .sheet(isPresented: $showingNewBook) {
                NewBookView { draft in
                    try await library.createBook(draft)
                } onCreated: { book in
                    openBook(book.summary.id)
                }
            }
            .onChange(of: library.books.map(\.id)) { _, ids in
                guard isRegular else { return }
                if let selectedBookID, ids.contains(selectedBookID) { return }
                selectedBookID = ids.first
                detailPath = NavigationPath()
            }
            .onChange(of: horizontalSizeClass) { _, sizeClass in
                if sizeClass == .regular {
                    selectedBookID = selectedBookID ?? library.books.first?.id
                    detailPath = NavigationPath()
                }
            }
    }

    @ViewBuilder
    private var columns: some View {
        if isRegular {
            HStack(spacing: 0) {
                NavigationStack {
                    shelf
                }
                .frame(minWidth: 280, idealWidth: 340, maxWidth: 420)

                Divider()

                NavigationStack(path: $detailPath) {
                    detailRoot
                        .navigationDestination(for: ReaderRoute.self) { route in
                            ReaderView(bookID: route.bookID, unit: route.unit)
                        }
                }
                .frame(maxWidth: .infinity)
            }
        } else {
            NavigationStack(path: $compactPath) {
                shelf
                    .navigationDestination(for: String.self) { id in
                        BookDetailView(bookID: id)
                            // Remember the compact selection so unfolding lands on
                            // the book being read rather than the first one.
                            .task(id: id) { selectedBookID = id }
                    }
                    .navigationDestination(for: ReaderRoute.self) { route in
                        ReaderView(bookID: route.bookID, unit: route.unit)
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

    @ToolbarContentBuilder
    private var newBookToolbar: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) {
            Button { showingNewBook = true } label: {
                Label("新建书籍", systemImage: "plus")
            }
            .disabled(!core.isReady)
        }
    }

    @ViewBuilder
    private var shelf: some View {
        Group {
            if library.isLoading && library.books.isEmpty {
                ProgressView("正在打开书架…")
            } else if library.books.isEmpty {
                emptyShelf
            } else {
                List {
                    ForEach(library.books) { book in
                        if isRegular {
                            // A split-view row is selected, not pushed: the chevron
                            // has to be drawn explicitly to match platform lists.
                            Button {
                                openBook(book.id)
                            } label: {
                                BookRow(book: book, showsDisclosure: true)
                            }
                            .buttonStyle(.plain)
                            .listRowBackground(
                                book.id == selectedBookID ? Color.accentColor.opacity(0.12) : nil
                            )
                            .accessibilityHint("打开单元和正文")
                            .accessibilityIdentifier("book.row.\(book.id)")
                        } else {
                            NavigationLink(value: book.id) {
                                BookRow(book: book, showsDisclosure: false)
                            }
                            .accessibilityHint("打开单元和正文")
                            .accessibilityIdentifier("book.row.\(book.id)")
                        }
                    }
                }
                .listStyle(.plain)
                .refreshable { await library.loadBooks() }
            }
        }
        .navigationTitle("我的书架")
        .toolbar { newBookToolbar }
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
        if isRegular {
            detailPath = NavigationPath()
        } else {
            compactPath.append(id)
        }
    }
}

private struct BookRow: View {
    let book: BookSummary
    var showsDisclosure: Bool = false

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
        .overlay(alignment: .trailing) {
            if showsDisclosure {
                Image(systemName: "chevron.forward")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
                    .accessibilityHidden(true)
            }
        }
    }
}

extension BookDetail: Identifiable {
    var id: String { summary.id }
}
