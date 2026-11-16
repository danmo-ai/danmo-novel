import SwiftUI

/// The review-list step of a unit's pipeline: pick fixable findings and submit
/// one targeted revise. Extracted from the old review panel so the task page can
/// render it inside a confirmation card — the picks, chapter filter and
/// default-blocking selection rules are unchanged.
struct ReviewFindingsEditor: View {
    let group: ReviewGroup
    let workspace: BookWorkspaceStore
    /// Re-review the whole unit (archives open lists first).
    let onRetryReview: () -> Void

    /// Picks are per unit and keyed candidate::finding, like the desktop.
    @State private var picked: Set<String> = []
    @State private var chapterFilter: Int?
    @State private var expanded = false

    private var readOnly: Bool { workspace.book?.summary.readOnly ?? false }

    private var visibleRows: [ReviewRow] {
        guard let filter = chapterFilter else { return group.rows }
        return group.rows.filter { $0.finding.chapter == filter }
    }

    private var selectableRows: [ReviewRow] { visibleRows.filter(\.canPick) }

    private var issueIDs: [String] {
        var ids: [String] = []
        for row in group.rows where row.canPick && picked.contains(row.id) {
            if !ids.contains(row.finding.id) { ids.append(row.finding.id) }
        }
        return ids
    }

    /// Latest rejected revise for this unit: the rework must stay visible.
    private var rejectedReason: String? {
        for run in workspace.runs where run.kind == "revise" && run.status == "rejected" {
            if run.spec?.unitId == group.unitId { return run.error ?? Copy.reviewRejectedNoReason }
        }
        return nil
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            summaryBar
            if group.verdict == .unfinished {
                VStack(alignment: .leading, spacing: 6) {
                    Text(Copy.reviewUnfinishedTitle).font(.footnote.weight(.semibold)).foregroundStyle(.orange)
                    Text(Copy.reviewUnfinishedBody).font(.caption).foregroundStyle(.secondary)
                    Button(Copy.actionReviewAgain, action: onRetryReview)
                        .font(.caption)
                        .disabled(workspace.busy || readOnly)
                }
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color.orange.opacity(0.08), in: .rect(cornerRadius: 10))
            }
            if let reason = rejectedReason {
                VStack(alignment: .leading, spacing: 4) {
                    Text(Copy.reviewRejectedTitle).font(.footnote.weight(.semibold)).foregroundStyle(.red)
                    Text(reason).font(.caption).foregroundStyle(.secondary)
                    Text(Copy.reviewRejectedHint).font(.caption2).foregroundStyle(.secondary)
                }
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color.red.opacity(0.05), in: .rect(cornerRadius: 10))
            }

            DisclosureGroup(isExpanded: $expanded) {
                VStack(alignment: .leading, spacing: 4) {
                    listTools
                    ForEach(visibleRows) { row in
                        FindingRowView(
                            row: row,
                            picked: picked.contains(row.id),
                            disabled: workspace.busy || readOnly
                        ) {
                            toggle(row)
                        }
                        Divider()
                    }
                }
                .padding(.top, 6)
            } label: {
                Text(Copy.taskReviewExpandFindings(group.rows.count))
                    .font(.footnote.weight(.medium))
            }

            submitBar
        }
        .task(id: group.rows.map(\.id)) { syncPicks() }
    }

    private var summaryBar: some View {
        HStack(spacing: 8) {
            Text(group.verdict == .blocking ? Copy.reviewBlockingTotal(group.blocking) : group.verdict.label)
                .font(.caption2.weight(.semibold))
                .padding(.horizontal, 8).padding(.vertical, 2)
                .background(verdictColor.opacity(0.12), in: .capsule)
                .foregroundStyle(verdictColor)
            Text(Copy.reviewGroupMeta(rows: group.rows.count, chapters: group.chapters.count, lists: group.candidateIds.count))
                .font(.caption2).foregroundStyle(.secondary)
            Spacer()
        }
    }

    private var verdictColor: Color {
        switch group.verdict {
        case .clean: .green
        case .blocking: .red
        case .unfinished: .orange
        }
    }

    private var listTools: some View {
        HStack(spacing: 10) {
            Button(allLabel) { toggleAll() }
                .font(.caption)
                .disabled(selectableRows.isEmpty || workspace.busy || readOnly)
            if group.chapters.count > 1 {
                Menu {
                    Button(Copy.reviewAllChapters) { chapterFilter = nil }
                    ForEach(group.chapters, id: \.self) { chapter in
                        Button(Copy.chapterHeading(chapter)) { chapterFilter = chapter }
                    }
                } label: {
                    Label(chapterFilter.map { Copy.chapterHeading($0) } ?? Copy.reviewAllChapters,
                          systemImage: "line.3.horizontal.decrease.circle")
                        .font(.caption)
                }
            }
            if group.unfinished > 0 {
                Text(Copy.reviewUnfinishedCount(group.unfinished)).font(.caption2).foregroundStyle(.secondary)
            }
            if group.unitLevel > 0 {
                Text(Copy.reviewUnitLevelCount(group.unitLevel)).font(.caption2).foregroundStyle(.secondary)
            }
            Spacer()
        }
    }

    private var submitBar: some View {
        VStack(alignment: .leading, spacing: 6) {
            // Say what will be submitted before the author commits to a call.
            Text(Copy.taskReviewSubmitSummary(issues: issueIDs.count, chapters: pickedChapterCount))
                .font(.caption2).foregroundStyle(.secondary)
            HStack {
                Spacer()
                Button(Copy.reviewAutoFix) {
                    workspace.reviseFindings(unitID: group.unitId, issueIDs: issueIDs)
                }
                .buttonStyle(.borderedProminent)
                .font(.footnote)
                .disabled(workspace.busy || readOnly || issueIDs.isEmpty)
                .accessibilityIdentifier("review.autofix.\(group.unitId)")
            }
        }
    }

    private var pickedChapterCount: Int {
        let chapters = group.rows
            .filter { $0.canPick && picked.contains($0.id) }
            .compactMap { $0.finding.chapter }
        return Set(chapters).count
    }

    private var allLabel: String {
        let rows = selectableRows
        let allSelected = !rows.isEmpty && rows.allSatisfy { picked.contains($0.id) }
        let scoped = chapterFilter != nil
        if allSelected { return scoped ? Copy.reviewDeselectChapter : Copy.reviewDeselectAll }
        return scoped ? Copy.reviewSelectChapter : Copy.reviewSelectAll
    }

    private func toggle(_ row: ReviewRow) {
        if picked.contains(row.id) { picked.remove(row.id) } else { picked.insert(row.id) }
    }

    private func toggleAll() {
        let rows = selectableRows
        guard !rows.isEmpty else { return }
        if rows.allSatisfy({ picked.contains($0.id) }) {
            for row in rows { picked.remove(row.id) }
        } else {
            for row in rows { picked.insert(row.id) }
        }
    }

    /// Default picks: blocking rows that are actually prose-fixable; stale picks
    /// are dropped when the list changes (desktop watch on candidates).
    private func syncPicks() {
        let valid = Set(group.rows.map(\.id))
        picked = picked.intersection(valid)
        for row in group.rows where row.finding.severity == "blocking" && row.canPick {
            picked.insert(row.id)
        }
        // A long list stays collapsed until the author opens it.
        if group.rows.count <= 6 { expanded = true }
    }
}
