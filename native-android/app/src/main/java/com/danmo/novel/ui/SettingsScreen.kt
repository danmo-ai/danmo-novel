package com.danmo.novel.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.MenuAnchorType
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.danmo.novel.R
import com.danmo.novel.data.Provider
import com.danmo.novel.data.ProviderModel
import com.danmo.novel.data.ProviderRequest

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    viewModel: LibraryViewModel,
    onAddProvider: () -> Unit,
    onEditProvider: (String) -> Unit,
) {
    val providers by viewModel.providers.collectAsStateWithLifecycle()
    val loading by viewModel.providersLoading.collectAsStateWithLifecycle()

    LaunchedEffect(Unit) { viewModel.loadProviders() }

    Column(Modifier.fillMaxSize()) {
        TopAppBar(
            title = { Text(stringResource(R.string.settings_title)) },
            actions = {
                IconButton(onClick = viewModel::loadProviders) {
                    Icon(Icons.Default.Refresh, contentDescription = stringResource(R.string.settings_refresh_providers))
                }
                IconButton(onClick = onAddProvider) {
                    Icon(Icons.Default.Add, contentDescription = stringResource(R.string.settings_add_provider))
                }
            },
        )
        if (loading) LinearProgressIndicator(Modifier.fillMaxWidth())
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item {
                Text(
                    stringResource(R.string.settings_providers_section),
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.primary,
                )
            }
            item {
                Text(
                    stringResource(R.string.settings_key_note),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
            if (providers.isEmpty() && !loading) {
                item {
                    Card(Modifier.fillMaxWidth()) {
                        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                            Text(stringResource(R.string.settings_no_providers_title), style = MaterialTheme.typography.titleMedium)
                            Text(stringResource(R.string.settings_no_providers_body))
                            OutlinedButton(onClick = onAddProvider) { Text(stringResource(R.string.settings_add_provider)) }
                        }
                    }
                }
            }
            items(providers, key = { it.id }) { provider ->
                Card(Modifier.fillMaxWidth().clickable { onEditProvider(provider.id) }) {
                    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text(provider.name, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                        Text(protocolName(provider.provider), color = MaterialTheme.colorScheme.primary)
                        provider.baseUrl?.takeIf { it.isNotBlank() }?.let {
                            Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                        Text(
                            stringResource(
                                R.string.settings_provider_models,
                                provider.models.count { it.enabled },
                                stringResource(
                                    if (provider.hasApiKey) R.string.settings_key_saved else R.string.settings_key_missing,
                                ),
                            ),
                        )
                        if (provider.models.isNotEmpty()) {
                            Text(
                                provider.models.joinToString("、") { it.name },
                                style = MaterialTheme.typography.bodySmall,
                                maxLines = 2,
                            )
                        }
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ProviderEditorScreen(viewModel: LibraryViewModel, providerId: String?, onBack: () -> Unit) {
    val providers by viewModel.providers.collectAsStateWithLifecycle()
    val loading by viewModel.providersLoading.collectAsStateWithLifecycle()
    val saving by viewModel.savingProvider.collectAsStateWithLifecycle()
    BackHandler(enabled = saving) { }
    val provider = providers.firstOrNull { it.id == providerId }
    var initialized by remember(providerId) { mutableStateOf(providerId == null || provider != null) }
    var name by remember(providerId) { mutableStateOf(provider?.name ?: "") }
    var protocol by remember(providerId) { mutableStateOf(provider?.provider ?: "openai") }
    var baseUrl by remember(providerId) { mutableStateOf(provider?.baseUrl.orEmpty()) }
    var defaultDialect by remember(providerId) { mutableStateOf(provider?.defaultProtocolDialect.orEmpty()) }
    var apiKey by remember(providerId) { mutableStateOf("") }
    var models by remember(providerId) { mutableStateOf(provider?.models.orEmpty()) }

    LaunchedEffect(providerId) { viewModel.loadProviders() }
    LaunchedEffect(provider) {
        if (!initialized && provider != null) {
            name = provider.name
            protocol = provider.provider
            baseUrl = provider.baseUrl.orEmpty()
            defaultDialect = provider.defaultProtocolDialect.orEmpty()
            models = provider.models
            initialized = true
        }
    }

    val duplicateNames = models.map { it.name.trim() }.filter { it.isNotBlank() }
        .groupingBy { it.lowercase() }.eachCount().any { it.value > 1 }
    val valid = name.isNotBlank() && models.all { it.name.isNotBlank() } && !duplicateNames

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Text(
                        stringResource(
                            if (providerId == null) R.string.provider_add_title else R.string.provider_edit_title,
                        ),
                    )
                },
                navigationIcon = {
                    IconButton(onClick = onBack, enabled = !saving) {
                        Icon(
                            Icons.AutoMirrored.Filled.ArrowBack,
                            contentDescription = stringResource(R.string.provider_back_to_settings),
                        )
                    }
                },
            )
        },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding)) {
            if (loading || saving) LinearProgressIndicator(Modifier.fillMaxWidth())
            if (!initialized && providerId != null) {
                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    if (loading) CircularProgressIndicator()
                    else Column(horizontalAlignment = Alignment.CenterHorizontally) {
                        Text(stringResource(R.string.provider_missing))
                        OutlinedButton(onClick = viewModel::loadProviders, modifier = Modifier.padding(top = 16.dp)) {
                            Text(stringResource(R.string.action_retry))
                        }
                    }
                }
            } else {
                LazyColumn(
                    modifier = Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(16.dp),
                    verticalArrangement = Arrangement.spacedBy(16.dp),
                ) {
                    item {
                        OutlinedTextField(
                            value = name,
                            onValueChange = { name = it },
                            label = { Text(stringResource(R.string.provider_name_label)) },
                            supportingText = {
                                if (providerId != null) Text(stringResource(R.string.provider_name_locked))
                            },
                            enabled = providerId == null && !saving,
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                    item {
                        ChoiceField(
                            label = stringResource(R.string.provider_protocol_label),
                            value = protocol,
                            choices = protocolChoices(protocol),
                            onValueChange = { protocol = it },
                            enabled = !saving,
                        )
                    }
                    item {
                        OutlinedTextField(
                            value = baseUrl,
                            onValueChange = { baseUrl = it },
                            label = { Text(stringResource(R.string.provider_base_url_label)) },
                            placeholder = { Text(stringResource(R.string.provider_base_url_hint)) },
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
                            enabled = !saving,
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                    item {
                        OutlinedTextField(
                            value = apiKey,
                            onValueChange = { apiKey = it },
                            label = { Text(stringResource(R.string.provider_api_key_label)) },
                            supportingText = {
                                Text(
                                    stringResource(
                                        if (provider?.hasApiKey == true) R.string.provider_api_key_keep
                                        else R.string.provider_api_key_hint,
                                    ),
                                )
                            },
                            visualTransformation = PasswordVisualTransformation(),
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                            enabled = !saving,
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                    if (protocol == "openai" || protocol == "local") {
                        item {
                            ChoiceField(
                                label = stringResource(R.string.provider_dialect_label),
                                value = defaultDialect,
                                choices = dialectChoices(defaultDialect, stringResource(R.string.dialect_auto)),
                                onValueChange = { defaultDialect = it },
                                enabled = !saving,
                            )
                        }
                    }
                    item {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Column(Modifier.weight(1f)) {
                                Text(stringResource(R.string.provider_models_title), style = MaterialTheme.typography.titleMedium)
                                Text(
                                    stringResource(R.string.provider_models_hint),
                                    style = MaterialTheme.typography.bodySmall,
                                )
                            }
                            IconButton(
                                enabled = !saving,
                                onClick = { models = models + ProviderModel("") },
                            ) {
                                Icon(
                                    Icons.Default.Add,
                                    contentDescription = stringResource(R.string.provider_model_add),
                                )
                            }
                        }
                    }
                    if (models.isEmpty()) {
                        item {
                            Text(
                                stringResource(R.string.provider_models_empty),
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                    items(models.size, key = { index -> "model-$index" }) { index ->
                        val model = models[index]
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
                            OutlinedTextField(
                                value = model.name,
                                onValueChange = { value ->
                                    models = models.toMutableList().also { it[index] = model.copy(name = value) }
                                },
                                label = { Text(stringResource(R.string.provider_model_id_label)) },
                                isError = model.name.isBlank(),
                                enabled = !saving,
                                singleLine = true,
                                modifier = Modifier.weight(1f),
                            )
                            Switch(
                                checked = model.enabled,
                                onCheckedChange = { enabled ->
                                    models = models.toMutableList().also { it[index] = model.copy(enabled = enabled) }
                                },
                                enabled = !saving,
                            )
                            IconButton(
                                enabled = !saving,
                                onClick = { models = models.filterIndexed { i, _ -> i != index } },
                            ) {
                                Icon(
                                    Icons.Default.Delete,
                                    contentDescription = stringResource(R.string.provider_model_delete),
                                )
                            }
                        }
                    }
                    if (duplicateNames) {
                        item {
                            Text(
                                stringResource(R.string.provider_model_duplicate),
                                color = MaterialTheme.colorScheme.error,
                            )
                        }
                    }
                    item {
                        Button(
                            enabled = valid && !saving,
                            onClick = {
                                viewModel.saveProvider(
                                    providerId,
                                    ProviderRequest(
                                        provider = protocol,
                                        name = name.trim(),
                                        apiKey = apiKey,
                                        baseUrl = baseUrl.trim().ifEmpty { null },
                                        defaultProtocolDialect = defaultDialect.ifEmpty { null },
                                        models = models.map { it.copy(name = it.name.trim()) },
                                    ),
                                    onSaved = onBack,
                                )
                            },
                            modifier = Modifier.fillMaxWidth(),
                        ) {
                            Text(stringResource(if (saving) R.string.action_saving else R.string.action_save))
                        }
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ChoiceField(
    label: String,
    value: String,
    choices: List<Pair<String, String>>,
    onValueChange: (String) -> Unit,
    enabled: Boolean,
) {
    var expanded by remember { mutableStateOf(false) }
    val autoLabel = stringResource(R.string.dialect_auto)
    ExposedDropdownMenuBox(expanded = expanded, onExpandedChange = { if (enabled) expanded = it }) {
        OutlinedTextField(
            value = choices.firstOrNull { it.first == value }?.second ?: value.ifEmpty { autoLabel },
            onValueChange = {},
            readOnly = true,
            enabled = enabled,
            label = { Text(label) },
            trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded) },
            modifier = Modifier.menuAnchor(MenuAnchorType.PrimaryNotEditable, enabled).fillMaxWidth(),
        )
        ExposedDropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
            choices.forEach { (choice, choiceLabel) ->
                DropdownMenuItem(
                    text = { Text(choiceLabel) },
                    onClick = {
                        onValueChange(choice)
                        expanded = false
                    },
                )
            }
        }
    }
}

private fun protocolChoices(current: String): List<Pair<String, String>> {
    val defaults = listOf(
        "openai" to "OpenAI Chat Completions",
        "openai_responses" to "OpenAI Responses",
        "anthropic" to "Anthropic Messages",
    )
    return if (defaults.any { it.first == current }) defaults else defaults + (current to current)
}

private fun dialectChoices(current: String, autoLabel: String): List<Pair<String, String>> {
    val defaults = listOf(
        "" to autoLabel,
        "openai" to "OpenAI",
        "deepseek" to "DeepSeek",
        "qwen" to "Qwen",
        "kimi" to "Kimi",
        "kimi_code" to "Kimi Code",
        "kimi_k3" to "Kimi K3",
        "glm" to "GLM",
        "minimax" to "MiniMax",
        "gemini" to "Gemini",
        "grok" to "Grok",
    )
    return if (defaults.any { it.first == current }) defaults else defaults + (current to current)
}

@Composable
private fun protocolName(protocol: String): String = when (protocol) {
    "openai" -> "OpenAI Chat Completions"
    "openai_responses" -> "OpenAI Responses"
    "anthropic" -> "Anthropic Messages"
    "local" -> stringResource(R.string.protocol_legacy_openai)
    else -> protocol
}
