package com.danmo.novel.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.danmo.novel.R
import com.danmo.novel.data.RunRecord

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RunsScreen(viewModel: LibraryViewModel) {
    val runs by viewModel.runs.collectAsStateWithLifecycle()
    val loading by viewModel.runsLoading.collectAsStateWithLifecycle()
    var expandedId by rememberSaveable { mutableStateOf<String?>(null) }

    LaunchedEffect(Unit) { viewModel.loadRuns() }

    Column(Modifier.fillMaxSize()) {
        TopAppBar(
            title = { Text(stringResource(R.string.logs_title)) },
            actions = {
                IconButton(onClick = viewModel::loadRuns) {
                    Icon(Icons.Default.Refresh, contentDescription = stringResource(R.string.logs_refresh))
                }
            },
        )
        if (loading) LinearProgressIndicator(Modifier.fillMaxWidth())
        if (runs.isEmpty() && !loading) {
            Column(
                modifier = Modifier.fillMaxSize().padding(32.dp),
                verticalArrangement = Arrangement.Center,
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Text(stringResource(R.string.logs_empty_title), style = MaterialTheme.typography.titleMedium)
                Text(
                    stringResource(R.string.logs_empty_body),
                    modifier = Modifier.padding(top = 12.dp),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        } else {
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                items(runs, key = { it.id }) { run ->
                    RunCard(
                        run = run,
                        expanded = expandedId == run.id,
                        onClick = { expandedId = if (expandedId == run.id) null else run.id },
                    )
                }
            }
        }
    }
}

@Composable
private fun RunCard(run: RunRecord, expanded: Boolean, onClick: () -> Unit) {
    Card(onClick = onClick, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(runKind(run.kind), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
            Text(
                runStatus(run.status),
                color = if (run.status == "failed") MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.primary,
            )
            Text(
                stringResource(
                    R.string.logs_calls,
                    run.modelId.ifBlank { stringResource(R.string.logs_model_unknown) },
                    run.callsUsed,
                ),
            )
            Text(
                run.createdAt.displayTime(),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            run.error?.takeIf { it.isNotBlank() }?.let {
                Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
            }
            if (expanded) {
                HorizontalDivider()
                DetailLine(stringResource(R.string.logs_run_id), run.id)
                DetailLine(stringResource(R.string.logs_book_id), run.bookId)
                run.candidateId?.takeIf { it.isNotBlank() }?.let {
                    DetailLine(stringResource(R.string.logs_candidate_id), it)
                }
                if (run.updatedAt.isNotBlank()) {
                    DetailLine(stringResource(R.string.logs_updated_at), run.updatedAt.displayTime())
                }
            }
        }
    }
}

@Composable
private fun DetailLine(label: String, value: String) {
    Column {
        Text(label, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(value, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
    }
}

@Composable
private fun String.displayTime(): String =
    if (isBlank()) stringResource(R.string.logs_time_unknown) else replace("T", " ").removeSuffix("Z").take(19)

/** Same labels as the desktop runs list (docs/mobile-copy.md), so one run reads
 * the same on a phone and on the Mac. */
@Composable
private fun runKind(kind: String): String = when (kind) {
    "plan" -> stringResource(R.string.run_kind_plan)
    "outline" -> stringResource(R.string.run_kind_outline)
    "write" -> stringResource(R.string.run_kind_write)
    "review" -> stringResource(R.string.run_kind_review)
    "revise" -> stringResource(R.string.run_kind_revise)
    "finalize" -> stringResource(R.string.run_kind_finalize)
    "idea" -> stringResource(R.string.run_kind_idea)
    else -> kind.ifBlank { stringResource(R.string.run_kind_unknown) }
}

@Composable
private fun runStatus(status: String): String = when (status) {
    "pending" -> stringResource(R.string.run_status_pending)
    "running" -> stringResource(R.string.run_status_running)
    "waiting_accept" -> stringResource(R.string.run_status_waiting_accept)
    "succeeded" -> stringResource(R.string.run_status_succeeded)
    "failed" -> stringResource(R.string.run_status_failed)
    "cancelled" -> stringResource(R.string.run_status_cancelled)
    "rejected" -> stringResource(R.string.run_status_rejected)
    "expired" -> stringResource(R.string.run_status_expired)
    else -> status.ifBlank { stringResource(R.string.run_status_unknown) }
}
