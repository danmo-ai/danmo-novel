package com.danmo.novel.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.danmo.novel.data.BookDetail
import com.danmo.novel.data.BookFile
import com.danmo.novel.data.BookSummary
import com.danmo.novel.data.LocalCore
import com.danmo.novel.data.NewBookDraft
import com.danmo.novel.data.NovelApi
import com.danmo.novel.data.Provider
import com.danmo.novel.data.ProviderRequest
import com.danmo.novel.data.RunRecord
import com.danmo.novel.data.UnitInfo
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import java.util.UUID

class LibraryViewModel(application: Application) : AndroidViewModel(application) {
    private val core = LocalCore(application)
    private val api = NovelApi(core)

    private val _books = MutableStateFlow<List<BookSummary>>(emptyList())
    val books = _books.asStateFlow()
    private val _ready = MutableStateFlow(false)
    val ready = _ready.asStateFlow()
    private val _starting = MutableStateFlow(false)
    val starting = _starting.asStateFlow()
    private val _error = MutableStateFlow<String?>(null)
    val error = _error.asStateFlow()
    private val _detail = MutableStateFlow<BookDetail?>(null)
    val detail = _detail.asStateFlow()
    private val _bookLoading = MutableStateFlow(false)
    val bookLoading = _bookLoading.asStateFlow()
    private val _readerUnit = MutableStateFlow<UnitInfo?>(null)
    val readerUnit = _readerUnit.asStateFlow()
    private val _readerFile = MutableStateFlow<BookFile?>(null)
    val readerFile = _readerFile.asStateFlow()
    private val _readerLoading = MutableStateFlow(false)
    val readerLoading = _readerLoading.asStateFlow()
    private val _providers = MutableStateFlow<List<Provider>>(emptyList())
    val providers = _providers.asStateFlow()
    private val _providersLoading = MutableStateFlow(false)
    val providersLoading = _providersLoading.asStateFlow()
    private val _savingProvider = MutableStateFlow(false)
    val savingProvider = _savingProvider.asStateFlow()
    private val _creatingBook = MutableStateFlow(false)
    val creatingBook = _creatingBook.asStateFlow()
    private val _runs = MutableStateFlow<List<RunRecord>>(emptyList())
    val runs = _runs.asStateFlow()
    private val _runsLoading = MutableStateFlow(false)
    val runsLoading = _runsLoading.asStateFlow()

    private var bookJob: Job? = null
    private var readerJob: Job? = null

    init {
        start()
    }

    fun start() {
        if (_starting.value || _ready.value) return
        _starting.value = true
        _error.value = null
        viewModelScope.launch {
            runCatching {
                core.start()
                _books.value = api.books()
            }.onSuccess {
                _ready.value = true
            }.onFailure {
                _error.value = it.displayMessage()
            }
            _starting.value = false
        }
    }

    private suspend fun awaitReady() {
        if (!_ready.value) _ready.first { it }
    }

    fun refreshBooks() = viewModelScope.launch {
        awaitReady()
        runCatching { _books.value = api.books() }
            .onFailure { _error.value = it.displayMessage() }
    }

    fun loadBook(id: String) {
        bookJob?.cancel()
        _bookLoading.value = true
        if (_detail.value?.summary?.id != id) _detail.value = null
        bookJob = viewModelScope.launch {
            awaitReady()
            runCatching { api.book(id) }
                .onSuccess { _detail.value = it }
                .onFailure { _error.value = it.displayMessage() }
            _bookLoading.value = false
        }
    }

    fun loadUnit(bookId: String, unitId: String) {
        readerJob?.cancel()
        _readerLoading.value = true
        _readerFile.value = null
        _readerUnit.value = null
        readerJob = viewModelScope.launch {
            awaitReady()
            runCatching {
                val book = _detail.value?.takeIf { it.summary.id == bookId } ?: api.book(bookId).also { _detail.value = it }
                val unit = book.units.firstOrNull { it.unitId == unitId }
                    ?: error("找不到这个写作单元")
                check(unit.hasProse && unit.prosePath.isNotBlank()) { "这个单元还没有已定稿正文" }
                val file = api.file(bookId, unit.prosePath)
                _readerUnit.value = unit
                _readerFile.value = file
            }.onFailure { _error.value = it.displayMessage() }
            _readerLoading.value = false
        }
    }

    fun createBook(title: String, genre: String, idea: String, onCreated: (BookDetail) -> Unit) {
        if (_creatingBook.value) return
        _creatingBook.value = true
        viewModelScope.launch {
            awaitReady()
            runCatching {
                val detail = api.createBook(NewBookDraft("book-${UUID.randomUUID().toString().take(8)}", title, genre, idea))
                _books.value = api.books()
                onCreated(detail)
            }.onFailure { _error.value = it.displayMessage() }
            _creatingBook.value = false
        }
    }

    fun createDemo(onCreated: (BookDetail) -> Unit) {
        if (_creatingBook.value) return
        _creatingBook.value = true
        viewModelScope.launch {
            awaitReady()
            runCatching {
                val detail = api.createDemo()
                _books.value = api.books()
                onCreated(detail)
            }.onFailure { _error.value = it.displayMessage() }
            _creatingBook.value = false
        }
    }

    fun loadProviders() {
        if (_providersLoading.value) return
        _providersLoading.value = true
        viewModelScope.launch {
            awaitReady()
            runCatching { _providers.value = api.providers() }
                .onFailure { _error.value = it.displayMessage() }
            _providersLoading.value = false
        }
    }

    fun saveProvider(id: String?, request: ProviderRequest, onSaved: () -> Unit) {
        if (_savingProvider.value) return
        _savingProvider.value = true
        viewModelScope.launch {
            awaitReady()
            runCatching {
                api.saveProvider(request, id)
                _providers.value = api.providers()
            }.onSuccess { onSaved() }
                .onFailure { _error.value = it.displayMessage() }
            _savingProvider.value = false
        }
    }

    fun loadRuns() {
        if (_runsLoading.value) return
        _runsLoading.value = true
        viewModelScope.launch {
            awaitReady()
            runCatching { _runs.value = api.runs() }
                .onFailure { _error.value = it.displayMessage() }
            _runsLoading.value = false
        }
    }

    fun dismissError() {
        _error.value = null
    }

    override fun onCleared() {
        core.stop()
        super.onCleared()
    }
}

private fun Throwable.displayMessage(): String? = when (this) {
    is CancellationException -> null
    else -> message?.takeIf { it.isNotBlank() } ?: "发生未知错误，请重试"
}
