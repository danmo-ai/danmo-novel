import SwiftUI

/// The logs tab: filterable run history with token/cache summaries and
/// per-call detail — the desktop RunsView.
struct RunListView: View {
    @Environment(LocalCoreService.self) private var core
    @Environment(LibraryStore.self) private var library

    @State private var runs: [Run] = []
    @State private var error: String?
    @State private var bookFilter = ""
    @State private var kindFilter = ""
    @State private var modelFilter = ""
    @State private var statusFilter = ""
    @State private var query = ""
    @State private var filtersExpanded = false

    private var api: NovelAPIClient { NovelAPIClient(core: core) }

    private var modelOptions: [String] { Array(Set(runs.map(\.modelId).filter { !$0.isEmpty })).sorted() }
    private var kindOptions: [String] { Array(Set(runs.map(\.kind))).sorted() }
    private var statusOptions: [String] { Array(Set(runs.map(\.status))).sorted() }

    private var filtered: [Run] {
        runs.filter { run in
            if !bookFilter.isEmpty && run.bookId != bookFilter { return false }
            if !kindFilter.isEmpty && run.kind != kindFilter { return false }
            if !modelFilter.isEmpty && run.modelId != modelFilter { return false }
            if !statusFilter.isEmpty && run.status != statusFilter { return false }
            if !query.isEmpty {
                let haystack = "\(run.bookId) \(run.modelId) \(run.spec?.unitId ?? "") \(run.error ?? "")".lowercased()
                if !haystack.contains(query.lowercased()) { return false }
            }
            return true
        }
    }

    private var totals: (calls: Int, input: Int, output: Int, duration: Int64, cache: CacheStat) {
        var calls = 0, input = 0, output = 0
        var duration: Int64 = 0
        var allCalls: [CallRecord] = []
        for run in filtered {
            calls += run.callsUsed
            input += run.inputTokens
            output += run.outputTokens
            duration += run.durationMs
            allCalls.append(contentsOf: run.calls ?? [])
        }
        return (calls, input, output, duration, CacheStat.of(allCalls))
    }

    private var hasFilters: Bool {
        !bookFilter.isEmpty || !kindFilter.isEmpty || !modelFilter.isEmpty || !statusFilter.isEmpty || !query.isEmpty
    }

    var body: some View {
        NavigationStack {
            Group {
                if runs.isEmpty {
                    ContentUnavailableView(
                        Copy.logsEmptyTitle,
                        systemImage: "list.bullet.rectangle",
                        description: Text(Copy.logsEmptyBody)
                    )
                } else {
                    List {
                        Section {
                            summary
                            filterBar
                        }
                        if filtered.isEmpty {
                            Section {
                                VStack(alignment: .leading, spacing: 4) {
                                    Text(Copy.logsNoMatchTitle).font(.subheadline.weight(.medium))
                                    Text(Copy.logsNoMatchBody).font(.caption).foregroundStyle(.secondary)
                                }
                            }
                        }
                        ForEach(filtered) { run in
                            NavigationLink {
                                RunDetailView(run: run, bookTitle: bookTitle(run.bookId), api: api)
                            } label: {
                                RunRow(run: run, bookTitle: bookTitle(run.bookId))
                            }
                            .accessibilityIdentifier("run.row.\(run.id)")
                        }
                    }
                    .refreshable { await load() }
                }
            }
            .navigationTitle(Copy.logsTitle)
            .searchable(text: $query, prompt: Copy.logsSearchHint)
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button { Task { await load() } } label: {
                        Label(Copy.bookRefresh, systemImage: "arrow.clockwise")
                    }
                }
            }
            .task {
                await library.loadBooks()
                await load()
            }
            .alert(Copy.logsLoadFailed, isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
                Button(Copy.actionOK) { error = nil }
            } message: { Text(error ?? "") }
        }
    }

    private var summary: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 16) {
                summaryCell(Copy.logsSummaryTasks, "\(filtered.count)")
                summaryCell(Copy.logsSummaryCalls, "\(totals.calls)")
                summaryCell(Copy.logsSummaryDuration, Copy.durationSeconds(totals.duration))
            }
            HStack(spacing: 16) {
                summaryCell(Copy.logsSummaryTokens, Copy.logsTokens(input: totals.input, output: totals.output))
                summaryCell(Copy.logsSummaryCache, totals.cache.hasData
                    ? "\(totals.cache.read.formatted()) · \(totals.cache.hitRateText)"
                    : Copy.logsCacheNoData)
            }
        }
        .padding(.vertical, 4)
    }

    private func summaryCell(_ label: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label).font(.caption2).foregroundStyle(.secondary)
            Text(value).font(.footnote.weight(.semibold)).lineLimit(1).minimumScaleFactor(0.7)
        }
    }

    private var filterBar: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Label(hasFilters ? Copy.logsFilterReset : Copy.reviewChapterFilter, systemImage: "line.3.horizontal.decrease.circle")
                    .font(.caption)
                Spacer()
                if hasFilters {
                    Button(Copy.logsFilterReset) {
                        bookFilter = ""; kindFilter = ""; modelFilter = ""; statusFilter = ""; query = ""
                    }
                    .font(.caption)
                }
                Button(filtersExpanded ? Copy.actionDone : Copy.actionEdit) { filtersExpanded.toggle() }
                    .font(.caption)
            }
            if filtersExpanded {
                filterMenu(Copy.logsFilterBook, selection: $bookFilter, all: Copy.logsFilterAllBooks,
                           options: library.books.map { ($0.id, $0.title) })
                filterMenu(Copy.logsFilterKind, selection: $kindFilter, all: Copy.logsFilterAllKinds,
                           options: kindOptions.map { ($0, Copy.runKind($0)) })
                filterMenu(Copy.logsFilterModel, selection: $modelFilter, all: Copy.logsFilterAllModels,
                           options: modelOptions.map { ($0, $0) })
                filterMenu(Copy.logsFilterStatus, selection: $statusFilter, all: Copy.logsFilterAllStatuses,
                           options: statusOptions.map { ($0, Copy.runStatus($0)) })
            }
        }
    }

    private func filterMenu(_ label: String, selection: Binding<String>, all: String, options: [(String, String)]) -> some View {
        Menu {
            Button(all) { selection.wrappedValue = "" }
            ForEach(options, id: \.0) { option in
                Button(option.1) { selection.wrappedValue = option.0 }
            }
        } label: {
            HStack {
                Text(label).font(.caption).foregroundStyle(.secondary)
                Spacer()
                Text(options.first { $0.0 == selection.wrappedValue }?.1 ?? all)
                    .font(.caption)
                Image(systemName: "chevron.up.chevron.down").font(.caption2).foregroundStyle(.tertiary)
            }
        }
    }

    private func bookTitle(_ id: String) -> String {
        library.books.first { $0.id == id }?.title ?? id
    }

    private func load() async {
        do { runs = try await api.runs(limit: 200) }
        catch { self.error = error.localizedDescription }
    }
}

