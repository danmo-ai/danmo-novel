import SwiftUI

/// One bible document definition, mirroring useBible.ts's definitions table.
struct BibleDoc: Identifiable, Hashable {
    let path: String
    let label: String
    let group: String
    let description: String
    let usage: String
    var secret = false

    var id: String { path }
}

enum BibleDocs {
    static let definitions: [BibleDoc] = [
        BibleDoc(path: "book-bible.md", label: Copy.docBibleLabel, group: Copy.bibleGroupDerived, description: Copy.docBibleDescription, usage: Copy.docBibleUsage),
        BibleDoc(path: "canon/world.md", label: Copy.docWorldLabel, group: Copy.bibleGroupDerived, description: Copy.docWorldDescription, usage: Copy.docBibleUsage),
        BibleDoc(path: "canon/goldfinger.md", label: Copy.docGoldfingerLabel, group: Copy.bibleGroupDerived, description: Copy.docGoldfingerDescription, usage: Copy.docBibleUsage),
        BibleDoc(path: "canon/foreshadow-schedule.md", label: Copy.docForeshadowLabel, group: Copy.bibleGroupDerived, description: Copy.docForeshadowDescription, usage: Copy.docForeshadowUsage),
        BibleDoc(path: "canon/platform-positioning.md", label: Copy.docPlatformLabel, group: Copy.bibleGroupConstraints, description: Copy.docPlatformDescription, usage: Copy.docPlatformUsage),
        BibleDoc(path: "canon/writing-rules.md", label: Copy.docRulesLabel, group: Copy.bibleGroupConstraints, description: Copy.docRulesDescription, usage: Copy.docRulesUsage),
        BibleDoc(path: "canon/style-fingerprint.md", label: Copy.docStyleLabel, group: Copy.bibleGroupConstraints, description: Copy.docStyleDescription, usage: Copy.docStyleUsage),
        BibleDoc(path: "canon/locked-terms.yaml", label: Copy.docLockedLabel, group: Copy.bibleGroupSecrets, description: Copy.docLockedDescription, usage: Copy.docLockedUsage),
        BibleDoc(path: "canon/author-lore.md", label: Copy.docLoreLabel, group: Copy.bibleGroupSecrets, description: Copy.docLoreDescription, usage: Copy.docLoreUsage, secret: true),
        BibleDoc(path: "continuity/state.yaml", label: Copy.docStateLabel, group: Copy.bibleGroupContinuity, description: Copy.docStateDescription, usage: Copy.docStateUsage),
        BibleDoc(path: "novel-state.yaml", label: Copy.docRunStateLabel, group: Copy.bibleGroupContinuity, description: Copy.docRunStateDescription, usage: Copy.docRunStateUsage),
    ]

    static let groups = [Copy.bibleGroupDerived, Copy.bibleGroupConstraints, Copy.bibleGroupCast, Copy.bibleGroupSecrets, Copy.bibleGroupContinuity]

    /// AI optimization is not offered for ledgers / locked terms / run state.
    static func canOptimize(path: String) -> Bool {
        if path.contains("#") { return false }
        if ["continuity/state.yaml", "novel-state.yaml", "canon/locked-terms.yaml"].contains(path) { return false }
        if path.hasPrefix("continuity/") { return false }
        return true
    }
}

/// The bible tab: grouped docs, cast cards with identity timeline, secrets
/// gate, manual editing and entity rewrite — BibleWorkspace.vue + useBible.ts.
struct BiblePanelView: View {
    let workspace: BookWorkspaceStore
    let openUnit: (UnitInfo) -> Void

    @State private var activeGroup = Copy.bibleGroupDerived
    @State private var selectedPath = "book-bible.md"
    @State private var selectedCast: String?
    @State private var showRelations = false
    @State private var showSecrets = false
    @State private var detail: Detail?
    @State private var identityEdit: IdentityEdit?
    @State private var promoting = false

    enum Detail: Identifiable, Hashable {
        case doc(BibleDoc)
        case dynamicDoc(path: String, label: String, description: String, usage: String)
        case cast(String)

