import SwiftUI

/// The single confirmation surface. Every result — outline, prose, review list,
/// revise, finalize, volume plan, entity rewrite — is confirmed here with the
/// same shell and the same three actions; only the preview renderer and the
/// primary verb change by kind.
///
/// Context travels with the card (contract, parent seed, gate summary) instead
/// of sending the author back to the unit page: LLM calls take minutes, so by
/// the time a result lands the author is no longer sitting on that unit.
struct TaskConfirmCard: View {
    let item: TaskItem
    let workspace: BookWorkspaceStore
    let onReject: (Candidate, String) -> Void
    let onOpenUnit: (String) -> Void

    @State private var detail: Candidate?
    @State private var loadingDetail = false
    @State private var accepting = false
    @State private var showContext = false
    @State private var rejectReason = ""
    @State private var showRejectSheet = false
    @State private var retryPrompt = false

    private var candidate: Candidate? { detail ?? item.candidate.map { workspace.resolvedCandidate($0) } }
    private var kind: String { item.kind }
    private var unit: UnitInfo? {
        guard let unitID = item.unitId else { return nil }
        return workspace.book?.unit(unitID)
    }
    private var readOnly: Bool { workspace.book?.summary.readOnly ?? false }
    private var isReviewList: Bool { kind == "review" }
    private var isRevise: Bool { kind == "revise" }
    private var isRefineWrite: Bool {
        kind == "write" && ((candidate?.baseChapters?.isEmpty == false) || (candidate?.hasBaseChapters ?? false)
            || item.run?.spec?.rewriteMode == "refine")
    }

