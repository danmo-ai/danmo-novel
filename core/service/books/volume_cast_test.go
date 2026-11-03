package books

import (
	"os"
	"testing"
)

func TestParseVolumeCastChineseColon(t *testing.T) {
	raw := `## 本卷人物（stem）

- wen-nianci：本卷在场主角。长说明
- lu-chen：心声位
- fang-ziheng（镜像暗线）：不出场
- 温念慈：中文应跳过
`
	got := parseVolumeCast(raw)
	want := []string{"wen-nianci", "lu-chen", "fang-ziheng"}
	if len(got) != len(want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
}

// 卷根字段的 `- cast: a,b,c` 是机器可读的人物表。以前只认 `## 本卷人物`，
// 用这种写法生成的卷纲解析出空 cast：细纲 prompt 没有人物卡，onStage 越界
// 检查也因 volumeCast 为空被静默跳过。
func TestParseVolumeCastRootFieldForms(t *testing.T) {
	cases := map[string][]string{
		"- cast: lu-chen,wen-nianci,zhao-dezhu":   {"lu-chen", "wen-nianci", "zhao-dezhu"},
		"- cast: [wen-nianci,lu-chen]":            {"wen-nianci", "lu-chen"},
		"- cast: wen-nianci，lu-chen":              {"wen-nianci", "lu-chen"},
		"- cast: lu-chen, lu-chen, 温念慈, Bad_Stem": {"lu-chen"},
		"- cast:": nil,
	}
	for raw, want := range cases {
		got := parseVolumeCast(raw)
		if len(got) != len(want) {
			t.Fatalf("%q: got=%v want=%v", raw, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%q: got=%v want=%v", raw, got, want)
			}
		}
	}
}

// 同一份卷纲里两种写法并存时去重，且顺序稳定（根字段在前）。
func TestParseVolumeCastMergesBothForms(t *testing.T) {
	raw := "- cast: lu-chen,wen-nianci\n\n## 本卷人物（stem）\n- wen-nianci：重复\n- zhao-dezhu：补充\n"
	got := parseVolumeCast(raw)
	want := []string{"lu-chen", "wen-nianci", "zhao-dezhu"}
	if len(got) != len(want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
}

func TestParseVolumeCastMindvoiceV05(t *testing.T) {
	b, err := os.ReadFile("/Users/nil.luo/.danmo-work/data/proj-1788358071970011000/files/novel/mindvoice-cop/outline/volumes/v05.md")
	if err != nil {
		t.Skip(err)
	}
	got := parseVolumeCast(string(b))
	if len(got) < 8 {
		t.Fatalf("v05 cast too few: %v", got)
	}
	for _, stem := range []string{"wen-nianci", "lu-chen", "li-yan", "fang-ziheng"} {
		ok := false
		for _, g := range got {
			if g == stem {
				ok = true
				break
			}
		}
		if !ok {
			t.Fatalf("missing %s in %v", stem, got)
		}
	}
}
