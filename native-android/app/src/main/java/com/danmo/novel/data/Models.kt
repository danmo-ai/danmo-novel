package com.danmo.novel.data

import kotlinx.serialization.Serializable

@Serializable
data class BookSummary(
    val id: String,
    val title: String,
    val genre: String = "",
    val stage: String = "",
    val unitCount: Int = 0,
    val reviewedCount: Int = 0,
    val openCandidates: Int = 0,
    val readOnly: Boolean = false,
)

@Serializable
data class UnitInfo(
    val unitId: String,
    val volume: Int = 1,
    val index: Int = 0,
    val status: String = "proposed",
    val title: String = "",
    val chapterFrom: Int = 0,
    val chapterTo: Int = 0,
    val hasProse: Boolean = false,
    val prosePath: String = "",
) {
    val statusTitle: String get() = when (status) {
        "reviewed", "finalized" -> "已定稿"
        "drafted" -> "待审校"
        "accepted" -> "待写正文"
        else -> "待写细纲"
    }
}

@Serializable
data class BookDetail(val summary: BookSummary, val units: List<UnitInfo> = emptyList(), val revision: String = "")
@Serializable
data class BookResponse(val book: BookDetail)
@Serializable
data class DemoBookResponse(val book: BookDetail)
@Serializable
data class BookFile(val path: String, val content: String)
@Serializable
data class NewBookDraft(val bookId: String, val title: String, val genre: String, val idea: String)

data class Chapter(val number: Int, val title: String, val body: String)

fun parseChapters(markdown: String): List<Chapter> {
    val heading = Regex("""^##[ \t]*第[ \t]*(\d+)[ \t]*章[ \t]*(.*?)[ \t]*$""")
    val result = mutableListOf<Chapter>()
    var number: Int? = null
    var title = ""
    val body = StringBuilder()
    fun flush() {
        number?.let { result += Chapter(it, title, body.toString().trim()) }
        body.clear()
    }
    markdown.replace("\r\n", "\n").replace('\r', '\n').lineSequence().forEach { line ->
        val match = heading.matchEntire(line)
        if (match != null) {
            flush()
            number = match.groupValues[1].toIntOrNull()
            title = match.groupValues[2].trim()
        } else if (number != null && line.trim() != "---") {
            body.appendLine(line)
        }
    }
    flush()
    return result
}

@Serializable
data class ProviderModel(val name: String, val enabled: Boolean = true, val protocolDialect: String? = null)
@Serializable
data class Provider(
    val id: String,
    val name: String,
    val provider: String,
    val baseUrl: String? = null,
    val defaultProtocolDialect: String? = null,
    val hasApiKey: Boolean = false,
    val models: List<ProviderModel> = emptyList(),
)
@Serializable
data class ProviderRequest(
    val provider: String,
    val name: String,
    val apiKey: String,
    val baseUrl: String? = null,
    val defaultProtocolDialect: String? = null,
    val models: List<ProviderModel>,
)
@Serializable
data class RunRecord(
    val id: String,
    val bookId: String,
    val kind: String,
    val status: String,
    val modelId: String = "",
    val callsUsed: Int = 0,
    val candidateId: String? = null,
    val createdAt: String = "",
    val updatedAt: String = "",
    val error: String? = null,
)
@Serializable
data class ErrorBody(val error: String)
