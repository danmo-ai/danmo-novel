import SwiftUI

struct ReaderView: View {
    @Environment(LocalCoreService.self) private var core
    let bookID: String
    let unit: UnitInfo

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

    var body: some View {
        Group {
            if let chapter = currentChapter {
                ScrollView {
                    VStack(alignment: .leading, spacing: 28) {
                        Text("第 \(chapter.number) 章")
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
                .safeAreaInset(edge: .bottom) {
                    if chapters.count > 1 {
                        chapterBar(chapter)
                    }
                }
            } else if let error {
                ContentUnavailableView(
                    "正文读取失败",
                    systemImage: "exclamationmark.triangle",
                    description: Text(error)
                )
            } else {
                ProgressView("正在打开正文…")
            }
        }
        .navigationTitle(unit.title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            // No custom back button: iOS 27.1+ may render the bar vertically on
            // Duo, and the system already provides back to the unit list. A
            // second back affordance would duplicate it.
            ToolbarItem(placement: .primaryAction) {
                Button { showingSettings = true } label: {
                    Label("阅读设置", systemImage: "textformat.size")
                }
            }
        }
        .sheet(isPresented: $showingSettings) {
            NavigationStack {
                Form {
                    Section("正文字号") {
                        HStack {
                            Text("小").font(.caption)
                            Slider(value: $fontSize, in: 16...28, step: 1)
                            Text("大").font(.title3)
                        }
                        Text("字号 \(Int(fontSize))")
                            .foregroundStyle(.secondary)
                    }
                }
                .navigationTitle("阅读设置")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { Button("完成") { showingSettings = false } }
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

    private func chapterBar(_ chapter: Chapter) -> some View {
        HStack {
            Button {
                selectChapter(offset: -1, from: chapter.number)
            } label: {
                Label("上一章", systemImage: "chevron.left")
            }
            .disabled(chapter.number == chapters.first?.number)

            Spacer()

            Menu {
                ForEach(chapters) { item in
                    Button("第 \(item.number) 章 · \(item.title)") { setChapter(item.number) }
                }
            } label: {
                Text("\(chapter.number) / \(chapters.count) 章")
                    .font(.subheadline)
            }

            Spacer()

            Button {
                selectChapter(offset: 1, from: chapter.number)
            } label: {
                Label("下一章", systemImage: "chevron.right")
            }
            .disabled(chapter.number == chapters.last?.number)
        }
        .padding()
        .background(.bar)
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
                error = "这份正文还没有可识别的章节。"
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
