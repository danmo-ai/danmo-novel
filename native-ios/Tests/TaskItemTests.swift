import XCTest
@testable import DanmoNovelApp

/// The task page fuses runs and candidates client-side. These lock the rules
/// that keep the inbox honest: no pending decision may vanish, and a unit's
/// review→revise pair must read as one thing.
final class TaskItemTests: XCTestCase {
    private func run(
        _ id: String,
        kind: String,
        status: String = "succeeded",
        unit: String? = "v01-U1",
        candidateId: String? = nil,
        createdAt: String = "2026-11-15T10:00:00Z"
    ) -> Run {
        var spec = TaskSpec(kind: kind, bookId: "b", baseRevision: "r1")
        spec.unitId = unit
        return Run(
            id: id, bookId: "b", kind: kind, status: status, spec: spec,
            policy: CallPolicy(maxCalls: 3, timeoutSec: nil, allowOneRepair: nil),
            modelId: "mock/model", callsUsed: 1, calls: nil, candidateId: candidateId,
            contextSummary: nil, error: nil, warnings: nil, createdAt: createdAt, updatedAt: nil,
            summaryInputTokens: 100, summaryOutputTokens: 50, summaryDurationMs: 1000,
            summaryCacheRead: 0, summaryCacheCreation: 0
        )
    }

    private func candidate(
        _ id: String,
        runId: String,
        kind: String,
        status: String = "open",
        unit: String? = "v01-U1",
        createdAt: String = "2026-11-15T10:00:00Z"
    ) -> Candidate {
        Candidate(
            id: id, runId: runId, bookId: "b", kind: kind, status: status, baseRevision: "r1",
            targetPath: nil, unitId: unit, volumeId: nil, chapters: nil, baseChapters: nil,
            unitOutlines: nil, mode: nil, preservePaths: nil, planMarkdown: nil, planSettings: nil,
            actualChapters: nil, replacement: nil, findings: nil, canonChanges: nil,
            createdAt: createdAt, acceptedAt: nil,
            hasChapters: true, hasBaseChapters: false, hasPlan: false,
            hasReplacement: false, hasOutlines: false, hasActual: false
        )
    }

    func testJoinsRunToCandidateAndPartitions() {
        let runs = [
            run("r1", kind: "write", status: "waiting_accept", candidateId: "c1"),
            run("r2", kind: "review", status: "running"),
            run("r3", kind: "outline", status: "failed"),
        ]
        let candidates = [candidate("c1", runId: "r1", kind: "write")]
        let items = TaskItem.build(candidates: candidates, runs: runs, activeRun: nil)

        XCTAssertEqual(items.count, 3)
        let byId = Dictionary(uniqueKeysWithValues: items.map { ($0.id, $0) })
        XCTAssertEqual(byId["r1"]?.section, .needsAction)
        XCTAssertEqual(byId["r1"]?.candidate?.id, "c1")
        XCTAssertEqual(byId["r2"]?.section, .running)
        // A failed run produced nothing, but must still be visible.
        XCTAssertEqual(byId["r3"]?.section, .closed)
        XCTAssertNil(byId["r3"]?.candidate)
    }

