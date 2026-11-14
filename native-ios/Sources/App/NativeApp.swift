import SwiftUI

@main
struct DanmoNovelApp: App {
    @State private var core: LocalCoreService
    @State private var library: LibraryStore

    init() {
        let core = LocalCoreService.shared
        _core = State(initialValue: core)
        _library = State(initialValue: LibraryStore(client: NovelAPIClient(core: core)))
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(core)
                .environment(library)
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
                .tabItem { Label("书架", systemImage: "books.vertical") }
            RunListView()
                .tabItem { Label("日志", systemImage: "list.bullet.rectangle") }
            SettingsView()
                .tabItem { Label("设置", systemImage: "gearshape") }
        }
        .overlay {
            if !core.isReady && core.error == nil {
                ProgressView("正在准备本地工作区…")
                    .padding(24)
                    .background(.regularMaterial, in: .rect(cornerRadius: 16))
            }
        }
        .task(id: core.isReady) {
            if core.isReady { await library.loadBooks() }
        }
        .alert("操作未完成", isPresented: Binding(
            get: { library.error != nil },
            set: { if !$0 { library.error = nil } }
        )) {
            Button("好") { library.error = nil }
        } message: {
            Text(library.error ?? "")
        }
    }
}