    /// The review group for this row: its own list, or the list folded into a revise.
    private var reviewGroup: ReviewGroup? {
        guard let unitID = item.unitId else { return nil }
        return workspace.reviewGroups.first { $0.unitId == unitID }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            header
            if item.section == .needsAction {
                if let candidate {
                    preview(candidate)
                    contextSection
                    actions(candidate)
                } else if isReviewList, let group = reviewGroup {
                    // A review list renders from the slim payload's findings.
                    ReviewFindingsEditor(group: group, workspace: workspace) { retryPrompt = true }
                    contextSection
                    reviewActions
                } else {
                    ProgressView(Copy.loading).frame(maxWidth: .infinity)
                }
            } else {
                closedSummary
            }
        }
        .padding(14)
        .background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: 14))
        .task(id: item.id) { await loadDetailIfNeeded() }
        .sheet(isPresented: $showRejectSheet) {
            if let candidate {
                RejectSheet(candidate: candidate, reason: $rejectReason) {
                    showRejectSheet = false
                    onReject(candidate, rejectReason)
                } onCancel: {
                    showRejectSheet = false
                }
            }
        }
        .alert(Copy.reviewRetryTitle(item.unitId ?? ""), isPresented: $retryPrompt) {
            Button(Copy.actionCancel, role: .cancel) {}
            Button(Copy.actionReviewAgain) {
                if let unitID = item.unitId { workspace.retryReview(unitID: unitID) }
            }
        } message: {
            if let group = reviewGroup {
                Text(Copy.reviewRetryMessage(findings: group.rows.count, lists: group.candidateIds.count))
            }
        }
    }

    // MARK: Shell

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text("\(item.kindTitle) · \(item.targetLabel)")
                    .font(.subheadline.weight(.semibold))
                Spacer()
                if let unitID = item.unitId {
                    Button(Copy.taskOpenUnit) { onOpenUnit(unitID) }
                        .font(.caption2)
                }
            }
            HStack(spacing: 8) {
                Text(item.createdAt.formattedDateTime).font(.caption2).foregroundStyle(.secondary)
                if !item.modelId.isEmpty {
                    Text(item.modelId).font(.caption2.monospaced()).foregroundStyle(.secondary).lineLimit(1)
                }
                if let unit {
                    Text(unit.statusTitle).font(.caption2).foregroundStyle(unit.isFinished ? .green : .orange)
                }
            }
            if item.foldedReviewCandidateId != nil {
                Text(Copy.taskFoldedReviewHint)
                    .font(.caption2).foregroundStyle(.secondary)
            }
        }
    }

    private var closedSummary: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 10) {
                Text(statusLabel).font(.caption.weight(.semibold)).foregroundStyle(statusColor)
                if let run = item.run {
                    Text(Copy.callsUsedOver(run.callsUsed, run.policy?.maxCalls))
                        .font(.caption2).foregroundStyle(.secondary)
                    Text(Copy.logsTokens(input: run.inputTokens, output: run.outputTokens))
                        .font(.caption2).foregroundStyle(.secondary)
                    Text(Copy.durationSeconds(run.durationMs)).font(.caption2).foregroundStyle(.secondary)
                }
                Spacer()
            }
            if let error = item.error, !error.isEmpty {
                Text(error).font(.caption).foregroundStyle(.red).textSelection(.enabled)
            }
            if let warnings = item.warnings, !warnings.isEmpty {
                ForEach(warnings, id: \.self) { warning in
                    Text(warning).font(.caption2).foregroundStyle(.orange)
                }
            }
        }
    }

    private var statusLabel: String {
        if let candidate, candidate.status != "open" { return Copy.candidateStatus(candidate.status) }
        return item.run?.statusTitle ?? Copy.emptyValue
    }

    private var statusColor: Color {
        switch item.run?.status {
        case "failed": .red
        case "succeeded": .green
        case "cancelled", "expired", "rejected": .secondary
        default: .secondary
        }
    }

    // MARK: Preview dispatch

    @ViewBuilder
    private func preview(_ candidate: Candidate) -> some View {
        if candidate.isSummary && candidate.needsDetail && detail == nil {
            Button {
                Task { await loadDetail(force: true) }
            } label: {
                HStack(spacing: 8) {
                    if loadingDetail { ProgressView() }
                    Text(Copy.candidateLoadContent).font(.footnote)
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 10)
            }
            .buttonStyle(.bordered)
            .accessibilityIdentifier("candidate.load.\(candidate.id)")
        } else if isRevise || isRefineWrite {
            Text(isRevise ? Copy.candidateReviseNote : Copy.candidateRefineNote)
                .font(.caption).foregroundStyle(.secondary)
            ChapterDiffView(base: candidate.baseChapters, revised: candidate.chapters ?? [])
            if isRevise, let group = reviewGroup, !group.addressedRows.isEmpty {
                DisclosureGroup(Copy.reviewFixAddressed(group.addressedRows.count)) {
                    VStack(alignment: .leading, spacing: 4) {
                        ForEach(group.addressedRows) { row in
                            Text((row.finding.severity == "blocking" ? Copy.reviewSeverityBlocking : Copy.reviewSeverityAdvisory) + " · " + row.finding.title)
                                .font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    .padding(.top, 6)
                }
                .font(.caption)
            }
        } else if kind == "outline" {
            outlinePreview(candidate)
        } else if kind == "finalize", let actual = candidate.actualChapters, !actual.isEmpty {
            finalizePreview(candidate, actual: actual)
        } else if kind == "structure_review" {
            structureReviewPreview(candidate)
        } else if isReviewList, let group = reviewGroup {
            ReviewFindingsEditor(group: group, workspace: workspace) { retryPrompt = true }
        } else if let chapters = candidate.chapters, !chapters.isEmpty {
            DisclosureGroup(Copy.contentWrite) {
                Text(chapters.map { "## 第\($0.chapter)章 \($0.title)\n\n\($0.body)" }.joined(separator: "\n\n---\n\n"))
                    .font(.system(.footnote, design: .serif))
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .padding(.top, 6)
            }
            .font(.footnote)
        } else if let replacement = candidate.replacement, !replacement.isEmpty {
            DisclosureGroup(Copy.contentWrite) {
                MarkdownBlocksView(raw: replacement).padding(.top, 6)
            }
            .font(.footnote)
        } else if candidate.planMarkdown != nil || candidate.planSettings != nil {
            planPreview(candidate)
        }
    }

    /// Outline candidates render as a contract, not raw JSON: the author judges
    /// desire/obstacle/choice and the per-chapter beats.
    @ViewBuilder
    private func outlinePreview(_ candidate: Candidate) -> some View {
        if let drafts = candidate.unitOutlines, !drafts.isEmpty {
            ForEach(drafts) { draft in
                if let unitID = draft.unitId {
                    let synthesized = outlineUnit(from: draft, unitID: unitID)
                    UnitContractView(unit: synthesized, book: workspace.book, compact: true)
                } else {
                    Text(draft.prettyJSON).font(.caption.monospaced()).foregroundStyle(.secondary)
                }
            }
            DisclosureGroup(Copy.taskViewOutlineSource) {
                Text(drafts.map(\.prettyJSON).joined(separator: "\n"))
                    .font(.caption2.monospaced())
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .padding(.top, 6)
            }
            .font(.caption)
        }
    }

    /// Builds a UnitInfo-shaped view of the candidate so the contract renderer
    /// can read it. The candidate's YAML is the source; scalar keys are pulled
    /// from the typed draft.
    private func outlineUnit(from draft: UnitOutlineDraft, unitID: String) -> UnitInfo {
        let existing = workspace.book?.unit(unitID)
        let raw = draft.yamlLike
        return UnitInfo(
            unitId: unitID,
            volume: existing?.volume ?? 0,
            index: existing?.index ?? 0,
            status: existing?.status ?? "proposed",
            title: draft.title ?? existing?.title ?? "",
            chapterFrom: draft.int("chapterFrom") ?? existing?.chapterFrom ?? 0,
            chapterTo: draft.int("chapterTo") ?? existing?.chapterTo ?? 0,
            hasProse: existing?.hasProse ?? false,
            outlinePath: existing?.outlinePath,
            prosePath: existing?.prosePath ?? "",
            chapterTarget: draft.int("chapterChars") ?? existing?.chapterTarget,
            outlineRaw: raw,
            proseRaw: nil
        )
    }

    private func finalizePreview(_ candidate: Candidate, actual: [ChapterActual]) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(Copy.candidateFinalizeNote).font(.caption).foregroundStyle(.secondary)
            let unverified = candidate.unverifiedEvidence
            if !unverified.isEmpty {
                Text(Copy.candidateUnverified(unverified.count)).font(.caption).foregroundStyle(.orange)
            }
            ForEach(actual) { chapter in
                DisclosureGroup {
                    VStack(alignment: .leading, spacing: 6) {
                        ForEach(chapter.stateDeltas ?? [], id: \.self) { delta in
                            let flagged = unverified.contains { $0.evidence == delta.evidence || ($0.anchor.map { delta.evidence.contains($0) } ?? false) }
                            VStack(alignment: .leading, spacing: 2) {
                                Text("\(delta.stem) · \(delta.field)：\(delta.from) → \(delta.to)").font(.caption)
                                Text(delta.evidence).font(.caption2).foregroundStyle(flagged ? .orange : .secondary)
                            }
                        }
                        if let plant = chapter.plant, !plant.isEmpty {
                            Text(Copy.candidatePlantActual + plant.joined(separator: "、")).font(.caption).foregroundStyle(.secondary)
                        }
                        if let payoff = chapter.payoff, !payoff.isEmpty {
                            Text(Copy.candidatePayoffActual + payoff.joined(separator: "、")).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    .padding(.top, 6)
                } label: {
                    Text("\(Copy.chapterHeading(chapter.chapter)) · \(chapter.summary)")
                        .font(.footnote.weight(.medium)).lineLimit(2)
                }
            }
        }
    }

    private func structureReviewPreview(_ candidate: Candidate) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(Copy.candidateStructureNote).font(.caption).foregroundStyle(.secondary)
            ForEach(candidate.findings ?? []) { finding in
                VStack(alignment: .leading, spacing: 3) {
                    Text(finding.title).font(.footnote.weight(.medium))
                        .foregroundStyle(finding.severity == "blocking" ? .red : .primary)
                    if let evidence = finding.evidence { Text(evidence).font(.caption).foregroundStyle(.secondary) }
                    if let suggestion = finding.suggestion {
                        Text(Copy.reviewSuggestionPrefix + suggestion).font(.caption).foregroundStyle(Color.accentColor)
                    }
                }
                .padding(.vertical, 3)
            }
        }
    }

    private func planPreview(_ candidate: Candidate) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("\(Copy.planMode(candidate.mode)) · \(Copy.candidatePlanFiles(PlanFileChange.writtenPaths(candidate: candidate).count))")
                .font(.footnote.weight(.medium))
            if candidate.mode == "volume" {
                if let outline = candidate.planMarkdown?["volume_outline"] {
                    outlineDisclosure(title: "\(Copy.candidateVolumeSection) · \(candidate.volumeId ?? "")", raw: outline, protocolName: "volume-outline/v1")
                }
                Text(Copy.candidatePlanVolumeNote).font(.caption).foregroundStyle(.secondary)
            } else {
                if let outline = candidate.planMarkdown?["book_outline"] {
                    outlineDisclosure(title: Copy.candidateOutlineSection, raw: outline, protocolName: "book-outline/v1")
                }
                if let settings = candidate.planSettings, !settings.isEmpty {
                    DisclosureGroup(Copy.candidateSettingsCount(settings.count)) {
                        VStack(alignment: .leading, spacing: 3) {
                            ForEach(settings.keys.sorted(), id: \.self) { path in
                                Text(path).font(.caption.monospaced()).foregroundStyle(.secondary)
                            }
                        }
                        .padding(.top, 6)
                    }
                    .font(.footnote)
                }
            }
            if let preserved = candidate.preservePaths, !preserved.isEmpty {
                Text(Copy.candidatePreserved + preserved.joined(separator: "、")).font(.caption).foregroundStyle(.secondary)
            }
            DisclosureGroup(Copy.candidatePreviewSection) {
                PlanDiffView(candidate: candidate, files: workspace.book?.files ?? [:]).padding(.top, 6)
            }
            .font(.footnote)
        }
        .task { await workspace.ensureFiles() }
    }

    private func outlineDisclosure(title: String, raw: String, protocolName: String) -> some View {
        DisclosureGroup(title) {
            let document = StructuredMarkdownParser.parse(raw, protocolName: protocolName)
            if document.legacy {
                Text(raw).font(.caption.monospaced()).foregroundStyle(.secondary)
                    .textSelection(.enabled).padding(.top, 6)
            } else {
                StructuredOutlineView(document: document, units: nil, readOnly: true).padding(.top, 6)
            }
        }
        .font(.footnote)
    }

    // MARK: Context (rich confirmation)

    @ViewBuilder
    private var contextSection: some View {
        if let unit {
            DisclosureGroup(isExpanded: $showContext) {
                VStack(alignment: .leading, spacing: 10) {
                    if kind == "outline", let seed = parentSeed(unit) {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(Copy.taskParentSeed).font(.caption.weight(.semibold)).foregroundStyle(Color.accentColor)
                            Text(seed).font(.caption).foregroundStyle(.secondary).textSelection(.enabled)
                        }
                    }
                    if kind == "finalize" {
                        gateSummary(unit)
                    }
                    UnitContractView(unit: unitWithRaw(unit), book: workspace.book, compact: true)
                }
                .padding(.top, 6)
            } label: {
                Text(Copy.taskUnitContext).font(.caption.weight(.medium)).foregroundStyle(Color.accentColor)
            }
            .task(id: unit.unitId) { await loadUnitRaw(unit) }
        }
    }

    /// The volume outline's seed for this unit: the parent contract the outline
    /// candidate must inherit from.
    private func parentSeed(_ unit: UnitInfo) -> String? {
        let path = String(format: "outline/volumes/v%02d.md", unit.volume)
        guard let doc = workspace.book?.structuredOutlines?[path]
            ?? workspace.book?.volumes.first(where: { $0.volume == unit.volume })?.structured else { return nil }
        guard let seed = doc.entities.first(where: { $0.kind == "单元" && $0.id == unit.unitId }) else { return nil }
        let keys = ["objective", "obstacle", "attempt", "setback", "choice", "cost", "outcome", "chapter_range"]
        let lines = keys.compactMap { key -> String? in
            let value = seed.value(key)
            return value.isEmpty ? nil : "\(Copy.outlineFieldLabel(key))：\(value)"
        }
        return lines.isEmpty ? nil : lines.joined(separator: "\n")
    }

    private func gateSummary(_ unit: UnitInfo) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(Copy.taskGateSummary).font(.caption.weight(.semibold)).foregroundStyle(Color.accentColor)
            if let group = reviewGroup {
                Text(Copy.reviewGroupMeta(rows: group.rows.count, chapters: group.chapters.count, lists: group.candidateIds.count))
                    .font(.caption2).foregroundStyle(group.blocking > 0 ? .red : .secondary)
            }
            if let continuity = workspace.continuity, continuity.units > 0, let last = continuity.lastUnit {
                Text(Copy.continuityEstablished(lastUnit: last, afterCh: continuity.afterCh, cast: continuity.cast, loops: continuity.openLoops))
                    .font(.caption2).foregroundStyle(.secondary)
            } else {
                Text(Copy.continuityMissing).font(.caption2).foregroundStyle(.secondary)
            }
        }
    }

    @State private var unitRaw: [String: String] = [:]

    private func unitWithRaw(_ unit: UnitInfo) -> UnitInfo {
        guard let raw = unitRaw[unit.unitId], !raw.isEmpty else { return unit }
        return UnitInfo(
            unitId: unit.unitId, volume: unit.volume, index: unit.index, status: unit.status,
            title: unit.title, chapterFrom: unit.chapterFrom, chapterTo: unit.chapterTo,
            hasProse: unit.hasProse, outlinePath: unit.outlinePath, prosePath: unit.prosePath,
            chapterTarget: unit.chapterTarget, outlineRaw: raw, proseRaw: unit.proseRaw
        )
    }

    private func loadUnitRaw(_ unit: UnitInfo) async {
        guard unitRaw[unit.unitId] == nil, let path = unit.outlinePath, !path.isEmpty else { return }
        if let file = try? await workspace.api.file(bookID: workspace.bookID, path: path) {
            unitRaw[unit.unitId] = file.content
        }
    }

    // MARK: Actions

    private func actions(_ candidate: Candidate) -> some View {
        HStack(spacing: 10) {
            if kind == "structure_review",
               (candidate.findings ?? []).contains(where: { $0.severity == "blocking" }) {
                Button(Copy.candidateRepair) { workspace.repairStructure(candidate) }
                    .buttonStyle(.borderedProminent).font(.footnote)
            }
            Spacer()
            Button(Copy.candidateReject, role: .destructive) {
                rejectReason = ""
                showRejectSheet = true
            }
            .buttonStyle(.bordered).font(.footnote)
            .disabled(readOnly)
            Button {
                accepting = true
                Task {
                    await workspace.accept(candidate)
                    accepting = false
                }
            } label: {
                if accepting { ProgressView() }
                else { Text(isRevise ? Copy.candidateAcceptRevise : Copy.candidateAccept) }
            }
            .buttonStyle(.borderedProminent).font(.footnote)
            .disabled(accepting || workspace.busy || readOnly)
            .accessibilityIdentifier("candidate.accept.\(candidate.id)")
        }
    }

    /// A review list's exits are submit-fix (inside the editor) or reject.
    private var reviewActions: some View {
        HStack {
            Spacer()
            Button(Copy.candidateReject, role: .destructive) {
                rejectReason = ""
                showRejectSheet = true
            }
            .buttonStyle(.bordered).font(.footnote)
            .disabled(readOnly)
        }
    }

    // MARK: Detail loading

    private func loadDetailIfNeeded() async {
        guard item.section == .needsAction, let row = item.candidate else { return }
        let cached = workspace.resolvedCandidate(row)
        if !cached.isSummary {
            detail = cached
            return
        }
        // Review lists render from findings already in the slim payload.
        if isReviewList { return }
        guard row.needsDetail else { return }
        await loadDetail(force: false)
    }

    private func loadDetail(force: Bool) async {
        guard let row = item.candidate, !loadingDetail else { return }
        if !force, let cached = detail, !cached.isSummary { return }
        loadingDetail = true
        detail = await workspace.candidateDetail(row.id)
        loadingDetail = false
    }
}