private struct RunRow: View {
    let run: Run
    let bookTitle: String

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack {
                Text(bookTitle).font(.subheadline.weight(.medium)).lineLimit(1)
                Spacer()
                Text(run.statusTitle)
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(statusColor)
            }
            HStack(spacing: 6) {
                Text(run.kindTitle).font(.caption)
                Text("·").foregroundStyle(.tertiary)
                Text(run.target).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                Spacer()
                Text(run.createdAt.formattedDateTime).font(.caption2).foregroundStyle(.secondary)
            }
            Text(run.modelId.isEmpty ? Copy.modelUnknown : run.modelId)
                .font(.caption2.monospaced()).foregroundStyle(.secondary).lineLimit(1)
            HStack(spacing: 10) {
                Text(Copy.logsTokens(input: run.inputTokens, output: run.outputTokens))
                    .font(.caption2).foregroundStyle(.secondary)
                Text(Copy.callsUsedOver(run.callsUsed, run.policy?.maxCalls))
                    .font(.caption2).foregroundStyle(.secondary)
                Text(Copy.durationSeconds(run.durationMs))
                    .font(.caption2).foregroundStyle(.secondary)
                if run.cache.hasData {
                    Text("\(Copy.logsSummaryCache) \(run.cache.hitRateText)")
                        .font(.caption2).foregroundStyle(Color.accentColor)
                }
            }
            if let error = run.error, !error.isEmpty {
                Text(error).font(.caption2).foregroundStyle(.red).lineLimit(2)
            }
        }
        .padding(.vertical, 3)
    }

    private var statusColor: Color {
        switch run.status {
        case "failed": .red
        case "waiting_accept": .orange
        case "succeeded": .green
        default: .secondary
        }
    }
}

/// One run's full detail: error, context summary, cache, per-call table,
/// warnings; running runs can be cancelled here.
struct RunDetailView: View {
    let run: Run
    let bookTitle: String
    let api: NovelAPIClient

    @State private var latest: Run?
    @State private var cancelling = false

    private var current: Run { latest ?? run }

