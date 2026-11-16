import SwiftUI

/// One unit's production surface: flow actions, contract or prose, manual
/// editing. The four production steps are one system toolbar Menu — symbol +
/// title, so the platform moves it to the vertical rail on iPhone Duo and
/// collapses whatever does not fit into the menu itself. The content is a
/// system List that already renders the prose; there is no separate reading
/// action here.
struct UnitWorkView: View {
    let workspace: BookWorkspaceStore
    let unitID: String
    /// Jumps to the task page, where every result is confirmed.
    var openTasks: () -> Void = {}

    @State private var proseRaw = ""
    @State private var outlineRaw = ""
    @State private var chapters: [Chapter] = []
    @State private var loading = false
    @State private var loadError: String?
    /// Paragraphs split once per chapter, not per layout pass.
    @State private var chapterParagraphs: [Int: [String]] = [:]
    @State private var contractExpanded = false
    @State private var editingOutline = false
    @State private var outlineDraft = ""
    @State private var editingChapter: Int?
    @State private var chapterDraft = ""
    @State private var copiedChapter: Int?

    private var unit: UnitInfo? { workspace.book?.unit(unitID) }
    private var readOnly: Bool { workspace.book?.summary.readOnly ?? false }

    /// The listing projection strips outlineRaw; inject the fetched YAML so
    /// the contract view can parse it (both the inline and outline poses).
    private var resolvedUnit: UnitInfo? {
        guard let unit else { return nil }
        guard !outlineRaw.isEmpty else { return unit }
        return UnitInfo(
            unitId: unit.unitId, volume: unit.volume, index: unit.index, status: unit.status,
            title: unit.title, chapterFrom: unit.chapterFrom, chapterTo: unit.chapterTo,
            hasProse: unit.hasProse, outlinePath: unit.outlinePath, prosePath: unit.prosePath,
            chapterTarget: unit.chapterTarget, outlineRaw: outlineRaw, proseRaw: unit.proseRaw
        )
    }

    /// The step matching the unit's status — UnitFlowActions.vue.
    private func primaryKind(_ unit: UnitInfo) -> String {
        switch unit.status {
        case "proposed": "outline"
        case "accepted": "write"
        case "drafted": "review"
        case "reviewed", "finalized": "finalize"
        default: ""
        }
    }

    private func flowTitle(_ kind: String, unit: UnitInfo) -> String {
        switch kind {
        case "outline": unit.status == "proposed" ? Copy.actionOutline : Copy.actionOutlineRewrite
        case "write": unit.hasProse ? Copy.actionWriteRewrite : Copy.actionWrite
        case "review": unit.isFinished ? Copy.actionReviewAgain : Copy.actionReview
        case "finalize": unit.status == "finalized" ? Copy.actionFinalizeAgain : Copy.actionFinalize
        default: kind
        }
    }

    private func flowSymbol(_ kind: String) -> String {
        switch kind {
        case "outline": "list.bullet.rectangle.portrait"
        case "write": "square.and.pencil"
        case "review": "checkmark.seal"
        case "finalize": "checkmark.circle"
        default: "circle"
        }
    }

    private func flowEnabled(_ kind: String, unit: UnitInfo) -> Bool {
        switch kind {
        case "outline": true
        case "write": ["accepted", "drafted", "reviewed", "finalized"].contains(unit.status)
        case "review": unit.hasProse
        case "finalize": unit.hasProse && unit.status != "proposed"
        default: false
        }
    }

    var body: some View {
        Group {
            if let unit {
                content(unit)
            } else {
                ContentUnavailableView(Copy.bookUnavailable, systemImage: "text.page")
            }
        }
        .navigationTitle(unit?.title.isEmpty == false ? unit!.title : Copy.unitUnnamed)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { if let unit { unitToolbar(unit) } }
        .workspaceErrorAlert(workspace)
        .task(id: unitID) {
            // The production surface owns the workspace selection, so a unit
            // opened through a system NavigationLink keeps primary/revise
            // targeting in sync without the row doing it manually.
            workspace.selectedUnitID = unitID
            await reloadContent()
        }
        .onChange(of: workspace.book?.revision) { _, _ in Task { await reloadContent() } }
        .sheet(isPresented: $editingOutline) {
            OutlineEditorSheet(
                title: Copy.unitEditOutline,
                draft: $outlineDraft,
                original: outlineRaw,
                saving: workspace.saving,
                onSave: {
                    guard let unit else { return false }
                    return await workspace.saveFile(path: unit.outlinePath ?? "", content: outlineDraft)
                },
                onClose: { editingOutline = false }
            )
        }
        .sheet(isPresented: Binding(get: { editingChapter != nil }, set: { if !$0 { editingChapter = nil } })) {
            if let number = editingChapter {
                ChapterEditorSheet(
                    chapter: number,
                    draft: $chapterDraft,
                    original: chapterOriginal(number),
                    saving: workspace.saving,
                    onSave: {
                        guard let updated = Chapter.replaceChapter(number, in: proseRaw, with: chapterDraft) else {
                            workspace.error = Copy.readerUnrecognized
                            return false
                        }
                        return await workspace.saveFile(path: unit?.prosePath ?? "", content: updated)
                    },
                    onClose: { editingChapter = nil }
                )
            }
        }
    }

