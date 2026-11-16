package com.danmo.novel.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
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
import androidx.compose.ui.res.stringArrayResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.danmo.novel.R

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ShelfScreen(viewModel: LibraryViewModel, onOpenBook: (String) -> Unit) {
    val books by viewModel.books.collectAsStateWithLifecycle()
    val creatingBook by viewModel.creatingBook.collectAsStateWithLifecycle()
    var creating by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) { viewModel.refreshBooks() }

    Column(Modifier.fillMaxSize()) {
        TopAppBar(
            title = { Text(stringResource(R.string.shelf_title)) },
            actions = {
                IconButton(onClick = viewModel::refreshBooks) {
                    Icon(Icons.Default.Refresh, contentDescription = stringResource(R.string.shelf_refresh))
                }
                IconButton(onClick = { creating = true }) {
                    Icon(Icons.Default.Add, contentDescription = stringResource(R.string.shelf_new_book))
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
                Text(stringResource(R.string.shelf_empty_title), style = MaterialTheme.typography.headlineSmall)
                Text(
                    stringResource(R.string.shelf_empty_body),
                    modifier = Modifier.padding(vertical = 16.dp),
                    style = MaterialTheme.typography.bodyLarge,
                )
                Button(
                    enabled = !creatingBook,
                    onClick = { viewModel.createDemo { onOpenBook(it.summary.id) } },
                ) { Text(stringResource(R.string.shelf_open_demo)) }
                OutlinedButton(
                    enabled = !creatingBook,
                    onClick = { creating = true },
                    modifier = Modifier.padding(top = 8.dp),
                ) { Text(stringResource(R.string.shelf_new_book)) }
            }
        } else {
            LazyColumn(Modifier.fillMaxSize()) {
                items(books, key = { it.id }) { book ->
                    ListItem(
                        headlineContent = { Text(book.title, fontWeight = FontWeight.SemiBold) },
                        supportingContent = {
                            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                                Text(
                                    stringResource(
                                        R.string.shelf_meta,
                                        book.genre.ifEmpty { stringResource(R.string.genre_unknown) },
                                        book.unitCount,
                                        book.reviewedCount,
                                    ),
                                )
                                if (book.openCandidates > 0) {
                                    Text(
                                        stringResource(R.string.shelf_candidates, book.openCandidates),
                                        color = MaterialTheme.colorScheme.primary,
                                    )
                                }
                                if (book.unitCount > 0) {
                                    LinearProgressIndicator(
                                        progress = { (book.reviewedCount.toFloat() / book.unitCount).coerceIn(0f, 1f) },
                                        modifier = Modifier.fillMaxWidth(),
                                    )
                                }
                            }
                        },
                        trailingContent = {
                            if (book.readOnly) {
                                Text(stringResource(R.string.shelf_read_only), style = MaterialTheme.typography.labelMedium)
                            }
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

@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
fun NewBookSheet(viewModel: LibraryViewModel, onDismiss: () -> Unit, onCreated: (String) -> Unit) {
    val genres = stringArrayResource(R.array.new_book_genres).toList()
    var title by remember { mutableStateOf("") }
    var genre by remember { mutableStateOf(genres.first()) }
    var idea by remember { mutableStateOf("") }
    val creating by viewModel.creatingBook.collectAsStateWithLifecycle()

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.padding(horizontal = 24.dp).padding(bottom = 32.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Text(stringResource(R.string.shelf_new_book), style = MaterialTheme.typography.headlineSmall)
            OutlinedTextField(
                value = title,
                onValueChange = { title = it },
                label = { Text(stringResource(R.string.new_book_name_label)) },
                modifier = Modifier.fillMaxWidth(),
                singleLine = true,
                enabled = !creating,
            )
            Text(stringResource(R.string.new_book_genre_label), style = MaterialTheme.typography.labelLarge)
            // FlowRow, not Row: five chips at a large font scale overflow a Row
            // and the last genre becomes unreachable.
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                genres.forEach { item ->
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
                label = { Text(stringResource(R.string.new_book_idea_hint)) },
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
            ) {
                Text(stringResource(if (creating) R.string.action_creating else R.string.action_create))
            }
        }
    }
}
