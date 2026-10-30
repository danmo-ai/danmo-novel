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
