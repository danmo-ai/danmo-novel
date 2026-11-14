import Foundation

@MainActor
@Observable
final class LibraryStore {
    private let api: NovelAPIClient
    var books: [BookSummary] = []
    var isLoading = false
    var error: String?

    init(client: NovelAPIClient) { api = client }

    func loadBooks() async {
        guard !isLoading else { return }
        isLoading = true
        defer { isLoading = false }
        do { books = try await api.books() }
        catch { self.error = error.localizedDescription }
    }

    func book(id: String) async throws -> BookDetail {
        try await api.book(id: id)
    }

    func createBook(_ draft: NewBookDraft) async throws -> BookDetail {
        let book = try await api.createBook(draft)
        try await reload()
        return book
    }

    func createDemoBook() async throws -> BookDetail {
        let response = try await api.createDemoBook()
        try await reload()
        return response.book
    }

    private func reload() async throws {
        books = try await api.books()
    }
}
