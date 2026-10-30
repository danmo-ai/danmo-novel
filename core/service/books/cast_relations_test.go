package books

import (
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func TestParseCastRelationsFormalAndAnchor(t *testing.T) {
	cast := []domain.CastCard{
		{Stem: "wen-nianci", Name: "温念慈", Role: "protagonist", Raw: `# 温念慈
` + "`role`: protagonist" + `

- **关系锚点**：裴国栋（组织信任的兜底）。

## 关系
| 对方 | 类型 | 当前 |
|------|------|------|
| lu-chen | 脑内搭档 | 不能牵手 |
| fang-ziheng | 宿敌 | 当众欣赏 |
`},
		{Stem: "lu-chen", Name: "陆沉", Role: "recurring", Raw: `# 陆沉
` + "`role`: recurring" + `
`},
		{Stem: "ma-jian-guo", Name: "马建国", Role: "recurring", Raw: `# 马建国
与主角关系：护犊子所长
`},
		{Stem: "pei-guo-dong", Name: "裴国栋", Role: "recurring", Raw: "# 裴国栋\n"},
		{Stem: "fang-ziheng", Name: "方子衡", Role: "volume_antagonist", Raw: "# 方子衡\n"},
	}

	rels := ParseCastRelations(cast)
	if len(rels) < 4 {
		t.Fatalf("expected several relations, got %d: %+v", len(rels), rels)
	}
	formal := 0
	for _, r := range rels {
		if r.Formal {
			formal++
		}
		if r.From == "wen-nianci" && r.To == "lu-chen" && !r.Formal {
			t.Fatal("formal edge should win / be recorded as formal")
		}
	}
	if formal < 2 {
		t.Fatalf("formal=%d rels=%+v", formal, rels)
	}
	foundProse := false
	for _, r := range rels {
		if r.From == "ma-jian-guo" && r.To == "wen-nianci" && !r.Formal {
			foundProse = true
		}
	}
	if !foundProse {
		t.Fatalf("missing 与主角关系 edge: %+v", rels)
	}
}

func TestResolveCastRefChineseName(t *testing.T) {
	cast := []domain.CastCard{
		{Stem: "chen-zu-zhang", Name: "陈组长"},
		{Stem: "lu-chen", Name: "陆沉"},
	}
	if got := ResolveCastRef("陆沉", cast); got != "lu-chen" {
		t.Fatalf("got %q", got)
	}
	if got := ResolveCastRef("chen-zu-zhang", cast); got != "chen-zu-zhang" {
		t.Fatalf("got %q", got)
	}
	if got := ResolveCastRef("陈组长", cast); got != "chen-zu-zhang" {
		t.Fatalf("got %q", got)
	}
}