    func testOrphanOpenCandidateIsNotLost() {
        // The run fell outside the list's limit window; the decision still stands.
        let candidates = [candidate("c-orphan", runId: "r-gone", kind: "write")]
        let items = TaskItem.build(candidates: candidates, runs: [], activeRun: nil)
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].section, .needsAction)
        XCTAssertEqual(items[0].candidate?.id, "c-orphan")
    }

    func testClosedCandidateIsHistoryNotInbox() {
        let candidates = [candidate("c-rejected", runId: "r-gone", kind: "write", status: "rejected")]
        let items = TaskItem.build(candidates: candidates, runs: [], activeRun: nil)
        XCTAssertEqual(items[0].section, .closed)
    }

    func testReviewAndReviseFoldIntoOneRowPerUnit() {
        // blockingRun deliberately allows this pair: the list is the revise's input.
        let runs = [
            run("r-review", kind: "review", status: "waiting_accept", candidateId: "c-review"),
            run("r-revise", kind: "revise", status: "waiting_accept", candidateId: "c-revise",
                createdAt: "2026-11-15T10:05:00Z"),
        ]
        let candidates = [
            candidate("c-review", runId: "r-review", kind: "review"),
            candidate("c-revise", runId: "r-revise", kind: "revise", createdAt: "2026-11-15T10:05:00Z"),
        ]
        let items = TaskItem.build(candidates: candidates, runs: runs, activeRun: nil)

        XCTAssertEqual(items.count, 1, "review list must fold into the revise row")
        XCTAssertEqual(items[0].kind, "revise")
        XCTAssertEqual(items[0].foldedReviewCandidateId, "c-review")
        XCTAssertEqual(items[0].section, .needsAction)
    }

    func testReviewStaysVisibleWithoutRevise() {
        let runs = [run("r-review", kind: "review", status: "waiting_accept", candidateId: "c-review")]
        let candidates = [candidate("c-review", runId: "r-review", kind: "review")]
        let items = TaskItem.build(candidates: candidates, runs: runs, activeRun: nil)
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].kind, "review")
        XCTAssertNil(items[0].foldedReviewCandidateId)
    }

    func testClosedReviseDoesNotFoldOpenReview() {
        let runs = [
            run("r-review", kind: "review", status: "waiting_accept", candidateId: "c-review"),
            run("r-revise", kind: "revise", status: "rejected", candidateId: "c-revise"),
        ]
        let candidates = [
            candidate("c-review", runId: "r-review", kind: "review"),
            candidate("c-revise", runId: "r-revise", kind: "revise", status: "rejected"),
        ]
        let items = TaskItem.build(candidates: candidates, runs: runs, activeRun: nil)
        XCTAssertEqual(items.count, 2)
        XCTAssertTrue(items.contains { $0.kind == "review" && $0.section == .needsAction })
    }

    func testAtMostOnePendingPerUnit() {
        // The backend locks per unit, so the inbox must never show two pending
        // decisions for one unit.
        let runs = [
            run("r1", kind: "write", status: "waiting_accept", unit: "v01-U1", candidateId: "c1"),
            run("r2", kind: "write", status: "waiting_accept", unit: "v01-U2", candidateId: "c2"),
        ]
        let candidates = [
            candidate("c1", runId: "r1", kind: "write", unit: "v01-U1"),
            candidate("c2", runId: "r2", kind: "write", unit: "v01-U2"),
        ]
        let items = TaskItem.build(candidates: candidates, runs: runs, activeRun: nil)
        let pending = items.filter { $0.section == .needsAction }
        let units = pending.compactMap(\.unitId)
        XCTAssertEqual(Set(units).count, units.count)
    }

    func testSortOrderInboxOldestFirstHistoryNewestFirst() {
        let runs = [
            run("old", kind: "write", status: "waiting_accept", candidateId: "c-old", createdAt: "2026-11-15T09:00:00Z"),
            run("new", kind: "write", status: "waiting_accept", unit: "v01-U2", candidateId: "c-new", createdAt: "2026-11-15T11:00:00Z"),
            run("h-old", kind: "outline", status: "succeeded", createdAt: "2026-11-15T08:00:00Z"),
            run("h-new", kind: "outline", status: "succeeded", createdAt: "2026-11-15T12:00:00Z"),
        ]
        let candidates = [
            candidate("c-old", runId: "old", kind: "write", createdAt: "2026-11-15T09:00:00Z"),
            candidate("c-new", runId: "new", kind: "write", unit: "v01-U2", createdAt: "2026-11-15T11:00:00Z"),
        ]
        let sorted = TaskItem.sorted(TaskItem.build(candidates: candidates, runs: runs, activeRun: nil))
        XCTAssertEqual(sorted.map(\.id), ["old", "new", "h-new", "h-old"])
    }

    func testActiveRunIsIncludedWhenNewerThanList() {
        let live = run("r-live", kind: "write", status: "running")
        let items = TaskItem.build(candidates: [], runs: [], activeRun: live)
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items[0].section, .running)
    }

    func testBookScopedTargetFallsBackToVolumeThenWholeBook() {
        var spec = TaskSpec(kind: "plan", bookId: "b", baseRevision: "r1")
        spec.volumeId = "v02"
        spec.mode = "volume"
        let volumeRun = Run(
            id: "r-plan", bookId: "b", kind: "plan", status: "waiting_accept", spec: spec,
            policy: nil, modelId: "m", callsUsed: 1, calls: nil, candidateId: nil,
            contextSummary: nil, error: nil, warnings: nil, createdAt: "2026-11-15T10:00:00Z", updatedAt: nil,
            summaryInputTokens: nil, summaryOutputTokens: nil, summaryDurationMs: nil,
            summaryCacheRead: nil, summaryCacheCreation: nil
        )
        let items = TaskItem.build(candidates: [], runs: [volumeRun], activeRun: nil)
        XCTAssertEqual(items[0].targetLabel, "v02")
        XCTAssertEqual(items[0].kindTitle, Copy.planMode("volume"))
    }
}
