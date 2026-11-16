import SwiftUI

/// Structured outline rendering + minimal field editing, mirroring
/// StructuredOutline.vue: root contract, threads, volume arcs, beats, the unit
/// seed index and the handoff. Field edits go through the server's minimal
/// Markdown patch (PATCH outline-field), never a client-side rewrite.
///
/// The content is a set of system List sections (`outlineSections`): headers,
/// row backgrounds and the unit index's DisclosureGroup chevron all belong to
/// the platform. The outline panel composes these sections into its own List
/// alongside the document picker; the task-card preview wraps them in a
/// non-scrolling List. Nesting a scroll-disabled List inside another List does
/// not render, so the sections are shared, not the container.
struct StructuredOutlineView: View {
    let document: StructuredMarkdown
    let units: [UnitInfo]?
    var readOnly = false
    var onEditField: ((MarkdownEntity?, MarkdownField) -> Void)?
    var onOpenUnit: ((UnitInfo) -> Void)?

    private var roots: [MarkdownEntity] { document.entities.filter { ["故事线", "承诺", "约束"].contains($0.kind) } }
    private var arcs: [MarkdownEntity] { document.entities.filter { $0.kind == "分卷" } }
    private var beats: [MarkdownEntity] { document.entities.filter { ["节拍", "转折"].contains($0.kind) } }
    private var seeds: [MarkdownEntity] { document.entities.filter { $0.kind == "单元" } }
    private var handoff: MarkdownEntity? { document.entities.first { $0.kind == "卷末交接" } }
    private var rootFields: [MarkdownField] {
        document.root.fields.filter { !["schema_version", "book_id", "volume_id"].contains($0.name) }
    }
    private var canEdit: Bool { !readOnly && onEditField != nil }

    /// Standalone use (the task-card preview): a non-scrolling List so it can
    /// sit inside another scroll surface.
    var body: some View {
        List { outlineSections }
            .listStyle(.insetGrouped)
            .scrollDisabled(true)
    }

    /// The document's sections, for composition into a parent List.
    @ViewBuilder
    var outlineSections: some View {
        headerSection
        if let diagnostics = document.diagnostics, !diagnostics.isEmpty {
            diagnosticsSection(diagnostics)
        }
        if !rootFields.isEmpty {
            Section {
                ForEach(rootFields, id: \.self) { field in
                    fieldRow(entity: nil, field: field)
                }
            }
        }
        if !roots.isEmpty {
            Section(Copy.outlineThreadsSection) {
                ForEach(roots, id: \.self) { entity in
                    threadRow(entity)
                }
            }
        }
        if !arcs.isEmpty {
            Section(Copy.outlineArcsSection) {
                ForEach(arcs, id: \.self) { arc in
                    arcRow(arc)
                }
            }
        }
        if !beats.isEmpty {
            Section(Copy.outlineBeatsSection) {
                ForEach(beats, id: \.self) { beat in
                    beatRow(beat)
                }
            }
        }
        if !seeds.isEmpty {
            Section {
                ForEach(seeds, id: \.self) { seed in
                    SeedRow(
                        seed: seed,
                        unit: units?.first { $0.unitId == seed.id },
                        readOnly: readOnly,
                        onEditField: onEditField,
                        onOpenUnit: onOpenUnit
                    )
                }
            } header: {
                Text("\(Copy.outlineSeedsSection) · \(seeds.count)")
            } footer: {
                Text(Copy.outlineSeedsHint)
            }
        }
        if let handoff {
            Section(Copy.outlineHandoff) {
                labeled(Copy.outlineArcDeliverables, handoff.value("deliverables"))
                labeled(Copy.outlineFieldLabel("next_volume_entry"), handoff.value("next_volume_entry"))
            }
        }
    }

    private var headerSection: some View {
        Section {
            VStack(alignment: .leading, spacing: 2) {
                Text(document.protocolName == "book-outline/v1" ? Copy.outlineWholeStructure : Copy.outlineVolumeStructure)
                    .font(.caption.weight(.semibold)).foregroundStyle(Color.accentColor)
                Text(document.title).font(.title3.bold())
            }
        }
    }

