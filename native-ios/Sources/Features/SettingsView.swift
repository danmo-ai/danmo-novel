import SwiftUI

/// The settings tab: providers, stage model assignments, review parameters,
/// per-model catalog, book directory + iCloud sync, privacy — the desktop
/// SettingsView's five tabs as one grouped form.
struct SettingsView: View {
    @Environment(LocalCoreService.self) private var core
    @State private var providers: [Provider] = []
    @State private var presets: [ProviderPreset] = []
    @State private var models: [LLMModel] = []
    @State private var editingProvider: ProviderEditorState?
    @State private var error: String?
    @State private var toast: String?
    @State private var sync: SyncStatus?
    @State private var syncing = false
    @State private var version = ""
    @State private var deletingProvider: Provider?

    private var api: NovelAPIClient { NovelAPIClient(core: core) }

    var body: some View {
        NavigationStack {
            List {
                providersSection
                modelsSection
                StageAssignmentsSection(api: api, models: models, notify: note, fail: { error = $0 })
                ReviewSettingsSection(api: api, notify: note, fail: { error = $0 })
                ModelConfigsSection(api: api, notify: note, fail: { error = $0 })
                directorySection
                syncSection
                privacySection
            }
            .navigationTitle(Copy.settingsTitle)
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button {
                        editingProvider = ProviderEditorState(provider: nil, preset: nil)
                    } label: { Label(Copy.addProvider, systemImage: "plus") }
                }
            }
            .refreshable { await load() }
            .task { await load() }
            .sheet(item: $editingProvider, onDismiss: { Task { await load() } }) { state in
                ProviderEditorView(state: state, api: api)
            }
            .alert(Copy.settingsLoadFailed, isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
                Button(Copy.actionOK) { error = nil }
            } message: { Text(error ?? "") }
            .alert(Copy.providerDeleteTitle, isPresented: Binding(get: { deletingProvider != nil }, set: { if !$0 { deletingProvider = nil } })) {
                Button(Copy.actionCancel, role: .cancel) { deletingProvider = nil }
                Button(Copy.actionDelete, role: .destructive) {
                    if let provider = deletingProvider {
                        deletingProvider = nil
                        Task { await deleteProvider(provider) }
                    }
                }
            } message: {
                Text(Copy.providerDeleteMessage)
            }
            .overlay(alignment: .bottom) {
                if let toast {
                    Text(toast)
                        .font(.footnote)
                        .padding(.horizontal, 16).padding(.vertical, 10)
                        .background(.regularMaterial, in: .capsule)
                        .padding(.bottom, 12)
                }
            }
        }
    }

    private func note(_ message: String) {
        toast = message
        Task {
            try? await Task.sleep(for: .seconds(3))
            if toast == message { toast = nil }
        }
    }

    private func deleteProvider(_ provider: Provider) async {
        do {
            try await api.deleteProvider(id: provider.id)
            await load()
        } catch {
            self.error = error.localizedDescription
        }
    }

    // MARK: Providers

    private var providersSection: some View {
        Section {
            if !presets.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(presets) { preset in
                            Button(preset.name) {
                                editingProvider = ProviderEditorState(provider: nil, preset: preset)
                            }
                            .font(.caption)
                            .buttonStyle(.bordered)
                        }
                    }
                }
            }
            ForEach(providers) { provider in
                Button {
                    editingProvider = ProviderEditorState(provider: provider, preset: nil)
                } label: {
                    VStack(alignment: .leading, spacing: 5) {
                        HStack {
                            Text(provider.name).font(.headline).foregroundStyle(.primary)
                            Spacer()
                            Image(systemName: provider.hasApiKey ? "key.fill" : "key.slash")
                                .foregroundStyle(provider.hasApiKey ? .green : .secondary)
                        }
                        Text(provider.provider).font(.subheadline).foregroundStyle(.secondary)
                        Text(provider.models.filter(\.enabled).map(\.name).joined(separator: "、"))
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .lineLimit(2)
                        if provider.models.filter(\.enabled).isEmpty {
                            Text(Copy.providerNoModels).font(.footnote).foregroundStyle(.orange)
                        }
                    }
                    .padding(.vertical, 4)
                }
                .swipeActions(edge: .trailing) {
                    Button(Copy.actionDelete, role: .destructive) { deletingProvider = provider }
                }
            }
        } header: {
            Text(Copy.providersSection)
        } footer: {
            Text(Copy.keyNote)
        }
    }

    // MARK: Available models + connectivity test

    private var modelsSection: some View {
        Section(Copy.modelsSection) {
            if models.isEmpty {
                Text(Copy.modelsEmpty).font(.footnote).foregroundStyle(.secondary)
            }
            ForEach(models) { model in
                ModelTestRow(model: model, api: api)
            }
        }
    }

    // MARK: Directory

    private var directorySection: some View {
        Section {
            if let root = sync?.libraryRoot, !root.isEmpty {
                Text(root)
                    .font(.footnote.monospaced())
                    .foregroundStyle(.secondary)
                    .lineLimit(3)
                    .truncationMode(.middle)
            }
            NavigationLink {
                LibraryDirectoryView(api: api, sync: sync) {
                    Task { await load() }
                }
            } label: {
                Label(Copy.libraryChange, systemImage: "folder")
            }
        } header: {
            Text(Copy.libraryRow)
        } footer: {
            Text((sync?.customLibraryRoot?.isEmpty == false) ? Copy.libraryCustomNote : Copy.libraryDefaultNote)
        }
    }

    // MARK: Sync

    private var syncSection: some View {
        Section {
            LabeledContent(Copy.libraryRow, value: core.syncEnabled ? Copy.iCloudDrive : Copy.thisDevice)
            LabeledContent(Copy.modelSettingsRow, value: core.syncEnabled ? Copy.sharedValue : Copy.localOnlyValue)
            if let pulled = sync?.lastPulledAt, let label = syncLabel(pulled) {
                LabeledContent(Copy.lastSync, value: label)
            }
            Button {
                Task { await syncNow() }
            } label: {
                Label(syncing ? Copy.syncing : Copy.syncNow, systemImage: "arrow.triangle.2.circlepath")
            }
            .disabled(syncing || sync?.enabled != true)
        } header: {
            Text(Copy.syncSection)
        } footer: {
            Text(core.syncUnavailableReason ?? Copy.syncFooter)
        }
    }

    private var privacySection: some View {
        Section(Copy.privacySection) {
            LabeledContent(Copy.storageRow, value: core.syncEnabled ? Copy.iCloudDrive : Copy.thisDevice)
            LabeledContent(Copy.accountRow, value: Copy.noAccountValue)
            LabeledContent(Copy.trackingRow, value: Copy.trackingNoneValue)
            LabeledContent(Copy.versionRow, value: version.isEmpty || version == "dev" ? Copy.versionDev : version)
        }
    }

    // MARK: Loading

    private func load() async {
        do { providers = try await api.providers() }
        catch { self.error = error.localizedDescription }
        presets = (try? await api.presets()) ?? []
        models = (try? await api.llmModels()) ?? []
        sync = try? await api.syncStatus()
        struct VersionResponse: Decodable { let version: String }
        version = (try? await api.get(VersionResponse.self, path: "/api/system/version"))?.version ?? ""
    }

    /// Pull then push: the same order the core uses at startup.
    private func syncNow() async {
        syncing = true
        defer { syncing = false }
        do {
            _ = try await api.syncPull()
            sync = try await api.syncPush()
            providers = try await api.providers()
            models = (try? await api.llmModels()) ?? models
            note(Copy.syncDone)
        } catch { self.error = error.localizedDescription }
    }

    private func syncLabel(_ raw: String) -> String? {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        guard let date = formatter.date(from: raw) ?? ISO8601DateFormatter().date(from: raw) else { return nil }
        return date.formatted(date: .abbreviated, time: .shortened)
    }
}

