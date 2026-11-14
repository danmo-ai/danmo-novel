import SwiftUI

struct SettingsView: View {
    @Environment(LocalCoreService.self) private var core
    @State private var providers: [Provider] = []
    @State private var isLoading = false
    @State private var editingProvider: Provider?
    @State private var error: String?

    var body: some View {
        NavigationStack {
            List {
                Section {
                    ForEach(providers) { provider in
                        VStack(alignment: .leading, spacing: 5) {
                            HStack {
                                Text(provider.name).font(.headline)
                                Spacer()
                                Image(systemName: provider.hasApiKey ? "key.fill" : "key.slash")
                                    .foregroundStyle(provider.hasApiKey ? .green : .secondary)
                            }
                            Text(provider.provider).font(.subheadline).foregroundStyle(.secondary)
                            if !provider.models.isEmpty {
                                Text(provider.models.filter(\.enabled).map(\.name).joined(separator: "、"))
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                            }
                        }
                        .padding(.vertical, 4)
                        .contentShape(Rectangle())
                        .onTapGesture { editingProvider = provider }
                    }
                } header: {
                    Text("模型提供商")
                } footer: {
                    Text("密钥仅写入本机工作区，不会出现在页面或日志中。模型调用只连接你配置的服务。")
                }
                Section("数据与隐私") {
                    LabeledContent("书稿存储", value: "本机")
                    LabeledContent("账户", value: "无需注册")
                    LabeledContent("跟踪", value: "无")
                }
            }
            .navigationTitle("设置")
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button { editingProvider = Provider.new } label: { Label("添加提供商", systemImage: "plus") }
                }
            }
            .refreshable { await load() }
            .task { await load() }
            .sheet(item: $editingProvider, onDismiss: { Task { await load() } }) { provider in
                ProviderEditorView(provider: provider) { request in
                    try await NovelAPIClient(core: core).saveProvider(request, id: provider.id.isEmpty ? nil : provider.id)
                }
            }
            .alert("设置读取失败", isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
                Button("好") { error = nil }
            } message: { Text(error ?? "") }
        }
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do { providers = try await NovelAPIClient(core: core).providers() }
        catch { self.error = error.localizedDescription }
    }
}

extension Provider {
    static var new: Provider? {
        Provider(id: "", name: "", provider: "openai", baseUrl: nil, hasApiKey: false, models: [])
    }
}

private struct ProviderEditorView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var preset = "openai"
    @State private var name = "OpenAI"
    @State private var baseURL = "https://api.openai.com/v1"
    @State private var apiKey = ""
    @State private var models = "gpt-4.1-mini"
    @State private var saving = false
    @State private var error: String?
    let provider: Provider?
    let onSave: (ProviderRequest) async throws -> Provider
    private let presets: [(id: String, name: String, url: String)] = [
        ("openai", "OpenAI", "https://api.openai.com/v1"),
        ("anthropic", "Anthropic", "https://api.anthropic.com"),
        ("deepseek", "DeepSeek", "https://api.deepseek.com/v1"),
        ("qwen", "通义千问", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
        ("kimi", "Moonshot", "https://api.moonshot.cn/v1")
    ]

    var body: some View {
        NavigationStack {
            Form {
                Picker("服务", selection: $preset) {
                    ForEach(presets, id: \.id) { Text($0.name).tag($0.id) }
                }
                    .onChange(of: preset) { _, value in
                    guard provider?.id.isEmpty != false, let item = presets.first(where: { $0.id == value }) else { return }
                    name = item.name
                    baseURL = item.url
                }
                TextField("名称", text: $name)
                TextField("Base URL", text: $baseURL)
                    .keyboardType(.URL)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                SecureField(provider?.hasApiKey == true ? "API Key（留空保持不变）" : "API Key", text: $apiKey)
                    .textContentType(.password)
                TextField("模型名称，多个用逗号分隔", text: $models, axis: .vertical)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            }
            .navigationTitle(provider?.id.isEmpty == false ? "编辑提供商" : "添加提供商")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("保存") { Task { await save() } }
                        .disabled((provider?.hasApiKey != true && apiKey.isEmpty) || name.isEmpty || models.isEmpty || saving)
                }
            }
            .alert("保存失败", isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
                Button("好") { error = nil }
            } message: { Text(error ?? "") }
        }
        .onAppear {
            guard let provider, !provider.id.isEmpty else { return }
            name = provider.name
            preset = provider.provider
            baseURL = provider.baseUrl ?? ""
            models = provider.models.map(\.name).joined(separator: ", ")
        }
    }

    private func save() async {
        saving = true
        defer { saving = false }
        let refs = models.split(separator: ",").map { ProviderModel(name: $0.trimmingCharacters(in: .whitespaces)) }
        do {
            _ = try await onSave(ProviderRequest(
                provider: preset,
                name: name,
                apiKey: apiKey,
                baseUrl: baseURL,
                defaultProtocolDialect: preset == "deepseek" ? "deepseek" : preset == "qwen" ? "qwen" : preset == "kimi" ? "kimi" : nil,
                models: refs
            ))
            apiKey = ""
            dismiss()
        } catch { self.error = error.localizedDescription }
    }
}