    var body: some View {
        List {
            Section {
                LabeledContent(Copy.logsFilterBook, value: bookTitle)
                LabeledContent(Copy.logsFilterKind, value: current.kindTitle)
                LabeledContent(Copy.logsFilterStatus, value: current.statusTitle)
                LabeledContent(Copy.runConfirmModel, value: current.modelId.isEmpty ? Copy.modelUnknown : current.modelId)
                LabeledContent(Copy.runConfirmBudget, value: Copy.callsUsedOver(current.callsUsed, current.policy?.maxCalls))
                LabeledContent(Copy.logsSummaryTokens, value: Copy.logsTokens(input: current.inputTokens, output: current.outputTokens))
                LabeledContent(Copy.logsSummaryDuration, value: Copy.durationSeconds(current.durationMs))
                if current.status == "running" || current.status == "pending" {
                    Button(Copy.runCancel, role: .destructive) {
                        cancelling = true
                        Task {
                            try? await api.cancelRun(id: current.id)
                            latest = try? await api.run(id: current.id)
                            cancelling = false
                        }
                    }
                    .disabled(cancelling)
                }
            }
            if let error = current.error, !error.isEmpty {
                Section(Copy.logsError) {
                    Text(error).font(.footnote).foregroundStyle(.red).textSelection(.enabled)
                }
            }
            if let warnings = current.warnings, !warnings.isEmpty {
                Section(Copy.runWarnings) {
                    ForEach(warnings, id: \.self) { warning in
                        Text(warning).font(.footnote).foregroundStyle(.orange)
                    }
                }
            }
            Section {
                Text(Copy.logsRunID(current.id)).font(.caption2.monospaced())
                Text(Copy.logsBaseRevision(current.spec?.baseRevision ?? "")).font(.caption2.monospaced())
                if let candidate = current.candidateId, !candidate.isEmpty {
                    Text(Copy.logsCandidate(candidate)).font(.caption2.monospaced())
                }
            }
            if let context = current.contextSummary, !context.isEmpty {
                Section(Copy.logsContext) {
                    FlowChips(items: context, tinted: true)
                }
            }
            if current.cache.hasData {
                Section(Copy.logsCache) {
                    Text(Copy.runCacheSummary(read: current.cache.read, rate: current.cache.hitRateText))
                        .font(.footnote)
                    if current.cache.creation > 0 {
                        Text(Copy.runCacheCreation(current.cache.creation)).font(.footnote)
                    }
                    Text(Copy.logsCacheHint).font(.caption2).foregroundStyle(.secondary)
                }
            }
            Section(Copy.logsCalls) {
                if let calls = current.calls, !calls.isEmpty {
                    ForEach(calls) { call in
                        CallRowView(call: call)
                    }
                } else {
                    Text(Copy.logsNoCalls).font(.footnote).foregroundStyle(.secondary)
                }
            }
        }
        .navigationTitle(current.kindTitle)
        .navigationBarTitleDisplayMode(.inline)
        .task {
            // Keep a running run's detail live without a manual refresh.
            latest = try? await api.run(id: run.id)
            while !Task.isCancelled {
                guard let latest, !latest.isTerminated else { return }
                try? await Task.sleep(for: .milliseconds(1500))
                self.latest = (try? await api.run(id: run.id)) ?? latest
            }
        }
    }
}

private struct CallRowView: View {
    let call: CallRecord

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack {
                Text("#\(call.index) · \(call.step)").font(.caption.weight(.semibold))
                Spacer()
                Text(Copy.durationSeconds(call.durationMs)).font(.caption2).foregroundStyle(.secondary)
            }
            Text(call.modelId).font(.caption2.monospaced()).foregroundStyle(.secondary)
            HStack(spacing: 10) {
                Text("in \(call.promptTokens.formatted())").font(.caption2)
                Text("out \(call.completionTokens.formatted())").font(.caption2)
                if let reasoning = call.reasoningTokens, reasoning > 0 {
                    Text("推理 \(reasoning.formatted())").font(.caption2).foregroundStyle(.secondary)
                }
            }
            .foregroundStyle(.secondary)
            HStack(spacing: 10) {
                if let read = call.cacheReadTokens, read > 0 {
                    Text("\(Copy.logsCache) \(Copy.callCacheText(read: read, prompt: call.promptTokens))")
                        .font(.caption2).foregroundStyle(Color.accentColor)
                }
                if let creation = call.cacheCreationTokens, creation > 0 {
                    Text(Copy.runCacheCreation(creation)).font(.caption2).foregroundStyle(.secondary)
                }
                if let finish = call.finishReason, finish == "length" {
                    Text("length").font(.caption2).foregroundStyle(.orange)
                }
            }
            if let error = call.error, !error.isEmpty {
                Text(error).font(.caption2).foregroundStyle(.red)
            }
        }
        .padding(.vertical, 2)
    }
}