        var id: String {
            switch self {
            case .doc(let doc): doc.path
            case .dynamicDoc(let path, _, _, _): path
            case .cast(let stem): "cast:\(stem)"
            }
        }
    }

    struct IdentityEdit: Identifiable {
        let id = UUID()
        var action: String // append | update | close
        var index: Int
        var segment: IdentitySegment
    }

    private var book: BookSnapshot? { workspace.book }
    private var readOnly: Bool { book?.summary.readOnly ?? false }

    private var docs: [(doc: BibleDoc, exists: Bool)] {
        BibleDocs.definitions.map { ($0, !(book?.file($0.path) ?? "").isEmpty) }
    }
    private var missingCount: Int { docs.filter { !$0.exists }.count }
    private var summaries: [String] {
        (book?.files ?? [:]).keys.filter { $0.range(of: #"^continuity/summaries/[^/]+\.md$"#, options: .regularExpression) != nil }.sorted()
    }
    private var commits: [String] {
        (book?.files ?? [:]).keys.filter { $0.range(of: #"^continuity/commits/[^/]+\.md$"#, options: .regularExpression) != nil }.sorted()
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                header
                if workspace.filesReady {
                    coverage
                    groupTabs
                    docList
                } else {
                    ProgressView(Copy.loading)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 40)
                }
            }
            .padding(16)
            .frame(maxWidth: 860)
            .frame(maxWidth: .infinity)
        }
        .background(Color(.systemGroupedBackground))
        .task { await workspace.ensureFiles() }
        .navigationDestination(item: $detail) { destination in
            detailView(destination)
        }
        .sheet(item: $identityEdit) { edit in
            IdentityEditorSheet(
                edit: edit,
                units: book?.units ?? [],
                castName: book?.cast.first { $0.stem == edit.segment.stem }?.name ?? edit.segment.stem,
                saving: workspace.saving
            ) { action, index, segment in
                Task {
                    await saveIdentity(action: action, index: index, segment: segment)
                }
            } onCancel: {
                identityEdit = nil
            }
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(Copy.bibleTitle).font(.headline)
                Spacer()
                if missingCount > 0 && !readOnly {
                    Button(Copy.bibleScaffold(missingCount)) {
                        Task { await workspace.scaffoldMissing() }
                    }
                    .font(.caption)
                }
            }
            Text(Copy.bibleIntro).font(.caption).foregroundStyle(.secondary)
        }
    }

    private var coverage: some View {
        FlowChips(items: [
            Copy.bibleCoverage(docs.filter(\.exists).count, docs.count),
            Copy.bibleCastCount(book?.cast.count ?? 0),
            Copy.bibleSummaryCount(summaries.count),
            Copy.bibleCommitCount(commits.count),
        ] + (missingCount > 0 ? [Copy.bibleMissing(missingCount)] : []))
    }

