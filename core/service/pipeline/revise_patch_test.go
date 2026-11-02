package pipeline

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

const patchProse = "他推开门。\n\n" +
	"屋里没有人，只有一盏将要熄灭的灯。\n\n" +
	"他走过去，把灯芯拨了拨。\n\n" +
	"「你回来了。」母亲在身后说。\n\n" +
	"他没有回头。"

func mustApply(t *testing.T, base string, edits ...chapterEdit) string {
	t.Helper()
	out, err := applyChapterEdits(base, edits)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	return out
}

func TestApplyChapterEditsSingleReplacement(t *testing.T) {
	out := mustApply(t, patchProse, chapterEdit{
		Find:    "他走过去，把灯芯拨了拨。",
		Replace: "他走过去，把灯芯拨亮了。",
	})
	if !strings.Contains(out, "拨亮了") || strings.Contains(out, "拨了拨") {
		t.Fatalf("replacement not applied:\n%s", out)
	}
	// 未涉及的段落必须逐字保留：补丁只能改动它声明的区间。
	if !strings.HasPrefix(out, "他推开门。\n\n屋里没有人") {
		t.Fatalf("untouched prefix changed:\n%s", out)
	}
}

// 模型常把多处改动按内容顺序而不是位置顺序输出。顺序不该影响结果。
func TestApplyChapterEditsOutOfOrder(t *testing.T) {
	out := mustApply(t, patchProse,
		chapterEdit{Find: "他没有回头。", Replace: "他终于回头了。"},
		chapterEdit{Find: "他推开门。", Replace: "他撞开门。"},
	)
	want := strings.Replace(strings.Replace(patchProse, "他推开门。", "他撞开门。", 1), "他没有回头。", "他终于回头了。", 1)
	if out != want {
		t.Fatalf("out-of-order edits produced wrong result:\n got=%s\nwant=%s", out, want)
	}
}

// 锚点可以跨段落：模型常把前后两段一起给出来以取得唯一性。
func TestApplyChapterEditsAcrossParagraphs(t *testing.T) {
	find := "只有一盏将要熄灭的灯。\n\n他走过去"
	out := mustApply(t, patchProse, chapterEdit{Find: find, Replace: "只有一盏将要熄灭的灯。\n\n他停了一下，才走过去"})
	if !strings.Contains(out, "他停了一下，才走过去") {
		t.Fatalf("cross-paragraph edit failed:\n%s", out)
	}
}

// replace 为空 = 删除。
func TestApplyChapterEditsDeletion(t *testing.T) {
	out := mustApply(t, patchProse, chapterEdit{Find: "他没有回头。", Replace: ""})
	if strings.Contains(out, "他没有回头") {
		t.Fatalf("deletion not applied:\n%s", out)
	}
}

// 行尾空白不应导致锚点失配：模型抄中文时经常丢掉行尾空格。
func TestApplyChapterEditsIgnoresTrailingWhitespace(t *testing.T) {
	base := "第一句。   \n第二句。\n第三句。"
	out := mustApply(t, base, chapterEdit{Find: "第一句。\n第二句。", Replace: "第一句改。\n第二句。"})
	if !strings.Contains(out, "第一句改。") {
		t.Fatalf("rstrip tier did not match:\n%s", out)
	}
}

// 弯引号 / 全角标点 / 全角空格都是同一字符的两种写法，归一后应当命中。
//
// 归一只折叠**同一个字符**的书写形式（「」↔""、：↔:、　↔ 空格），不会补字、
// 删字或改实词——所以模型抄错一个字仍然会被拒绝，不会靠归一糊过去。
func TestApplyChapterEditsNormalizesPunctuation(t *testing.T) {
	base := "他说：「好。」然后走了。\n\n她　停在门口。"
	a := mustApply(t, base, chapterEdit{
		Find:    `他说:"好。"然后走了。`,
		Replace: `他说:"随你。"然后走了。`,
	})
	if !strings.Contains(a, "随你") {
		t.Fatalf("quote/fullwidth-colon normalization failed:\n%s", a)
	}
	b := mustApply(t, base, chapterEdit{Find: "她 停在门口。", Replace: "她仍停在门口。"})
	if !strings.Contains(b, "她仍停在门口") {
		t.Fatalf("ideographic-space normalization failed:\n%s", b)
	}
	// 归一不是模糊匹配：实词抄错仍然必须失败。
	if _, err := applyChapterEdits(base, []chapterEdit{{Find: "他说：「行。」然后走了。", Replace: "x"}}); err == nil {
		t.Fatal("a paraphrase must not be rescued by normalization")
	}
}