// MARK: - Model connectivity test row

private struct ModelTestRow: View {
    let model: LLMModel
    let api: NovelAPIClient
    @State private var testing = false
    @State private var result: String?
    @State private var failed = false

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(model.id).font(.caption.monospaced()).lineLimit(2)
                if let result {
                    Text(result).font(.caption2).foregroundStyle(failed ? .red : .green)
                }
            }
            Spacer()
            Button(testing ? Copy.modelTesting : Copy.modelTest) {
                testing = true
                Task {
                    do {
                        let response = try await api.testModel(id: model.id)
                        failed = !response.ok
                        result = response.ok ? Copy.modelTestOK(response.durationMs ?? 0) : (response.error ?? Copy.modelTestFailed)
                    } catch {
                        failed = true
                        result = error.localizedDescription
                    }
                    testing = false
                }
            }
            .font(.caption)
            .buttonStyle(.bordered)
            .disabled(testing)
        }
        .padding(.vertical, 2)
    }
}

// MARK: - Provider editor

struct ProviderEditorState: Identifiable {
    let id = UUID()
    let provider: Provider?
    let preset: ProviderPreset?
}

struct ProviderEditorView: View {
    @Environment(\.dismiss) private var dismiss
    let state: ProviderEditorState
    let api: NovelAPIClient

