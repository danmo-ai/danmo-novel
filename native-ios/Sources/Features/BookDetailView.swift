import SwiftUI

struct BookDetailView: View {
    @Environment(LibraryStore.self) private var library
    @Environment(LocalCoreService.self) private var core
    let bookID: String

    @State private var book: BookDetail?
    @State private var error: String?
    @State private var content: BookContent = .units
    @State private var bible: BookFile?
    @State private var isLoading = false

    enum BookContent: String, CaseIterable, Identifiable {
        case units = "单元"
        case bible = "设定"
        var id: String { rawValue }
    }

    /// Grouped by volume once per body pass instead of rebuilding it for every
    /// row, and without a VStack around the List (which collapses its height in
    /// the split column).
    private var volumeGroups: [(volume: Int, units: [UnitInfo])] {
        guard let book else { return [] }
        return Dictionary(grouping: book.units, by: \.volume)
            .map { (volume: $0.key, units: $0.value) }
            .sorted { $0.volume < $1.volume }
    }

    var body: some View {
        // This view deliberately owns no NavigationStack: the shelf's navigator
        // pushes the reader, so one stack governs book -> unit -> reader.
        Group {
            if let book {
                contentView(book)
            } else if isLoading {
                ProgressView("正在读取书稿…")
            } else {
                ContentUnavailableView(
                    "暂时打不开这本书",
                    systemImage: "book.closed",
                    description: Text(error ?? "")
                )
            }
        }
        .navigationTitle(book?.summary.title ?? "书籍")
        .navigationBarTitleDisplayMode(.large)
        .toolbar {
            ToolbarItem(placement: .principal) {
                Picker("内容", selection: $content) {
                    ForEach(BookContent.allCases) { Text($0.rawValue).tag($0) }
                }
                .pickerStyle(.segmented)
                .frame(maxWidth: 240)
            }
            ToolbarItem(placement: .primaryAction) {
                Button { Task { await load() } } label: {
                    Label("刷新", systemImage: "arrow.clockwise")
                }
                .disabled(isLoading)
            }
        }
        .task(id: bookID) {
            // Re-entering the same book (e.g. returning from the reader) must not
            // clear and refetch it: that caused a full-page flash on every back.
            guard book?.summary.id != bookID else { return }
            book = nil
            bible = nil
            error = nil
            await load()
        }
    }

    @ViewBuilder
    private func contentView(_ book: BookDetail) -> some View {
        switch content {
        case .units:
            if book.units.isEmpty {
                ContentUnavailableView(
                    "还没有单元",
                    systemImage: "text.page",
                    description: Text("在模型设置中配置提供商后，即可继续完善创意与大纲。")
                )
            } else {
                List {
                    ForEach(volumeGroups, id: \.volume) { group in
                        Section("第 \(group.volume) 卷") {
                            ForEach(group.units) { unit in
                                if unit.hasProse {
                                    NavigationLink(value: ShelfRoute.reader(
                                        ReaderRoute(bookID: book.summary.id, unit: unit)
                                    )) {
                                        UnitRow(unit: unit)
                                    }
                                    .accessibilityIdentifier("unit.row.\(unit.unitId)")
                                } else {
                                    UnitRow(unit: unit)
                                        .foregroundStyle(.secondary)
                                }
                            }
                        }
                    }
                }
                .listStyle(.insetGrouped)
            }
        case .bible:
            Group {
                if let bible {
                    ScrollView {
                        Text(bible.content)
                            .font(.body)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding()
                    }
                } else {
                    ProgressView()
                }
            }
            .task(id: book.summary.id) { await loadBible(book.summary.id) }
        }
    }

    private func loadBible(_ id: String) async {
        guard bible == nil else { return }
        bible = try? await NovelAPIClient(core: core).file(bookID: id, path: "book-bible.md")
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            book = try await library.book(id: bookID)
            error = nil
        } catch {
            book = nil
            self.error = error.localizedDescription
        }
    }
}

private struct UnitRow: View {
    let unit: UnitInfo

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack {
                Text(unit.unitId)
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                Spacer()
                Text(unit.statusTitle)
                    .font(.caption)
                    .foregroundStyle(unit.status == "reviewed" ? .green : .orange)
            }
            Text(unit.title.isEmpty ? "未命名单元" : unit.title)
                .font(.headline)
            if unit.chapterFrom > 0 {
                Text("第 \(unit.chapterFrom)–\(unit.chapterTo) 章")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 5)
    }
}
