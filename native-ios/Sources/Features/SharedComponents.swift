import SwiftUI

/// Inline sentence diff: additions green, deletions red strikethrough. One
/// Text per chapter keeps selection and layout cheap.
struct ChapterDiffView: View {
    let base: [ChapterDraft]?
    let revised: [ChapterDraft]
    @State private var showUnchanged = false
    @State private var rows: [TextDiff.ChapterRow] = []

    private var changedRows: [TextDiff.ChapterRow] { rows.filter(\.changed) }
    private var unchangedRows: [TextDiff.ChapterRow] { rows.filter { !$0.changed } }
    private var visibleRows: [TextDiff.ChapterRow] { showUnchanged ? rows : changedRows }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 10) {
                Text(Copy.diffChangedChapters(changedRows.count))
                    .font(.subheadline.weight(.semibold))
                let added = changedRows.reduce(0) { $0 + $1.added }
                let removed = changedRows.reduce(0) { $0 + $1.removed }
                if added > 0 { Text(Copy.diffAdded(added)).font(.caption).foregroundStyle(.green) }
                if removed > 0 { Text(Copy.diffRemoved(removed)).font(.caption).foregroundStyle(.red) }
                Spacer()
            }
            if !unchangedRows.isEmpty {
                Toggle(Copy.diffShowUnchanged(unchangedRows.count), isOn: $showUnchanged)
                    .font(.caption)
            }
            if changedRows.isEmpty {
                Text(Copy.diffNoChange)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .padding(12)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: 10))
            }
            ForEach(visibleRows) { row in
                DisclosureGroup {
                    diffText(row.segments)
                        .padding(.vertical, 8)
                } label: {
                    HStack(spacing: 8) {
                        Text(Copy.chapterHeading(row.chapter)).font(.subheadline.weight(.semibold))
                        Text(row.title).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                        Spacer()
                        Text(row.changed ? Copy.diffChangedFlag : Copy.diffUnchangedFlag)
                            .font(.caption2)
                            .padding(.horizontal, 8).padding(.vertical, 2)
                            .background(row.changed ? Color.red.opacity(0.1) : Color.secondary.opacity(0.1), in: .capsule)
                            .foregroundStyle(row.changed ? .red : .secondary)
                    }
                }
            }
        }
        .task(id: revised) { rows = TextDiff.chapterRows(base: base, revised: revised) }
    }

    private func diffText(_ segments: [TextDiff.Segment]) -> Text {
        var result = Text("")
        for segment in segments {
            switch segment.kind {
            case .same:
                result = result + Text(segment.text).foregroundColor(.secondary)
            case .add:
                result = result + Text(segment.text).foregroundColor(.green)
            case .del:
                result = result + Text(segment.text).strikethrough().foregroundColor(.red)
            }
        }
        return result.font(.system(.footnote, design: .serif))
    }
}

/// Per-file line diff for plan candidates (kept paths marked, not written).
struct PlanDiffView: View {
    let candidate: Candidate
    let files: [String: String]
    @State private var rows: [PlanFileChange] = []

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            let changed = rows.filter { $0.status == .changed }.count
            let new = rows.filter { $0.status == .new }.count
            let same = rows.filter { $0.status == .same }.count
            let kept = rows.filter { $0.status == .kept }.count
            HStack(spacing: 12) {
                if changed > 0 { Text(Copy.diffStatChanged(changed)).font(.caption.weight(.semibold)) }
                if new > 0 { Text(Copy.diffStatNew(new)).font(.caption.weight(.semibold)).foregroundStyle(.green) }
                if same > 0 { Text(Copy.diffStatSame(same)).font(.caption).foregroundStyle(.secondary) }
                if kept > 0 { Text(Copy.diffStatKept(kept)).font(.caption).foregroundStyle(.secondary) }
            }
            ForEach(rows) { row in
                DisclosureGroup {
                    content(row)
                } label: {
                    HStack {
                        Text(row.path).font(.caption.monospaced()).lineLimit(2)
                        Spacer()
                        Text(row.statusLabel)
                            .font(.caption2)
                            .foregroundStyle(row.status == .new ? .green : row.status == .changed ? .orange : .secondary)
                    }
                }
            }
        }
        .task(id: candidate.id) { rows = PlanFileChange.rows(candidate: candidate, files: files) }
    }

    @ViewBuilder
    private func content(_ row: PlanFileChange) -> some View {
        switch row.status {
        case .kept:
            Text(Copy.diffKeptNote).font(.caption).foregroundStyle(.secondary).padding(.vertical, 6)
        case .same:
            Text(Copy.diffSameNote).font(.caption).foregroundStyle(.secondary).padding(.vertical, 6)
        case .new, .changed:
            ScrollView(.horizontal) {
                lineDiffText(TextDiff.lineDiff(before: row.current, after: row.next))
                    .padding(.vertical, 6)
            }
        }
    }

    private func lineDiffText(_ segments: [TextDiff.Segment]) -> Text {
        var result = Text("")
        for segment in segments {
            let prefix = segment.kind == .add ? "+ " : segment.kind == .del ? "- " : "  "
            let piece = Text(prefix + segment.text + "\n")
            switch segment.kind {
            case .same: result = result + piece.foregroundColor(.secondary)
            case .add: result = result + piece.foregroundColor(.green)
            case .del: result = result + piece.foregroundColor(.red)
            }
        }
        return result.font(.caption2.monospaced())
    }
}