/// Reject with an optional reason (revise/review keep the reason for history).
struct RejectSheet: View {
    let candidate: Candidate
    @Binding var reason: String
    let onConfirm: () -> Void
    let onCancel: () -> Void

    private var isRevise: Bool { candidate.kind == "revise" }
    private var isReview: Bool { candidate.kind == "review" }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(isRevise ? Copy.rejectReviseMessage : isReview ? Copy.rejectReviewMessage : Copy.candidatesEmptyBody)
                        .font(.footnote).foregroundStyle(.secondary)
                } header: {
                    Text(isRevise ? Copy.rejectReviseTitle : isReview ? Copy.rejectReviewTitle : Copy.candidateReject)
                }
                if isRevise || isReview {
                    Section(Copy.rejectReasonTitle) {
                        TextField(Copy.rejectReasonHint, text: $reason, axis: .vertical)
                            .lineLimit(2...5)
                    }
                }
            }
            .navigationTitle(Copy.candidateReject)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(Copy.actionCancel, action: onCancel) }
                ToolbarItem(placement: .confirmationAction) {
                    Button(Copy.rejectConfirm, role: .destructive, action: onConfirm)
                }
            }
        }
        .presentationDetents([.medium])
    }
}

extension String {
    /// ISO8601 timestamps from the Go core → short local display.
    var formattedDateTime: String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = formatter.date(from: self) {
            return date.formatted(date: .abbreviated, time: .shortened)
        }
        formatter.formatOptions = [.withInternetDateTime]
        if let date = formatter.date(from: self) {
            return date.formatted(date: .abbreviated, time: .shortened)
        }
        return self
    }
}