// 短锚点只要本章真出现一次就必须放行——长度不是门槛，唯一性才是。
func TestApplyChapterEditsAllowsShortUniqueAnchor(t *testing.T) {
	out := mustApply(t, patchProse, chapterEdit{Find: "拨了拨", Replace: "拨亮"})
	if !strings.Contains(out, "把灯芯拨亮。") {
		t.Fatalf("short unique anchor rejected:\n%s", out)
	}
}

func TestApplyChapterEditsRejectsMissingAnchor(t *testing.T) {
	_, err := applyChapterEdits(patchProse, []chapterEdit{{
		Find: "他走过去，把灯芯掐灭了。", Replace: "他走过去，把灯芯拨亮了。",
	}})
	if err == nil {
		t.Fatal("an anchor that matches nowhere must be rejected")
	}
	// 必须把本章最接近的真实原文喂回去，模型才知道自己抄错了哪个字。
	if !strings.Contains(err.Error(), "最接近的原文") || !strings.Contains(err.Error(), "把灯芯拨了拨") {
		t.Fatalf("error should quote the nearest real line: %v", err)
	}
}

// 命中多处时不能擅自取第一处：模型指的是哪一处无法从输出推断。
func TestApplyChapterEditsRejectsAmbiguousAnchor(t *testing.T) {
	base := "开头一句。\n\n他走了。他走了。\n\n结尾。"
	_, err := applyChapterEdits(base, []chapterEdit{{Find: "他走了。", Replace: "她走了。"}})
	if err == nil {
		t.Fatal("an ambiguous anchor must be rejected, not silently applied to the first hit")
	}
	if !strings.Contains(err.Error(), "出现 2 次") {
		t.Fatalf("error should report the hit count: %v", err)
	}
	// 行号是模型扩展上下文时唯一可靠的参考。
	if !strings.Contains(err.Error(), "第 3 行") {
		t.Fatalf("error should carry the line number: %v", err)
	}
	if !strings.Contains(err.Error(), "扩一句") {
		t.Fatalf("error should tell the model how to fix it: %v", err)
	}
}

// 短且不命中时要说清"过短"这个方向性原因，避免模型继续往更短改。
func TestApplyChapterEditsFlagsTooShortAnchor(t *testing.T) {
	_, err := applyChapterEdits(patchProse, []chapterEdit{{Find: "好啊。", Replace: "行啊。"}})
	if err == nil {
		t.Fatal("missing short anchor must be rejected")
	}
	if !strings.Contains(err.Error(), "过短") {
		t.Fatalf("error should explain that the anchor is too short: %v", err)
	}
}

func TestApplyChapterEditsRejectsOverlap(t *testing.T) {
	_, err := applyChapterEdits(patchProse, []chapterEdit{
		{Find: "他走过去，把灯芯拨了拨。", Replace: "甲"},
		{Find: "把灯芯拨了拨。\n\n「你回来了。」", Replace: "乙"},
	})
	if err == nil {
		t.Fatal("overlapping edits must be rejected")
	}
	if !strings.Contains(err.Error(), "重叠") || !strings.Contains(err.Error(), "合并") {
		t.Fatalf("error should ask the model to merge them: %v", err)
	}
}

func TestApplyChapterEditsRejectsEmptyEdits(t *testing.T) {
	if _, err := applyChapterEdits(patchProse, nil); err == nil {
		t.Fatal("empty edits must be rejected")
	}
}

func TestApplyChapterEditsRejectsNoOpEdit(t *testing.T) {
	_, err := applyChapterEdits(patchProse, []chapterEdit{{
		Find: "他推开门。", Replace: "他推开门。",
	}})
	if err == nil {
		t.Fatal("a no-op edit must be rejected")
	}
	if !strings.Contains(err.Error(), "空操作") {
		t.Fatalf("error=%v", err)
	}
}

func TestParseChapterPatchFillsMissingChapter(t *testing.T) {
	ch := domain.ChapterDraft{Chapter: 7, Title: "门", Body: patchProse}
	draft, err := parseChapterPatch(`{"edits":[{"find":"他推开门。","replace":"他撞开门。"}]}`, ch)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Chapter != 7 {
		t.Fatalf("chapter=%d want 7 (filled from the call scope)", draft.Chapter)
	}
	// 没给 title 时沿用原标题：修订不该丢章名。
	if draft.Title != "门" {
		t.Fatalf("title=%q want the original", draft.Title)
	}
}

func TestParseChapterPatchRejectsWrongChapter(t *testing.T) {
	ch := domain.ChapterDraft{Chapter: 7, Title: "门", Body: patchProse}
	_, err := parseChapterPatch(`{"chapter":9,"edits":[{"find":"他推开门。","replace":"他撞开门。"}]}`, ch)
	if err == nil {
		t.Fatal("a patch aimed at another chapter must be rejected")
	}
	if !strings.Contains(err.Error(), "chapter") {
		t.Fatalf("error=%v", err)
	}
}