    private var groupTabs: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(BibleDocs.groups, id: \.self) { group in
                    Button {
                        activeGroup = group
                        if group == Copy.bibleGroupCast { showRelations = true }
                    } label: {
                        Text(group)
                            .font(.footnote.weight(activeGroup == group ? .semibold : .regular))
                            .padding(.horizontal, 14).padding(.vertical, 7)
                            .background(activeGroup == group ? Color.accentColor.opacity(0.15) : Color.secondary.opacity(0.08), in: .capsule)
                            .foregroundStyle(activeGroup == group ? Color.accentColor : .secondary)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
    }

    @ViewBuilder
    private var docList: some View {
        VStack(alignment: .leading, spacing: 8) {
            if activeGroup == Copy.bibleGroupCast {
                row(label: Copy.relationsTitle, subtitle: Copy.relationsNote, badge: nil) {
                    showRelations = true
                    detail = nil
                    relationsSheet = true
                }
                if let cast = book?.cast, !cast.isEmpty {
                    ForEach(cast) { card in
                        row(
                            label: card.name,
                            subtitle: card.tagline ?? card.stem,
                            badge: card.isCandidate ? Copy.bibleCandidateBadge : card.roleTitle
                        ) {
                            selectedCast = card.stem
                            showSecrets = false
                            detail = .cast(card.stem)
                        }
                    }
                } else {
                    Text(Copy.bibleCastEmpty).font(.caption).foregroundStyle(.secondary)
                }
            } else if activeGroup == Copy.bibleGroupContinuity {
                ForEach(docs.filter { $0.doc.group == activeGroup }, id: \.doc.id) { item in
                    docRow(item)
                }
                ForEach(summaries, id: \.self) { path in
                    let name = (path as NSString).lastPathComponent.replacingOccurrences(of: ".md", with: "")
                    row(label: Copy.summaryDocLabel(name), subtitle: path, badge: nil) {
                        detail = .dynamicDoc(path: path, label: Copy.summaryDocLabel(name), description: Copy.summaryDocDescription, usage: Copy.summaryDocUsage)
                    }
                }
                ForEach(commits, id: \.self) { path in
                    let name = (path as NSString).lastPathComponent.replacingOccurrences(of: ".md", with: "")
                    row(label: Copy.commitDocLabel(name), subtitle: path, badge: nil) {
                        detail = .dynamicDoc(path: path, label: Copy.commitDocLabel(name), description: Copy.commitDocDescription, usage: Copy.commitDocUsage)
                    }
                }
            } else {
                ForEach(docs.filter { $0.doc.group == activeGroup }, id: \.doc.id) { item in
                    docRow(item)
                }
            }
        }
        .sheet(isPresented: $relationsSheet) {
            NavigationStack {
                ScrollView {
                    RelationGraphView(cards: book?.cast ?? []) { stem in
                        relationsSheet = false
                        detail = .cast(stem)
                    }
                    .padding(16)
                }
                .navigationTitle(Copy.relationsTitle)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button(Copy.actionClose) { relationsSheet = false }
                    }
                }
            }
        }
    }

    @State private var relationsSheet = false

    private func docRow(_ item: (doc: BibleDoc, exists: Bool)) -> some View {
        row(label: item.doc.label, subtitle: item.doc.description, badge: item.exists ? nil : Copy.bibleNotCreated) {
            selectedCast = nil
            selectedPath = item.doc.path
            showSecrets = false
            detail = .doc(item.doc)
        }
    }

    private func row(label: String, subtitle: String, badge: String?, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack {
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 6) {
                        Text(label).font(.subheadline.weight(.medium)).foregroundStyle(.primary)
                        if let badge {
                            Text(badge)
                                .font(.caption2)
                                .padding(.horizontal, 6).padding(.vertical, 1)
                                .background(Color.orange.opacity(0.12), in: .capsule)
                                .foregroundStyle(.orange)
                        }
                    }
                    Text(subtitle).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                }
                Spacer()
                Image(systemName: "chevron.right").font(.caption2).foregroundStyle(.tertiary)
            }
            .padding(12)
            .background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: 12))
        }
        .buttonStyle(.plain)
    }

    // MARK: Detail

    @ViewBuilder
    private func detailView(_ destination: Detail) -> some View {
        switch destination {
        case .doc(let doc):
            BibleDocDetail(
                workspace: workspace,
                title: doc.label,
                description: doc.description,
                usage: doc.usage,
                path: doc.path,
                raw: doc.path == "novel-state.yaml" ? (book?.stateRaw ?? book?.file(doc.path) ?? "") : (book?.file(doc.path) ?? ""),
                secret: doc.secret,
                canOptimize: BibleDocs.canOptimize(path: doc.path)
            )
        case .dynamicDoc(let path, let label, let description, let usage):
            BibleDocDetail(
                workspace: workspace,
                title: label,
                description: description,
                usage: usage,
                path: path,
                raw: book?.file(path) ?? "",
                secret: false,
                canOptimize: false
            )
        case .cast(let stem):
            if let card = book?.cast.first(where: { $0.stem == stem }) {
                castDetail(card)
            }
        }
    }

    private func castDetail(_ card: CastCard) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                VStack(alignment: .leading, spacing: 4) {
                    HStack(spacing: 8) {
                        Text(card.name).font(.title3.bold())
                        Text(card.roleTitle)
                            .font(.caption2)
                            .padding(.horizontal, 8).padding(.vertical, 2)
                            .background(Color.accentColor.opacity(0.1), in: .capsule)
                            .foregroundStyle(Color.accentColor)
                        if card.isCandidate {
                            Text(Copy.bibleCandidatePending)
                                .font(.caption2)
                                .padding(.horizontal, 8).padding(.vertical, 2)
                                .background(Color.orange.opacity(0.12), in: .capsule)
                                .foregroundStyle(.orange)
                        }
                    }
                    if let tagline = card.tagline, !tagline.isEmpty {
                        Text(tagline).font(.footnote).foregroundStyle(.secondary)
                    }
                    Text(Copy.bibleCastUsage).font(.caption2).foregroundStyle(Color.accentColor)
                }

                HStack(spacing: 10) {
                    if card.isCandidate && !readOnly {
                        Button(promoting ? Copy.biblePromoting : Copy.biblePromote) {
                            Task { await promote(card) }
                        }
                        .buttonStyle(.borderedProminent)
                        .font(.footnote)
                        .disabled(promoting)
                    }
                    if !readOnly {
                        Button(Copy.bibleOptimizeCast) {
                            workspace.startRewrite(targetPath: "canon/cast/\(card.stem).md", entityKind: "cast", title: card.name)
                        }
                        .buttonStyle(.bordered)
                        .font(.footnote)
                        .disabled(workspace.busy)
                    }
                    Spacer()
                }

                identityTimeline(card)

                MarkdownBlocksView(raw: castBody(card))
                DisclosureGroup(Copy.bibleViewSource) {
                    Text(card.raw ?? "")
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.top, 6)
                }
                .font(.footnote)
            }
            .padding(16)
            .frame(maxWidth: 760)
            .frame(maxWidth: .infinity)
        }
        .background(Color(.systemGroupedBackground))
        .navigationTitle(card.name)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if !readOnly {
                ToolbarItem(placement: .primaryAction) {
                    NavigationLink {
                        FileEditorView(workspace: workspace, path: "canon/cast/\(card.stem).md", initial: card.raw ?? "", title: card.name)
                    } label: {
                        Label(Copy.actionEdit, systemImage: "pencil")
                    }
                }
            }
        }
    }

    /// Strip the H1 (duplicated by the header) and status/role/tagline meta
    /// lines, like the desktop cast rendering.
    private func castBody(_ card: CastCard) -> String {
        var skippedTitle = false
        let blocks = MarkdownBlocks.parse(card.raw ?? "").filter { block in
            if case .heading(let level, let text) = block, !skippedTitle, level == 1, text == card.name {
                skippedTitle = true
                return false
            }
            if case .paragraph(let text) = block,
               Rx.matches("^`?(status|role|tagline)`?\\s*[:：]", in: text, multiline: false) {
                return false
            }
            return true
        }
        return blocks.map { block in
            switch block {
            case .heading(let level, let text): String(repeating: "#", count: level) + " " + text
            case .paragraph(let text): text
            case .bullets(let items): items.map { "- \($0)" }.joined(separator: "\n")
            case .quote(let text): "> " + text
            case .table(let headers, let rows):
                ([headers] + rows).map { "| " + $0.joined(separator: " | ") + " |" }.joined(separator: "\n")
            }
        }.joined(separator: "\n\n")
    }

    private func identityTimeline(_ card: CastCard) -> some View {
        let segments = (book?.identitySegments ?? []).enumerated().filter { $0.element.stem == card.stem }
        return VStack(alignment: .leading, spacing: 8) {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(Copy.bibleIdentityTitle).font(.footnote.weight(.semibold))
                    Text(Copy.bibleIdentityNote).font(.caption2).foregroundStyle(.secondary)
                }
                Spacer()
            }
            if segments.isEmpty {
                Text(Copy.bibleIdentityMissing).font(.caption).foregroundStyle(.orange)
            }
            ForEach(segments, id: \.offset) { index, segment in
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 8) {
                        Text(segment.fromUnit + " → " + (segment.toUnit ?? Copy.bibleIdentityUntil))
                            .font(.caption.monospaced()).foregroundStyle(Color.accentColor)
                        Spacer()
                        if !readOnly {
                            Button(Copy.bibleIdentityFix) {
                                identityEdit = IdentityEdit(action: "update", index: indexOf(segment), segment: segment)
                            }
                            .font(.caption2)
                            if segment.toUnit == nil {
                                Button(Copy.bibleIdentityClose) {
                                    identityEdit = IdentityEdit(action: "close", index: indexOf(segment), segment: segment)
                                }
                                .font(.caption2)
                            }
                        }
                    }
                    Text(segment.roleTitle).font(.footnote.weight(.medium))
                    if let org = segment.org, !org.isEmpty {
                        Text(org).font(.caption).foregroundStyle(.secondary)
                    }
                    if let note = segment.note, !note.isEmpty {
                        Text(note).font(.caption2).foregroundStyle(.secondary).italic()
                    }
                }
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(segment.toUnit == nil ? Color.accentColor.opacity(0.06) : Color.secondary.opacity(0.05), in: .rect(cornerRadius: 8))
            }
            if !readOnly {
                HStack(spacing: 10) {
                    Button(segments.isEmpty ? Copy.bibleIdentityAdd : Copy.bibleIdentityAppend) {
                        identityEdit = IdentityEdit(
                            action: "append", index: -1,
                            segment: IdentitySegment(stem: card.stem, roleTitle: "", org: "", fromUnit: book?.units.first?.unitId ?? "v01-U1", toUnit: nil, note: nil)
                        )
                    }
                    .font(.caption)
                    Button(Copy.bibleIdentityRewrite) {
                        workspace.startRewrite(targetPath: "canon/identity-timeline.yaml", entityKind: "identity", title: "\(card.name) · \(Copy.bibleIdentityTitle)")
                    }
                    .font(.caption)
                    .disabled(workspace.busy)
                }
            }
        }
        .padding(12)
        .background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: 12))
    }

    /// The identity-segments API addresses the global array index.
    private func indexOf(_ segment: IdentitySegment) -> Int {
        (book?.identitySegments ?? []).firstIndex(of: segment) ?? 0
    }

    private func promote(_ card: CastCard) async {
        guard let book else { return }
        guard let identity = (book.identitySegments ?? []).first(where: { $0.stem == card.stem }) else {
            workspace.note(Copy.biblePromoteNeedIdentity)
            identityEdit = IdentityEdit(
                action: "append", index: -1,
                segment: IdentitySegment(stem: card.stem, roleTitle: "", org: "", fromUnit: book.units.first?.unitId ?? "v01-U1", toUnit: nil, note: nil)
            )
            return
        }
        promoting = true
        defer { promoting = false }
        do {
            try await workspace.api.promoteCast(bookID: book.summary.id, stem: card.stem, initialIdentity: identity, baseRevision: book.revision)
            await workspace.reload()
            workspace.note(Copy.biblePromoted(card.name))
        } catch { workspace.fail(error) }
    }

    private func saveIdentity(action: String, index: Int, segment: IdentitySegment) async {
        guard let book else { return }
        do {
            try await workspace.api.identitySegments(bookID: book.summary.id, action: action, index: index, segment: segment, baseRevision: book.revision)
            await workspace.reload()
            identityEdit = nil
            workspace.note(Copy.bibleIdentitySaved)
        } catch { workspace.fail(error) }
    }
}

