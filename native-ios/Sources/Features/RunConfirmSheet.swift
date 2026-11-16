import SwiftUI

/// The LLM call confirmation dialog — the mobile counterpart of
/// RunConfirmDialog.vue. Same guarantees: the author sees exactly what will be
/// sent (preview shares the pipeline's assembly), the budget, the rewrite mode,
/// optional reference knowledge, and the write-boundary note before any call.
struct RunConfirmSheet: View {
    @Environment(\.dismiss) private var dismiss
    let workspace: BookWorkspaceStore
    let request: BookWorkspaceStore.RunConfirmRequest

    @State private var packet: ContextPacket?
    @State private var loading = true
    @State private var loadError: String?
    @State private var instruction = ""
    @State private var rewriteMode = "fresh"
    @State private var knowledgeDocs: [KnowledgeDoc] = []
    @State private var selectedKnowledge: Set<String> = []
    @State private var knowledgeOpen = false
    @State private var knowledgeShowAll = false

    private var spec: TaskSpec { request.spec }
    private var isFinalize: Bool { spec.kind == "finalize" }
    private var isWrite: Bool { spec.kind == "write" }
    private var writeUnit: UnitInfo? {
        guard let unitID = spec.unitId else { return nil }
        return workspace.book?.unit(unitID)
    }
    private var canRefine: Bool { isWrite && (writeUnit?.hasProse ?? false) }
    private var isSupersedeReview: Bool { spec.kind == "review" && spec.supersedeReview == true }
    private var openReviewLists: Int {
        guard isSupersedeReview else { return 0 }
        return workspace.candidates.filter { $0.kind == "review" && (spec.unitId == nil || $0.unitId == spec.unitId) }.count
    }
    private var isDerivedRewrite: Bool {
        guard spec.kind == "rewrite_entity", let path = spec.targetPath else { return false }
        return path.hasPrefix("canon/cast/")
            || ["canon/foreshadow-schedule.md", "canon/author-lore.md", "canon/world.md", "canon/goldfinger.md", "book-bible.md"].contains(path)
    }

    private var previewSpec: TaskSpec {
        var copy = spec
        if isWrite { copy.rewriteMode = rewriteMode }
        return copy
    }

    private var modelLabel: String {
        let model = workspace.defaultModelFor(spec.kind)
        return model.isEmpty ? Copy.runConfirmModelByStage : model
    }

    private var riskNote: String? {
        if isFinalize { return Copy.riskFinalize }
        if spec.kind == "plan" && spec.mode == "volume" {
            let volume = Int((spec.volumeId ?? "v01").dropFirst().filter(\.isNumber)) ?? 1
            return volume >= 2 ? Copy.riskVolumePlanLater(volume - 1) : Copy.riskVolumePlan
        }
        if isDerivedRewrite { return Copy.riskDerivedRewrite }
        if isWrite && rewriteMode == "refine" { return Copy.riskWriteRefine }
        if isWrite && canRefine { return Copy.riskWriteFresh }
        if isSupersedeReview && openReviewLists > 0 { return Copy.riskSupersedeReview(openReviewLists) }
        if spec.kind == "structure_review" { return Copy.riskStructureReview }
        if spec.kind == "rewrite_entity" { return Copy.riskEntityRewrite }
        return nil
    }

