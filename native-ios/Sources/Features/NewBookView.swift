import SwiftUI

struct NewBookView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var title = ""
    @State private var genre = "玄幻"
    @State private var idea = ""
    @State private var isSaving = false
    @State private var error: String?
    let onSave: (NewBookDraft) async throws -> BookDetail
    let onCreated: (BookDetail) -> Void
    private let genres = ["玄幻", "仙侠", "都市", "悬疑", "言情"]

    var body: some View {
        NavigationStack {
            Form {
                Section("故事") {
                    TextField("书名", text: $title)
                    Picker("题材", selection: $genre) {
                        ForEach(genres, id: \.self) { Text($0) }
                    }
                    TextField("主角、困境、目标和核心冲突", text: $idea, axis: .vertical)
                        .lineLimit(3...6)
                }
                Section {
                    Text("接下来可以在本机补充大纲、人物和写作规则。生成内容需要你自己的模型 API Key，阅读和整理不需要。")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            .navigationTitle("新建书籍")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("取消") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("创建") { Task { await save() } }
                        .disabled(title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || isSaving)
                }
            }
            .disabled(isSaving)
            .overlay { if isSaving { ProgressView() } }
            .alert("无法创建书籍", isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
                Button("好") { error = nil }
            } message: { Text(error ?? "") }
        }
        .presentationDetents([.medium, .large])
    }

    private func save() async {
        isSaving = true
        defer { isSaving = false }
        let cleanTitle = title.trimmingCharacters(in: .whitespacesAndNewlines)
        do {
            let book = try await onSave(NewBookDraft(
                bookId: "book-\(UUID().uuidString.lowercased().prefix(8))",
                title: cleanTitle,
                genre: genre,
                idea: idea.trimmingCharacters(in: .whitespacesAndNewlines)
            ))
            dismiss()
            onCreated(book)
        } catch { self.error = error.localizedDescription }
    }
}
