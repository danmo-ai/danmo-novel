package novel

import (
	"fmt"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 写正文的系统提示词必须用合同值，而不是让学生「约 3 个场面」自己猜。
//
// 2026-11 的实测：目标 2500 字的书写到 3160–6210 字，全是场面数写死 3、
// 每场 1000 字造成的。现在字数区间由 ChapterBand 给出，场面数交给细纲。
func TestWriteSystemPromptUsesContractLengthAndSceneRanges(t *testing.T) {
	for _, target := range []int{2500, 3000, 3500} {
		floor, ceil := domain.ChapterBand(target)
		prompt := WriteSystemPromptFor(target)
		for _, want := range []string{
			fmt.Sprintf("每章 %d 字，允许 %d–%d 字", target, floor, ceil),
			fmt.Sprintf("%d–%d 字", domain.SceneCharsMin, domain.SceneCharsMax),
			"按细纲给出的场面节拍分场",
		} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("target %d: system prompt missing %q:\n%s", target, want, prompt)
			}
		}
		if strings.Contains(prompt, "拆成约 3 个场面") {
			t.Fatalf("target %d: system prompt still hardcodes three scenes:\n%s", target, prompt)
		}
	}
}