    @State private var protocolType = "openai"
    @State private var name = ""
    @State private var baseURL = ""
    @State private var apiKey = ""
    @State private var defaultDialect = ""
    @State private var models: [ProviderModel] = []
    @State private var manualModel = ""
    @State private var saving = false
    @State private var fetching = false
    @State private var error: String?

    private var isEditing: Bool { state.provider != nil }
    private var hasKey: Bool { state.provider?.hasApiKey ?? false }

    private let dialects = ["", "openai", "deepseek", "qwen", "kimi", "kimi_code", "kimi_k3", "glm", "minimax", "gemini", "grok"]

    var body: some View {
        NavigationStack {
            Form {
                Section(Copy.providerFieldSection) {
                    Picker(Copy.providerProtocolLabel, selection: $protocolType) {
                        Text(Copy.providerProtocolOpenAI).tag("openai")
                        Text(Copy.providerProtocolResponses).tag("openai_responses")
                        Text(Copy.providerProtocolAnthropic).tag("anthropic")
                        Text(Copy.providerProtocolMock).tag("mock")
                    }
                    .disabled(isEditing)
                    TextField(Copy.providerName, text: $name)
                        .disabled(isEditing)
                    SecureField(hasKey ? Copy.providerAPIKeyKeep : Copy.providerAPIKey, text: $apiKey)
                        .textContentType(.password)
                        .textInputAutocapitalization(.never)
                    TextField(Copy.providerBaseUrlLabel, text: $baseURL)
                        .keyboardType(.URL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    Picker(Copy.providerDialectLabel, selection: $defaultDialect) {
                        ForEach(dialects, id: \.self) { dialect in
                            Text(dialect.isEmpty ? Copy.providerDialectAuto : dialect).tag(dialect)
                        }
                    }
                }
                Section(Copy.providerModelsSection) {
                    HStack {
                        TextField(Copy.providerAddModelHint, text: $manualModel)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .onSubmit(addManualModel)
                        Button(Copy.providerAddModel, action: addManualModel)
                            .font(.caption)
                            .disabled(manualModel.trimmingCharacters(in: .whitespaces).isEmpty)
                    }
                    Button {
                        Task { await fetchModels() }
                    } label: {
                        if fetching { ProgressView() }
                        else { Label(Copy.providerFetchModels, systemImage: "arrow.clockwise") }
                    }
                    .disabled(fetching)
                    ForEach(models.indices, id: \.self) { index in
                        VStack(alignment: .leading, spacing: 4) {
                            HStack {
                                Text(models[index].name).font(.footnote.monospaced())
                                Spacer()
                                Toggle("", isOn: $models[index].enabled)
                                    .labelsHidden()
                            }
                            Picker(Copy.providerDialectLabel, selection: Binding(
                                get: { models[index].protocolDialect ?? "" },
                                set: { models[index].protocolDialect = $0.isEmpty ? nil : $0 }
                            )) {
                                ForEach(dialects, id: \.self) { dialect in
                                    Text(dialect.isEmpty ? Copy.providerDialectAuto : dialect).tag(dialect)
                                }
                            }
                            .font(.caption)
                        }
                    }
                    .onDelete { models.remove(atOffsets: $0) }
                }
            }
            .navigationTitle(isEditing ? Copy.editProvider : Copy.addProvider)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(Copy.actionCancel) { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(Copy.actionSave) { Task { await save() } }
                        .disabled((!hasKey && apiKey.isEmpty && protocolType != "mock") || name.isEmpty || saving)
                }
            }
            .alert(Copy.saveFailed, isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
                Button(Copy.actionOK) { error = nil }
            } message: { Text(error ?? "") }
        }
        .onAppear(perform: prefill)
    }