/// One bible document: structured rendering (state / locked terms / markdown),
/// secrets gate, source view, edit and rewrite.
struct BibleDocDetail: View {
    let workspace: BookWorkspaceStore
    let title: String
    let description: String
    let usage: String
    let path: String
    let raw: String
    let secret: Bool
    let canOptimize: Bool

    @State private var revealed = false

    private var renderMode: String {
        if path == "novel-state.yaml" { return "state" }
        if path == "canon/locked-terms.yaml" { return "locked" }
        return "markdown"
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(description).font(.caption).foregroundStyle(.secondary)
                    Text(Copy.usageLine(usage)).font(.caption2).foregroundStyle(Color.accentColor)
                    Text(path).font(.caption2.monospaced()).foregroundStyle(.tertiary)
                }
                if secret && !revealed {
                    VStack(spacing: 10) {
                        Text(Copy.bibleSecretTitle).font(.headline)
                        Text(Copy.bibleSecretNote).font(.caption).foregroundStyle(.secondary)
                        Button(Copy.bibleSecretReveal) { revealed = true }
                            .buttonStyle(.borderedProminent)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 30)
                } else if raw.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    VStack(spacing: 8) {
                        Text(Copy.bibleMissingDocTitle).font(.subheadline.weight(.semibold))
                        Text(description).font(.caption).foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 24)
                } else {
                    switch renderMode {
                    case "state": stateView
                    case "locked": lockedView
                    default: MarkdownBlocksView(raw: raw)
                    }
                    DisclosureGroup(Copy.bibleViewSource) {
                        Text(raw)
                            .font(.caption.monospaced())
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.top, 6)
                    }
                    .font(.footnote)
                }
            }
            .padding(16)
            .frame(maxWidth: 760)
            .frame(maxWidth: .infinity)
        }
        .background(Color(.systemGroupedBackground))
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                if canOptimize && !(workspace.book?.summary.readOnly ?? false) && !raw.isEmpty {
                    Button {
                        workspace.startRewrite(targetPath: path, entityKind: entityKind, title: title)
                    } label: {
                        Label(Copy.bibleOptimize, systemImage: "wand.and.stars")
                    }
                    .disabled(workspace.busy)
                }
                if !(workspace.book?.summary.readOnly ?? false) {
                    NavigationLink {
                        FileEditorView(workspace: workspace, path: path, initial: raw, title: title)
                    } label: {
                        Label(Copy.actionEdit, systemImage: "pencil")
                    }
                }
            }
        }
    }

    private var entityKind: String {
        if path == "book-bible.md" { return "bible" }
        if path.contains("volumes/") { return "volume" }
        return "canon"
    }

    private var stateView: some View {
        let state = StateView.parse(raw)
        return VStack(alignment: .leading, spacing: 10) {
            if state.fields.isEmpty {
                Text(Copy.bibleStateEmpty).font(.caption).foregroundStyle(.secondary)
            }
            ForEach(state.fields) { field in
                LabeledFieldView(label: field.label, value: field.value)
            }
            if !state.gates.isEmpty {
                FlowChips(items: state.gates.map { gate in
                    let icon = gate.value == "pass" ? Copy.gatePass : gate.value == "fail" ? Copy.gateFail : Copy.gateUnknown
                    return "\(gate.key) · \(icon)"
                })
            }
        }
    }

    private var lockedView: some View {
        let locked = LockedTermsView.parse(raw)
        return VStack(alignment: .leading, spacing: 14) {
            VStack(alignment: .leading, spacing: 6) {
                Text("\(Copy.bibleLockedSection) · \(Copy.bibleLockedHint)").font(.footnote.weight(.semibold))
                if locked.locked.isEmpty {
                    Text(Copy.bibleLockedEmpty).font(.caption).foregroundStyle(.secondary)
                }
                ForEach(locked.locked) { term in
                    HStack {
                        Text(term.term).font(.caption.weight(.medium))
                        Spacer()
                        Text(term.until).font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
            VStack(alignment: .leading, spacing: 6) {
                Text(Copy.bibleComplianceSection).font(.footnote.weight(.semibold))
                if locked.compliance.isEmpty {
                    Text(Copy.bibleComplianceEmpty).font(.caption).foregroundStyle(.secondary)
                }
                ForEach(locked.compliance, id: \.self) { item in
                    Text("• " + item).font(.caption).foregroundStyle(.secondary)
                }
            }
            if !locked.aliases.isEmpty {
                VStack(alignment: .leading, spacing: 6) {
                    Text(Copy.bibleAliasSection).font(.footnote.weight(.semibold))
                    ForEach(locked.aliases) { alias in
                        HStack {
                            Text(alias.alias).font(.caption.weight(.medium))
                            Spacer()
                            Text(alias.real).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
    }
}

/// Full-file source editor with baseRevision conflict protection.
struct FileEditorView: View {
    let workspace: BookWorkspaceStore
    let path: String
    let initial: String
    let title: String

    @Environment(\.dismiss) private var dismiss
    @State private var draft: String

    init(workspace: BookWorkspaceStore, path: String, initial: String, title: String) {
        self.workspace = workspace
        self.path = path
        self.initial = initial
        self.title = title
        _draft = State(initialValue: initial)
    }

    var body: some View {
        VStack(spacing: 0) {
            TextEditor(text: $draft)
                .font(.caption.monospaced())
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .padding(.horizontal, 8)
        }
        .navigationTitle(title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button(workspace.saving ? Copy.saving : Copy.actionSave) {
                    Task {
                        if await workspace.saveFile(path: path, content: draft) {
                            dismiss()
                        }
                    }
                }
                .disabled(workspace.saving)
            }
        }
    }
}

/// Identity segment editor (append / update / close), server-validated.
struct IdentityEditorSheet: View {
    let edit: BiblePanelView.IdentityEdit
    let units: [UnitInfo]
    let castName: String
    let saving: Bool
    let onSave: (String, Int, IdentitySegment) -> Void
    let onCancel: () -> Void

    @State private var segment: IdentitySegment
    @State private var action: String
    @State private var index: Int

    init(edit: BiblePanelView.IdentityEdit, units: [UnitInfo], castName: String, saving: Bool,
         onSave: @escaping (String, Int, IdentitySegment) -> Void, onCancel: @escaping () -> Void) {
        self.edit = edit
        self.units = units
        self.castName = castName
        self.saving = saving
        self.onSave = onSave
        self.onCancel = onCancel
        _segment = State(initialValue: edit.segment)
        _action = State(initialValue: edit.action)
        _index = State(initialValue: edit.index)
    }

    private var title: String {
        switch action {
        case "append": Copy.identityActionAppend
        case "update": Copy.identityActionUpdate
        default: Copy.identityActionClose
        }
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(Copy.identityDialogNote).font(.caption).foregroundStyle(.secondary)
                } header: {
                    Text("\(title) · \(castName)")
                }
                Section {
                    TextField(Copy.identityRoleLabel, text: $segment.roleTitle)
                        .disabled(action == "close")
                    TextField(Copy.identityOrgLabel, text: Binding(get: { segment.org ?? "" }, set: { segment.org = $0 }))
                        .disabled(action == "close")
                    Picker(Copy.identityFromLabel, selection: $segment.fromUnit) {
                        ForEach(units) { unit in
                            Text("\(unit.unitId) · \(unit.title)").tag(unit.unitId)
                        }
                    }
                    .disabled(action != "append")
                    if action == "close" {
                        Picker(Copy.identityToLabel, selection: Binding(get: { segment.toUnit ?? units.first?.unitId ?? "" }, set: { segment.toUnit = $0 })) {
                            ForEach(units) { unit in
                                Text("\(unit.unitId) · \(unit.title)").tag(unit.unitId)
                            }
                        }
                    } else {
                        TextField(Copy.identityNoteLabel, text: Binding(get: { segment.note ?? "" }, set: { segment.note = $0 }))
                    }
                }
                if action == "append" {
                    Section {
                        Text(Copy.identityImpact(from: segment.fromUnit, role: segment.roleTitle, org: segment.org ?? ""))
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(Copy.actionCancel, action: onCancel) }
                ToolbarItem(placement: .confirmationAction) {
                    Button(saving ? Copy.identityValidating : Copy.identityValidateSave) {
                        if action == "close" && segment.toUnit == nil {
                            segment.toUnit = units.first?.unitId
                        }
                        onSave(action, index, segment)
                    }
                    .disabled(saving || (action != "close" && segment.roleTitle.trimmingCharacters(in: .whitespaces).isEmpty))
                }
            }
        }
        .presentationDetents([.medium, .large])
    }
}