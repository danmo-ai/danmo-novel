import SwiftUI

/// The knowledge base tab: categorized/filterable article list, structured
/// reading, local-copy editing with stage/genre tags, and built-in restore —
/// the desktop KnowledgeView.
struct KnowledgeView: View {
    @Environment(LocalCoreService.self) private var core
    @Environment(LibraryStore.self) private var library

    @State private var docs: [KnowledgeDoc] = []
    @State private var search = ""
    @State private var category = "all"
    @State private var genreFilter = "all"
    @State private var error: String?
    @State private var loading = false

    private var api: NovelAPIClient { NovelAPIClient(core: core) }

    private var filtered: [KnowledgeDoc] {
        docs.filter { doc in
            if category != "all" && doc.category != category { return false }
            if genreFilter == "common" && !KnowledgeFilter.isCommon(doc) { return false }
            if genreFilter != "all" && genreFilter != "common" && !(doc.genres ?? []).contains(genreFilter) { return false }
            if !search.isEmpty {
                let haystack = "\(doc.title)\(doc.summary)\(doc.id)".lowercased()
                if !haystack.contains(search.lowercased()) { return false }
            }
            return true
        }
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 8) {
                            categoryChip("all", Copy.knowledgeCategoryAll)
                            categoryChip("general", Copy.knowledgeCategoryGeneral)
                            categoryChip("genre", Copy.knowledgeCategoryGenre)
                            categoryChip("custom", Copy.knowledgeCategoryCustom)
                        }
                    }
                    Menu {
                        Button(Copy.knowledgeGenreAll) { genreFilter = "all" }
                        Button(Copy.knowledgeGenreCommon) { genreFilter = "common" }
                        ForEach(Copy.knowledgeGenres, id: \.self) { genre in
                            Button(Copy.knowledgeGenreExclusive(genre)) { genreFilter = genre }
                        }
                    } label: {
                        Label(genreFilterLabel, systemImage: "line.3.horizontal.decrease.circle")
                            .font(.footnote)
                    }
                }
                if filtered.isEmpty {
                    Text(Copy.knowledgeNoMatch)
                        .font(.footnote).foregroundStyle(.secondary)
                }
                ForEach(filtered) { doc in
                    NavigationLink {
                        KnowledgeDetailView(api: api, doc: doc) {
                            Task { await load() }
                        }
                    } label: {
                        KnowledgeRow(doc: doc)
                    }
                    .accessibilityIdentifier("knowledge.row.\(doc.id)")
                }
            }
            .searchable(text: $search, prompt: Copy.knowledgeSearchHint)
            .navigationTitle(Copy.knowledgeTitle)
            .toolbar {
                ToolbarItem(placement: .principal) {
                    VStack(spacing: 0) {
                        Text(Copy.knowledgeTitle).font(.headline)
                        Text(Copy.knowledgeCount(docs.count)).font(.caption2).foregroundStyle(.secondary)
                    }
                }
            }
            .refreshable { await load() }
            .task { await load() }
            .alert(Copy.settingsLoadFailed, isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
                Button(Copy.actionOK) { error = nil }
            } message: { Text(error ?? "") }
        }
    }

    private var genreFilterLabel: String {
        switch genreFilter {
        case "all": Copy.knowledgeGenreAll
        case "common": Copy.knowledgeGenreCommon
        default: Copy.knowledgeGenreExclusive(genreFilter)
        }
    }

    private func categoryChip(_ id: String, _ label: String) -> some View {
        Button {
            category = id
        } label: {
            Text(label)
                .font(.caption.weight(category == id ? .semibold : .regular))
                .padding(.horizontal, 12).padding(.vertical, 5)
                .background(category == id ? Color.accentColor.opacity(0.15) : Color.secondary.opacity(0.08), in: .capsule)
                .foregroundStyle(category == id ? Color.accentColor : .secondary)
        }
        .buttonStyle(.plain)
    }

    private func load() async {
        loading = true
        defer { loading = false }
        do { docs = try await api.knowledgeDocs() }
        catch { self.error = error.localizedDescription }
    }
}