    private func prefill() {
        if let provider = state.provider {
            protocolType = provider.provider
            name = provider.name
            baseURL = provider.baseUrl ?? ""
            defaultDialect = provider.defaultProtocolDialect ?? ""
            models = provider.models
        } else if let preset = state.preset {
            protocolType = preset.provider
            name = preset.name
            baseURL = preset.baseUrl ?? ""
            defaultDialect = preset.defaultProtocolDialect ?? ""
        } else {
            name = "OpenAI"
            baseURL = "https://api.openai.com/v1"
        }
    }

    private func addManualModel() {
        let trimmed = manualModel.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty, !models.contains(where: { $0.name == trimmed }) else { return }
        models.append(ProviderModel(name: trimmed))
        manualModel = ""
    }

    private var request: ProviderRequest {
        ProviderRequest(
            provider: protocolType,
            name: name,
            apiKey: apiKey,
            baseUrl: baseURL,
            defaultProtocolDialect: defaultDialect.isEmpty ? nil : defaultDialect,
            models: models
        )
    }

    private func fetchModels() async {
        fetching = true
        defer { fetching = false }
        do {
            let fetched = try await api.fetchModels(request)
            let previous = Dictionary(uniqueKeysWithValues: models.map { ($0.name, $0) })
            models = fetched.map { model in
                var merged = model
                if merged.protocolDialect == nil { merged.protocolDialect = previous[model.name]?.protocolDialect }
                return merged
            }
        } catch { self.error = error.localizedDescription }
    }

    private func save() async {
        saving = true
        defer { saving = false }
        do {
            _ = try await api.saveProvider(request, id: state.provider?.id)
            dismiss()
        } catch { self.error = error.localizedDescription }
    }
}

// MARK: - Stage assignments

private struct StageAssignmentsSection: View {
    let api: NovelAPIClient
    let models: [LLMModel]
    let notify: (String) -> Void
    let fail: (String) -> Void

    @State private var assignments: [String: String] = [:]
    @State private var saving = false
    @State private var loaded = false

    private let roles: [(id: String, label: String, hint: String)] = [
        ("plan", Copy.stagePlanLabel, Copy.stagePlanHint),
        ("outline", Copy.stageOutlineLabel, Copy.stageOutlineHint),
        ("write", Copy.stageWriteLabel, Copy.stageWriteHint),
        ("review", Copy.stageReviewLabel, Copy.stageReviewHint),
        ("finalize", Copy.stageFinalizeLabel, Copy.stageFinalizeHint),
        ("local_edit", Copy.stageLocalEditLabel, Copy.stageLocalEditHint),
    ]

    /// modelId is provider/model[/effort]; split for the two-level pickers.
    private func split(_ id: String) -> (base: String, effort: String) {
        let parts = id.split(separator: "/").map(String.init)
        if parts.count >= 3 { return (parts[0...1].joined(separator: "/"), parts[2]) }
        return (id, "")
    }