    // Knowledge filtering mirrors the desktop dialog: default-injected articles
    // are read-only notes (never checkboxes), author docs never appear.
    private var knowledgeStage: String { KnowledgeFilter.stage(for: spec) }
    private var bookGenre: String { workspace.book?.summary.genre ?? "" }
    private var bookSubgenre: String { workspace.book?.summary.subgenre ?? "" }
    private var autoInjected: [KnowledgeDoc] {
        knowledgeDocs.filter { ($0.isDefault ?? false) && KnowledgeFilter.appliesTo(stages: $0.stages, stage: knowledgeStage) }
    }
    private var suitableKnowledge: [KnowledgeDoc] {
        knowledgeDocs.filter {
            !($0.isDefault ?? false) && !$0.isAuthorDoc
                && KnowledgeFilter.appliesTo(stages: $0.stages, stage: knowledgeStage)
                && KnowledgeFilter.appliesToGenre($0, bookGenre: bookGenre, bookSubgenre: bookSubgenre)
        }
    }
    private var shownKnowledge: [KnowledgeDoc] {
        knowledgeShowAll
            ? knowledgeDocs.filter { !($0.isDefault ?? false) && !$0.isAuthorDoc }
            : suitableKnowledge
    }
    private var hiddenKnowledgeCount: Int {
        knowledgeDocs.filter { !($0.isDefault ?? false) && !$0.isAuthorDoc }.count - suitableKnowledge.count
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    LabeledContent(Copy.runConfirmModel, value: modelLabel)
                    LabeledContent(Copy.runConfirmBudget, value: isFinalize
                        ? Copy.runConfirmBudgetLocal
                        : (packet.map { Copy.runConfirmBudgetCalls($0.policy.maxCalls) } ?? "…"))
                    LabeledContent(Copy.runConfirmContext, value: packet.map {
                        Copy.runConfirmContextSize(blocks: $0.blocks.count, kb: Double($0.totalBytes) / 1024)
                    } ?? "…")
                } header: {
                    Text(request.title)
                } footer: {
                    if let riskNote { Text(riskNote) }
                }

                if canRefine {
                    Section(Copy.runConfirmRewriteMode) {
                        Picker(Copy.runConfirmRewriteMode, selection: $rewriteMode) {
                            Text(Copy.runConfirmRewriteRefine).tag("refine")
                            Text(Copy.runConfirmRewriteFresh).tag("fresh")
                        }
                        .pickerStyle(.segmented)
                        .onChange(of: rewriteMode) { _, _ in Task { await loadPreview() } }
                        Text(Copy.runConfirmRewriteNote).font(.caption).foregroundStyle(.secondary)
                    }
                }

                Section(Copy.runConfirmContextLabel) {
                    if loading {
                        HStack { ProgressView(); Text(Copy.runConfirmPreviewLoading).font(.footnote).foregroundStyle(.secondary) }
                    } else if let loadError {
                        ErrorBanner(message: loadError) { Task { await loadPreview() } }
                    } else if let packet {
                        DisclosureGroup(Copy.runConfirmSystemPrompt) {
                            Text(packet.systemPrompt)
                                .font(.caption.monospaced())
                                .foregroundStyle(.secondary)
                                .textSelection(.enabled)
                        }
                        if !packet.basisBlocks.isEmpty {
                            DisclosureGroup(Copy.runConfirmBasisGroup) {
                                ForEach(packet.basisBlocks) { block in contextBlock(block) }
                            }
                        }
                        ForEach(packet.contentBlocks) { block in contextBlock(block) }
                    }
                }

                if !isFinalize && !knowledgeDocs.isEmpty {
                    knowledgeSection
                }

