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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.danmo.novel.R
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
                title = {
                    Text(
                        book?.summary?.title ?: stringResource(R.string.book_fallback_title),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = stringResource(R.string.book_back_to_shelf))
                    }
                },
                actions = {
                    IconButton(onClick = { viewModel.loadBook(bookId) }) {
                        Icon(Icons.Default.Refresh, contentDescription = stringResource(R.string.book_refresh))
                    }
                },
            )
        },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding)) {
            if (loading) LinearProgressIndicator(Modifier.fillMaxWidth())
            when {
                book == null && !loading -> EmptyState(
                    message = stringResource(R.string.book_not_loaded),
                    action = stringResource(R.string.action_retry),
                ) { viewModel.loadBook(bookId) }
                book == null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                book.units.isEmpty() -> EmptyState(
                    message = stringResource(R.string.book_no_units),
                    action = stringResource(R.string.action_refresh),
                ) { viewModel.loadBook(bookId) }
                else -> LazyColumn(
                    modifier = Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(bottom = 24.dp),
                ) {
                    item {
                        Text(
                            if (book.summary.readOnly) {
                                stringResource(
                                    R.string.book_meta_read_only,
                                    book.summary.genre.ifEmpty { stringResource(R.string.genre_unknown) },
                                    book.units.size,
                                )
                            } else {
                                stringResource(
                                    R.string.book_meta,
                                    book.summary.genre.ifEmpty { stringResource(R.string.genre_unknown) },
                                    book.units.size,
                                )
                            },
                            modifier = Modifier.padding(horizontal = 16.dp, vertical = 12.dp),
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    book.units.groupBy { it.volume }.forEach { (volume, units) ->
                        item(key = "volume-$volume") {
                            Text(
                                stringResource(R.string.book_volume_title, volume),
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
                                    Text(
                                        stringResource(
                                            R.string.unit_meta,
                                            unit.unitId,
                                            unit.chapterFrom,
                                            unit.chapterTo,
                                            unit.statusTitle,
                                        ),
                                    )
                                },
                                trailingContent = {
                                    if (unit.hasProse) {
                                        Icon(
                                            Icons.AutoMirrored.Filled.MenuBook,
                                            contentDescription = stringResource(R.string.book_open_prose),
                                        )
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
    val bodyFallbackTitle = stringResource(R.string.reader_body_fallback_title)
    val chapters = remember(file?.content, bodyFallbackTitle) {
        val parsed = file?.content?.let(::parseChapters).orEmpty()
        if (parsed.isNotEmpty()) parsed
        else file?.content?.takeIf { it.isNotBlank() }
            ?.let { listOf(Chapter(0, bodyFallbackTitle, it.trim())) }
            .orEmpty()
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
                        Icon(
                            Icons.AutoMirrored.Filled.ArrowBack,
                            contentDescription = stringResource(R.string.reader_back_to_units),
                        )
                    }
                },
            )
        },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding)) {
            if (loading) LinearProgressIndicator(Modifier.fillMaxWidth())
            when {
                chapters.isEmpty() && loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                chapters.isEmpty() -> EmptyState(
                    message = stringResource(R.string.reader_empty),
                    action = stringResource(R.string.action_retry),
                ) { viewModel.loadUnit(bookId, unitId) }
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
                                    label = { Text(stringResource(R.string.reader_chapter_chip, chapter.number)) },
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
                            Icon(
                                Icons.AutoMirrored.Filled.KeyboardArrowLeft,
                                contentDescription = stringResource(R.string.reader_previous_chapter),
                            )
                        }
                        Text(
                            if (chapter.number > 0) {
                                stringResource(R.string.reader_chapter_title, chapter.number, chapter.title).trim()
                            } else {
                                chapter.title
                            },
                            modifier = Modifier.weight(1f).padding(horizontal = 4.dp),
                            style = MaterialTheme.typography.titleMedium,
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis,
                        )
                        IconButton(
                            enabled = currentIndex < chapters.lastIndex,
                            onClick = { selectedIndex = currentIndex + 1 },
                        ) {
                            Icon(
                                Icons.AutoMirrored.Filled.KeyboardArrowRight,
                                contentDescription = stringResource(R.string.reader_next_chapter),
                            )
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
                            chapter.body.ifBlank { stringResource(R.string.reader_empty_chapter) },
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