    // MARK: Toolbar (system bar: one group for the four production steps)

    /// The four steps as a single system Menu. One symbol + title item is what
    /// the platform moves to the vertical rail on Duo; the steps live inside
    /// the menu, so a bar that cannot fit everything collapses into it instead
    /// of spilling across two axes. The step matching the unit's status takes
    /// the group's symbol and a checkmark inside the menu — the menu's label
    /// cannot carry a prominent style, so the emphasis moves to the icon.
    @ToolbarContentBuilder
    private func unitToolbar(_ unit: UnitInfo) -> some ToolbarContent {
        ToolbarItem(placement: .primaryAction) {
            Menu {
                flowItem("outline", unit: unit)
                flowItem("write", unit: unit)
                flowItem("review", unit: unit)
                flowItem("finalize", unit: unit)
            } label: {
                Label(Copy.unitFlowActions, systemImage: flowSymbol(primaryKind(unit)))
            }
            .accessibilityIdentifier("unit.flow.menu")
        }
    }

    private func flowItem(_ kind: String, unit: UnitInfo) -> some View {
        Button {
            workspace.runUnitAction(kind, unit: unit)
        } label: {
            if primaryKind(unit) == kind {
                Label(flowTitle(kind, unit: unit), systemImage: "checkmark")
            } else {
                Label(flowTitle(kind, unit: unit), systemImage: flowSymbol(kind))
            }
        }
        .disabled(workspace.busy || readOnly || !flowEnabled(kind, unit: unit))
        .accessibilityIdentifier("unit.action.\(kind)")
    }

    // MARK: Content (system List)

    @ViewBuilder
    private func content(_ unit: UnitInfo) -> some View {
        List {
            Section {
                HStack(spacing: 8) {
                    Text(unit.unitId).font(.caption.monospaced()).foregroundStyle(.secondary)
                    Text(unit.statusTitle)
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(unit.isFinished ? .green : .orange)
                    if unit.chapterFrom > 0 {
                        Text(Copy.unitChapters(from: unit.chapterFrom, to: unit.chapterTo))
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
                pendingPointer(unit)
                if let error = loadError {
                    ErrorBanner(message: error) { Task { await reloadContent() } }
                }
            }

            if unit.hasProse {
                contractSection(unit)
                proseSection(unit)
            } else {
                outlineSection(unit)
            }
        }
        .listStyle(.insetGrouped)
    }

    /// Confirmation lives on the task page. This row only points there, so a
    /// result that lands minutes later is never silently invisible.
    @ViewBuilder
    private func pendingPointer(_ unit: UnitInfo) -> some View {
        let pending = workspace.pendingTasks(unitID: unit.unitId)
        if !pending.isEmpty {
            Button {
                openTasks()
            } label: {
                HStack(spacing: 10) {
                    Image(systemName: "checklist").foregroundStyle(Color.accentColor)
                    Text(Copy.taskUnitPending(pending.count)).font(.subheadline)
                    Spacer()
                    Text(Copy.taskGoConfirm).font(.subheadline).foregroundStyle(.secondary)
                }
            }
            .accessibilityIdentifier("unit.pending.\(unit.unitId)")
        }
    }

    // MARK: Contract

    private func contractSection(_ unit: UnitInfo) -> some View {
        Section {
            DisclosureGroup(isExpanded: $contractExpanded) {
                UnitContractView(unit: resolvedUnit ?? unit, book: workspace.book, compact: true)
                    .padding(.top, 8)
            } label: {
                Text("\(Copy.unitContractTitle) · \(Copy.unitContractHint)")
                    .font(.subheadline.weight(.medium))
            }
        }
    }

    // MARK: Prose

    @ViewBuilder
    private func proseSection(_ unit: UnitInfo) -> some View {
        Section {
            if loading && chapters.isEmpty {
                HStack(spacing: 8) {
                    ProgressView()
                    Text(Copy.loading).font(.footnote).foregroundStyle(.secondary)
                }
            } else if chapters.isEmpty {
                Text(Copy.readerUnrecognized).font(.footnote).foregroundStyle(.secondary)
            }
            ForEach(chapters) { chapter in
                chapterRow(chapter, unit: unit)
            }
        } header: {
            Text(Copy.unitProseSummary(chapters: chapters.count, words: chapters.reduce(0) { $0 + $1.wordCount }))
        }
    }

    private func chapterRow(_ chapter: Chapter, unit: UnitInfo) -> some View {
        let paragraphs = chapterParagraphs[chapter.number] ?? []
        let split = splitIndex(paragraphs: paragraphs, words: chapter.wordCount)
        return VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline) {
                Text(Copy.chapterMenuLabel(number: chapter.number, title: chapter.title))
                    .font(.headline)
                Spacer()
                chapterMenu(chapter)
            }
            VStack(alignment: .leading, spacing: 14) {
                ForEach(paragraphs.indices, id: \.self) { index in
                    Text(paragraphs[index])
                        .font(.system(.body, design: .serif))
                        .lineSpacing(8)
                    if index == split {
                        HStack(spacing: 8) {
                            Rectangle().fill(.secondary.opacity(0.3)).frame(height: 1)
                            Text(Copy.chapterSplitHint).font(.caption2).foregroundStyle(.secondary)
                            Rectangle().fill(.secondary.opacity(0.3)).frame(height: 1)
                        }
                    }
                }
            }
            .textSelection(.enabled)
            HStack {
                Text(Copy.chapterWords(chapter.wordCount)).font(.caption).foregroundStyle(.secondary)
                if split >= 0, let info = splitWords(paragraphs: paragraphs, at: split, total: chapter.wordCount) {
                    Text(Copy.chapterSplitDetail(above: info.above, below: info.below))
                        .font(.caption2).foregroundStyle(Color.accentColor)
                }
            }
        }
        .padding(.vertical, 4)
        .swipeActions(edge: .trailing) {
            if !readOnly {
                Button { beginChapterEdit(chapter) } label: {
                    Label(Copy.unitEditChapter, systemImage: "pencil")
                }
                .tint(.orange)
            }
            Button { copyChapter(chapter) } label: {
                Label(Copy.unitCopyChapter, systemImage: "doc.on.doc")
            }
        }
        .contextMenu {
            Button { copyChapter(chapter) } label: {
                Label(Copy.unitCopyChapter, systemImage: "doc.on.doc")
            }
            if !readOnly {
                Button { beginChapterEdit(chapter) } label: {
                    Label(Copy.unitEditChapter, systemImage: "pencil")
                }
            }
        }
    }

