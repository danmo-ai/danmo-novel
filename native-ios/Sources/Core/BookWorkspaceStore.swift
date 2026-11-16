import Foundation
import Observation

/// Shares one workspace store per open book between the book view and pushed
/// unit/reader destinations. The full snapshot is megabytes; a second fetch
/// per push would double both memory and latency. Only the current book is
/// kept — opening another book evicts the previous workspace.
@MainActor
@Observable
final class WorkspaceRegistry {
    private let api: NovelAPIClient
    private var current: (bookID: String, store: BookWorkspaceStore)?

    init(api: NovelAPIClient) { self.api = api }

    func workspace(bookID: String) -> BookWorkspaceStore {
        if let current, current.bookID == bookID { return current.store }
        let store = BookWorkspaceStore(api: api)
        current = (bookID, store)
        return store
    }
}

/// The book workspace's data and actions — the mobile counterpart of the
/// desktop's `useBookWorkspace` + app store. Views decide layout; every rule
/// about runs, candidates and the ledger lives here.
@MainActor
@Observable
final class BookWorkspaceStore {
    /// The task page replaced both the review tab and the candidate tab: every
    /// result is confirmed in one place, fused with the operation log.
    enum Tab: String, CaseIterable, Identifiable {
        case write, outline, bible, tasks
        var id: String { rawValue }
        var title: String {
            switch self {
            case .write: Copy.contentWrite
            case .outline: Copy.contentOutline
            case .bible: Copy.contentBible
            case .tasks: Copy.contentTasks
            }
        }
    }

    let api: NovelAPIClient
    private(set) var bookID: String = ""

    var book: BookSnapshot?
    var primary: PrimaryAction?
    var continuity: ContinuitySummary?
    var candidates: [Candidate] = []
    var candidateHistory: [Candidate] = []
    var runs: [Run] = []
    var activeRun: Run?
    var assignments: [ModelAssignment] = []
    var models: [LLMModel] = []

    /// Panel documents (outline/bible) are fetched lazily, only when one of
    /// those tabs is opened. Tracked against the revision they were loaded
    /// for, so a save/accept invalidates them automatically.
    private(set) var filesLoadedRevision: String?
    private var filesTask: Task<Void, Never>?
    /// Review archive is fetched on first expand of the history section.
    private(set) var historyLoaded = false
    private var historyTask: Task<Void, Never>?
    /// Full candidate payloads, fetched when a card is expanded. Keyed by id;
    /// an entry is dropped when the candidate leaves the open list.
    private var candidateDetails: [String: Candidate] = [:]
    private var detailTasks: [String: Task<Void, Never>] = [:]

    var tab: Tab = .write
    var selectedUnitID: String?
    var isLoading = false
    var busy = false
    var saving = false
    var error: String?
    var toast: String?

    /// The run confirmation dialog request (nil = closed).
    var confirm: RunConfirmRequest?
    /// The live run progress sheet.
    var showRunProgress = false
    /// Finalize accept blocked by local gates: explicit override prompt.
    var forceAcceptPrompt: ForceAcceptPrompt?

    private var pollTask: Task<Void, Never>?
    private var toastTask: Task<Void, Never>?

    init(api: NovelAPIClient) { self.api = api }

    struct RunConfirmRequest: Identifiable {
        let id = UUID()
        var spec: TaskSpec
        var title: String
    }

    struct ForceAcceptPrompt: Identifiable {
        let id = UUID()
        let candidate: Candidate
        let message: String
    }

    // MARK: Derived

    var selectedUnit: UnitInfo? {
        guard let selectedUnitID else { return nil }
        return book?.unit(selectedUnitID)
    }

