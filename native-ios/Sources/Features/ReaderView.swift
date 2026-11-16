import SwiftUI

struct ReaderView: View {
    @Environment(LocalCoreService.self) private var core
    let bookID: String
    let unit: UnitInfo
    /// Standalone (compact-width push) owns the navigation title, because it is
    /// the only content on screen. As the secondary pane of the reading
    /// arrangement it must not: the two panes share one navigation bar, and that
    /// bar sits above the unit catalog, so its title has to name the book, not
    /// whichever unit happens to be open.
    var showsNavigationTitle = true

    @State private var chapters: [Chapter] = []
    @State private var selectedChapter: Int?
    /// Paragraphs are split once per chapter. Doing it inside `body` re-split
    /// the whole chapter on every scroll frame, which is the main scroll jank.
    @State private var paragraphs: [String] = []
    @State private var error: String?
    @State private var fontSize = 19.0
    @State private var showingSettings = false

    private var currentChapter: Chapter? {
        chapters.first { $0.number == selectedChapter } ?? chapters.first
    }

    private var content: some View {
        Group {
            if let chapter = currentChapter {
                ScrollView {
                    VStack(alignment: .leading, spacing: 28) {
                        Text(Copy.chapterHeading(chapter.number))
                            .font(.headline)
                            .foregroundStyle(.secondary)
                            .accessibilityIdentifier("reader.chapter")
                        if !chapter.title.isEmpty {
                            Text(chapter.title)
                                .font(.title.bold())
                        }
                        VStack(alignment: .leading, spacing: 22) {
                            ForEach(paragraphs.indices, id: \.self) { index in
                                Text(paragraphs[index])
                                    .font(.system(size: fontSize, design: .serif))
                                    .lineSpacing(fontSize * 0.55)
                                    .frame(maxWidth: .infinity, alignment: .leading)
                            }
                        }
                        // One selection scope for the whole chapter instead of a
                        // text-selection layer per paragraph.
                        .textSelection(.enabled)
                    }
                    .frame(maxWidth: 680)
                    .padding(.horizontal, 24)
                    .padding(.vertical, 28)
                    .frame(maxWidth: .infinity)
                }
                .scrollIndicators(.automatic)
                .background(Color(.systemBackground))
                .accessibilityIdentifier("reader.scroll")
                // Switching chapters replaces the scroll content, but a
                // `ScrollView` keeps its old point offset across that swap, so
                // the reader landed mid-chapter (or mid-air when the next
                // chapter was shorter) instead of on the chapter's first line.
                // A per-chapter identity gives every chapter a fresh scroll
                // origin — the same "jump to chapter start" the Android reader
                // gets from `scrollTo(0)`.
                .id(chapter.number)
            } else if let error {
                ContentUnavailableView(
                    Copy.readerFailed,
                    systemImage: "exclamationmark.triangle",
                    description: Text(error)
                )
            } else {
                ProgressView(Copy.readerOpening)
            }
        }
    }

    var body: some View {
        Group {
            if showsNavigationTitle {
                content
                    .navigationTitle(unit.title)
                    .navigationBarTitleDisplayMode(.inline)
            } else {
                content
            }
        }
        .toolbar {
            // Chapter paging is navigation, so it belongs in the system bar
            // rather than a custom bottom bar: on iPhone Duo the bar lays out
            // vertically beside the content, which preserves vertical space for
            // reading. Symbol + title items are what the system moves to the
            // vertical axis; a text-only item would stay horizontal and split
            // the bar into two axes.
            ToolbarItemGroup(placement: .primaryAction) {
                if let chapter = currentChapter, chapters.count > 1 {
                    Button {
                        selectChapter(offset: -1, from: chapter.number)
                    } label: {
                        Label(Copy.previousChapter, systemImage: "chevron.up")
                    }
                    .disabled(chapter.number == chapters.first?.number)

                    Button {
                        selectChapter(offset: 1, from: chapter.number)
                    } label: {
                        Label(Copy.nextChapter, systemImage: "chevron.down")
                    }
                    .disabled(chapter.number == chapters.last?.number)

                    Menu {
                        ForEach(chapters) { item in
                            Button(Copy.chapterMenuLabel(number: item.number, title: item.title)) {
                                setChapter(item.number)
                            }
                        }
                    } label: {
                        Label(
                            Copy.chapterCountLabel(number: chapter.number, count: chapters.count),
                            systemImage: "list.number"
                        )
                    }
                }
                Button { showingSettings = true } label: {
                    Label(Copy.readerSettings, systemImage: "textformat.size")
                }
            }
        }
        .sheet(isPresented: $showingSettings) {
            NavigationStack {
                Form {
                    Section(Copy.readerFontSection) {
                        HStack {
                            Text(Copy.readerFontSmall).font(.caption)
                            Slider(value: $fontSize, in: 16...28, step: 1)
                            Text(Copy.readerFontLarge).font(.title3)
                        }
                        Text(Copy.readerFontSize(Int(fontSize)))
                            .foregroundStyle(.secondary)
                    }
                }
                .navigationTitle(Copy.readerSettings)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { Button(Copy.readerSettingsDone) { showingSettings = false } }
            }
            .presentationDetents([.height(230)])
        }
        .task(id: "\(bookID)-\(unit.id)") {
            chapters = []
            paragraphs = []
            selectedChapter = nil
            error = nil
            await load()
        }
    }

    private func load() async {
        do {
            let file = try await NovelAPIClient(core: core).file(bookID: bookID, path: unit.prosePath)
            let parsed = Chapter.parse(file.content)
            chapters = parsed
            if let first = parsed.first {
                setChapter(first.number)
            } else {
                paragraphs = []
                selectedChapter = nil
                error = Copy.readerUnrecognized
            }
        } catch {
            self.error = error.localizedDescription
        }
    }

    /// Single place that changes chapters, so paragraphs and the selection can
    /// never disagree for a frame.
    private func setChapter(_ number: Int) {
        selectedChapter = number
        guard let chapter = chapters.first(where: { $0.number == number }) else {
            paragraphs = []
            return
        }
        paragraphs = chapter.body
            .components(separatedBy: .newlines)
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
    }

    private func selectChapter(offset: Int, from number: Int) {
        guard let index = chapters.firstIndex(where: { $0.number == number }) else { return }
        let next = index + offset
        if chapters.indices.contains(next) { setChapter(chapters[next].number) }
    }
}