    /// The chapter's actions as a system menu: the platform owns the glyph,
    /// sizing and overflow, and VoiceOver reads it as a menu button.
    private func chapterMenu(_ chapter: Chapter) -> some View {
        Menu {
            Button { copyChapter(chapter) } label: {
                Label(Copy.unitCopyChapter, systemImage: "doc.on.doc")
            }
            if !readOnly {
                Button { beginChapterEdit(chapter) } label: {
                    Label(Copy.unitEditChapter, systemImage: "pencil")
                }
            }
        } label: {
            Label(Copy.unitChapterActions, systemImage: "ellipsis.circle")
                .labelStyle(.iconOnly)
        }
        .accessibilityIdentifier("unit.chapter.menu.\(chapter.number)")
    }

    private func copyChapter(_ chapter: Chapter) {
        UIPasteboard.general.string = "第\(chapter.number)章 \(chapter.title)\n\n\(chapter.body)"
        copiedChapter = chapter.number
        workspace.note(Copy.unitCopied)
        Task {
            try? await Task.sleep(for: .seconds(1.6))
            if copiedChapter == chapter.number { copiedChapter = nil }
        }
    }

    private func beginChapterEdit(_ chapter: Chapter) {
        chapterDraft = chapterOriginal(chapter.number)
        editingChapter = chapter.number
    }

    /// The exact markdown handed to the editor; also the discard baseline.
    private func chapterOriginal(_ number: Int) -> String {
        guard let chapter = chapters.first(where: { $0.number == number }) else { return "" }
        return "## 第\(number)章\(chapter.title.isEmpty ? "" : " \(chapter.title)")\n\n\(chapter.body)"
    }

    /// Display-only split suggestion: the paragraph boundary nearest half the
    /// chapter's words, only above 5000 chars (desktop SPLIT_HINT_CHARS).
    private func splitIndex(paragraphs: [String], words: Int) -> Int {
        guard words > 5000, paragraphs.count >= 2 else { return -1 }
        let half = Double(words) / 2
        var acc = 0
        var index = -1
        var bestDiff = Double.infinity
        for i in 0..<(paragraphs.count - 1) {
            acc += paragraphs[i].filter { !$0.isWhitespace }.count
            let diff = abs(Double(acc) - half)
            if diff < bestDiff { bestDiff = diff; index = i }
        }
        return index
    }

