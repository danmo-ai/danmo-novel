package com.danmo.novel.data

import kotlinx.serialization.encodeToString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class NovelApiEncodingTest {
    @Test
    fun writeRequestsAttachTheirJsonBodyBeforeMethodIsSet() {
        fun bodyOf(request: okhttp3.Request): String? = request.body?.let { body ->
            val buffer = okio.Buffer()
            body.writeTo(buffer)
            buffer.readUtf8()
        }

        // Regression: POST/PUT used to call method(method, null) first, which
        // OkHttp rejects outright, so every write from the app failed.
        val post = buildCoreRequest("POST", "/api/books/demo", "{}")
        assertEquals("POST", post.method)
        assertEquals("{}", bodyOf(post))

        val put = buildCoreRequest("PUT", "/api/llm/configs/id", """{"name":"x"}""")
        assertEquals("PUT", put.method)
        assertEquals("""{"name":"x"}""", bodyOf(put))

        val get = buildCoreRequest("GET", "/api/books", "")
        assertEquals("GET", get.method)
        assertNull(bodyOf(get))
    }

    @Test
    fun pathAndQueryEncodingPreserveRestContracts() {
        assertEquals("book%20a%2Fb", "book a/b".pathSegment())
        assertEquals("outline%2Fv01%2Ba+one.md", "outline/v01+a one.md".queryValue())
    }

    @Test
    fun newBookDraftUsesCamelCaseBookId() {
        val encoded = apiJson.encodeToString(NewBookDraft("book-1", "示例", "玄幻", "主角与冲突"))

        assertEquals("""{"bookId":"book-1","title":"示例","genre":"玄幻","idea":"主角与冲突"}""", encoded)
    }

    @Test
    fun providerRequestKeepsBlankKeyAndOmitsAbsentOptionals() {
        val encoded = apiJson.encodeToString(
            ProviderRequest(
                provider = "openai",
                name = "测试服务",
                apiKey = "",
                models = listOf(ProviderModel("model-a", enabled = true)),
            ),
        )

        assertTrue(encoded.contains(""""apiKey":"""""))
        assertFalse(encoded.contains("protocolDialect"))
        assertFalse(encoded.contains("baseUrl"))
        assertFalse(encoded.contains("defaultProtocolDialect"))
        assertEquals(
            ProviderRequest("openai", "测试服务", "", models = listOf(ProviderModel("model-a"))),
            apiJson.decodeFromString<ProviderRequest>(encoded),
        )
    }
}
