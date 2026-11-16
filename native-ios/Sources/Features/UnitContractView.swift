import SwiftUI

/// The unit writing contract: desire/obstacle/choice fields, meta chips,
/// per-chapter plans with scene beats — the mobile UnitContract.vue.
struct UnitContractView: View {
    let unit: UnitInfo
    let book: BookSnapshot?
    var compact = false

    private var contract: UnitContract { UnitContract(unit: unit, book: book) }

    var body: some View {
        let model = contract
        VStack(alignment: .leading, spacing: 14) {
            if !model.hasContent && model.orphanSceneGroups.isEmpty {
                VStack(spacing: 6) {
                    Text(Copy.unitContractEmpty).font(.subheadline)
                    Text(Copy.unitContractEmptyHint).font(.caption).foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 16)
            } else {
                if !model.meta.isEmpty || !model.onStage.isEmpty {
                    FlowChips(items: model.meta + model.onStage.map { book?.castName($0) ?? $0 })
                }
                if !model.onStageIdentities.isEmpty {
                    FlowChips(items: model.onStageIdentities.map { "\($0.name) · \($0.identity)" }, tinted: true)
                }
                if !model.gapFromPrev.isEmpty {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(Copy.contractGapNote).font(.caption.weight(.semibold)).foregroundStyle(Color.accentColor)
                        Text(model.gapFromPrev).font(.footnote).foregroundStyle(.secondary)
                    }
                    .padding(10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color.accentColor.opacity(0.05), in: .rect(cornerRadius: 10))
                }
                ForEach(model.fields) { field in
                    LabeledFieldView(label: field.label, value: field.value)
                }
                if !model.infoControl.isEmpty {
                    LabeledFieldView(label: Copy.contractInfoControl, value: model.infoControl)
                }
                if !model.chapterPlans.isEmpty {
                    Text("\(Copy.contractChapterPlans) · \(Copy.chaptersCount(model.chapterPlans.count))")
                        .font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                    ForEach(model.chapterPlans) { chapter in
                        chapterPlan(chapter)
                    }
                }
                if !model.orphanSceneGroups.isEmpty {
                    DisclosureGroup {
                        ForEach(model.orphanSceneGroups, id: \.chapter) { group in
                            Text(Copy.chapterHeading(Int(group.chapter) ?? 0))
                                .font(.caption.weight(.semibold)).foregroundStyle(Color.accentColor)
                            ForEach(group.scenes) { scene in sceneBeat(scene) }
                        }
                    } label: {
                        Text("\(Copy.contractOrphanScenes) · \(Copy.contractOrphanHint)")
                            .font(.caption)
                    }
                }
            }
        }
    }

    private func chapterPlan(_ chapter: UnitContract.ChapterPlan) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(Copy.chapterHeading(Int(chapter.chapter) ?? 0))
                    .font(.footnote.weight(.semibold)).foregroundStyle(Color.accentColor)
                Spacer()
                Text(Copy.dayOffset(chapter.dayOffset)).font(.caption2).foregroundStyle(.secondary)
            }
            labeledLine(Copy.contractGoal, chapter.goal)
            labeledLine(Copy.contractChange, chapter.change)
            labeledLine(Copy.contractCutHook, chapter.cutHook)
            if chapter.scenes.isEmpty {
                Text(Copy.contractNoScenes).font(.caption).foregroundStyle(.orange)
            } else {
                Text("\(Copy.contractScenes) · \(Copy.scenesCount(chapter.scenes.count))")
                    .font(.caption.weight(.semibold)).foregroundStyle(Color.accentColor)
                ForEach(chapter.scenes) { scene in sceneBeat(scene) }
            }
        }
        .padding(12)
        .background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: 10))
    }

    private func sceneBeat(_ scene: UnitContract.Scene) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                Text(scene.id).font(.caption.weight(.semibold)).foregroundStyle(.orange)
                if !scene.who.isEmpty { Text(scene.who).font(.caption2).foregroundStyle(.secondary) }
                if !scene.place.isEmpty { Text(scene.place).font(.caption2).foregroundStyle(.secondary) }
            }
            labeledLine(Copy.contractGoal, scene.goal)
            labeledLine(Copy.contractSceneConflict, scene.conflict)
            labeledLine(Copy.contractSceneTurn, scene.turn)
            labeledLine(Copy.contractSceneOutcome, scene.outcome)
            if !scene.info.isEmpty {
                Text(scene.info).font(.caption2).foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 4)
    }

    @ViewBuilder
    private func labeledLine(_ label: String, _ value: String) -> some View {
        if !value.isEmpty {
            (Text(label + " ").font(.caption.weight(.semibold)) + Text(value).font(.caption).foregroundStyle(.secondary))
        }
    }
}

/// Wrapping chip row (meta info, cast names, identities).
struct FlowChips: View {
    let items: [String]
    var tinted = false

    var body: some View {
        // A simple wrapping layout via Layout keeps chips readable at any width.
        ChipLayout(spacing: 6) {
            ForEach(items, id: \.self) { item in
                Text(item)
                    .font(.caption2)
                    .padding(.horizontal, 8).padding(.vertical, 4)
                    .background(tinted ? Color.orange.opacity(0.1) : Color.secondary.opacity(0.08), in: .rect(cornerRadius: 6))
                    .foregroundStyle(tinted ? .orange : .secondary)
            }
        }
    }
}

struct ChipLayout: Layout {
    var spacing: CGFloat = 6

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let maxWidth = proposal.width ?? .infinity
        var x: CGFloat = 0
        var y: CGFloat = 0
        var rowHeight: CGFloat = 0
        for subview in subviews {
            let size = subview.sizeThatFits(.unspecified)
            if x + size.width > maxWidth, x > 0 {
                x = 0
                y += rowHeight + spacing
                rowHeight = 0
            }
            x += size.width + spacing
            rowHeight = max(rowHeight, size.height)
        }
        return CGSize(width: maxWidth == .infinity ? x : maxWidth, height: y + rowHeight)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX
        var y = bounds.minY
        var rowHeight: CGFloat = 0
        for subview in subviews {
            let size = subview.sizeThatFits(.unspecified)
            if x + size.width > bounds.maxX, x > bounds.minX {
                x = bounds.minX
                y += rowHeight + spacing
                rowHeight = 0
            }
            subview.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(size))
            x += size.width + spacing
            rowHeight = max(rowHeight, size.height)
        }
    }
}