    private func diagnosticsSection(_ diagnostics: [OutlineDiagnostic]) -> some View {
        Section {
            ForEach(diagnostics, id: \.self) { diagnostic in
                Text(Copy.outlineDiagnosticLine(diagnostic.line ?? 0, path: diagnostic.path, message: diagnostic.message))
                    .font(.caption).foregroundStyle(.orange)
            }
        } header: {
            Text(Copy.outlineDiagnostics).foregroundStyle(.orange)
        }
    }

    /// A root-contract field as a tappable row; the system draws the row and
    /// the trailing edit affordance, not a hand-built card.
    private func fieldRow(entity: MarkdownEntity?, field: MarkdownField) -> some View {
        Button {
            if canEdit { onEditField?(entity, field) }
        } label: {
            VStack(alignment: .leading, spacing: 3) {
                HStack {
                    Text(Copy.outlineFieldLabel(field.name))
                        .font(.caption.weight(.semibold)).foregroundStyle(Color.accentColor)
                    Spacer()
                    if canEdit {
                        Image(systemName: "pencil").font(.caption2).foregroundStyle(.secondary)
                    }
                }
                Text(field.value).font(.footnote).foregroundStyle(.secondary)
                    .multilineTextAlignment(.leading)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!canEdit)
    }

    private func threadRow(_ entity: MarkdownEntity) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text(entity.id).font(.footnote.weight(.semibold))
                Spacer()
                Text(entity.value("kind")).font(.caption2).foregroundStyle(.secondary)
            }
            Text(entity.value("description")).font(.caption).foregroundStyle(.secondary)
            HStack(spacing: 6) {
                Text(entity.value("opening_state")).font(.caption2).foregroundStyle(.secondary)
                Text("→").font(.caption2).foregroundStyle(Color.accentColor)
                Text(entity.value("ending_state")).font(.caption2).foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 2)
        .contentShape(Rectangle())
        .contextMenu {
            if canEdit, let field = entity.fields.first(where: { $0.name == "description" }) {
                Button(Copy.outlineEditDescription, systemImage: "pencil") { onEditField?(entity, field) }
            }
        }
        .swipeActions(edge: .trailing) {
            if canEdit, let field = entity.fields.first(where: { $0.name == "description" }) {
                Button { onEditField?(entity, field) } label: {
                    Label(Copy.actionEdit, systemImage: "pencil")
                }
                .tint(.orange)
            }
        }
    }

    private func arcRow(_ arc: MarkdownEntity) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text("\(arc.id) · \(arc.value("chapter_range"))")
                    .font(.caption2).foregroundStyle(Color.accentColor)
                Text(arc.value("title")).font(.headline)
                Text(arc.value("dramatic_question")).font(.caption).foregroundStyle(.secondary)
            }
            Text(arc.value("purpose")).font(.caption).foregroundStyle(.secondary)
            HStack(alignment: .top, spacing: 8) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(Copy.outlineArcEntry).font(.caption2.weight(.semibold)).foregroundStyle(Color.accentColor)
                    Text(arc.value("entry_state")).font(.caption).foregroundStyle(.secondary)
                }
                Text("→").foregroundStyle(Color.accentColor)
                VStack(alignment: .leading, spacing: 2) {
                    Text(Copy.outlineArcExit).font(.caption2.weight(.semibold)).foregroundStyle(Color.accentColor)
                    Text(arc.value("exit_state")).font(.caption).foregroundStyle(.secondary)
                }
            }
            labeled(Copy.outlineArcDeliverables, arc.value("deliverables"))
            labeled(Copy.outlineArcOpenQuestions, arc.value("open_questions"))
        }
        .padding(.vertical, 2)
        .contentShape(Rectangle())
        .contextMenu {
            if canEdit, let field = arc.fields.first(where: { $0.name == "purpose" }) {
                Button(Copy.outlineEditPurpose, systemImage: "pencil") { onEditField?(arc, field) }
            }
            if canEdit, let field = arc.fields.first(where: { $0.name == "deliverables" }) {
                Button(Copy.outlineArcDeliverables, systemImage: "pencil") { onEditField?(arc, field) }
            }
        }
        .swipeActions(edge: .trailing) {
            if canEdit, let field = arc.fields.first(where: { $0.name == "purpose" }) {
                Button { onEditField?(arc, field) } label: {
                    Label(Copy.actionEdit, systemImage: "pencil")
                }
                .tint(.orange)
            }
        }
    }

    private func beatRow(_ beat: MarkdownEntity) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text("\(beat.id) · \(beat.value("chapter_window"))")
                .font(.caption2).foregroundStyle(.secondary)
            Text(beat.value("event")).font(.footnote.weight(.medium))
            Text("\(beat.value("cause")) → \(beat.value("effect"))")
                .font(.caption).foregroundStyle(.secondary)
        }
        .padding(.vertical, 2)
    }

    private func labeled(_ label: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 1) {
            Text(label).font(.caption2.weight(.semibold)).foregroundStyle(Color.accentColor)
            Text(value.isEmpty ? Copy.emptyValue : value).font(.caption).foregroundStyle(.secondary)
        }
    }
}

