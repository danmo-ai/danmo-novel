import SwiftUI

@main
struct DanmoNovelApp: App {
    @State private var core: LocalCoreService
    @State private var library: LibraryStore
    @State private var workspaces: WorkspaceRegistry

    init() {
        let core = LocalCoreService.shared
        let api = NovelAPIClient(core: core)
        _core = State(initialValue: core)
        _library = State(initialValue: LibraryStore(client: api))
        _workspaces = State(initialValue: WorkspaceRegistry(api: api))
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(core)
                .environment(library)
                .environment(workspaces)
                // No custom tint. A hardcoded accent (this used to be .indigo)
                // overrides the system accent everywhere — controls, links,
                // progress, the selected tab — and ignores the user's own tint
                // choice in Settings > Accessibility. A brand colour, if we ever
                // want one, belongs in an AccentColor asset so it still adapts to
                // dark mode and to Increase Contrast.
                .task { core.start() }
        }
    }
}

struct RootView: View {
    @Environment(LocalCoreService.self) private var core
    @Environment(LibraryStore.self) private var library

    var body: some View {
        TabView {
            ShelfView()
                .tabItem { Label(Copy.tabShelf, systemImage: "books.vertical") }
            RunListView()
                .tabItem { Label(Copy.tabLogs, systemImage: "list.bullet.rectangle") }
            KnowledgeView()
                .tabItem { Label(Copy.tabKnowledge, systemImage: "text.book.closed") }
            SettingsView()
                .tabItem { Label(Copy.tabSettings, systemImage: "gearshape") }
        }
        .overlay {
            if !core.isReady && core.error == nil {
                ProgressView(Copy.corePreparing)
                    .padding(24)
                    .background(.regularMaterial, in: .rect(cornerRadius: 16))
            }
        }
        .task(id: core.isReady) {
            if core.isReady { await library.loadBooks() }
        }
        .alert(Copy.errorOperationFailed, isPresented: Binding(
            get: { library.error != nil },
            set: { if !$0 { library.error = nil } }
        )) {
            Button(Copy.actionOK) { library.error = nil }
        } message: {
            Text(library.error ?? "")
        }
    }
}
