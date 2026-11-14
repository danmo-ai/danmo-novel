package com.danmo.novel.ui

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
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowLeft
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.automirrored.filled.MenuBook
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.danmo.novel.data.Chapter
import com.danmo.novel.data.parseChapters

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun BookScreen(
    viewModel: LibraryViewModel,
    bookId: String,
    onBack: () -> Unit,
    onOpenUnit: (String) -> Unit,
) {
    val detail by viewModel.detail.collectAsStateWithLifecycle()
    val loading by viewModel.bookLoading.collectAsStateWithLifecycle()
    val book = detail?.takeIf { it.summary.id == bookId }

    LaunchedEffect(bookId) { viewModel.loadBook(bookId) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(book?.summary?.title ?: "书籍", maxLines = 1, overflow = TextOverflow.Ellipsis) },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "返回书架")
                    }
                },
                actions = {
                    IconButton(onClick = { viewModel.loadBook(bookId) }) {
                        Icon(Icons.Default.Refresh, contentDescription = "刷新书籍")
                    }
                },
            )
        },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding)) {
            if (loading) LinearProgressIndicator(Modifier.fillMaxWidth())
            when {
                book == null && !loading -> EmptyState("尚未载入书籍", "重试") { viewModel.loadBook(bookId) }
                book == null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                book.units.isEmpty() -> EmptyState("这本书还没有写作单元", "刷新") { viewModel.loadBook(bookId) }
                else -> LazyColumn(
                    modifier = Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(bottom = 24.dp),
                ) {
                    item {
                        Text(
                            "${book.summary.genre.ifEmpty { "未分类" }} · ${book.units.size} 个单元${if (book.summary.readOnly) " · 只读" else ""}",
                            modifier = Modifier.padding(horizontal = 16.dp, vertical = 12.dp),
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    book.units.groupBy { it.volume }.forEach { (volume, units) ->
                        item(key = "volume-$volume") {
                            Text(
                                "第 ${volume} 卷",
                                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp),
                                style = MaterialTheme.typography.titleSmall,
                                color = MaterialTheme.colorScheme.primary,
                            )
                        }
                        items(units, key = { it.unitId }) { unit ->
                            ListItem(
                                headlineContent = {
                                    Text(
                                        unit.title.ifBlank { unit.unitId },
                                        maxLines = 2,
                                        overflow = TextOverflow.Ellipsis,
                                        fontWeight = FontWeight.Medium,
                                    )
                                },
                                supportingContent = {
                                    Text("${unit.unitId} · 第 ${unit.chapterFrom}–${unit.chapterTo} 章 · ${unit.statusTitle}")
                                },
                                trailingContent = {
                                    if (unit.hasProse) {
                                        Icon(Icons.AutoMirrored.Filled.MenuBook, contentDescription = "阅读正文")
                                    }
                                },
                                modifier = Modifier.clickable(enabled = unit.hasProse) { onOpenUnit(unit.unitId) },
                            )
                            HorizontalDivider(Modifier.padding(start = 16.dp))
                        }
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun UnitReaderScreen(
    viewModel: LibraryViewModel,
    bookId: String,
    unitId: String,
    onBack: () -> Unit,
) {
    val unit by viewModel.readerUnit.collectAsStateWithLifecycle()
    val file by viewModel.readerFile.collectAsStateWithLifecycle()
    val loading by viewModel.readerLoading.collectAsStateWithLifecycle()
    val chapters = remember(file?.content) {
        val parsed = file?.content?.let(::parseChapters).orEmpty()
        if (parsed.isNotEmpty()) parsed
        else file?.content?.takeIf { it.isNotBlank() }?.let { listOf(Chapter(0, "正文", it.trim())) }.orEmpty()
    }
    var selectedIndex by rememberSaveable(bookId, unitId, file?.path) { mutableIntStateOf(0) }
    val currentIndex = selectedIndex.coerceIn(0, (chapters.size - 1).coerceAtLeast(0))

    val readerScrollState = rememberScrollState()
    LaunchedEffect(bookId, unitId) { viewModel.loadUnit(bookId, unitId) }
    LaunchedEffect(currentIndex, file?.path) { readerScrollState.scrollTo(0) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Text(
                        unit?.title?.ifBlank { unitId } ?: unitId,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "返回单元列表")
                    }
                },
            )
        },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding)) {
            if (loading) LinearProgressIndicator(Modifier.fillMaxWidth())
            when {
                chapters.isEmpty() && loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                chapters.isEmpty() -> EmptyState("这个单元还没有可阅读的正文", "重试") { viewModel.loadUnit(bookId, unitId) }
                else -> {
                    if (chapters.size > 1) {
                        LazyRow(
                            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
                            horizontalArrangement = Arrangement.spacedBy(8.dp),
                        ) {
                            itemsIndexed(chapters) { index, chapter ->
                                FilterChip(
                                    selected = currentIndex == index,
                                    onClick = { selectedIndex = index },
                                    label = { Text("第 ${chapter.number} 章") },
                                )
                            }
                        }
                        HorizontalDivider()
                    }
                    val chapter = chapters[currentIndex]
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        IconButton(enabled = currentIndex > 0, onClick = { selectedIndex = currentIndex - 1 }) {
                            Icon(Icons.AutoMirrored.Filled.KeyboardArrowLeft, contentDescription = "上一章")
                        }
                        Text(
                            if (chapter.number > 0) "第 ${chapter.number} 章 ${chapter.title}".trim() else chapter.title,
                            modifier = Modifier.weight(1f).padding(horizontal = 4.dp),
                            style = MaterialTheme.typography.titleMedium,
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis,
                        )
                        IconButton(
                            enabled = currentIndex < chapters.lastIndex,
                            onClick = { selectedIndex = currentIndex + 1 },
                        ) {
                            Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, contentDescription = "下一章")
                        }
                    }
                    SelectionContainer(
                        modifier = Modifier
                            .weight(1f)
                            .fillMaxWidth()
                            .verticalScroll(readerScrollState)
                            .padding(horizontal = 24.dp, vertical = 16.dp),
                    ) {
                        Text(
                            chapter.body.ifBlank { "本章没有正文。" },
                            style = MaterialTheme.typography.bodyLarge.copy(fontSize = 18.sp, lineHeight = 30.sp),
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun EmptyState(message: String, action: String, onAction: () -> Unit) {
    Column(
        modifier = Modifier.fillMaxSize().padding(32.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(message, style = MaterialTheme.typography.bodyLarge)
        Button(onClick = onAction, modifier = Modifier.padding(top = 16.dp)) { Text(action) }
    }
}
