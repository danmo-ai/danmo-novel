package com.danmo.novel.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ShelfScreen(viewModel: LibraryViewModel, onOpenBook: (String) -> Unit) {
    val books by viewModel.books.collectAsStateWithLifecycle()
    val creatingBook by viewModel.creatingBook.collectAsStateWithLifecycle()
    var creating by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) { viewModel.refreshBooks() }

    Column(Modifier.fillMaxSize()) {
        TopAppBar(
            title = { Text("我的书架") },
            actions = {
                IconButton(onClick = viewModel::refreshBooks) {
                    Icon(Icons.Default.Refresh, contentDescription = "刷新书架")
                }
                IconButton(onClick = { creating = true }) {
                    Icon(Icons.Default.Add, contentDescription = "新建书籍")
                }
            },
        )
        if (creatingBook) LinearProgressIndicator(Modifier.fillMaxWidth())
        if (books.isEmpty()) {
            Column(
                modifier = Modifier.fillMaxSize().padding(32.dp),
                verticalArrangement = Arrangement.Center,
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Text("开始你的第一本书", style = MaterialTheme.typography.headlineSmall)
                Text(
                    "书稿保存在本机。先读一本完整示例，或从自己的灵感开始。",
                    modifier = Modifier.padding(vertical = 16.dp),
                    style = MaterialTheme.typography.bodyLarge,
                )
                Button(
                    enabled = !creatingBook,
                    onClick = { viewModel.createDemo { onOpenBook(it.summary.id) } },
                ) { Text("看看示例书") }
                OutlinedButton(
                    enabled = !creatingBook,
                    onClick = { creating = true },
                    modifier = Modifier.padding(top = 8.dp),
                ) { Text("新建书籍") }
            }
        } else {
            LazyColumn(Modifier.fillMaxSize()) {
                items(books, key = { it.id }) { book ->
                    ListItem(
                        headlineContent = { Text(book.title, fontWeight = FontWeight.SemiBold) },
                        supportingContent = {
                            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                                Text("${book.genre.ifEmpty { "未分类" }} · ${book.unitCount} 个单元 · ${book.reviewedCount} 篇定稿")
                                if (book.openCandidates > 0) Text("${book.openCandidates} 个候选待处理", color = MaterialTheme.colorScheme.primary)
                                if (book.unitCount > 0) {
                                    LinearProgressIndicator(
                                        progress = { (book.reviewedCount.toFloat() / book.unitCount).coerceIn(0f, 1f) },
                                        modifier = Modifier.fillMaxWidth(),
                                    )
                                }
                            }
                        },
                        trailingContent = {
                            if (book.readOnly) Text("只读", style = MaterialTheme.typography.labelMedium)
                        },
                        modifier = Modifier.clickable { onOpenBook(book.id) },
                    )
                    HorizontalDivider()
                }
            }
        }
    }

    if (creating) {
        NewBookSheet(
            viewModel = viewModel,
            onDismiss = { if (!creatingBook) creating = false },
            onCreated = {
                creating = false
                onOpenBook(it)
            },
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NewBookSheet(viewModel: LibraryViewModel, onDismiss: () -> Unit, onCreated: (String) -> Unit) {
    var title by remember { mutableStateOf("") }
    var genre by remember { mutableStateOf("玄幻") }
    var idea by remember { mutableStateOf("") }
    val creating by viewModel.creatingBook.collectAsStateWithLifecycle()

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.padding(horizontal = 24.dp).padding(bottom = 32.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Text("新建书籍", style = MaterialTheme.typography.headlineSmall)
            OutlinedTextField(
                value = title,
                onValueChange = { title = it },
                label = { Text("书名") },
                modifier = Modifier.fillMaxWidth(),
                singleLine = true,
                enabled = !creating,
            )
            Text("题材", style = MaterialTheme.typography.labelLarge)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                listOf("玄幻", "都市", "悬疑", "言情").forEach { item ->
                    FilterChip(
                        selected = genre == item,
                        onClick = { genre = item },
                        label = { Text(item) },
                        enabled = !creating,
                    )
                }
            }
            OutlinedTextField(
                value = idea,
                onValueChange = { idea = it },
                label = { Text("主角、困境、目标和核心冲突") },
                modifier = Modifier.fillMaxWidth(),
                minLines = 3,
                enabled = !creating,
            )
            Button(
                enabled = title.isNotBlank() && !creating,
                onClick = {
                    viewModel.createBook(title.trim(), genre, idea.trim()) {
                        onCreated(it.summary.id)
                    }
                },
                modifier = Modifier.fillMaxWidth(),
            ) { Text(if (creating) "正在创建…" else "创建") }
        }
    }
}
