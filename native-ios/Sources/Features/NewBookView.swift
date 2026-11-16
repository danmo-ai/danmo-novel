import SwiftUI

/// The new-book form: basic info + creative seed, the same fields as the
/// desktop 立项 form. Everything lands in outline/idea.md for later use; the
/// idea-expansion / structured-planning flow itself is deferred to desktop.
struct NewBookView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var draft = NewBookDraft(bookId: "", title: "", genre: Copy.genres[0])
    @State private var idTouched = false
    @State private var isSaving = false
    @State private var error: String?
    let onSave: (NewBookDraft) async throws -> BookDetail
    let onCreated: (BookDetail) -> Void

    var body: some View {
        NavigationStack {
            Form {
                Section(Copy.newBookSection) {
                    TextField(Copy.newBookNameLabel, text: $draft.title)
                        .onChange(of: draft.title) { _, title in
                            guard !idTouched else { return }
                            draft.bookId = Self.slugify(title)
                        }
                    TextField(Copy.newBookIDLabel + " · " + Copy.newBookIDHint, text: $draft.bookId)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .onChange(of: draft.bookId) { _, _ in idTouched = true }
                    Picker(Copy.newBookGenreLabel, selection: $draft.genre) {
                        ForEach(Copy.genres, id: \.self) { Text($0) }
                    }
                    TextField(Copy.newBookSubgenreLabel + " · " + Copy.newBookSubgenreHint, text: $draft.subgenre)
                    TextField(Copy.newBookPlatformLabel + " · " + Copy.newBookPlatformHint, text: $draft.platform)
                    TextField(Copy.newBookAudienceLabel + " · " + Copy.newBookAudienceHint, text: $draft.audience)
                    TextField(Copy.newBookLengthLabel + " · " + Copy.newBookLengthHint, text: $draft.length)
                    TextField(Copy.newBookChapterCharsLabel + " · " + Copy.newBookChapterCharsHint, text: $draft.chapterChars)
                        .keyboardType(.numberPad)
                }
                Section(Copy.newBookSeedSection) {
                    TextField(Copy.newBookIdeaLabel + " · " + Copy.newBookIdeaHint, text: $draft.idea, axis: .vertical)
                        .lineLimit(2...5)
                    TextField(Copy.newBookToneLabel + " · " + Copy.newBookToneHint, text: $draft.tone)
                    TextField(Copy.newBookPleasureLabel + " · " + Copy.newBookPleasureHint, text: $draft.pleasure, axis: .vertical)
                        .lineLimit(1...3)
                    TextField(Copy.newBookProtagonistLabel + " · " + Copy.newBookProtagonistHint, text: $draft.protagonist, axis: .vertical)
                        .lineLimit(1...3)
                    TextField(Copy.newBookConflictLabel + " · " + Copy.newBookConflictHint, text: $draft.conflict, axis: .vertical)
                        .lineLimit(1...3)
                    TextField(Copy.newBookEndingLabel + " · " + Copy.newBookEndingHint, text: $draft.ending)
                    TextField(Copy.newBookPOVLabel + " · " + Copy.newBookPOVHint, text: $draft.pov)
                    TextField(Copy.newBookTaboosLabel + " · " + Copy.newBookTaboosHint, text: $draft.taboos, axis: .vertical)
                        .lineLimit(1...3)
                    TextField(Copy.newBookReferencesLabel + " · " + Copy.newBookReferencesHint, text: $draft.references, axis: .vertical)
                        .lineLimit(1...3)
                }
                Section {
                    Text(Copy.newBookFootnote)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            .navigationTitle(Copy.shelfNewBook)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(Copy.actionCancel) { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button(Copy.actionCreate) { Task { await save() } }
                        .disabled(draft.title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || isSaving)
                }
            }
            .disabled(isSaving)
            .overlay { if isSaving { ProgressView() } }
            .alert(Copy.newBookFailed, isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
                Button(Copy.actionOK) { error = nil }
            } message: { Text(error ?? "") }
        }
        .presentationDetents([.large])
    }

    /// Same slug rule as the desktop: lowercase ascii, dashes; a Chinese title
    /// slugs to empty and falls back to a timestamp id the author can edit.
    static func slugify(_ text: String) -> String {
        let lowered = text.lowercased().trimmingCharacters(in: .whitespacesAndNewlines)
        var slug = ""
        var lastWasDash = false
        for character in lowered {
            if character.isASCII && (character.isLetter || character.isNumber) {
                slug.append(character)
                lastWasDash = false
            } else if !lastWasDash && !slug.isEmpty {
                slug.append("-")
                lastWasDash = true
            }
        }
        slug = slug.trimmingCharacters(in: CharacterSet(charactersIn: "-"))
        if slug.isEmpty && !lowered.isEmpty {
            let stamp = String(Int(Date().timeIntervalSince1970), radix: 36)
            return "book-" + String(stamp.suffix(6))
        }
        return slug
    }

    private func save() async {
        isSaving = true
        defer { isSaving = false }
        var clean = draft
        clean.title = draft.title.trimmingCharacters(in: .whitespacesAndNewlines)
        clean.idea = draft.idea.trimmingCharacters(in: .whitespacesAndNewlines)
        if clean.bookId.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            clean.bookId = "book-\(UUID().uuidString.lowercased().prefix(8))"
        }
        do {
            let book = try await onSave(clean)
            dismiss()
            onCreated(book)
        } catch { self.error = error.localizedDescription }
    }
}