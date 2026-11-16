import SwiftUI

/// The book workspace: primary next-step banner + 写作 / 大纲 / 设定 / 审校 /
/// 候选 tabs, mirroring the desktop BookView. This view deliberately owns no
/// NavigationStack: the shelf's navigator pushes unit work and the reader, so
/// one stack governs book → unit → reader in both poses.
struct BookDetailView: View {
    @Environment(LibraryStore.self) private var library
    @Environment(LocalCoreService.self) private var core
    @Environment(WorkspaceRegistry.self) private var registry
    let bookID: String
    /// Appends a route to the shelf's active stack (compact or split detail).
    var push: (ShelfRoute) -> Void = { _ in }
    /// Pops pushed destinations back to this book page (run progress → tasks).
    var popToBook: () -> Void = {}

    @State private var workspace: BookWorkspaceStore?

    var body: some View {
        Group {
            if let workspace {
                content(workspace)
            } else {
                ProgressView(Copy.bookLoading)
            }
        }
        .navigationTitle(workspace?.book?.summary.title ?? library.books.first { $0.id == bookID }?.title ?? Copy.bookFallbackTitle)
        .navigationBarTitleDisplayMode(.large)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Button {
                    Task { await workspace?.reload() }
                } label: {
                    Label(Copy.bookRefresh, systemImage: "arrow.clockwise")
                }
                .disabled(workspace?.isLoading ?? true)
            }
        }
        .task(id: bookID) {
            if workspace == nil {
                workspace = registry.workspace(bookID: bookID)
            }
            await workspace?.open(bookID: bookID)
        }
        .onDisappear { workspace?.stopPolling() }
    }

    @ViewBuilder
    private func content(_ workspace: BookWorkspaceStore) -> some View {
        VStack(spacing: 0) {
            if workspace.book != nil {
                primaryBanner(workspace)
                tabPicker(workspace)
                Divider()
                tabContent(workspace)
            } else if workspace.isLoading {
                Spacer()
                ProgressView(Copy.bookLoading)
                Spacer()
            } else {
                ContentUnavailableView(
                    Copy.bookUnavailable,
                    systemImage: "book.closed",
                    description: Text(workspace.error ?? "")
                )
            }
        }
        .overlay(alignment: .bottom) {
            if let toast = workspace.toast {
                Text(toast)
                    .font(.footnote)
                    .padding(.horizontal, 16).padding(.vertical, 10)
                    .background(.regularMaterial, in: .capsule)
                    .padding(.bottom, 12)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            }
        }
        .animation(.easeInOut(duration: 0.2), value: workspace.toast)
        .sheet(item: Binding(get: { workspace.confirm }, set: { workspace.confirm = $0 })) { request in
            RunConfirmSheet(workspace: workspace, request: request)
        }
        .sheet(isPresented: Binding(get: { workspace.showRunProgress }, set: { workspace.showRunProgress = $0 })) {
            RunProgressSheet(workspace: workspace, popToBook: popToBook)
        }
        .alert(Copy.forceAcceptTitle, isPresented: Binding(
            get: { workspace.forceAcceptPrompt != nil },
            set: { if !$0 { workspace.forceAcceptPrompt = nil } }
        )) {
            Button(Copy.actionCancel, role: .cancel) { workspace.forceAcceptPrompt = nil }
            Button(Copy.forceAcceptAction, role: .destructive) {
                if let prompt = workspace.forceAcceptPrompt {
                    workspace.forceAcceptPrompt = nil
                    Task { await workspace.accept(prompt.candidate, force: true) }
                }
            }
        } message: {
            Text(workspace.forceAcceptPrompt?.message ?? "")
        }
        .workspaceErrorAlert(workspace)
    }

    // MARK: Primary task

    private func primaryBanner(_ workspace: BookWorkspaceStore) -> some View {
        let info = workspace.primaryInfo
        return HStack(alignment: .center, spacing: 12) {
            VStack(alignment: .leading, spacing: 3) {
                Text("\(Copy.primaryKicker) · \(info.target)")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(Color.accentColor)
                Text(info.description)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                if workspace.book?.summary.readOnly == true {
                    Button(Copy.enableWrite) { Task { await workspace.enableWrite() } }
                        .font(.caption)
                }
            }
            Spacer()
            Button {
                workspace.primaryAction()
            } label: {
                if workspace.busy {
                    ProgressView()
                } else {
                    Text(info.label)
                        .font(.footnote.weight(.semibold))
                        .lineLimit(2)
                        .multilineTextAlignment(.center)
                }
            }
            .buttonStyle(.borderedProminent)
            .disabled(workspace.busy || !info.enabled || (workspace.book?.summary.readOnly ?? false))
            .accessibilityIdentifier("book.primary")
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .background(Color.accentColor.opacity(0.05))
    }

    // MARK: Tabs

    private func tabPicker(_ workspace: BookWorkspaceStore) -> some View {
        Picker(Copy.contentPickerLabel, selection: Binding(get: { workspace.tab }, set: { workspace.tab = $0 })) {
            ForEach(BookWorkspaceStore.Tab.allCases) { tab in
                if tab == .tasks && workspace.pendingTaskCount > 0 {
                    Text("\(tab.title) \(workspace.pendingTaskCount)").tag(tab)
                } else {
                    Text(tab.title).tag(tab)
                }
            }
        }
        .pickerStyle(.segmented)
        .padding(.horizontal, 16)
        .padding(.vertical, 8)
        .accessibilityIdentifier("book.tabs")
    }

    @ViewBuilder
    private func tabContent(_ workspace: BookWorkspaceStore) -> some View {
        switch workspace.tab {
        case .write:
            unitList(workspace)
        case .outline:
            OutlinePanelView(workspace: workspace) { unit in
                workspace.selectedUnitID = unit.unitId
                push(.unit(bookID: bookID, unitID: unit.unitId))
            }
        case .bible:
            BiblePanelView(workspace: workspace) { unit in
                workspace.selectedUnitID = unit.unitId
                push(.unit(bookID: bookID, unitID: unit.unitId))
            }
        case .tasks:
            TaskListView(workspace: workspace) { unitID in
                workspace.selectedUnitID = unitID
                push(.unit(bookID: bookID, unitID: unitID))
            }
        }
    }

    // MARK: Write tab = unit list

    @ViewBuilder
    private func unitList(_ workspace: BookWorkspaceStore) -> some View {
        if workspace.book?.units.isEmpty ?? true {
            ContentUnavailableView(
                Copy.bookNoUnits,
                systemImage: "text.page",
                description: Text(Copy.bookNoUnitsBody)
            )
        } else {
            List {
                ForEach(workspace.volumeGroups) { group in
                    Section {
                        ForEach(group.units) { unit in
                            // A value-based NavigationLink: the shelf's stacks
                            // resolve ShelfRoute in both poses, and the system
                            // draws the chevron and row highlight (rule 7).
                            NavigationLink(value: ShelfRoute.unit(bookID: bookID, unitID: unit.unitId)) {
                                UnitRow(unit: unit)
                            }
                            .accessibilityIdentifier("unit.row.\(unit.unitId)")
                        }
                    } header: {
                        HStack {
                            Text(Copy.volumeTitle(group.volume))
                            if !group.title.isEmpty {
                                Text(group.title).font(.caption2).foregroundStyle(.secondary).lineLimit(1)
                            }
                            Spacer()
                            Text(Copy.volumeProgress(group.reviewed, group.units.count))
                                .font(.caption2)
                                .foregroundStyle(.green)
                        }
                    }
                }
            }
            .listStyle(.insetGrouped)
            .refreshable { await workspace.reload() }
        }
    }
}

private struct UnitRow: View {
    let unit: UnitInfo

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack {
                Text(unit.unitId)
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                Spacer()
                if unit.hasProse {
                    Image(systemName: "book.closed")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                Text(unit.statusTitle)
                    .font(.caption)
                    .foregroundStyle(unit.isFinished ? .green : .orange)
            }
            Text(unit.title.isEmpty ? Copy.unitUnnamed : unit.title)
                .font(.headline)
            if unit.chapterFrom > 0 {
                Text(Copy.unitChapters(from: unit.chapterFrom, to: unit.chapterTo))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 5)
        .contentShape(Rectangle())
    }
}