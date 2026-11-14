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
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
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
            title = { Text("运行记录") },
            actions = {
                IconButton(onClick = viewModel::loadRuns) {
                    Icon(Icons.Default.Refresh, contentDescription = "刷新运行记录")
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
                Text("尚无运行记录", style = MaterialTheme.typography.titleMedium)
                Text(
                    "这里会记录本机的模型调用、耗时与失败原因。当前原生版本不会启动模型生成。",
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
            Text("${run.modelId.ifBlank { "未指定模型" }} · ${run.callsUsed} 次调用")
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
                DetailLine("运行 ID", run.id)
                DetailLine("书籍 ID", run.bookId)
                run.candidateId?.takeIf { it.isNotBlank() }?.let { DetailLine("候选 ID", it) }
                if (run.updatedAt.isNotBlank()) DetailLine("更新时间", run.updatedAt.displayTime())
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

private fun String.displayTime(): String = if (isBlank()) "时间未记录" else replace("T", " ").removeSuffix("Z").take(19)

private fun runKind(kind: String): String = when (kind) {
    "plan" -> "立项规划"
    "outline" -> "单元细纲"
    "write" -> "正文写作"
    "review" -> "正文审校"
    "revise" -> "正文修订"
    "finalize" -> "定稿核对"
    "idea" -> "创意扩写"
    else -> kind.ifBlank { "未知任务" }
}

private fun runStatus(status: String): String = when (status) {
    "pending" -> "等待执行"
    "running" -> "执行中"
    "waiting_accept" -> "等待接受候选"
    "succeeded" -> "已完成"
    "failed" -> "失败"
    "cancelled" -> "已取消"
    "rejected" -> "候选已拒绝"
    "expired" -> "候选已失效"
    else -> status.ifBlank { "未知状态" }
}