    private func efforts(for base: String) -> [String] {
        let available = models.first { $0.id == base }?.availableEfforts ?? []
        return [""] + (available.isEmpty ? (base.isEmpty ? [] : ["off", "low", "medium", "high", "xhigh"]) : available)
    }

    var body: some View {
        Section {
            ForEach(roles, id: \.id) { role in
                let current = split(assignments[role.id] ?? "")
                VStack(alignment: .leading, spacing: 6) {
                    Text(role.label).font(.footnote.weight(.semibold))
                    Picker(role.label, selection: Binding(
                        get: { current.base },
                        set: { base in
                            assignments[role.id] = base.isEmpty ? "" : (current.effort.isEmpty ? base : "\(base)/\(current.effort)")
                        }
                    )) {
                        Text(Copy.stageUnset).tag("")
                        ForEach(models) { model in
                            Text(model.id).tag(model.id)
                        }
                    }
                    .labelsHidden()
                    Picker(Copy.stageEffortLabel, selection: Binding(
                        get: { current.effort },
                        set: { effort in
                            assignments[role.id] = current.base.isEmpty ? "" : (effort.isEmpty ? current.base : "\(current.base)/\(effort)")
                        }
                    )) {
                        ForEach(efforts(for: current.base), id: \.self) { effort in
                            Text(effort.isEmpty ? Copy.stageEffortDefault : Copy.stageEffortOff(effort)).tag(effort)
                        }
                    }
                    .labelsHidden()
                    .disabled(current.base.isEmpty)
                    Text(role.hint).font(.caption2).foregroundStyle(.secondary)
                }
                .padding(.vertical, 4)
            }
            Button(saving ? Copy.saving : Copy.stagesSave) {
                Task { await save() }
            }
            .disabled(saving || !loaded)
        } header: {
            Text(Copy.stagesSection)
        }
        .task {
            guard !loaded else { return }
            let settings = try? await api.settings()
            for assignment in settings?.modelAssignments ?? [] {
                assignments[assignment.role] = assignment.modelId
            }
            loaded = true
        }
    }

    private func save() async {
        saving = true
        defer { saving = false }
        let payload = roles.map { ModelAssignment(role: $0.id, modelId: assignments[$0.id] ?? "") }
        do {
            _ = try await api.saveAssignments(payload)
            notify(Copy.stagesSaved)
        } catch { fail(error.localizedDescription) }
    }
}

// MARK: - Review settings

private struct ReviewSettingsSection: View {
    let api: NovelAPIClient
    let notify: (String) -> Void
    let fail: (String) -> Void

    @State private var rounds = 3
    @State private var concurrency = 3
    @State private var saving = false
    @State private var loaded = false

    var body: some View {
        Section {
            Picker(Copy.reviewRoundsLabel, selection: $rounds) {
                ForEach(1...5, id: \.self) { count in
                    Text(Copy.reviewRoundsValue(count)).tag(count)
                }
            }
            Text(Copy.reviewRoundsHint).font(.caption2).foregroundStyle(.secondary)
            Picker(Copy.reviewConcurrencyLabel, selection: $concurrency) {
                ForEach([1, 2, 3, 4, 6, 8], id: \.self) { count in
                    Text("\(count)").tag(count)
                }
            }
            Text(Copy.reviewConcurrencyHint).font(.caption2).foregroundStyle(.secondary)
            Button(saving ? Copy.saving : Copy.actionSave) {
                Task { await save() }
            }
            .disabled(saving || !loaded)
        } header: {
            Text(Copy.reviewSettingsSection)
        } footer: {
            Text(Copy.reviewSettingsNote)
        }
        .task {
            guard !loaded else { return }
            if let settings = try? await api.settings(), let review = settings.review {
                rounds = max(1, review.rounds)
                concurrency = max(1, review.concurrency)
            }
            loaded = true
        }
    }