                if !isFinalize {
                    Section(Copy.runConfirmInstruction) {
                        TextField(Copy.runConfirmInstructionHint, text: $instruction, axis: .vertical)
                            .lineLimit(3...8)
                    }
                }
            }
            .navigationTitle(request.title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(Copy.actionCancel) { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(isFinalize ? Copy.runConfirmFinalizeAction : Copy.runConfirmAction) {
                        workspace.confirmRun(
                            instruction: instruction.trimmingCharacters(in: .whitespacesAndNewlines),
                            knowledgeIds: Array(selectedKnowledge),
                            rewriteMode: isWrite ? rewriteMode : nil
                        )
                        dismiss()
                    }
                    .disabled(loading || loadError != nil)
                }
            }
        }
        .task {
            // Refine is the common intent when prose exists (desktop default).
            if canRefine { rewriteMode = "refine" }
            await loadPreview()
            knowledgeDocs = (try? await workspace.api.knowledgeDocs()) ?? []
        }
        .presentationDetents([.large])
    }

    private func contextBlock(_ block: ContextBlock) -> some View {
        DisclosureGroup {
            Text(block.content)
                .font(.caption.monospaced())
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(block.label).font(.footnote.weight(.medium))
                Text("\(block.source) · \(block.bytes) B")
                    .font(.caption2).foregroundStyle(.secondary)
            }
        }
    }

    private var knowledgeSection: some View {
        Section {
            DisclosureGroup(isExpanded: $knowledgeOpen) {
                Text(Copy.runConfirmKnowledgeNote).font(.caption).foregroundStyle(.secondary)
                if shownKnowledge.isEmpty {
                    Text(Copy.runConfirmKnowledgeEmpty).font(.caption).foregroundStyle(.secondary)
                }
                ForEach(shownKnowledge) { doc in
                    let mismatch = KnowledgeFilter.mismatchReasons(doc, stage: knowledgeStage, bookGenre: bookGenre, bookSubgenre: bookSubgenre)
                    // System multi-select rows: the whole row toggles and the
                    // trailing checkmark is the platform's selection idiom —
                    // no hand-drawn checkbox glyph.
                    Button {
                        if selectedKnowledge.contains(doc.id) { selectedKnowledge.remove(doc.id) }
                        else { selectedKnowledge.insert(doc.id) }
                    } label: {
                        HStack(alignment: .top, spacing: 10) {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(doc.title).font(.footnote.weight(.medium)).foregroundStyle(.primary)
                                Text(doc.summary).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                            }
                            Spacer()
                            VStack(alignment: .trailing, spacing: 2) {
                                Text(String(format: "%.1fKB", Double(doc.bytes) / 1024))
                                    .font(.caption2).foregroundStyle(.secondary)
                                if !mismatch.isEmpty {
                                    Text(mismatch).font(.caption2).foregroundStyle(.red)
                                } else if !KnowledgeFilter.isCommon(doc) {
                                    Text(KnowledgeFilter.genreSummary(doc)).font(.caption2).foregroundStyle(Color.accentColor).lineLimit(1)
                                }
                            }
                            Image(systemName: "checkmark")
                                .font(.footnote.weight(.semibold))
                                .foregroundStyle(.tint)
                                .opacity(selectedKnowledge.contains(doc.id) ? 1 : 0)
                        }
                        .opacity(mismatch.isEmpty ? 1 : 0.55)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(selectedKnowledge.contains(doc.id) ? [.isSelected] : [])
                }
            } label: {
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        Text(Copy.runConfirmKnowledge)
                        if !selectedKnowledge.isEmpty {
                            Text(Copy.runConfirmKnowledgePicked(selectedKnowledge.count))
                                .font(.caption).foregroundStyle(Color.accentColor)
                        }
                    }
                    Text(Copy.runConfirmKnowledgeScope(
                        stage: Copy.knowledgeStageLabel(knowledgeStage),
                        genre: bookGenre.isEmpty ? Copy.genreUnknown : bookGenre + (bookSubgenre.isEmpty ? "" : " / \(bookSubgenre)"),
                        count: suitableKnowledge.count
                    ))
                    .font(.caption2).foregroundStyle(.secondary)
                }
            }
            if !autoInjected.isEmpty {
                Text(Copy.runConfirmKnowledgeAuto + autoInjected.map(\.title).joined(separator: "、"))
                    .font(.caption).foregroundStyle(.secondary)
            }
            if hiddenKnowledgeCount > 0 {
                Button(knowledgeShowAll ? Copy.runConfirmKnowledgeShowSuitable : Copy.runConfirmKnowledgeShowAll) {
                    knowledgeShowAll.toggle()
                }
                .font(.caption)
            }
        }
    }

    private func loadPreview() async {
        loading = true
        loadError = nil
        do {
            packet = try await workspace.api.previewRun(previewSpec)
        } catch {
            packet = nil
            loadError = error.localizedDescription
        }
        loading = false
    }
}

