package books

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// ChapterTarget 是「这一章写多少字」的唯一读取口。
//
// 以前这里叫 WordTargetPerChapter，直接拿细纲 word_target 除以章数、不设上下限：
// 细纲写 word_target: 30000、单元 6 章，就成了「每章 5000 字」——和知识库的
// 2000–3500 打架，写作硬门拦下五章。现在：
//
//  1. 细纲可显式写 chapter_chars（单章目标），这是首选；
//  2. 老书只有 word_target（单元总字数）时按章数折成单章；
//  3. 都没有就用默认值；
//  4. 结果一律夹进 [ChapterCharsMin, ChapterCharsMax]。
//
// 全仓（写作/审校/修订/定稿/UI/提示词）只允许通过它取每章目标，
// 不允许再出现字面量 2000/3500。
func ChapterTarget(unit domain.UnitInfo) int {
	if n, ok := outlineInt(unit.OutlineRaw, "chapter_chars"); ok {
		return domain.ClampChapterChars(n)
	}
	// 不读 word_target：单元总字数是派生值，手写的那份会和章节数脱钩
	// （30000/6 章曾被读成每章 5000）。旧书由迁移把 word_target 折算成
	// chapter_chars 后删除。
	return domain.ChapterCharsDefault
}

// UnitWordTarget 是本单元的目标总字数：单章目标 × 章数。
// 它永远是派生值，不读细纲里手写的 word_target——那是旧格式的输入。
func UnitWordTarget(unit domain.UnitInfo) int {
	return ChapterTarget(unit) * ChapterCount(unit)
}

// ChapterCount 是单元的章数（至少 1）。
func ChapterCount(unit domain.UnitInfo) int {
	n := unit.ChapterTo - unit.ChapterFrom + 1
	if n <= 0 {
		return 1
	}
	return n
}

// OutlineChapterTarget 是接受细纲时用的同一个读数：只有章节范围、没有
// UnitInfo 的场合（例如刚解析出来的 UnitOutlineDraft）。
func OutlineChapterTarget(from, to int, raw string) int {
	return ChapterTarget(domain.UnitInfo{ChapterFrom: from, ChapterTo: to, OutlineRaw: raw})
}

func outlineInt(raw, key string) (int, bool) {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*(.+)$`)
	m := re.FindStringSubmatch(raw)
	if len(m) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(strings.Trim(strings.TrimSpace(m[1]), `"'`))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
