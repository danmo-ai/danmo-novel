package novel

import (
	"strconv"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 单章字数只有一个来源（domain.ChapterChars*），但知识库文章、新书模板、
// 文档里要出现具体数字给人看。这些文章写占位符，注入提示词或展示时再展开，
// 于是"改一个常量，全部文案跟着变"，不会出现文章说 2000–3500、细纲说 5000
// 这种互相打架的局面。
//
// 占位符：
//
//	{{chapter_chars_min}}      2000
//	{{chapter_chars_max}}      3500
//	{{chapter_chars_default}}  2500
var policyPlaceholders = []struct {
	token string
	value string
}{
	{"{{chapter_chars_min}}", strconv.Itoa(domain.ChapterCharsMin)},
	{"{{chapter_chars_max}}", strconv.Itoa(domain.ChapterCharsMax)},
	{"{{chapter_chars_default}}", strconv.Itoa(domain.ChapterCharsDefault)},
}

// ExpandPolicy replaces chapter-length placeholders with the current numbers.
// It is called wherever article text reaches a model or the UI, so author
// overlays with placeholders keep tracking the code.
func ExpandPolicy(text string) string {
	if !strings.Contains(text, "{{chapter_chars") {
		return text
	}
	for _, p := range policyPlaceholders {
		text = strings.ReplaceAll(text, p.token, p.value)
	}
	return text
}
