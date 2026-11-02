package novel

import (
	"sort"
	"strings"
)

// 题材词汇表与别名解析。
//
// 这一层原本只在 knowledge 里，用来决定"某篇题材文章要不要给这本书"。但
// GenreBullets（写时约束）也需要同一套解析：一本 genre 写成 `悬疑刑侦` 的书，
// knowledge 能认出它属于悬疑，写时约束却因为精确查表落到"通用"。同一份题材
// 元数据被两处用两套规则解读，就是这类静默降级的来源。所以词汇表放在最低层
// novel，由 knowledge 复用。

// GenreFamily is one of the closed book-genre families used to decide whether
// a genre-specific article is offered. The names are the 检索标题 from
// `07-genre-platform`; a book's `genre` is expected to be one of them, and
// GenreFamilies maps common variations onto them.
type GenreFamily struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// GenreFamiliesList is the closed genre vocabulary, in display order.
var GenreFamiliesList = []GenreFamily{
	{ID: "玄幻", Label: "玄幻"},
	{ID: "仙侠", Label: "仙侠"},
	{ID: "都市", Label: "都市"},
	{ID: "悬疑", Label: "悬疑"},
	{ID: "现代言情", Label: "现代言情"},
	{ID: "古代言情", Label: "古代言情"},
	{ID: "仕途扫黑", Label: "仕途扫黑"},
	{ID: "系统穿越", Label: "系统穿越"},
}

func GenreOrder() map[string]int {
	m := make(map[string]int, len(GenreFamiliesList))
	for i, g := range GenreFamiliesList {
		m[g.ID] = i
	}
	return m
}

// genreAliases maps the spellings that appear in book metadata (platform main
// categories, reading tags) onto families. It only widens matching; an unknown
// name still falls back to a substring scan over the family names.
var genreAliases = map[string][]string{
	"玄幻": {"玄幻"}, "奇幻": {"玄幻"}, "东方玄幻": {"玄幻"}, "西方奇幻": {"玄幻"},
	"仙侠": {"仙侠"}, "修真": {"仙侠"}, "武侠": {"仙侠"}, "凡人修仙": {"仙侠"}, "古典仙侠": {"仙侠"},
	"都市": {"都市"}, "现实": {"都市"}, "都市日常": {"都市"}, "都市高武": {"都市"}, "战神赘婿": {"都市"},
	"悬疑": {"悬疑"}, "推理": {"悬疑"}, "刑侦": {"悬疑"}, "灵异": {"悬疑"}, "悬疑灵异": {"悬疑"},
	"言情":   {"现代言情", "古代言情"},
	"现代言情": {"现代言情"}, "都市言情": {"现代言情"},
	"古代言情": {"古代言情"}, "古言": {"古代言情"}, "宫斗": {"古代言情"},
	"仕途扫黑": {"仕途扫黑"}, "仕途": {"仕途扫黑"}, "官场": {"仕途扫黑"}, "扫黑": {"仕途扫黑"},
	"系统穿越": {"系统穿越"}, "系统": {"系统穿越"}, "签到": {"系统穿越"}, "快穿": {"系统穿越"}, "穿越": {"系统穿越"},
}

// GenreFamilies maps a book genre/subgenre (or an article's genre tag) onto the
// closed family set. Matching is set intersection, so an article tagged 言情
// matches both 现代言情 and 古代言情 books, and 悬疑灵异 matches 悬疑.
func GenreFamilies(name string) []string {
	n := strings.TrimSpace(name)
	if n == "" {
		return nil
	}
	if fams, ok := genreAliases[n]; ok {
		return fams
	}
	// Substring fallback: 悬疑脑洞 → 悬疑, 都市修真 → 都市+仙侠. Alias keys
	// participate so a platform category built out of an alias (修真) still
	// reaches its family (仙侠).
	seen := map[string]bool{}
	var hit []string
	add := func(fams []string) {
		for _, f := range fams {
			if !seen[f] {
				seen[f] = true
				hit = append(hit, f)
			}
		}
	}
	for _, f := range GenreFamiliesList {
		if strings.Contains(n, f.ID) {
			add([]string{f.ID})
		}
	}
	for alias, fams := range genreAliases {
		if alias != n && strings.Contains(n, alias) {
			add(fams)
		}
	}
	if len(hit) > 0 {
		sort.Slice(hit, func(i, j int) bool {
			ri, iok := genreOrder[hit[i]]
			rj, jok := genreOrder[hit[j]]
			if iok && jok {
				return ri < rj
			}
			return iok
		})
		return hit
	}
	return []string{n}
}

var genreOrder = GenreOrder()

// subgenreAliases maps subgenre spellings onto the canonical name used by
// knowledge article tags. 95-genre-routing 把警察、法医、社会派都归在
// 「刑侦探案」这一子类，所以书里写成 年代刑侦 / 刑侦 / 探案 都指向同一个子类；
// 若按字面比较，这些书就既拿不到子类文风、也拿不到子类文章。
var subgenreAliases = map[string]string{
	"刑侦探案": "刑侦探案",
	"刑侦":   "刑侦探案",
	"年代刑侦": "刑侦探案",
	"探案":   "刑侦探案",
	"侦探推理": "刑侦探案",
	"侦探":   "刑侦探案",
	"警察":   "刑侦探案",
	"法医":   "刑侦探案",
	"社会派":  "刑侦探案",
}

// CanonicalSubgenre returns the canonical subgenre name for a book's raw
// subgenre, or "" when the spelling maps to nothing known.
func CanonicalSubgenre(subgenre string) string {
	s := strings.TrimSpace(subgenre)
	if s == "" {
		return ""
	}
	if c, ok := subgenreAliases[s]; ok {
		return c
	}
	// 允许「年代刑侦·双主角」这类组合写法：包含规范名或其唯一别名即归一。
	for alias, canonical := range subgenreAliases {
		if alias == canonical {
			continue
		}
		if strings.Contains(s, alias) {
			return canonical
		}
	}
	return ""
}

// SubgenreMatched reports whether a tagged subgenre (article front matter) and
// a book subgenre name the same 子类.
func SubgenreMatched(tagged, book string) bool {
	t, b := CanonicalSubgenre(tagged), CanonicalSubgenre(book)
	if t != "" && b != "" {
		return t == b
	}
	return strings.EqualFold(strings.TrimSpace(tagged), strings.TrimSpace(book))
}
