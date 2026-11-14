import SwiftUI

struct RunListView: View {
    @Environment(LocalCoreService.self) private var core
    @State private var runs: [RunRecord] = []
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Group {
                if runs.isEmpty {
                    ContentUnavailableView("暂无模型日志", systemImage: "list.bullet.rectangle", description: Text("模型调用、耗时与失败原因会记录在这里。"))
                } else {
                    List(runs) { run in
                        VStack(alignment: .leading, spacing: 6) {
                            HStack {
                                Text(run.kind).font(.headline)
                                Spacer()
                                Text(run.statusTitle)
                                    .font(.caption)
                                    .foregroundStyle(run.status == "failed" ? .red : .secondary)
                            }
                            Text(run.modelId).font(.subheadline).lineLimit(2)
                            Text("\(run.bookId) · \(run.callsUsed) 次调用")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                            if let error = run.error, !error.isEmpty {
                                Text(error).font(.footnote).foregroundStyle(.red)
                            }
                        }
                        .padding(.vertical, 4)
                    }
                    .refreshable { await load() }
                }
            }
            .navigationTitle("模型日志")
            .task { await load() }
            .alert("日志读取失败", isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
                Button("好") { error = nil }
            } message: { Text(error ?? "") }
        }
    }

    private func load() async {
        do { runs = try await NovelAPIClient(core: core).runs() }
        catch { self.error = error.localizedDescription }
    }
}