private struct KnowledgeRow: View {
    let doc: KnowledgeDoc

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Text(doc.title).font(.subheadline.weight(.medium))
                if doc.isDefault ?? false {
                    badge(Copy.knowledgeDefaultBadge, color: .red)
                } else if doc.isAuthorDoc {
                    badge(Copy.knowledgeAuthorBadge, color: .secondary)
                }
            }
            Text(doc.summary.isEmpty ? doc.id : doc.summary)
                .font(.caption).foregroundStyle(.secondary).lineLimit(2)
            HStack(spacing: 6) {
                Text(metaLine).font(.caption2).foregroundStyle(Color.accentColor)
                Spacer()
            }
            Text(KnowledgeFilter.stageSummary(doc.stages))
                .font(.caption2)
                .foregroundStyle((doc.stages ?? []).isEmpty ? .orange : .secondary)
            Text("\(Copy.knowledgeGenresLabel) · \(KnowledgeFilter.genreSummary(doc))")
                .font(.caption2).foregroundStyle(.secondary)
        }
        .padding(.vertical, 3)
    }

    private var metaLine: String {
        let categoryText = doc.category == "genre" ? Copy.knowledgeCategoryGenreShort
            : doc.category == "general" ? Copy.knowledgeCategoryMethod : Copy.knowledgeCategoryCustomShort
        let sourceText = doc.customized ? Copy.knowledgeEditedShort : doc.builtin ? Copy.knowledgeBuiltinBadge : Copy.knowledgePersonalShort
        return "\(categoryText) · \(sourceText) · \(String(format: "%.1f", Double(doc.bytes) / 1024))KB"
    }

    private func badge(_ text: String, color: Color) -> some View {
        Text(text)
            .font(.caption2.weight(.semibold))
            .padding(.horizontal, 6).padding(.vertical, 1)
            .background(color.opacity(0.1), in: .capsule)
            .foregroundStyle(color)
    }
}

/// One article: tags, structured body, edit-as-local-copy, restore built-in.
struct KnowledgeDetailView: View {
    let api: NovelAPIClient
    let doc: KnowledgeDoc
    let onChanged: () -> Void