/// Live run progress: kind/status, budget, per-call token rows, cache hit rate,
/// warnings and errors — the desktop run drawer. Cancel lives in the toolbar
/// like any other sheet action; a finished run routes to the task page, where
/// every result is confirmed.
struct RunProgressSheet: View {
    @Environment(\.dismiss) private var dismiss
    let workspace: BookWorkspaceStore
    /// Pops the shelf's stack back to the book page (the tasks tab lives there).
    var popToBook: () -> Void = {}

    @State private var showCancelConfirm = false

    private var run: Run? { workspace.activeRun }
    private var isRunning: Bool { run?.status == "running" || run?.status == "pending" }

    var body: some View {
        NavigationStack {
            List {
                if let run {
                    Section {
                        LabeledContent(run.kindTitle, value: run.statusTitle)
                        LabeledContent(Copy.runConfirmModel, value: run.modelId.isEmpty ? Copy.modelUnknown : run.modelId)
                        LabeledContent(Copy.runConfirmBudget, value: Copy.runCallsProgress(run.callsUsed, run.policy?.maxCalls))
                        if run.cache.hasData {
                            Text(Copy.runCacheSummary(read: run.cache.read, rate: run.cache.hitRateText)
                                 + (run.cache.creation > 0 ? " · " + Copy.runCacheCreation(run.cache.creation) : ""))
                                .font(.caption).foregroundStyle(.secondary)
                        }
                        if isRunning {
                            HStack(spacing: 10) {
                                ProgressView()
                                Text(Copy.runInProgressHint).font(.footnote).foregroundStyle(.secondary)
                            }
                        } else if run.status == "waiting_accept" {
                            // The candidate tab was fused into the task page;
                            // this row is the live route there, not a dead hint.
                            Button {
                                workspace.tab = .tasks
                                dismiss()
                                popToBook()
                            } label: {
                                HStack(spacing: 10) {
                                    Image(systemName: "checklist").foregroundStyle(Color.accentColor)
                                    Text(Copy.runWaitingAccept).font(.subheadline)
                                    Spacer()
                                    Text(Copy.taskGoConfirm).font(.subheadline).foregroundStyle(.secondary)
                                }
                            }
                            .accessibilityIdentifier("run.goto.tasks")
                        }
                    }
                    if let calls = run.calls, !calls.isEmpty {
                        Section(Copy.logsCalls) {
                            ForEach(calls) { call in
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(Copy.callRow(index: call.index, step: call.step, input: call.promptTokens, output: call.completionTokens, ms: call.durationMs))
                                        .font(.caption.monospaced())
                                    if let read = call.cacheReadTokens, read > 0 {
                                        Text(Copy.logsCache + " " + Copy.callCacheText(read: read, prompt: call.promptTokens))
                                            .font(.caption2).foregroundStyle(.secondary)
                                    }
                                    if let error = call.error, !error.isEmpty {
                                        Text(error).font(.caption2).foregroundStyle(.red)
                                    }
                                }
                            }
                        }
                    }
                    if let warnings = run.warnings, !warnings.isEmpty {
                        Section(Copy.runWarnings) {
                            ForEach(warnings, id: \.self) { warning in
                                Text(warning).font(.footnote).foregroundStyle(.orange)
                            }
                        }
                    }
                    if let error = run.error, !error.isEmpty {
                        Section(Copy.logsError) {
                            Text(error).font(.footnote).foregroundStyle(.red)
                            if run.status == "failed" {
                                Text(Copy.runFailedHint).font(.caption).foregroundStyle(.secondary)
                            }
                        }
                    }
                } else {
                    Text(Copy.runProgressNone).foregroundStyle(.secondary)
                }
            }
            .navigationTitle(Copy.runProgressTitle)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    if isRunning {
                        Button(Copy.runCancel, role: .destructive) { showCancelConfirm = true }
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(Copy.actionClose) { dismiss() }
                }
            }
            .confirmationDialog(Copy.runCancelConfirmTitle, isPresented: $showCancelConfirm, titleVisibility: .visible) {
                Button(Copy.runCancel, role: .destructive) { workspace.cancelActiveRun() }
                Button(Copy.actionCancel, role: .cancel) {}
            }
        }
        .presentationDetents([.medium, .large])
    }
}