/// Structured plain-Markdown rendering for bible docs and knowledge articles.
struct MarkdownBlocksView: View {
    let raw: String
    @State private var blocks: [MarkdownBlocks.Block] = []

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(blocks) { block in
                switch block {
                case .heading(let level, let text):
                    Text(text)
                        .font(level == 1 ? .title2.bold() : level == 2 ? .title3.bold() : .headline)
                        .padding(.top, level <= 2 ? 8 : 2)
                case .paragraph(let text):
                    Text(text).font(.subheadline).foregroundStyle(.secondary)
                        .textSelection(.enabled)
                case .bullets(let items):
                    VStack(alignment: .leading, spacing: 4) {
                        ForEach(items, id: \.self) { item in
                            HStack(alignment: .firstTextBaseline, spacing: 8) {
                                Text("•").foregroundStyle(.secondary)
                                Text(item).font(.subheadline).foregroundStyle(.secondary)
                            }
                        }
                    }
                case .quote(let text):
                    Text(text)
                        .font(.subheadline)
                        .padding(10)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(Color.accentColor.opacity(0.06), in: .rect(cornerRadius: 8))
                case .table(let headers, let rows):
                    ScrollView(.horizontal) {
                        Grid(alignment: .leading, horizontalSpacing: 14, verticalSpacing: 6) {
                            GridRow {
                                ForEach(headers.indices, id: \.self) { index in
                                    Text(headers[index]).font(.caption.weight(.semibold))
                                }
                            }
                            Divider()
                            ForEach(rows.indices, id: \.self) { rowIndex in
                                GridRow {
                                    ForEach(rows[rowIndex].indices, id: \.self) { cellIndex in
                                        Text(rows[rowIndex][cellIndex]).font(.caption).foregroundStyle(.secondary)
                                    }
                                }
                            }
                        }
                        .padding(.vertical, 4)
                    }
                }
            }
        }
        .task(id: raw) { blocks = MarkdownBlocks.parse(raw) }
    }
}

/// One review finding row: severity, source, chapter, rounds-hit badge and
/// the pick toggle (unit-level / unfinished rows are not pickable).
struct FindingRowView: View {
    let row: ReviewRow
    let picked: Bool
    let disabled: Bool
    let onToggle: () -> Void

    private var finding: ReviewFinding { row.finding }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                Button(action: onToggle) {
                    Image(systemName: picked ? "checkmark.circle.fill" : "circle")
                        .foregroundStyle(row.canPick && !disabled ? Color.accentColor : Color.secondary.opacity(0.4))
                }
                .buttonStyle(.plain)
                .disabled(!row.canPick || disabled)
                .accessibilityLabel(pickHint)

