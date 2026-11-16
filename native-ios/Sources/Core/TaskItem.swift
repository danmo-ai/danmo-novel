import Foundation

/// One row of the task page: a run joined with the candidate it produced.
///
/// LLM calls take minutes, so the author's real question is "what is waiting
/// for me", not "what did this unit just produce". The task page answers it by
/// fusing the candidate inbox with the operation log: pending confirmations,
/// in-flight calls and finished/failed history in one timeline.
///
/// This is a pure client-side join — no new endpoint. The slim run list carries
/// `candidateId` and the slim candidate list carries `runId`, and the workspace
/// already fetches both.
struct TaskItem: Identifiable, Hashable {
    enum Section: Int, CaseIterable {
        /// Candidate is open: the author must accept / reject / submit a fix.
        case needsAction
        /// Call still pending or running.
        case running
        /// Terminal: accepted, rejected, expired, cancelled, failed, or a
        /// succeeded run that produced no candidate (e.g. archived review).
        case closed
    }

    let run: Run?
    let candidate: Candidate?
    let section: Section
    /// The unit this row belongs to, when it is unit-scoped.
    let unitId: String?
    /// Set when this row is the active item of a folded review→revise pair.
    let foldedReviewCandidateId: String?

    var id: String { run?.id ?? candidate?.id ?? UUID().uuidString }
    var kind: String { candidate?.kind ?? run?.kind ?? "" }
    var createdAt: String { candidate?.createdAt ?? run?.createdAt ?? "" }
    var modelId: String { run?.modelId ?? "" }
    var error: String? { run?.error }
    var warnings: [String]? { run?.warnings }

    /// Target label: unit, else volume, else the rewritten file, else 全书.
    var targetLabel: String {
        if let unitId, !unitId.isEmpty { return unitId }
        if let target = candidate?.targetPath, !target.isEmpty { return target }
        if let volume = candidate?.volumeId ?? run?.spec?.volumeId, !volume.isEmpty { return volume }
        return Copy.runTargetWholeBook
    }

    var kindTitle: String {
        if kind == "plan" { return Copy.planMode(candidate?.mode ?? run?.spec?.mode) }
        if kind == "write" {
            let refine = (candidate?.baseChapters?.isEmpty == false)
                || (candidate?.hasBaseChapters ?? false)
                || run?.spec?.rewriteMode == "refine"
            if refine { return Copy.kindWriteRefine }
        }
        return Copy.runKind(kind)
    }

    /// True when confirming this row needs the candidate's full payloads.
    var needsCandidateDetail: Bool { candidate?.needsDetail ?? false }

    static func build(candidates: [Candidate], runs: [Run], activeRun: Run?) -> [TaskItem] {
        let candidateByRunId = Dictionary(
            candidates.compactMap { candidate -> (String, Candidate)? in
                guard !candidate.runId.isEmpty else { return nil }
                return (candidate.runId, candidate)
            },
            uniquingKeysWith: { first, _ in first }
        )
        var claimedCandidateIds: Set<String> = []
        var items: [TaskItem] = []

        // Runs are the primary table: one row per request, including the ones
        // that failed and produced nothing.
        for run in runs {
            let candidate = run.candidateId.flatMap { id in candidates.first { $0.id == id } }
                ?? candidateByRunId[run.id]
            if let candidate { claimedCandidateIds.insert(candidate.id) }
            items.append(TaskItem(
                run: run,
                candidate: candidate,
                section: section(run: run, candidate: candidate),
                unitId: unitId(run: run, candidate: candidate),
                foldedReviewCandidateId: nil
            ))
        }

        // Orphan candidates: the run fell outside the list's limit window, but
        // an open candidate is still a pending decision and must not vanish.
        for candidate in candidates where !claimedCandidateIds.contains(candidate.id) {
            items.append(TaskItem(
                run: nil,
                candidate: candidate,
                section: candidate.status == "open" ? .needsAction : .closed,
                unitId: unitId(run: nil, candidate: candidate),
                foldedReviewCandidateId: nil
            ))
        }

        // The live run may be newer than the fetched list.
        if let activeRun, !items.contains(where: { $0.run?.id == activeRun.id }) {
            let candidate = activeRun.candidateId.flatMap { id in candidates.first { $0.id == id } }
            items.append(TaskItem(
                run: activeRun,
                candidate: candidate,
                section: section(run: activeRun, candidate: candidate),
                unitId: unitId(run: activeRun, candidate: candidate),
                foldedReviewCandidateId: nil
            ))
        }

        return foldReviewPairs(items)
    }

    /// A unit's review list stays open while its revise runs — the list *is* the
    /// revise's input, so `blockingRun` deliberately allows the pair. Visually
    /// that must still read as one thing per unit: drop the review row and mark
    /// the revise row as carrying it.
    private static func foldReviewPairs(_ items: [TaskItem]) -> [TaskItem] {
        var reviseByUnit: [String: TaskItem] = [:]
        for item in items where item.kind == "revise" && item.section != .closed {
            guard let unitId = item.unitId, !unitId.isEmpty else { continue }
            if let existing = reviseByUnit[unitId] {
                if item.createdAt > existing.createdAt { reviseByUnit[unitId] = item }
            } else {
                reviseByUnit[unitId] = item
            }
        }
        guard !reviseByUnit.isEmpty else { return items }

        var reviewIdByUnit: [String: String] = [:]
        for item in items where item.kind == "review" && item.section == .needsAction {
            guard let unitId = item.unitId, reviseByUnit[unitId] != nil,
                  let candidateId = item.candidate?.id else { continue }
            reviewIdByUnit[unitId] = candidateId
        }

        return items.compactMap { item in
            // Hide the folded review row.
            if item.kind == "review", let unitId = item.unitId,
               reviewIdByUnit[unitId] == item.candidate?.id {
                return nil
            }
            // Mark the revise row that carries it.
            if item.kind == "revise", let unitId = item.unitId,
               reviseByUnit[unitId]?.id == item.id,
               let reviewId = reviewIdByUnit[unitId] {
                return TaskItem(
                    run: item.run, candidate: item.candidate, section: item.section,
                    unitId: item.unitId, foldedReviewCandidateId: reviewId
                )
            }
            return item
        }
    }

    private static func section(run: Run?, candidate: Candidate?) -> Section {
        if let candidate, candidate.status == "open" { return .needsAction }
        if let run, run.status == "pending" || run.status == "running" { return .running }
        return .closed
    }

    private static func unitId(run: Run?, candidate: Candidate?) -> String? {
        if let unit = candidate?.unitId, !unit.isEmpty { return unit }
        if let unit = run?.spec?.unitId, !unit.isEmpty { return unit }
        return nil
    }

    /// needsAction oldest-first (the longest-waiting decision leads), running
    /// next, closed newest-first.
    static func sorted(_ items: [TaskItem]) -> [TaskItem] {
        items.sorted { lhs, rhs in
            if lhs.section != rhs.section { return lhs.section.rawValue < rhs.section.rawValue }
            switch lhs.section {
            case .needsAction: return lhs.createdAt < rhs.createdAt
            case .running, .closed: return lhs.createdAt > rhs.createdAt
            }
        }
    }
}
