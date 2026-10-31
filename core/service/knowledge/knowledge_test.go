package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/resource/novel"
)

func TestBuiltinKnowledgeAvailableWithoutLibrary(t *testing.T) {
	root := t.TempDir()
	docs, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != len(novel.BuiltinKnowledge()) || len(docs) != 25 {
		t.Fatalf("got %d built-in documents, want 25", len(docs))
	}
	seen := map[string]bool{}
	for _, doc := range docs {
		if !doc.Builtin || doc.Customized || doc.Title == "" || doc.Summary == "" {
			t.Errorf("invalid built-in metadata: %+v", doc)
		}
		if seen[doc.ID] {
			t.Errorf("duplicate document ID: %s", doc.ID)
		}
		seen[doc.ID] = true
		raw, err := Read(root, doc.ID)
		if err != nil || raw == "" {
			t.Errorf("Read(%s) = %q, %v", doc.ID, raw, err)
		}
	}
	for _, id := range []string{"01-pacing-structure", "21-workflow-candidate-accept", "25-block-diagnosis-scene-rescue"} {
		if !seen[id] {
			t.Errorf("missing built-in article %s", id)
		}
	}
}

func TestLocalOverlayAndRestore(t *testing.T) {
	root := t.TempDir()
	const id = "01-pacing-structure"
	original, err := Read(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(root, id, "# 我的节奏笔记\n\n个人修改。", nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := Read(root, id)
	if raw != "# 我的节奏笔记\n\n个人修改。" {
		t.Fatalf("overlay not used: %q", raw)
	}
	doc := findDoc(t, root, id)
	if !doc.Builtin || !doc.Customized || doc.Title != "我的节奏笔记" {
		t.Fatalf("unexpected overlay metadata: %+v", doc)
	}
	restored, err := Restore(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if restored != original {
		t.Fatal("restore did not expose the embedded original")
	}
	if _, err := os.Stat(filepath.Join(Root(root), id+".md")); !os.IsNotExist(err) {
		t.Fatalf("restore should remove only the local overlay: %v", err)
	}
	doc = findDoc(t, root, id)
	if doc.Customized {
		t.Fatal("restored built-in is still marked customized")
	}
	// Restoring an untouched document is safe and does not create a directory.
	if _, err := Restore(root, id); err != nil {
		t.Fatal(err)
	}
}

func TestCustomDocumentAndBuiltinsStaySeparate(t *testing.T) {
	root := t.TempDir()
	if err := Write(root, "my-book-methods", "# 本书方法\n\n仅本书使用。", nil); err != nil {
		t.Fatal(err)
	}
	docs, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != len(novel.BuiltinKnowledge())+1 {
		t.Fatalf("got %d documents", len(docs))
	}
	doc := findDoc(t, root, "my-book-methods")
	if doc.Builtin || doc.Customized || doc.Category != "custom" {
		t.Fatalf("unexpected custom document: %+v", doc)
	}
	if _, err := Restore(root, "my-book-methods"); !os.IsNotExist(err) {
		t.Fatalf("custom documents cannot be restored as built-ins: %v", err)
	}
	if raw, _ := Read(root, "my-book-methods"); raw == "" {
		t.Fatal("failed custom restore must not delete content")
	}
}

func TestReadRejectsUnknownAndUnsafeIDs(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"", "missing", "../secret", "nested/article", `nested\\article`} {
		if _, err := Read(root, id); err == nil {
			t.Errorf("Read(%q) unexpectedly succeeded", id)
		}
	}
	if err := Write(root, "../secret", "no", nil); err == nil {
		t.Fatal("unsafe write unexpectedly succeeded")
	}
}

func TestImportPreservesLocalEdits(t *testing.T) {
	root := t.TempDir()
	if err := Write(root, "01-pacing-structure", "personal", nil); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "01-pacing-structure.md"), []byte("replacement"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "custom.md"), []byte("# 自定义\n\n正文"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := ImportFromDir(root, source)
	if err != nil || n != 1 {
		t.Fatalf("ImportFromDir = %d, %v", n, err)
	}
	raw, _ := Read(root, "01-pacing-structure")
	if raw != "personal" {
		t.Fatalf("import overwrote personal content: %q", raw)
	}
	if !strings.Contains(mustRead(t, root, "custom"), "自定义") {
		t.Fatal("new custom article was not imported")
	}
}

func TestFrontMatterTagsAreStrippedFromProse(t *testing.T) {
	root := t.TempDir()
	const id = "my-steps"
	if err := os.MkdirAll(Root(root), 0o755); err != nil {
		t.Fatal(err)
	}
	handwritten := "---\nstages: [Write, outline, bogus, write]\n---\n# 我的方法\n\n正文第一段。"
	if err := os.WriteFile(filepath.Join(Root(root), id+".md"), []byte(handwritten), 0o644); err != nil {
		t.Fatal(err)
	}
	body, err := Read(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "stages:") || !strings.HasPrefix(body, "# 我的方法") {
		t.Fatalf("front matter leaked into prose: %q", body)
	}
	doc := findDoc(t, root, id)
	if strings.Join(doc.Stages, ",") != "outline,write" {
		t.Fatalf("stages = %v, want normalized [outline write]", doc.Stages)
	}
	if doc.Summary != "正文第一段。" {
		t.Fatalf("summary should skip front matter, got %q", doc.Summary)
	}
	if !doc.Applies("write") || doc.Applies("plan") {
		t.Fatalf("Applies mismatch for %v", doc.Stages)
	}
}

func TestHorizontalRuleIsNotMistakenForFrontMatter(t *testing.T) {
	plain := "---\n# 分节标题\n\n正文。"
	fm, body := splitFrontMatter(plain)
	if len(fm.Stages) != 0 || body != plain {
		t.Fatalf("plain Markdown was swallowed: fm=%v body=%q", fm.Stages, body)
	}
	// A header without stages parses fine and keeps the body intact.
	_, body = splitFrontMatter("---\ntitle: x\n---\n# 标题\n\n正文。")
	if body != "# 标题\n\n正文。" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestBuiltinArticlesAreTagged(t *testing.T) {
	root := t.TempDir()
	docs, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range docs {
		if !doc.Builtin {
			continue
		}
		if len(doc.Stages) == 0 {
			t.Errorf("built-in article %s has no stage tags", doc.ID)
		}
		for _, s := range doc.Stages {
			if _, ok := stageOrder[s]; !ok {
				t.Errorf("%s uses unknown stage %q", doc.ID, s)
			}
		}
	}
	// Fiction-genre guides must not be offered at 卷纲, where structure rules win.
	if genre := findDoc(t, root, "11-genre-suspense-investigation"); genre.Applies("volume") {
		t.Fatalf("genre guide should not claim 卷纲: %v", genre.Stages)
	}
	// The production-chain overview applies to every step.
	for _, stage := range Stages {
		if !findDoc(t, root, "21-workflow-candidate-accept").Applies(stage.ID) {
			t.Errorf("workflow overview should apply to %s", stage.ID)
		}
	}
}

func TestOverlayInheritsOrOverridesBuiltinTags(t *testing.T) {
	root := t.TempDir()
	const id = "01-pacing-structure"
	builtinTags, err := TagsOf(root, id)
	if err != nil || len(builtinTags.Stages) == 0 {
		t.Fatalf("builtin tags = %+v, %v", builtinTags, err)
	}
	// A prose-only overlay (no front matter) keeps the built-in tags.
	if err := Write(root, id, "# 我的节奏笔记\n\n个人修改。", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := TagsOf(root, id); strings.Join(got.Stages, ",") != strings.Join(builtinTags.Stages, ",") {
		t.Fatalf("overlay tags = %v, want inherited %v", got.Stages, builtinTags.Stages)
	}
	// Explicit tags replace the inherited ones.
	if err := Write(root, id, "# 我的节奏笔记\n\n个人修改。", &Tags{Stages: []string{"review"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := TagsOf(root, id); strings.Join(got.Stages, ",") != "review" {
		t.Fatalf("overlay tags = %v, want [review]", got.Stages)
	}
	// A nil keep preserves whatever the overlay declares.
	if err := Write(root, id, "# 我的节奏笔记\n\n再次修改。", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := TagsOf(root, id); strings.Join(got.Stages, ",") != "review" {
		t.Fatalf("tags not preserved on body-only save: %v", got.Stages)
	}
	restored, err := Restore(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(restored, "stages:") {
		t.Fatal("restore should return prose without front matter")
	}
}

func TestGenreTagsSplitCommonFromGenreSpecific(t *testing.T) {
	root := t.TempDir()
	// Every genre guide must name a family, so a 悬疑 book never sees 玄幻 rules.
	for _, id := range []string{
		"11-genre-suspense-investigation", "12-genre-urban-politics", "14-genre-xuanhuan",
		"15-genre-xianxia", "16-genre-urban", "17-genre-romance-modern",
		"18-genre-romance-historical", "19-genre-system",
	} {
		doc := findDoc(t, root, id)
		if len(doc.Genres) != 1 {
			t.Errorf("%s genres = %v, want exactly one family", id, doc.Genres)
		}
		if _, ok := genreOrder[doc.Genres[0]]; !ok {
			t.Errorf("%s uses unknown family %q", id, doc.Genres[0])
		}
	}
	// Method articles stay 公共 so they are offered to every book.
	for _, id := range []string{"01-pacing-structure", "05-style-deslop", "07-genre-platform", "21-workflow-candidate-accept"} {
		if doc := findDoc(t, root, id); !doc.IsCommon() {
			t.Errorf("%s should be 公共, got genres=%v subgenres=%v", id, doc.Genres, doc.Subgenres)
		}
	}
	// 人味文风 is clamped to the 刑侦探案 subgenre.
	crime := findDoc(t, root, "13-crime-human-flavor")
	if crime.GenreMatched("悬疑", "刑侦探案") != true {
		t.Fatal("crime guide should match 悬疑/刑侦探案")
	}
	if crime.GenreMatched("悬疑", "") || crime.GenreMatched("悬疑", "悬疑灵异") {
		t.Fatal("crime guide must not match other 悬疑 subgenres")
	}
}

func TestGenreFamilyAliases(t *testing.T) {
	cases := map[string][]string{
		"悬疑":   {"悬疑"},
		"悬疑灵异": {"悬疑"},
		"刑侦":   {"悬疑"},
		"都市修真": {"仙侠", "都市"}, // ordered by the genre vocabulary, not by the text
		"言情":   {"现代言情", "古代言情"},
		"官场":   {"仕途扫黑"},
		"系统":   {"系统穿越"},
		"":     nil,
	}
	for in, want := range cases {
		got := GenreFamilies(in)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("GenreFamilies(%q) = %v, want %v", in, got, want)
		}
	}
	// A book titled 悬疑 must not pull in 玄幻 articles, and 言情 books must
	// accept both romance guides.
	suspense := Doc{Genres: []string{"玄幻"}}
	if suspense.GenreMatched("悬疑", "") {
		t.Fatal("玄幻 article matched a 悬疑 book")
	}
	liu := Doc{Genres: []string{"古代言情"}}
	if liu.GenreMatched("言情", "") != true || liu.GenreMatched("现代言情", "") {
		t.Fatal("古代言情 article must match generic 言情 only through its own family")
	}
	common := Doc{}
	for _, g := range []string{"玄幻", "悬疑", "言情", ""} {
		if !common.GenreMatched(g, "") {
			t.Errorf("公共 article should match genre %q", g)
		}
	}
}

func TestGenreTagsPersistAndRoundTrip(t *testing.T) {
	root := t.TempDir()
	const id = "genre-note"
	tags := &Tags{Stages: []string{"write"}, Genres: []string{" 悬疑 ", "玄幻", "悬疑"}, Subgenres: []string{"刑侦探案", " "}}
	if err := Write(root, id, "# 题材笔记\n\n正文。", tags); err != nil {
		t.Fatal(err)
	}
	doc := findDoc(t, root, id)
	if strings.Join(doc.Genres, ",") != "玄幻,悬疑" {
		t.Fatalf("genres = %v, want normalized [玄幻 悬疑]", doc.Genres)
	}
	if strings.Join(doc.Subgenres, ",") != "刑侦探案" {
		t.Fatalf("subgenres = %v", doc.Subgenres)
	}
	// A body-only save keeps both stage and genre tags.
	if err := Write(root, id, "# 题材笔记\n\n改过的正文。", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := TagsOf(root, id); strings.Join(got.Genres, ",") != "玄幻,悬疑" || len(got.Stages) != 1 {
		t.Fatalf("tags lost on save: %+v", got)
	}
	// Dropping the tags makes the article 公共 again and removes the header.
	if err := Write(root, id, "# 题材笔记\n\n纯正文。", &Tags{}); err != nil {
		t.Fatal(err)
	}
	raw, _ := readRaw(root, id)
	if strings.Contains(raw, "---") || strings.Contains(raw, "genres:") {
		t.Fatalf("empty tags should leave plain Markdown: %q", raw)
	}
}

func TestUntaggedArticlesApplyEverywhere(t *testing.T) {
	root := t.TempDir()
	if err := Write(root, "untagged", "# 未标记\n\n随手笔记。", nil); err != nil {
		t.Fatal(err)
	}
	doc := findDoc(t, root, "untagged")
	if len(doc.Stages) != 0 {
		t.Fatalf("untagged article got stages: %v", doc.Stages)
	}
	for _, stage := range Stages {
		if !doc.Applies(stage.ID) {
			t.Errorf("untagged article should apply to %s", stage.ID)
		}
	}
}

func findDoc(t *testing.T, root, id string) Doc {
	t.Helper()
	docs, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range docs {
		if doc.ID == id {
			return doc
		}
	}
	t.Fatalf("document %s not found", id)
	return Doc{}
}

func mustRead(t *testing.T, root, id string) string {
	t.Helper()
	raw, err := Read(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