                Text(finding.source == "local_gate" ? Copy.reviewSourceLocal : Copy.reviewSourceAI)
                if let chapter = finding.chapter, chapter > 0 {
                    Text(Copy.chapterHeading(chapter))
                } else {
                    Text(Copy.reviewWholeUnit)
                }
                if let lens = finding.lens, !lens.isEmpty { Text(lens) }
                if let hits = finding.roundsHit, hits > 1 {
                    Text(Copy.reviewHits(hits))
                        .padding(.horizontal, 6)
                        .background(Color.red.opacity(0.1), in: .capsule)
                        .foregroundStyle(.red)
                        .accessibilityHint(Copy.reviewHitsHint)
                }
                Spacer()
                Text(severityLabel)
                    .fontWeight(.semibold)
                    .foregroundStyle(severityColor)
            }
            .font(.caption)
            .foregroundStyle(.secondary)

            Text(finding.title)
                .font(.subheadline.weight(.medium))
                .foregroundStyle(ReviewState.isBlocking(finding) ? .red : .primary)
            if let evidence = finding.evidence, !evidence.isEmpty {
                Text(evidence).font(.footnote).foregroundStyle(.secondary)
            }
            if let suggestion = finding.suggestion, !suggestion.isEmpty {
                Text(Copy.reviewSuggestionPrefix + suggestion)
                    .font(.footnote).foregroundStyle(Color.accentColor)
            }
            if !ReviewState.isProseFixable(finding) && !ReviewState.isUnfinished(finding) {
                Text(Copy.reviewUnitLevelNote).font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 6)
        .contextMenu {
            Button(Copy.reviewCopyFinding, systemImage: "doc.on.doc") {
                UIPasteboard.general.string = copyText
            }
        }
    }

    private var severityLabel: String {
        if ReviewState.isUnfinished(finding) { return Copy.reviewSeverityUnfinished }
        return finding.severity == "blocking" ? Copy.reviewSeverityBlocking : Copy.reviewSeverityAdvisory
    }

    private var severityColor: Color {
        if ReviewState.isUnfinished(finding) { return .orange }
        return finding.severity == "blocking" ? .red : .secondary
    }

    private var pickHint: String {
        if ReviewState.isUnfinished(finding) { return Copy.reviewPickUnfinishedHint }
        if !ReviewState.isProseFixable(finding) { return Copy.reviewPickUnitLevelHint }
        return Copy.reviewPickHint
    }

    private var copyText: String {
        var text = "【第\(finding.chapter.map(String.init) ?? "?")章】\(finding.title)"
        if let evidence = finding.evidence, !evidence.isEmpty { text += "\n\(evidence)" }
        if let suggestion = finding.suggestion, !suggestion.isEmpty { text += "\n\(Copy.reviewSuggestionPrefix)\(suggestion)" }
        return text
    }
}

/// A small labeled key/value block used across contract / outline / bible.
struct LabeledFieldView: View {
    let label: String
    let value: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(label).font(.caption.weight(.semibold)).foregroundStyle(Color.accentColor)
            Text(value).font(.footnote).foregroundStyle(.secondary)
                .textSelection(.enabled)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(10)
        .background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: 10))
    }
}

/// The workspace's operation-failure alert, attached to the surface that is
/// actually on screen. A run started from the unit page fails while the book
/// page is pushed away, and SwiftUI does not present an alert from an
/// off-screen destination — so every surface that can trigger a workspace
/// action carries this modifier, not just the book page.
struct WorkspaceErrorAlert: ViewModifier {
    let workspace: BookWorkspaceStore

    func body(content: Content) -> some View {
        content.alert(Copy.errorOperationFailed, isPresented: Binding(
            get: { workspace.error != nil },
            set: { if !$0 { workspace.error = nil } }
        )) {
            Button(Copy.actionOK) { workspace.error = nil }
        } message: {
            Text(workspace.error ?? "")
        }
    }
}

extension View {
    func workspaceErrorAlert(_ workspace: BookWorkspaceStore) -> some View {
        modifier(WorkspaceErrorAlert(workspace: workspace))
    }
}

/// Error banner with an optional retry, shared by the workspace tabs.
struct ErrorBanner: View {
    let message: String
    var retry: (() -> Void)?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(message).font(.footnote).foregroundStyle(.red)
            if let retry {
                Button(Copy.actionRetry, action: retry).font(.footnote)
            }
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.red.opacity(0.06), in: .rect(cornerRadius: 10))
    }
}