    private func splitWords(paragraphs: [String], at index: Int, total: Int) -> (above: Int, below: Int)? {
        guard index >= 0 else { return nil }
        let above = paragraphs[...index].reduce(0) { $0 + $1.filter { !$0.isWhitespace }.count }
        return (above, max(total - above, 0))
    }

    // MARK: Outline (no prose yet)

    @ViewBuilder
    private func outlineSection(_ unit: UnitInfo) -> some View {
        Section {
            if !readOnly {
                Button {
                    outlineDraft = outlineRaw
                    editingOutline = true
                } label: {
                    Label(Copy.unitEditOutline, systemImage: "pencil")
                }
            }
            UnitContractView(unit: resolvedUnit ?? unit, book: workspace.book)
            DisclosureGroup(Copy.unitViewOutlineSource) {
                Text(outlineRaw)
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.top, 6)
            }
            .font(.footnote)
        }
    }

    // MARK: Loading

    private func reloadContent() async {
        guard let unit else { return }
        // The workspace snapshot is the listing projection: raws are stripped.
        // Prose comes from the file endpoint; the outline from its YAML file.
        loading = true
        defer { loading = false }
        do {
            if unit.hasProse {
                let file = try await workspace.api.file(bookID: workspace.bookID, path: unit.prosePath)
                proseRaw = file.content
                let parsed = Chapter.parse(proseRaw)
                chapters = parsed
                // Split once per chapter, not per layout pass.
                chapterParagraphs = Dictionary(uniqueKeysWithValues: parsed.map { chapter in
                    (chapter.number, chapter.body
                        .components(separatedBy: "\n")
                        .map { $0.trimmingCharacters(in: .whitespaces) }
                        .filter { !$0.isEmpty })
                })
            } else {
                proseRaw = ""
                chapters = []
                chapterParagraphs = [:]
            }
            if let path = unit.outlinePath, !path.isEmpty {
                let file = try await workspace.api.file(bookID: workspace.bookID, path: path)
                outlineRaw = file.content
            }
            loadError = nil
        } catch {
            loadError = error.localizedDescription
        }
    }
}

// MARK: - Editors

/// The outline YAML editor: a form sheet with system cancel/save, keyboard
/// dismissal by drag, and a discard confirmation when the draft has changed.
private struct OutlineEditorSheet: View {
    let title: String
    @Binding var draft: String
    let original: String
    let saving: Bool
    let onSave: () async -> Bool
    let onClose: () -> Void

    @State private var showDiscard = false

    private var dirty: Bool { draft != original }

    var body: some View {
        NavigationStack {
            TextEditor(text: $draft)
                .font(.caption.monospaced())
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .scrollDismissesKeyboard(.interactively)
                .padding(.horizontal, 8)
                .navigationTitle(title)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button(Copy.actionCancel) {
                            if dirty { showDiscard = true } else { onClose() }
                        }
                        .disabled(saving)
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button(saving ? Copy.saving : Copy.actionSave) {
                            Task { if await onSave() { onClose() } }
                        }
                        .disabled(saving)
                    }
                }
                .confirmationDialog(Copy.discardTitle, isPresented: $showDiscard, titleVisibility: .visible) {
                    Button(Copy.discardConfirm, role: .destructive) { onClose() }
                    Button(Copy.discardContinue, role: .cancel) {}
                } message: {
                    Text(Copy.discardMessage)
                }
        }
        .interactiveDismissDisabled(dirty && !saving)
    }
}

/// One chapter's markdown editor; same sheet contract as the outline editor.
private struct ChapterEditorSheet: View {
    let chapter: Int
    @Binding var draft: String
    let original: String
    let saving: Bool
    let onSave: () async -> Bool
    let onClose: () -> Void

    @State private var showDiscard = false

    private var dirty: Bool { draft != original }

    var body: some View {
        NavigationStack {
            TextEditor(text: $draft)
                .font(.system(.body, design: .serif))
                .autocorrectionDisabled()
                .scrollDismissesKeyboard(.interactively)
                .padding(.horizontal, 8)
                .accessibilityIdentifier("unit.chapter.editor")
                .navigationTitle(Copy.chapterHeading(chapter))
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button(Copy.actionCancel) {
                            if dirty { showDiscard = true } else { onClose() }
                        }
                        .disabled(saving)
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button(saving ? Copy.saving : Copy.actionSave) {
                            Task { if await onSave() { onClose() } }
                        }
                        .disabled(saving)
                    }
                }
                .confirmationDialog(Copy.discardTitle, isPresented: $showDiscard, titleVisibility: .visible) {
                    Button(Copy.discardConfirm, role: .destructive) { onClose() }
                    Button(Copy.discardContinue, role: .cancel) {}
                } message: {
                    Text(Copy.discardMessage)
                }
        }
        .interactiveDismissDisabled(dirty && !saving)
    }
}