import SwiftUI

/// The task page: the book's single confirmation surface. Pending decisions,
/// in-flight calls and finished history are one timeline, because LLM calls take
/// minutes and the author returns asking "what is waiting for me".
///
/// Every kind is confirmed here — outline, prose, review list, revise, finalize,
/// volume plan, entity rewrite — with the same card shell. The unit page keeps
/// production and reading; it no longer owns confirmation.
struct TaskListView: View {
    let workspace: BookWorkspaceStore
    let openUnit: (String) -> Void

    @State private var showHistory = false
    @State private var rejecting: Candidate?
    @State private var rejectReason = ""

    private var items: [TaskItem] { workspace.taskItems }
    private var needsAction: [TaskItem] { items.filter { $0.section == .needsAction } }
    private var running: [TaskItem] { items.filter { $0.section == .running } }
    private var closed: [TaskItem] { items.filter { $0.section == .closed } }

    var body: some View {
        Group {
            if items.isEmpty {
                ContentUnavailableView(
                    Copy.taskEmptyTitle,
                    systemImage: "checklist",
                    description: Text(Copy.taskEmptyBody)
                )
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 14) {
                        overviewBar
                        if needsAction.isEmpty && running.isEmpty {
                            Text(Copy.taskNothingPending)
                                .font(.footnote).foregroundStyle(.secondary)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .padding(12)
                                .background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: 12))
                        }
                        if !needsAction.isEmpty {
                            sectionHeader(Copy.taskSectionNeedsAction(needsAction.count))
                            ForEach(needsAction) { item in
                                TaskConfirmCard(
                                    item: item,
                                    workspace: workspace,
                                    onReject: { candidate, reason in
                                        Task { await workspace.reject(candidate, reason: reason) }
                                    },
                                    onOpenUnit: openUnit
                                )
                            }
                        }
                        if !running.isEmpty {
                            sectionHeader(Copy.taskSectionRunning(running.count))
                            ForEach(running) { item in
                                TaskConfirmCard(
                                    item: item, workspace: workspace,
                                    onReject: { _, _ in }, onOpenUnit: openUnit
                                )
                            }
                        }
                        if !closed.isEmpty {
                            historySection
                        }
                    }
                    .padding(16)
                    .frame(maxWidth: 860)
                    .frame(maxWidth: .infinity)
                }
                .background(Color(.systemGroupedBackground))
                .refreshable { await workspace.refreshActivity() }
            }
        }
        .task { await workspace.refreshActivity() }
    }

    /// The old review tab's book-level stats keep a home here as one line, so
    /// removing that tab does not lose the quality overview.
    private var overviewBar: some View {
        let groups = workspace.reviewGroups
        let blocking = groups.reduce(0) { $0 + $1.blocking }
        let findings = groups.reduce(0) { $0 + $1.rows.count }
        return VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 12) {
                Text(Copy.taskTitle).font(.headline)
                Spacer()
                if findings > 0 {
                    Text(Copy.reviewBlockingTotal(blocking))
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(blocking > 0 ? .red : .secondary)
                    Text(Copy.reviewFindingsTotal(findings))
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
            if let continuity = workspace.continuity, continuity.units > 0, let last = continuity.lastUnit {
                Text(Copy.continuityEstablished(lastUnit: last, afterCh: continuity.afterCh, cast: continuity.cast, loops: continuity.openLoops))
                    .font(.caption2).foregroundStyle(.secondary)
            } else {
                Text(Copy.continuityMissing).font(.caption2).foregroundStyle(.secondary)
            }
        }
    }

    private func sectionHeader(_ title: String) -> some View {
        Text(title)
            .font(.footnote.weight(.semibold))
            .foregroundStyle(.secondary)
            .padding(.top, 4)
    }

    private var historySection: some View {
        DisclosureGroup(isExpanded: $showHistory) {
            VStack(alignment: .leading, spacing: 10) {
                ForEach(closed.prefix(40)) { item in
                    TaskConfirmCard(
                        item: item, workspace: workspace,
                        onReject: { _, _ in }, onOpenUnit: openUnit
                    )
                }
            }
            .padding(.top, 8)
        } label: {
            Text(Copy.taskSectionHistory(closed.count))
                .font(.footnote.weight(.semibold))
        }
        .padding(12)
        .background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: 12))
    }
}
