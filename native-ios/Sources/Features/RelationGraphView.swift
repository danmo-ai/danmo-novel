import SwiftUI

/// Cast relation graph: protagonist centered, others on an ellipse; solid
/// edges are formal `## 关系` tables, dashed edges are prose relations.
/// Tapping a node lists its relations and opens the card.
struct RelationGraphView: View {
    let cards: [CastCard]
    let openCard: (String) -> Void

    @State private var selected: String?

    private var protagonist: CastCard? { cards.first { CastRelations.isProtagonist($0.role) } }
    private var relations: [CastRelation] { CastRelations.parse(cards: cards) }

    private struct Node: Identifiable {
        let card: CastCard
        let point: CGPoint
        let center: Bool
        var id: String { card.stem }
    }

    private func nodes(in size: CGSize) -> [Node] {
        let centerX = size.width / 2
        let centerY = size.height / 2
        let radiusX = size.width / 2 - 44
        let radiusY = size.height / 2 - 44
        var out: [Node] = []
        if let protagonist {
            out.append(Node(card: protagonist, point: CGPoint(x: centerX, y: centerY), center: true))
        }
        let others = cards.filter { $0.stem != protagonist?.stem }
        for (index, card) in others.enumerated() {
            let angle = -Double.pi / 2 + Double(index) * 2 * Double.pi / Double(max(1, others.count))
            out.append(Node(card: card, point: CGPoint(x: centerX + cos(angle) * radiusX, y: centerY + sin(angle) * radiusY), center: false))
        }
        return out
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text(Copy.relationsNote).font(.caption).foregroundStyle(.secondary)
                if protagonist == nil {
                    Text(Copy.relationsNoProtagonist).font(.caption).foregroundStyle(.orange)
                }
                Text(Copy.relationsCount(relations.count, cards.count)).font(.caption2).foregroundStyle(.secondary)
            }
            if cards.isEmpty {
                Text(Copy.relationsEmpty).font(.footnote).foregroundStyle(.secondary)
            } else {
                GeometryReader { proxy in
                    let layout = nodes(in: proxy.size)
                    let byStem = Dictionary(uniqueKeysWithValues: layout.map { ($0.card.stem, $0) })
                    ZStack {
                        Canvas { context, _ in
                            for relation in relations {
                                guard let a = byStem[relation.from], let b = byStem[relation.to] else { continue }
                                var path = Path()
                                path.move(to: a.point)
                                path.addLine(to: b.point)
                                let style = StrokeStyle(
                                    lineWidth: 1.5,
                                    dash: relation.formal ? [] : [5, 5]
                                )
                                context.stroke(path, with: .color(Color.accentColor.opacity(relation.formal ? 0.6 : 0.4)), style: style)
                            }
                        }
                        ForEach(layout) { node in
                            Button {
                                selected = selected == node.card.stem ? nil : node.card.stem
                            } label: {
                                VStack(spacing: 4) {
                                    Circle()
                                        .fill(node.center ? Color.accentColor : Color.accentColor.opacity(0.15))
                                        .overlay(Circle().stroke(selected == node.card.stem ? Color.orange : Color.accentColor, lineWidth: selected == node.card.stem ? 3 : 1.5))
                                        .frame(width: node.center ? 44 : 34, height: node.center ? 44 : 34)
                                    Text(node.card.name)
                                        .font(.caption2)
                                        .foregroundStyle(.primary)
                                        .lineLimit(1)
                                }
                            }
                            .buttonStyle(.plain)
                            .position(node.point)
                            .accessibilityIdentifier("relation.node.\(node.card.stem)")
                        }
                    }
                }
                .frame(height: 320)
                .background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: 14))

                HStack(spacing: 16) {
                    HStack(spacing: 6) {
                        Rectangle().fill(Color.accentColor).frame(width: 20, height: 2)
                        Text(Copy.relationsLegendFormal).font(.caption2).foregroundStyle(.secondary)
                    }
                    HStack(spacing: 6) {
                        HStack(spacing: 3) {
                            ForEach(0..<4, id: \.self) { _ in
                                Rectangle().fill(Color.accentColor.opacity(0.5)).frame(width: 4, height: 2)
                            }
                        }
                        Text(Copy.relationsLegendInformal).font(.caption2).foregroundStyle(.secondary)
                    }
                }

                if let selected, let card = cards.first(where: { $0.stem == selected }) {
                    VStack(alignment: .leading, spacing: 8) {
                        HStack {
                            Text(card.name).font(.headline)
                            Spacer()
                            Button(Copy.relationsOpenCard) { openCard(card.stem) }
                                .font(.caption)
                        }
                        if let tagline = card.tagline, !tagline.isEmpty {
                            Text(tagline).font(.caption).foregroundStyle(.secondary)
                        }
                        let own = relations.filter { $0.from == selected || $0.to == selected }
                        if own.isEmpty {
                            Text(Copy.relationsNoneHint).font(.caption).foregroundStyle(.secondary)
                        }
                        ForEach(own) { relation in
                            VStack(alignment: .leading, spacing: 2) {
                                Text("\(name(relation.from)) ↔ \(name(relation.to))")
                                    .font(.caption.weight(.semibold))
                                Text(relation.type + (relation.current.isEmpty ? "" : " · \(relation.current)"))
                                    .font(.caption).foregroundStyle(.secondary)
                                Text(relation.formal ? Copy.relationsFormal : Copy.relationsInformal)
                                    .font(.caption2).foregroundStyle(.tertiary)
                            }
                            .padding(.vertical, 3)
                        }
                    }
                    .padding(12)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: 12))
                } else {
                    Text(Copy.relationsPickHint).font(.caption).foregroundStyle(.secondary)
                }
            }
        }
    }

    private func name(_ stem: String) -> String {
        cards.first { $0.stem == stem }?.name ?? stem
    }
}