    var hasBookOutline: Bool {
        !(book?.file("outline/book_outline.md") ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    /// The task page's timeline: runs joined with their candidates, partitioned
    /// into pending / running / history, with a unit's review→revise pair folded
    /// into one row.
    var taskItems: [TaskItem] {
        TaskItem.sorted(TaskItem.build(candidates: candidates, runs: runs, activeRun: activeRun))
    }

    var pendingTaskCount: Int { taskItems.filter { $0.section == .needsAction }.count }

    /// Pending decisions for one unit, for the unit page's pointer row.
    func pendingTasks(unitID: String) -> [TaskItem] {
        taskItems.filter { $0.section == .needsAction && $0.unitId == unitID }
    }

    var reviewGroups: [ReviewGroup] {
        ReviewGroup.build(candidates: candidates, runs: runs, activeRun: activeRun)
    }

    struct VolumeGroup: Identifiable {
        let volume: Int
        let units: [UnitInfo]
        let reviewed: Int
        let title: String
        var id: Int { volume }
    }

    var volumeGroups: [VolumeGroup] {
        guard let book else { return [] }
        var groups: [VolumeGroup] = []
        for unit in book.units {
            if let index = groups.firstIndex(where: { $0.volume == unit.volume }) {
                let group = groups[index]
                groups[index] = VolumeGroup(
                    volume: group.volume,
                    units: group.units + [unit],
                    reviewed: group.reviewed + (unit.isFinished ? 1 : 0),
                    title: group.title
                )
            } else {
                let title = book.volumes.first { $0.volume == unit.volume }?.title ?? ""
                groups.append(VolumeGroup(volume: unit.volume, units: [unit], reviewed: unit.isFinished ? 1 : 0, title: title))
            }
        }
        return groups.sorted { $0.volume < $1.volume }
    }

    /// The "next step" banner, mirroring the desktop primaryInfo (the deferred
    /// idea/init flow is reported, not offered).
    var primaryInfo: (label: String, description: String, target: String, enabled: Bool) {
        guard let primary else { return (Copy.primaryContinue, Copy.primarySelectNext, "", false) }
        let target = primary.unit ?? Copy.volumeTitle(primary.volume ?? 1)
        switch primary.kind {
        case "outline":
            return (Copy.primaryOutline(target), Copy.primaryOutlineDesc, target, true)
        case "write":
            return (Copy.primaryWrite(target), Copy.primaryWriteDesc, target, true)
        case "review":
            return (Copy.primaryReview(target), Copy.primaryReviewDesc, target, true)
        case "finalize":
            return (Copy.primaryFinalize(target), Copy.primaryFinalizeDesc, target, true)
        case "plan":
            if hasBookOutline {
                return (Copy.primaryVolumePlan(primary.volume ?? 1), Copy.primaryVolumePlanDesc, target, true)
            }
            return (Copy.primaryPlanDeferred, Copy.primaryPlanDeferredDesc, Copy.primaryWholeBook, false)
        case "idea":
            return (Copy.primaryPlanDeferred, Copy.primaryPlanDeferredDesc, Copy.primaryWholeBook, false)
        default:
            return (Copy.primaryContinue, Copy.primarySelectNext, target, false)
        }
    }

    func defaultModelFor(_ role: String) -> String {
        let key = (role == "plan" || role == "rewrite_entity") ? "plan" : role
        if let assignment = assignments.first(where: { $0.role == key }), !assignment.modelId.isEmpty {
            return assignment.modelId
        }
        return models.first?.id ?? ""
    }

    // MARK: Loading (three tiers: skeleton → panel files → detail on demand)

    func open(bookID: String) async {
        let sameBook = self.bookID == bookID && book != nil
        self.bookID = bookID
        if !sameBook {
            book = nil
            candidates = []
            candidateHistory = []
            runs = []
            activeRun = nil
            selectedUnitID = nil
            tab = .write
            filesLoadedRevision = nil
            historyLoaded = false
            candidateDetails = [:]
        }
        await reload()
        if selectedUnitID == nil || book?.unit(selectedUnitID ?? "") == nil {
            selectedUnitID = book?.units.first?.unitId
        }
        if assignments.isEmpty && models.isEmpty {
            await loadLLM()
        }
    }

    /// Tier 1: the navigation skeleton plus summary-level activity. No panel
    /// documents, no candidate payloads, no archive — those arrive only when
    /// a view actually needs them.
    func reload() async {
        isLoading = true
        defer { isLoading = false }
        do {
            let response = try await api.bookSummary(id: bookID)
            applySkeleton(response)
            error = nil
            async let openCandidates = api.candidates(bookID: bookID)
            async let bookRuns = api.runs(bookID: bookID)
            candidates = (try? await openCandidates) ?? candidates
            runs = (try? await bookRuns) ?? runs
            pruneCandidateDetails()
            // Panel documents already on screen must follow the new revision.
            if filesLoadedRevision != nil { await ensureFiles() }
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func applySkeleton(_ response: BookResponse) {
        // Keep any loaded panel documents across a skeleton refresh; the
        // skeleton itself carries none.
        if let existing = book, filesLoadedRevision == existing.revision {
            var merged = response.book
            merged = BookSnapshot(
                summary: merged.summary,
                stateRaw: existing.stateRaw,
                volumes: merged.volumes.isEmpty ? existing.volumes : merged.volumes,
                units: merged.units,
                cast: merged.cast.map { card in
                    var updated = card
                    if let old = existing.cast.first(where: { $0.stem == card.stem }) { updated.raw = old.raw }
                    return updated
                },
                files: existing.files,
                revision: merged.revision,
                identitySegments: existing.identitySegments,
                structuredOutlines: existing.structuredOutlines
            )
            book = merged
        } else {
            book = response.book
        }
        primary = response.primary
        continuity = response.continuity
    }

    /// Tier 2: panel documents for the outline/bible tabs. Idempotent per
    /// revision; concurrent callers share one request.
    func ensureFiles() async {
        guard let book else { return }
        if filesLoadedRevision == book.revision { return }
        if let filesTask {
            await filesTask.value
            return
        }
        let task = Task<Void, Never> { [weak self] in
            guard let self else { return }
            do {
                let response = try await self.api.bookPanelFiles(id: self.bookID)
                // Only adopt when it matches the skeleton on screen; otherwise
                // the next ensureFiles call re-fetches against the new revision.
                guard self.book?.revision == response.book.revision else { return }
                self.book = response.book
                self.primary = response.primary
                self.continuity = response.continuity
                self.filesLoadedRevision = response.book.revision
            } catch {
                self.error = error.localizedDescription
            }
        }
        filesTask = task
        await task.value
        filesTask = nil
    }

    var filesReady: Bool {
        guard let book else { return false }
        return filesLoadedRevision == book.revision
    }

    /// Refresh candidates/runs after a mutation without re-reading the book.
    /// The archive is only refreshed if it was already loaded.
    func refreshActivity() async {
        async let openCandidates = api.candidates(bookID: bookID)
        async let bookRuns = api.runs(bookID: bookID)
        candidates = (try? await openCandidates) ?? candidates
        runs = (try? await bookRuns) ?? runs
        pruneCandidateDetails()
        if historyLoaded { await loadHistory() }
    }

    /// Tier 3 (archive): fetched on first expand of the review history.
    func loadHistory() async {
        if let historyTask {
            await historyTask.value
            return
        }
        let task = Task<Void, Never> { [weak self] in
            guard let self else { return }
            self.candidateHistory = (try? await self.api.candidateHistory(bookID: self.bookID, kind: "review")) ?? self.candidateHistory
            self.historyLoaded = true
        }
        historyTask = task
        await task.value
        historyTask = nil
    }

    /// Tier 3 (candidate detail): full payloads for one card, cached until the
    /// candidate leaves the open list. Summary rows render without it.
    func candidateDetail(_ id: String) async -> Candidate? {
        if let cached = candidateDetails[id] { return cached }
        if let task = detailTasks[id] {
            await task.value
            return candidateDetails[id]
        }
        let task = Task<Void, Never> { [weak self] in
            guard let self else { return }
            if let full = try? await self.api.candidate(id: id) {
                self.candidateDetails[id] = full
            }
        }
        detailTasks[id] = task
        await task.value
        detailTasks[id] = nil
        return candidateDetails[id]
    }

    /// The renderable candidate: full detail when loaded, summary row otherwise.
    func resolvedCandidate(_ candidate: Candidate) -> Candidate {
        candidateDetails[candidate.id] ?? candidate
    }

    private func pruneCandidateDetails() {
        let openIDs = Set(candidates.map(\.id))
        candidateDetails = candidateDetails.filter { openIDs.contains($0.key) }
    }

    func loadLLM() async {
        async let settings = api.settings()
        async let llmModels = api.llmModels()
        assignments = (try? await settings)?.modelAssignments ?? []
        models = (try? await llmModels) ?? []
    }

    func note(_ message: String) {
        toast = message
        toastTask?.cancel()
        toastTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(4))
            guard !Task.isCancelled else { return }
            if self?.toast == message { self?.toast = nil }
        }
    }

    func fail(_ error: Error) { self.error = error.localizedDescription }

    // MARK: Run construction (opens the confirm dialog)

    private func volumeTag(_ volume: Int) -> String { String(format: "v%02d", volume) }

    /// Generic unit-scoped task. Review always supersedes old lists (the
    /// desktop does the same; the confirm dialog spells out the consequence).
    func start(kind: String, unit: UnitInfo? = nil) {
        guard let book, !busy else { return }
        var spec = TaskSpec(kind: kind, bookId: book.summary.id, baseRevision: book.revision)
        let target = unit ?? selectedUnit
        if let target, kind != "plan" && kind != "idea" {
            spec.unitId = target.unitId
            if target.volume > 0 { spec.volumeId = volumeTag(target.volume) }
        } else if kind == "outline", let volume = primary?.volume {
            spec.volumeId = volumeTag(volume)
        }
        if kind == "review" { spec.supersedeReview = true }
        let targetLabel = target.map { "\($0.unitId) \($0.title)" } ?? Copy.volumeTitle(primary?.volume ?? 1)
        confirm = RunConfirmRequest(
            spec: spec,
            title: "\(Copy.runKind(kind)) · \(targetLabel.trimmingCharacters(in: .whitespaces))"
        )
    }

    func startPlanVolume(_ volume: Int) {
        guard let book, !busy else { return }
        var spec = TaskSpec(kind: "plan", bookId: book.summary.id, baseRevision: book.revision)
        spec.mode = "volume"
        spec.volumeId = volumeTag(volume)
        let verb = (book.volumes.isEmpty ? Copy.actionOutline : Copy.outlineRewriteVolume)
        confirm = RunConfirmRequest(spec: spec, title: "\(verb) · \(Copy.volumeTitle(volume))")
    }

    func startRewrite(targetPath: String, entityKind: String, title: String) {
        guard let book, !busy else { return }
        var spec = TaskSpec(kind: "rewrite_entity", bookId: book.summary.id, baseRevision: book.revision)
        spec.targetPath = targetPath
        spec.entityKind = entityKind
        let label = entityKind == "cast" ? Copy.bibleOptimizeCast : Copy.bibleOptimize
        confirm = RunConfirmRequest(spec: spec, title: "\(label) · \(title)")
    }

    func startStructureReview(path: String) {
        guard let book, !busy else { return }
        var spec = TaskSpec(kind: "structure_review", bookId: book.summary.id, baseRevision: book.revision)
        spec.targetPath = path
        confirm = RunConfirmRequest(spec: spec, title: "\(Copy.outlineStructureReview) · \(path)")
    }

    func repairStructure(_ candidate: Candidate) {
        guard let book, let path = candidate.targetPath, !busy else { return }
        var spec = TaskSpec(kind: "rewrite_entity", bookId: book.summary.id, baseRevision: book.revision)
        spec.targetPath = path
        spec.entityKind = "structure_repair"
        spec.volumeId = candidate.volumeId
        spec.issueIds = (candidate.findings ?? []).filter { $0.severity == "blocking" }.map(\.id)
        confirm = RunConfirmRequest(spec: spec, title: "\(Copy.candidateRepair) · \(path)")
    }

    /// The banner's next step.
    func primaryAction() {
        guard let primary else { return }
        if let unitID = primary.unit, let unit = book?.unit(unitID) {
            selectedUnitID = unit.unitId
        }
        switch primary.kind {
        case "plan":
            if hasBookOutline { startPlanVolume(primary.volume ?? 1) }
        case "outline", "write", "review", "finalize":
            start(kind: primary.kind, unit: selectedUnit)
        default:
            break
        }
    }

    /// Unit flow buttons only start the call. The result is confirmed on the
    /// task page, so no tab jump happens here — the author is not sent to an
    /// empty surface while the call runs for minutes.
    func runUnitAction(_ kind: String, unit: UnitInfo) {
        selectedUnitID = unit.unitId
        start(kind: kind, unit: unit)
    }

    // MARK: Run execution + polling

    func confirmRun(instruction: String, knowledgeIds: [String], rewriteMode: String?) {
        guard var request = confirm, !busy else { return }
        confirm = nil
        if !instruction.isEmpty { request.spec.instruction = instruction }
        if !knowledgeIds.isEmpty { request.spec.knowledgeIds = knowledgeIds }
        if let rewriteMode { request.spec.rewriteMode = rewriteMode }
        Task { await launch(request.spec) }
    }

    /// Targeted revise from picked review findings (no confirm dialog: the
    /// desktop starts it directly, the product is still a candidate).
    func reviseFindings(unitID: String, issueIDs: [String]) {
        guard let book, !busy, !issueIDs.isEmpty else { return }
        selectedUnitID = unitID
        var spec = TaskSpec(kind: "revise", bookId: book.summary.id, baseRevision: book.revision)
        spec.unitId = unitID
        spec.issueIds = issueIDs
        Task { await launch(spec, revise: true) }
    }

    /// Re-review a whole unit: archives open lists first (supersedeReview).
    func retryReview(unitID: String) {
        guard let book, !busy else { return }
        var spec = TaskSpec(kind: "review", bookId: book.summary.id, baseRevision: book.revision)
        spec.unitId = unitID
        spec.supersedeReview = true
        Task { await launch(spec) }
    }

    private func launch(_ spec: TaskSpec, revise: Bool = false) async {
        busy = true
        defer { busy = false }
        do {
            let run = try await api.startRun(spec)
            activeRun = run
            showRunProgress = true
            note(Copy.runStarted)
            startPolling(runID: run.id, revise: revise)
        } catch {
            fail(error)
        }
    }

    private func startPolling(runID: String, revise: Bool) {
        pollTask?.cancel()
        pollTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(1200))
                guard let self, !Task.isCancelled else { return }
                do {
                    let run = try await self.api.run(id: runID)
                    self.activeRun = run
                    if run.isTerminated {
                        await self.refreshActivity()
                        if revise {
                            if run.status == "waiting_accept" { self.note(Copy.reviewReviseStarted) }
                            else if run.status == "failed" { self.error = run.error ?? Copy.reviewReviseFailed }
                        }
                        return
                    }
                } catch {
                    self.fail(error)
                    return
                }
            }
        }
    }

    func cancelActiveRun() {
        guard let run = activeRun else { return }
        Task {
            do {
                try await api.cancelRun(id: run.id)
                note(Copy.runCancelled)
            } catch { fail(error) }
        }
    }

    func stopPolling() { pollTask?.cancel() }

    // MARK: Accept / reject

    func accept(_ candidate: Candidate, force: Bool = false) async {
        do {
            _ = try await api.acceptCandidate(id: candidate.id, baseRevision: candidate.baseRevision, forceRiskAccept: force)
            note(Copy.candidateAccepted)
            if candidate.kind == "revise", let unitID = candidate.unitId { selectedUnitID = unitID }
            await reload()
        } catch {
            let message = error.localizedDescription
            // Finalize candidates carry local gate blocks; the server refuses a
            // plain accept. Offer one explicit override instead of a dead end.
            if !force && candidate.kind == "finalize" && (message.contains("blocking") || message.contains("阻断")) {
                forceAcceptPrompt = ForceAcceptPrompt(candidate: candidate, message: message)
                return
            }
            fail(error)
        }
    }

    func reject(_ candidate: Candidate, reason: String) async {
        do {
            try await api.rejectCandidate(id: candidate.id, reason: reason)
            await refreshActivity()
            if candidate.kind == "revise" {
                if let unitID = candidate.unitId { selectedUnitID = unitID }
                // Stay on the task page: the review list is still there to retry.
                note(Copy.rejectReviseDone)
            } else if candidate.kind == "review" {
                note(Copy.rejectReviewDone)
            }
        } catch { fail(error) }
    }

    func rejectUnitFindings(unitID: String, candidateIDs: [String], reason: String) async {
        guard !candidateIDs.isEmpty else { return }
        busy = true
        defer { busy = false }
        do {
            for id in candidateIDs { try await api.rejectCandidate(id: id, reason: reason) }
            await refreshActivity()
            note(Copy.rejectReviewDone)
        } catch { fail(error) }
    }

    // MARK: File edits

    func saveFile(path: String, content: String) async -> Bool {
        guard let book, !path.isEmpty else { return false }
        saving = true
        defer { saving = false }
        do {
            try await api.saveFile(bookID: book.summary.id, path: path, content: content, baseRevision: book.revision)
            note(Copy.saved)
            await reload()
            return true
        } catch {
            fail(error)
            return false
        }
    }

    func saveOutlineField(path: String, kind: String, entityID: String, field: String, value: String) async -> Bool {
        guard let book else { return false }
        saving = true
        defer { saving = false }
        do {
            _ = try await api.saveOutlineField(bookID: book.summary.id, path: path, kind: kind, entityID: entityID, field: field, value: value, baseRevision: book.revision)
            note(Copy.outlineFieldSaved)
            await reload()
            return true
        } catch {
            fail(error)
            return false
        }
    }

    func enableWrite() async {
        do {
            try await api.enableWrite(bookID: bookID)
            await reload()
            note(Copy.enableWriteDone)
        } catch { fail(error) }
    }

    func scaffoldMissing() async {
        do {
            try await api.scaffoldMissing(bookID: bookID)
            await reload()
            note(Copy.bibleScaffoldDone)
        } catch { fail(error) }
    }
}