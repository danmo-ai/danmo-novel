package com.danmo.novel.data

import android.content.Context
import danmonovel.Danmonovel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import java.io.File

@Serializable
data class CoreResult(val ok: Boolean = false, val error: String? = null, val home: String? = null)

@Serializable
data class CoreResponse(val status: Int, val body: String)

/** In-process REST transport: no listening TCP socket on the phone. */
class LocalCore(private val context: Context) {
    private val mutex = Mutex()
    private val json = Json { ignoreUnknownKeys = true }
    @Volatile
    private var ready = false

    suspend fun start() = withContext(Dispatchers.IO) {
        // A ViewModel can be recreated after onCleared(). Reconcile with the
        // process-global Go server instead of skipping start with stale ready=true.
        val result = mutex.withLock {
            val home = File(context.filesDir, "DanmoNovel").apply { mkdirs() }
            json.decodeFromString<CoreResult>(Danmonovel.start(home.absolutePath))
        }
        if (!result.ok) {
            ready = false
            error(result.error ?: "本地内核启动失败")
        }
        ready = true
    }

    suspend fun request(method: String, path: String, body: String = ""): CoreResponse = withContext(Dispatchers.IO) {
        check(ready) { "本地工作区尚未就绪" }
        json.decodeFromString(Danmonovel.request(method, path, body, ""))
    }

    fun stop() {
        ready = false
        Danmonovel.stop()
    }
}
