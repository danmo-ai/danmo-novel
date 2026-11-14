package com.danmo.novel.data

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import java.net.URLEncoder

/** OkHttp executes the usual REST pipeline; this interceptor supplies the
 * response in-process so no unauthenticated phone-local HTTP port is needed. */
class LocalCoreInterceptor(private val core: LocalCore) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        val url = request.url
        val path = url.encodedPath + (url.encodedQuery?.let { "?$it" } ?: "")
        val body = request.body?.let {
            val buffer = okio.Buffer()
            it.writeTo(buffer)
            buffer.readUtf8()
        } ?: ""
        val coreResponse = runBlocking { core.request(request.method, path, body) }
        return Response.Builder()
            .request(request)
            .protocol(Protocol.HTTP_1_1)
            .code(coreResponse.status)
            .message(if (coreResponse.status in 200..299) "OK" else "Local API error")
            .header("Content-Type", "application/json; charset=utf-8")
            .body(coreResponse.body.toResponseBody("application/json".toMediaType()))
            .build()
    }
}

@PublishedApi
internal val apiJson = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
    encodeDefaults = true
}

class NovelApi(private val core: LocalCore) {
    private val client = OkHttpClient.Builder().addInterceptor(LocalCoreInterceptor(core)).build()

    private suspend inline fun <reified T> request(method: String, path: String, body: String = ""): T {
        val requestBuilder = Request.Builder().url("http://danmo.local$path").method(method, null)
        if (body.isNotEmpty()) requestBuilder.method(method, body.toRequestBody("application/json".toMediaType()))
        client.newCall(requestBuilder.build()).execute().use { response ->
            val responseBody = response.body?.string().orEmpty()
            if (!response.isSuccessful) {
                val message = runCatching { apiJson.decodeFromString<ErrorBody>(responseBody).error }.getOrNull()
                throw IllegalStateException(message ?: "本地服务返回错误（${response.code}）")
            }
            return apiJson.decodeFromString(responseBody)
        }
    }

    suspend fun books(): List<BookSummary> = request("GET", "/api/books")
    suspend fun book(id: String): BookDetail = request<BookResponse>("GET", "/api/books/${id.pathSegment()}").book
    suspend fun createBook(draft: NewBookDraft): BookDetail = request("POST", "/api/books", apiJson.encodeToString(draft))
    suspend fun createDemo(): BookDetail = request<DemoBookResponse>("POST", "/api/books/demo", "{}").book
    suspend fun file(bookId: String, path: String): BookFile = request("GET", "/api/books/${bookId.pathSegment()}/file?path=${path.queryValue()}")
    suspend fun providers(): List<Provider> = request("GET", "/api/llm/configs")
    suspend fun saveProvider(provider: ProviderRequest, id: String? = null): Provider {
        val path = if (id == null) "/api/llm/configs" else "/api/llm/configs/${id.pathSegment()}"
        return request(if (id == null) "POST" else "PUT", path, apiJson.encodeToString(provider))
    }
    suspend fun runs(bookId: String? = null): List<RunRecord> = request(
        "GET",
        if (bookId == null) "/api/runs" else "/api/runs?bookId=${bookId.queryValue()}",
    )
}

fun String.pathSegment(): String = URLEncoder.encode(this, "UTF-8").replace("+", "%20")
fun String.queryValue(): String = URLEncoder.encode(this, "UTF-8")
