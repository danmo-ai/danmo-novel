import Foundation

/// Review verdict rules — must stay identical to `domain.ReviewVerdict` in Go
/// and `reviewState.ts` on the desktop, or the badge and the actual gate would
/// contradict each other.
enum ReviewState {
    /// These two are not opinions: the chapter was not fully reviewed
    /// (budget truncation / failed call). Advisory, but never "clean".
    static func isUnfinished(_ finding: ReviewFinding) -> Bool {
        finding.id == "review-truncated" || finding.id == "review-incomplete"
    }

    /// Fixable by a prose patch: must land on a specific chapter. Unit-level
    /// conclusions (stale-*, identity planning) cannot be fixed by revise and
    /// the server rejects the whole batch if one is included.
    static func isProseFixable(_ finding: ReviewFinding) -> Bool {
        (finding.chapter ?? 0) > 0
    }

    /// Only blocking findings seen by >= 2 independent rounds block. Single
    /// round blocking is unstable; roundsHit == 0 (legacy data, local gates)
    /// keeps the original semantics.
    static func isBlocking(_ finding: ReviewFinding) -> Bool {
        if isUnfinished(finding) || finding.severity != "blocking" { return false }
        let hits = finding.roundsHit ?? 0
        return hits == 0 || hits >= 2
    }

    enum Verdict: String {
        case clean, blocking, unfinished

        var label: String {
            switch self {
            case .clean: Copy.verdictClean
            case .blocking: Copy.verdictBlocking
            case .unfinished: Copy.verdictUnfinished
            }
        }
    }

    static func verdict(_ findings: [ReviewFinding]) -> Verdict {
        var unfinished = false
        for finding in findings {
            if isUnfinished(finding) { unfinished = true; continue }
            if isBlocking(finding) { return .blocking }
        }
        return unfinished ? .unfinished : .clean
    }
}

/// One finding row bound to its candidate list (selection key is per-list).
struct ReviewRow: Identifiable, Hashable {
    let finding: ReviewFinding
    let candidateId: String
    let unitId: String

    var id: String { "\(candidateId)::\(finding.id)" }
    var canPick: Bool { ReviewState.isProseFixable(finding) && !ReviewState.isUnfinished(finding) }
}

/// Per-unit review group, mirroring useReview's groups.
struct ReviewGroup: Identifiable {
    let unitId: String
    let rows: [ReviewRow]
    let candidateIds: [String]
    let revise: Candidate?
    let addressedRows: [ReviewRow]
    let remaining: Int
    let runningRevise: Bool

    var id: String { unitId }
    var blocking: Int { rows.filter { ReviewState.isBlocking($0.finding) }.count }
    var unfinished: Int { rows.filter { ReviewState.isUnfinished($0.finding) }.count }
    var unitLevel: Int { rows.filter { !ReviewState.isProseFixable($0.finding) && !ReviewState.isUnfinished($0.finding) }.count }
    var verdict: ReviewState.Verdict { ReviewState.verdict(rows.map(\.finding)) }
    var chapters: [Int] { Array(Set(rows.compactMap { $0.finding.chapter }.filter { $0 > 0 })).sorted() }

    static func build(candidates: [Candidate], runs: [Run], activeRun: Run?) -> [ReviewGroup] {
        var byUnit: [String: (rows: [ReviewRow], candidateIds: [String])] = [:]
        var order: [String] = []
        for candidate in candidates where candidate.kind == "review" {
            let key = candidate.unitId ?? "未归属"
            if byUnit[key] == nil { byUnit[key] = ([], []); order.append(key) }
            if !(byUnit[key]!.candidateIds.contains(candidate.id)) {
                byUnit[key]!.candidateIds.append(candidate.id)
            }
            for finding in candidate.findings ?? [] {
                byUnit[key]!.rows.append(ReviewRow(finding: finding, candidateId: candidate.id, unitId: key))
            }
        }
        return order.sorted().map { unitId in
            let group = byUnit[unitId]!
            var latest: Candidate?
            for candidate in candidates where candidate.kind == "revise" && candidate.unitId == unitId {
                if latest == nil || candidate.createdAt > latest!.createdAt { latest = candidate }
            }
            let addressed: Set<String> = {
                guard let latest, let run = runs.first(where: { $0.id == latest.runId }) else { return [] }
                return Set(run.spec?.issueIds ?? [])
            }()
            let addressedRows = group.rows.filter { addressed.contains($0.finding.id) }
            let remaining = group.rows.filter { !addressed.contains($0.finding.id) && !ReviewState.isUnfinished($0.finding) }.count
            let running: Bool = {
                guard let activeRun, activeRun.kind == "revise", activeRun.spec?.unitId == unitId else { return false }
                return activeRun.status == "running" || activeRun.status == "pending"
            }()
            return ReviewGroup(
                unitId: unitId,
                rows: group.rows,
                candidateIds: group.candidateIds,
                revise: latest,
                addressedRows: addressedRows,
                remaining: remaining,
                runningRevise: running
            )
        }
    }
}