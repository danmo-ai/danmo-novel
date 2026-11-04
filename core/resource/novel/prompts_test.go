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
		prompt := WriteChapterSystemPromptFor(target)
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

// 细纲提示词必须把 stateDeltas 定义成「维护状态」，而不只是「记录变化」。
//
// 实测代价：一条 health 从 v01-U1 记录后没有任何单元写终值，于是全量快照把它
// 一路带到 v01-U7——63 个故事日之后，写作包仍把它当作「当前身体状况」。
// 光靠一句「字段失效要写终值」不够：模型得知道要先读上下文里的前置快照、
// 逐字段对照，还要处理被系统标注为「可能已变」的字段。
func TestOutlineSystemPromptRequiresStateUpkeep(t *testing.T) {
	prompt := OutlineSystemPrompt()
	for _, want := range []string{
		"维护状态",    // 任务定位：不只记变化
		"连续性状态",   // 指明从上下文哪块读前置状态
		"完整快照",    // 说清那块是全量而非增量
		"必须写显式终值", // 失效字段的硬要求
		"可能已变",    // 与陈旧标注对齐
		"onStage 里每个有状态变化的人物至少一条", // 覆盖要求
		"不要为了凑数把没变的字段抄一遍",         // 防止另一个极端
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("outline prompt missing %q", want)
		}
	}
	// 词表仍由 domain 单一来源注入，不能写死在提示词里。
	if !strings.Contains(prompt, domain.StateDeltaFieldList()) {
		t.Fatalf("field vocabulary must come from domain.StateDeltaFields:\n%s", prompt)
	}
}