    @State private var content: KnowledgeContent?
    @State private var editing = false
    @State private var draft = ""
    @State private var draftStages: Set<String> = []
    @State private var draftGenres: Set<String> = []
    @State private var draftSubgenres = ""
    @State private var saving = false
    @State private var error: String?
    @State private var restorePrompt = false
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                header
                if let content {
                    stageBar(content)
                    genreBar(content)
                    if editing {
                        TextEditor(text: $draft)
                            .font(.caption.monospaced())
                            .autocorrectionDisabled()
                            .frame(minHeight: 420)
                            .overlay(RoundedRectangle(cornerRadius: 8).stroke(Color.secondary.opacity(0.2)))
                    } else {
                        MarkdownBlocksView(raw: content.content)
                    }
                } else {
                    ProgressView(Copy.loading)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 30)
                }
            }
            .padding(16)
            .frame(maxWidth: 800)
            .frame(maxWidth: .infinity)
        }
        .background(Color(.systemGroupedBackground))
        .navigationTitle(doc.title)
        .navigationBarTitleDisplayMode(.inline)
        .navigationBarBackButtonHidden(editing)
        .toolbar {
            if editing {
                ToolbarItem(placement: .cancellationAction) {
                    Button(Copy.actionCancel) { cancelEdit() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(saving ? Copy.saving : Copy.knowledgeSaveCopy) { Task { await save() } }
                        .disabled(saving)
                }
            } else {
                ToolbarItemGroup(placement: .primaryAction) {
                    if doc.builtin && doc.customized {
                        Button(Copy.knowledgeRestore) { restorePrompt = true }
                            .disabled(saving)
                    }
                    Button(Copy.knowledgeEdit) { beginEdit() }
                        .disabled(content == nil)
                }
            }
        }
        .task { await load() }
        .alert(Copy.knowledgeRestoreTitle, isPresented: $restorePrompt) {
            Button(Copy.actionCancel, role: .cancel) {}
            Button(Copy.knowledgeRestore, role: .destructive) { Task { await restore() } }
        } message: {
            Text(Copy.knowledgeRestoreMessage)
        }
        .alert(Copy.errorOperationFailed, isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
            Button(Copy.actionOK) { error = nil }
        } message: { Text(error ?? "") }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                Text(doc.customized ? Copy.knowledgeEditedBadge : doc.builtin ? Copy.knowledgeBuiltinBadge : Copy.knowledgeCustomBadge)
                    .font(.caption2.weight(.semibold))
                    .padding(.horizontal, 8).padding(.vertical, 2)
                    .background(Color.accentColor.opacity(0.1), in: .capsule)
                    .foregroundStyle(Color.accentColor)
                if doc.isDefault ?? false {
                    Text(Copy.knowledgeDefaultBadge).font(.caption2).foregroundStyle(.red)
                }
                if doc.isAuthorDoc {
                    Text(Copy.knowledgeAuthorBadge).font(.caption2).foregroundStyle(.secondary)
                }
            }
            Text(Copy.knowledgePath(doc.id, customized: doc.customized))
                .font(.caption2.monospaced()).foregroundStyle(.tertiary)
        }
    }

    private func stageBar(_ content: KnowledgeContent) -> some View {
        let stages = editing ? Array(draftStages) : (content.stages ?? [])
        return VStack(alignment: .leading, spacing: 6) {
            Text(Copy.knowledgeStagesLabel).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
            ChipLayout(spacing: 6) {
                ForEach(Copy.knowledgeStages, id: \.id) { stage in
                    tagChip(stage.label, on: stages.contains(stage.id)) {
                        guard editing else { return }
                        if draftStages.contains(stage.id) { draftStages.remove(stage.id) }
                        else { draftStages.insert(stage.id) }
                    }
                }
            }
            if stages.isEmpty {
                Text(Copy.knowledgeStagesUntagged).font(.caption2).foregroundStyle(.orange)
            }
        }
    }

    private func genreBar(_ content: KnowledgeContent) -> some View {
        let genres = editing ? Array(draftGenres) : (content.genres ?? [])
        let subgenres = content.subgenres ?? []
        return VStack(alignment: .leading, spacing: 6) {
            Text(Copy.knowledgeGenresLabel).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
            ChipLayout(spacing: 6) {
                ForEach(Copy.knowledgeGenres, id: \.self) { genre in
                    tagChip(genre, on: genres.contains(genre)) {
                        guard editing else { return }
                        if draftGenres.contains(genre) { draftGenres.remove(genre) }
                        else { draftGenres.insert(genre) }
                    }
                }
            }
            if genres.isEmpty {
                Text(Copy.knowledgeGenresCommon).font(.caption2).foregroundStyle(.secondary)
            } else if !editing {
                Text(Copy.knowledgeGenresRestricted).font(.caption2).foregroundStyle(.secondary)
            }
            if editing {
                TextField(Copy.knowledgeSubgenreLabel + " · " + Copy.knowledgeSubgenreHint, text: $draftSubgenres)
                    .font(.caption)
                    .textFieldStyle(.roundedBorder)
            } else if !subgenres.isEmpty {
                Text(Copy.knowledgeSubgenreOnly(subgenres.joined(separator: "、")))
                    .font(.caption2).foregroundStyle(.secondary)
            }
        }
    }

    private func tagChip(_ label: String, on: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(label)
                .font(.caption2)
                .padding(.horizontal, 10).padding(.vertical, 4)
                .background(on ? Color.accentColor : Color.secondary.opacity(0.08), in: .capsule)
                .foregroundStyle(on ? .white : .secondary)
        }
        .buttonStyle(.plain)
        .disabled(!editing)
    }

    private func load() async {
        do { content = try await api.knowledge(id: doc.id) }
        catch { self.error = error.localizedDescription }
    }

    private func beginEdit() {
        guard let content else { return }
        draft = content.content
        draftStages = Set(content.stages ?? [])
        draftGenres = Set(content.genres ?? [])
        draftSubgenres = (content.subgenres ?? []).joined(separator: "、")
        editing = true
    }

    private func cancelEdit() {
        editing = false
    }

    private func save() async {
        saving = true
        defer { saving = false }
        let subgenres = draftSubgenres
            .split(whereSeparator: { "、,， ".contains($0) })
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
        let update = KnowledgeUpdate(
            content: draft,
            stages: Copy.knowledgeStages.map(\.id).filter { draftStages.contains($0) },
            genres: Copy.knowledgeGenres.filter { draftGenres.contains($0) },
            subgenres: subgenres
        )
        do {
            try await api.saveKnowledge(id: doc.id, update: update)
            content = try await api.knowledge(id: doc.id)
            editing = false
            onChanged()
        } catch { self.error = error.localizedDescription }
    }

    private func restore() async {
        saving = true
        defer { saving = false }
        do {
            content = try await api.restoreKnowledge(id: doc.id)
            editing = false
            onChanged()
        } catch { self.error = error.localizedDescription }
    }
}