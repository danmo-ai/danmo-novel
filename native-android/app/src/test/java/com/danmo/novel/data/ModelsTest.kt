package com.danmo.novel.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ModelsTest {
    @Test
    fun parseChaptersReadsHeadingsBodyAndCrLf() {
        val markdown = """
            不应进入正文的前言
            ## 第1章 雨夜
            第一段。

            第二段。
            ---
            ## 第 2 章 归途 
            第二章正文。
        """.trimIndent().replace("\n", "\r\n")

        val chapters = parseChapters(markdown)

        assertEquals(2, chapters.size)
        assertEquals(Chapter(1, "雨夜", "第一段。\n\n第二段。"), chapters[0])
        assertEquals(Chapter(2, "归途", "第二章正文。"), chapters[1])
    }

    @Test
    fun parseChaptersKeepsOnlyBodyInsideChapterHeadings() {
        val chapters = parseChapters("# 书名\n\n引言\n## 第3章\n正文\n### 小节不是新章\n仍属本章")

        assertEquals(listOf(Chapter(3, "", "正文\n### 小节不是新章\n仍属本章")), chapters)
    }

    @Test
    fun parseChaptersReturnsEmptyForUnstructuredMarkdown() {
        assertTrue(parseChapters("只有一段没有章标题的正文").isEmpty())
    }
}