/// One unit-seed row: a system DisclosureGroup whose label carries the unit
/// identity and whose expanded body holds the full causal contract. The
/// chevron is the system's, and the production entry is a real button.
private struct SeedRow: View {
    let seed: MarkdownEntity
    let unit: UnitInfo?
    let readOnly: Bool
    let onEditField: ((MarkdownEntity?, MarkdownField) -> Void)?
    let onOpenUnit: ((UnitInfo) -> Void)?

    @State private var expanded = false

    private var canEdit: Bool { !readOnly && onEditField != nil }

    var body: some View {
        DisclosureGroup(isExpanded: $expanded) {
            VStack(alignment: .leading, spacing: 6) {
                Text("\(seed.value("entry_cause")) → \(seed.value("objective")) → \(seed.value("choice")) → \(seed.value("outcome"))")
                    .font(.caption).foregroundStyle(.secondary)
                labeled(Copy.outlineSeedObstacle, seed.value("obstacle"))
                labeled(Copy.outlineSeedAttempt, "\(seed.value("attempt"))；\(seed.value("setback"))")
                labeled(Copy.outlineSeedCost, seed.value("cost"))
                labeled(Copy.outlineSeedStateChanges, seed.value("state_changes"))
                labeled(Copy.outlineSeedNextHook, seed.value("next_hook"))
                if !seed.value("clock_deadline").isEmpty {
                    labeled(Copy.outlineSeedDeadline, seed.value("clock_deadline"))
                }
                HStack(spacing: 14) {
                    if canEdit, let field = seed.fields.first(where: { $0.name == "outcome" }) {
                        Button(Copy.outlineEditOutcome) { onEditField?(seed, field) }
                            .font(.caption)
                    }
                    if let unit {
                        Button(nextAction(unit)) { onOpenUnit?(unit) }
                            .font(.caption.weight(.semibold))
                            .buttonStyle(.bordered)
                            .accessibilityIdentifier("outline.seed.open.\(seed.id)")
                    }
                }
                .padding(.top, 2)
            }
            .padding(.top, 6)
        } label: {
            VStack(alignment: .leading, spacing: 6) {
                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("\(seed.id) · \(seed.value("chapter_range"))")
                            .font(.caption2).foregroundStyle(Color.accentColor)
                        Text(seed.value("title")).font(.footnote.weight(.semibold))
                            .foregroundStyle(.primary)
                    }
                    Spacer()
                    Text(unit?.statusTitle ?? Copy.outlineSeedPending)
                        .font(.caption2).foregroundStyle(unit?.isFinished == true ? .green : .orange)
                }
                labeled(Copy.outlineSeedObjective, seed.value("objective"))
                labeled(Copy.outlineSeedChoice, seed.value("choice"))
                labeled(Copy.outlineSeedOutcome, seed.value("outcome"))
                labeled(Copy.outlineSeedClock, clockLabel)
            }
            .contentShape(Rectangle())
        }
        .accessibilityIdentifier("outline.seed.\(seed.id)")
    }

    private var clockLabel: String {
        let start = seed.value("clock_start_day")
        let end = seed.value("clock_end_day")
        guard !start.isEmpty, !end.isEmpty else { return Copy.outlineSeedClockPending }
        let gap = seed.value("clock_gap_days").isEmpty ? "0" : seed.value("clock_gap_days")
        return Copy.outlineSeedClock(start: start, end: end, gap: gap)
    }

    private func nextAction(_ unit: UnitInfo) -> String {
        switch unit.status {
        case "proposed": Copy.outlineNextOutline
        case "accepted": Copy.outlineNextWrite
        case "drafted": Copy.outlineNextReview
        case "reviewed": Copy.outlineNextFinalize
        case "finalized": Copy.outlineNextView
        default: Copy.outlineNextProduce
        }
    }

    private func labeled(_ label: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 1) {
            Text(label).font(.caption2.weight(.semibold)).foregroundStyle(Color.accentColor)
            Text(value.isEmpty ? Copy.emptyValue : value).font(.caption).foregroundStyle(.secondary)
        }
    }
}