    private func save() async {
        saving = true
        defer { saving = false }
        do {
            _ = try await api.saveReviewSettings(ReviewSettings(concurrency: concurrency, rounds: rounds))
            notify(Copy.reviewSettingsSaved)
        } catch { fail(error.localizedDescription) }
    }
}

// MARK: - Model catalog

private struct ModelConfigsSection: View {
    let api: NovelAPIClient
    let notify: (String) -> Void
    let fail: (String) -> Void

    @State private var configs: [ModelConfig] = []
    @State private var saving = false
    @State private var loaded = false
    @State private var editingIndex: Int?
    @State private var adding = false
    @State private var newName = ""

    var body: some View {
        Section {
            ForEach(configs.indices, id: \.self) { index in
                Button {
                    editingIndex = index
                } label: {
                    HStack {
                        Text(configs[index].model).font(.footnote.monospaced()).foregroundStyle(.primary)
                        Spacer()
                        Text(configSummary(configs[index])).font(.caption2).foregroundStyle(.secondary)
                    }
                }
            }
            HStack {
                Button(Copy.modelConfigAdd) { adding = true }
                    .font(.footnote)
                Spacer()
                Button(Copy.modelConfigReset) {
                    Task { await reset() }
                }
                .font(.footnote)
                .disabled(saving)
            }
            Button(saving ? Copy.saving : Copy.actionSave) {
                Task { await save() }
            }
            .disabled(saving || !loaded)
        } header: {
            Text(Copy.modelConfigSection)
        } footer: {
            Text(Copy.modelConfigNote)
        }
        .task {
            guard !loaded else { return }
            configs = (try? await api.modelConfigs()) ?? []
            loaded = true
        }
        .sheet(isPresented: Binding(get: { editingIndex != nil }, set: { if !$0 { editingIndex = nil } })) {
            if let index = editingIndex, configs.indices.contains(index) {
                ModelConfigEditor(config: $configs[index]) {
                    configs.remove(at: index)
                    editingIndex = nil
                }
            }
        }
        .alert(Copy.modelConfigAdd, isPresented: $adding) {
            TextField(Copy.modelConfigNameHint, text: $newName)
            Button(Copy.actionCancel, role: .cancel) { newName = "" }
            Button(Copy.providerAddModel) {
                let trimmed = newName.trimmingCharacters(in: .whitespaces)
                if !trimmed.isEmpty && !configs.contains(where: { $0.model == trimmed }) {
                    configs.append(ModelConfig(model: trimmed))
                    editingIndex = configs.count - 1
                }
                newName = ""
            }
        }
    }

    private func configSummary(_ config: ModelConfig) -> String {
        var parts: [String] = []
        if let window = config.contextWindow { parts.append("\(window / 1000)K") }
        if let output = config.maxOutput { parts.append("\(output / 1000)K out") }
        if let temperature = config.temperature { parts.append(String(format: "t%.1f", temperature)) }
        return parts.joined(separator: " · ")
    }

    private func save() async {
        saving = true
        defer { saving = false }
        do {
            configs = try await api.saveModelConfigs(configs.filter { !$0.model.trimmingCharacters(in: .whitespaces).isEmpty })
            notify(Copy.modelConfigSaved)
        } catch { fail(error.localizedDescription) }
    }

    private func reset() async {
        saving = true
        defer { saving = false }
        do {
            configs = try await api.refreshModelConfigs()
            notify(Copy.modelConfigResetDone)
        } catch { fail(error.localizedDescription) }
    }
}

private struct ModelConfigEditor: View {
    @Binding var config: ModelConfig
    let onDelete: () -> Void
    @Environment(\.dismiss) private var dismiss

    @State private var effortsText = ""
    @State private var stopText = ""

