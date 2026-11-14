package com.danmo.novel.data

import kotlinx.serialization.encodeToString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NovelApiEncodingTest {
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