/// The outline tab: book outline + per-volume outlines with structure review,
/// volume-plan generation and raw-MD editing — OutlineWorkspace.vue.
///
/// The shell is a single system List: a document-selection Menu row, a
/// document-actions Menu row, and the structured outline's sections composed
/// in directly (a nested scroll-disabled List would not render). Raw-MD and
/// field editing open form sheets with system cancel/save and a discard
/// confirmation, never an inline editor with hand-built buttons.
struct OutlinePanelView: View {
    let workspace: BookWorkspaceStore
    let openUnit: (UnitInfo) -> Void

    @State private var activeVolume = 0
    @State private var showBook = true
    @State private var editingRaw = false
    @State private var rawDraft = ""
    @State private var editingField: FieldEdit?

    struct FieldEdit: Identifiable {
        let id = UUID()
        let path: String
        let kind: String
        let entityID: String
        let field: String
        var value: String
        let label: String
    }

    private var book: BookSnapshot? { workspace.book }
    private var bookRaw: String { book?.file("outline/book_outline.md") ?? "" }
    private var bookDoc: StructuredMarkdown? { book?.structuredOutlines?["outline/book_outline.md"] }
    private var volume: VolumeInfo? {
        book?.volumes.first { $0.volume == activeVolume } ?? book?.volumes.first
    }
    private var volumePath: String {
        String(format: "outline/volumes/v%02d.md", volume?.volume ?? max(activeVolume, 1))
    }
    private var volumeDoc: StructuredMarkdown? {
        volume?.structured ?? book?.structuredOutlines?[volumePath]
    }
    private var readOnly: Bool { book?.summary.readOnly ?? false }
    private var currentLegacy: Bool { (showBook ? bookDoc?.legacy : volumeDoc?.legacy) ?? false }
    private var currentDoc: StructuredMarkdown? { showBook ? bookDoc : volumeDoc }
    private var currentRaw: String { showBook ? bookRaw : (volume?.raw ?? "") }
    private var documentLabel: String {
        showBook ? Copy.outlineBookTab : Copy.volumeTitle(volume?.volume ?? max(activeVolume, 1))
    }