    var body: some View {
        NavigationStack {
            Form {
                Section(Copy.modelConfigCapacity) {
                    intField(Copy.modelConfigContextWindow, value: $config.contextWindow)
                    intField(Copy.modelConfigMaxOutput, value: $config.maxOutput)
                    Toggle(Copy.modelConfigVision, isOn: Binding(get: { config.vision ?? false }, set: { config.vision = $0 }))
                }
                Section(Copy.modelConfigSampling) {
                    doubleField(Copy.modelConfigTemperature, value: $config.temperature)
                    doubleField(Copy.modelConfigTopP, value: $config.topP)
                    doubleField(Copy.modelConfigFrequencyPenalty, value: $config.frequencyPenalty)
                    doubleField(Copy.modelConfigPresencePenalty, value: $config.presencePenalty)
                    TextField(Copy.modelConfigStop, text: $stopText)
                        .textInputAutocapitalization(.never)
                        .onChange(of: stopText) { _, text in
                            let parts = text.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
                            config.stop = parts.isEmpty ? nil : parts
                        }
                }
                Section(Copy.modelConfigReasoning) {
                    TextField(Copy.modelConfigEffortsHint, text: $effortsText)
                        .textInputAutocapitalization(.never)
                        .onChange(of: effortsText) { _, text in
                            let parts = text.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
                            config.availableEfforts = parts.isEmpty ? nil : parts
                        }
                }
                Section {
                    Button(Copy.modelConfigDelete, role: .destructive) {
                        onDelete()
                        dismiss()
                    }
                }
            }
            .navigationTitle(config.model)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(Copy.actionDone) { dismiss() }
                }
            }
        }
        .onAppear {
            effortsText = (config.availableEfforts ?? []).joined(separator: ", ")
            stopText = (config.stop ?? []).joined(separator: ", ")
        }
    }

    private func intField(_ label: String, value: Binding<Int?>) -> some View {
        HStack {
            Text(label).font(.footnote)
            Spacer()
            TextField(Copy.modelConfigPlaceholder, value: value, format: .number)
                .keyboardType(.numberPad)
                .multilineTextAlignment(.trailing)
                .frame(maxWidth: 140)
        }
    }

    private func doubleField(_ label: String, value: Binding<Double?>) -> some View {
        HStack {
            Text(label).font(.footnote)
            Spacer()
            TextField(Copy.modelConfigPlaceholder, value: value, format: .number)
                .keyboardType(.decimalPad)
                .multilineTextAlignment(.trailing)
                .frame(maxWidth: 140)
        }
    }
}

// MARK: - Library directory

private struct LibraryDirectoryView: View {
    let api: NovelAPIClient
    let sync: SyncStatus?
    let onChanged: () -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var path = ""
    @State private var moving = false
    @State private var error: String?

    var body: some View {
        Form {
            Section {
                TextField(Copy.libraryPathLabel, text: $path)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .font(.footnote.monospaced())
                Text(Copy.libraryChangeNote).font(.caption).foregroundStyle(.secondary)
            } header: {
                Text(Copy.libraryChange)
            }
            Section {
                Button(moving ? Copy.libraryMoving : Copy.libraryMoveConfirm) {
                    Task { await move(to: path.trimmingCharacters(in: .whitespaces)) }
                }
                .disabled(moving)
                if sync?.customLibraryRoot?.isEmpty == false {
                    Button(Copy.libraryReset) {
                        Task { await move(to: "") }
                    }
                    .disabled(moving)
                }
            }
        }
        .navigationTitle(Copy.libraryRow)
        .navigationBarTitleDisplayMode(.inline)
        .onAppear { path = sync?.libraryRoot ?? "" }
        .alert(Copy.errorOperationFailed, isPresented: Binding(get: { error != nil }, set: { if !$0 { error = nil } })) {
            Button(Copy.actionOK) { error = nil }
        } message: { Text(error ?? "") }
    }

    private func move(to target: String) async {
        moving = true
        defer { moving = false }
        do {
            let response = try await api.setLibraryRoot(target)
            onChanged()
            dismiss()
        } catch { self.error = error.localizedDescription }
    }
}