    var body: some View {
        Group {
            if workspace.filesReady {
                outlineList
            } else {
                ProgressView(Copy.loading)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .task { await workspace.ensureFiles() }
        .sheet(item: $editingField) { edit in
            FieldEditorSheet(edit: edit, saving: workspace.saving) { value in
                Task {
                    if await workspace.saveOutlineField(path: edit.path, kind: edit.kind, entityID: edit.entityID, field: edit.field, value: value) {
                        editingField = nil
                    }
                }
            } onCancel: {
                editingField = nil
            }
        }
        .sheet(isPresented: $editingRaw) {
            RawOutlineEditorSheet(
                title: documentLabel,
                draft: $rawDraft,
                original: currentRaw,
                saving: workspace.saving,
                readOnly: readOnly,
                onSave: {
                    let path = showBook ? "outline/book_outline.md" : volumePath
                    return await workspace.saveFile(path: path, content: rawDraft)
                },
                onClose: { editingRaw = false }
            )
        }
    }

    private var outlineList: some View {
        List {
            Section {
                documentPickerRow
                if let doc = currentDoc, !doc.legacy {
                    documentActionsRow
                }
            } footer: {
                Text(Copy.outlineIntro)
            }
            documentContent
        }
        .listStyle(.insetGrouped)
    }

    /// Book / volume selection as a system Menu: the platform owns the
    /// checkmark, the popover and the overflow, not a hand-built chip row.
    private var documentPickerRow: some View {
        Menu {
            Button {
                showBook = true
            } label: {
                if showBook { Label(Copy.outlineBookTab, systemImage: "checkmark") }
                else { Text(Copy.outlineBookTab) }
            }
            ForEach(book?.volumes ?? []) { vol in
                Button {
                    activeVolume = vol.volume
                    showBook = false
                } label: {
                    if !showBook && volume?.volume == vol.volume {
                        Label(Copy.volumeTitle(vol.volume), systemImage: "checkmark")
                    } else {
                        Text(Copy.volumeTitle(vol.volume))
                    }
                }
            }
        } label: {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(Copy.outlineSelectDocument).font(.caption).foregroundStyle(.secondary)
                    Text(documentLabel).font(.subheadline.weight(.semibold))
                }
                Spacer()
                Image(systemName: "chevron.up.chevron.down").font(.caption).foregroundStyle(.secondary)
            }
            .contentShape(Rectangle())
        }
        .accessibilityIdentifier("outline.document.menu")
    }

    /// The document's actions as a system Menu plus a format badge. Generation
    /// and structure review start runs; raw editing opens a sheet.
    private var documentActionsRow: some View {
        HStack {
            Text(currentLegacy ? Copy.outlineLegacy : Copy.outlineStructured)
                .font(.caption2).foregroundStyle(currentLegacy ? .orange : .green)
            Spacer()
            Menu {
                Button {
                    rawDraft = currentRaw
                    editingRaw = true
                } label: {
                    Label(Copy.outlineRawEdit, systemImage: "doc.text")
                }
                if !readOnly {
                    Button {
                        workspace.startStructureReview(path: showBook ? "outline/book_outline.md" : volumePath)
                    } label: {
                        Label(Copy.outlineStructureReview, systemImage: "checkmark.seal")
                    }
                    .disabled(workspace.busy)
                    if !showBook {
                        Button {
                            workspace.startPlanVolume(volume?.volume ?? max(activeVolume, 1))
                        } label: {
                            Label(volume == nil ? Copy.outlineGenerateVolume(volume?.volume ?? 1) : Copy.outlineRewriteVolume,
                                  systemImage: "square.and.pencil")
                        }
                        .disabled(workspace.busy)
                    }
                }
            } label: {
                Label(Copy.actionMore, systemImage: "ellipsis.circle")
            }
            .accessibilityIdentifier("outline.actions.menu")
        }
    }

    @ViewBuilder
    private var documentContent: some View {
        if showBook {
            bookContent
        } else {
            volumeContent
        }
    }

    @ViewBuilder
    private var bookContent: some View {
        if let bookDoc, !bookDoc.legacy {
            StructuredOutlineView(
                document: bookDoc,
                units: book?.units,
                readOnly: readOnly,
                onEditField: { entity, field in
                    editingField = FieldEdit(
                        path: "outline/book_outline.md",
                        kind: entity?.kind ?? "root",
                        entityID: entity?.id ?? "",
                        field: field.name,
                        value: field.value,
                        label: "\(entity?.id ?? Copy.outlineBookTab) · \(Copy.outlineFieldLabel(field.name))"
                    )
                },
                onOpenUnit: openUnit
            ).outlineSections
        } else if !bookRaw.isEmpty {
            legacyDocument(bookRaw)
        } else {
            Section {
                ContentUnavailableView(Copy.outlineEmptyBook, systemImage: "text.book.closed")
            }
        }
    }

    @ViewBuilder
    private var volumeContent: some View {
        if let volumeDoc, !volumeDoc.legacy {
            StructuredOutlineView(
                document: volumeDoc,
                units: (book?.units ?? []).filter { $0.volume == (volume?.volume ?? activeVolume) },
                readOnly: readOnly,
                onEditField: { entity, field in
                    editingField = FieldEdit(
                        path: volumePath,
                        kind: entity?.kind ?? "root",
                        entityID: entity?.id ?? "",
                        field: field.name,
                        value: field.value,
                        label: "\(entity?.id ?? Copy.volumeTitle(volume?.volume ?? 1)) · \(Copy.outlineFieldLabel(field.name))"
                    )
                },
                onOpenUnit: openUnit
            ).outlineSections
        } else if let volume, let raw = volume.raw, !raw.isEmpty {
            legacyDocument(raw)
        } else {
            Section {
                ContentUnavailableView {
                    Label(Copy.outlineEmptyVolume, systemImage: "text.book.closed")
                } actions: {
                    if !readOnly {
                        Button(Copy.outlineGenerateVolume(max(activeVolume, 1))) {
                            workspace.startPlanVolume(max(activeVolume, 1))
                        }
                        .buttonStyle(.borderedProminent)
                        .disabled(workspace.busy)
                    }
                }
            }
        }
    }

    private func legacyDocument(_ raw: String) -> some View {
        Section {
            Text(Copy.outlineLegacyNote).font(.caption).foregroundStyle(.orange)
            Text(raw)
                .font(.caption.monospaced())
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
        }
    }
}

/// Raw-Markdown editing as a form sheet: system cancel/save, drag-to-dismiss
/// keyboard, and a discard confirmation when the draft has changed. Saving
/// re-parses and validates the whole document server-side.
private struct RawOutlineEditorSheet: View {
    let title: String
    @Binding var draft: String
    let original: String
    let saving: Bool
    let readOnly: Bool
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
                        Button(saving ? Copy.saving : Copy.outlineSaveRaw) {
                            Task { if await onSave() { onClose() } }
                        }
                        .disabled(saving || readOnly)
                    }
                }
                .safeAreaInset(edge: .bottom) {
                    Text(Copy.outlineRawNote)
                        .font(.caption2).foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 16).padding(.vertical, 6)
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

/// Single-field editing as a form sheet; the server applies a minimal Markdown
/// patch, so the note explains that nothing else is rewritten.
struct FieldEditorSheet: View {
    let edit: OutlinePanelView.FieldEdit
    let saving: Bool
    let onSave: (String) -> Void
    let onCancel: () -> Void
    @State private var value: String
    @State private var showDiscard = false

    init(edit: OutlinePanelView.FieldEdit, saving: Bool, onSave: @escaping (String) -> Void, onCancel: @escaping () -> Void) {
        self.edit = edit
        self.saving = saving
        self.onSave = onSave
        self.onCancel = onCancel
        _value = State(initialValue: edit.value)
    }

    private var dirty: Bool { value != edit.value }

    var body: some View {
        NavigationStack {
            TextEditor(text: $value)
                .font(.footnote)
                .autocorrectionDisabled()
                .scrollDismissesKeyboard(.interactively)
                .padding(.horizontal, 8)
                .navigationTitle(edit.label)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button(Copy.actionCancel) {
                            if dirty { showDiscard = true } else { onCancel() }
                        }
                        .disabled(saving)
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button(saving ? Copy.saving : Copy.actionSave) { onSave(value) }
                            .disabled(saving || value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    }
                }
                .safeAreaInset(edge: .bottom) {
                    Text(Copy.outlineEditFieldNote)
                        .font(.caption2).foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 16).padding(.vertical, 6)
                }
                .confirmationDialog(Copy.discardTitle, isPresented: $showDiscard, titleVisibility: .visible) {
                    Button(Copy.discardConfirm, role: .destructive) { onCancel() }
                    Button(Copy.discardContinue, role: .cancel) {}
                } message: {
                    Text(Copy.discardMessage)
                }
        }
        .interactiveDismissDisabled(dirty && !saving)
        .presentationDetents([.medium, .large])
    }
}