package knowledge

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/resource/novel"
	"gopkg.in/yaml.v3"
)

// Stage is one LLM-call step whose confirm dialog lets the author attach
// reference articles. The IDs are what an article declares in its front matter
// (`stages: [plan, outline]`); keep them aligned with the run kinds that show
// the run confirm dialog.
type Stage struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Stages is the closed stage vocabulary, in display order.
var Stages = []Stage{
	{ID: "plan", Label: "立项"},
	{ID: "volume", Label: "卷纲"},
	{ID: "outline", Label: "单元细纲"},
	{ID: "write", Label: "单元正文"},
	{ID: "review", Label: "审校"},
	{ID: "entity", Label: "设定重写"},
}

var stageOrder = func() map[string]int {
	m := make(map[string]int, len(Stages))
	for i, s := range Stages {
		m[s.ID] = i
	}
	return m
}()

// NormalizeStages trims, lowercases, dedupes and drops unknown stage IDs, then
// orders the result by the stage vocabulary (not by author input order).
func NormalizeStages(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		id := strings.ToLower(strings.TrimSpace(raw))
		if _, ok := stageOrder[id]; !ok || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return stageOrder[out[i]] < stageOrder[out[j]] })
	return out
}

// 题材词汇表已下移到 core/resource/novel（那是最低层，写时约束与知识匹配
// 必须共用同一套解析）。这里保留同名导出，避免调用方与前端镜像的语义分叉。
type GenreFamily = novel.GenreFamily

var GenreFamiliesList = novel.GenreFamiliesList

var genreOrder = novel.GenreOrder()

// GenreFamilies maps a book genre/subgenre (or an article's genre tag) onto the
// closed family set. See novel.GenreFamilies.
func GenreFamilies(name string) []string { return novel.GenreFamilies(name) }

func intersects(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// NormalizeGenres trims and dedupes article genre tags, ordering known families
// by the vocabulary and leaving unknown names after them.
func NormalizeGenres(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		g := strings.TrimSpace(raw)
		if g == "" || seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, iok := genreOrder[out[i]]
		rj, jok := genreOrder[out[j]]
		switch {
		case iok && jok:
			return ri < rj
		case iok:
			return true
		case jok:
			return false
		default:
			return out[i] < out[j]
		}
	})
	return out
}

func normalizeSubgenres(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// Doc is one reference article. Knowledge is NOT canon: it is method/style/
// subject reference the author may attach to a call explicitly. Built-in
// documents ship in the binary; same-ID local files are author-owned overlays.
type Doc struct {
	ID         string `json:"id"`      // filename without .md
	Title      string `json:"title"`   // first H1 or id
	Summary    string `json:"summary"` // first non-heading paragraph, clipped
	Bytes      int    `json:"bytes"`
	Category   string `json:"category"` // general | genre | custom
	Builtin    bool   `json:"builtin"`
	Customized bool   `json:"customized"` // local content differs from the built-in version
	// Stages lists the steps this article applies to (front matter `stages:`).
	// Empty means untagged: the article stays selectable at every step.
	Stages []string `json:"stages"`
	// Genres/Subgenres narrow the article to a book genre (front matter
	// `genres:` / `subgenres:`). Empty both = 公共, offered for every book.
	Genres    []string `json:"genres"`
	Subgenres []string `json:"subgenres"`
	// Default marks a 默认注入 article: the pipeline always adds it to that
	// stage's baseline prompt, so the picker must hide it instead of offering
	// a switch that would double-inject the same text.
	Default bool `json:"default,omitempty"`
	// Audience is "model" (default) or "author". Author docs (平台运营、应用流程、
	// 出处索引) never reach a prompt and are hidden from the picker.
	Audience string `json:"audience,omitempty"`
}

// ForModel reports whether the article may be injected into an LLM call.
// This is the single guard the picker and the pipeline both consult, so an
// author-only document cannot reach a prompt through a forgotten code path.
func (d Doc) ForModel() bool { return d.Audience != AudienceAuthor }

// Applies reports whether the article is offered for the given step. Untagged
// articles apply everywhere so a new article is never silently invisible.
func (d Doc) Applies(stage string) bool {
	if stage == "" || len(d.Stages) == 0 {
		return true
	}
	for _, s := range d.Stages {
		if s == stage {
			return true
		}
	}
	return false
}

// GenreMatched reports whether the article applies to a book of this genre.
// 公共 articles (no genre tags) always match; genre articles compare families
// so 悬疑灵异 finds 悬疑 and 言情 finds both romance families. Subgenre tags are
// an extra restriction (人味文风 only inside 刑侦探案).
func (d Doc) GenreMatched(genre, subgenre string) bool {
	if len(d.Genres) == 0 && len(d.Subgenres) == 0 {
		return true
	}
	if len(d.Genres) > 0 {
		book := GenreFamilies(genre)
		ok := false
		for _, g := range d.Genres {
			if intersects(book, GenreFamilies(g)) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(d.Subgenres) > 0 {
		sub := strings.TrimSpace(subgenre)
		if sub == "" {
			return false
		}
		ok := false
		for _, s := range d.Subgenres {
			// 子类按规范名比较：书里写 年代刑侦、文章标 刑侦探案，是同一个子类。
			if novel.SubgenreMatched(s, sub) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// IsCommon reports whether the article is 公共 (no genre restriction).
func (d Doc) IsCommon() bool { return len(d.Genres) == 0 && len(d.Subgenres) == 0 }

// CategoryOf classifies by filename prefix: NN-genre-* → genre, NN-* → general,
// everything else → custom (作者自建).
func CategoryOf(id string) string {
	if len(id) > 2 && id[0] >= '0' && id[0] <= '9' && id[1] >= '0' && id[1] <= '9' && id[2] == '-' {
		if len(id) > 9 && id[3:8] == "genre" {
			return "genre"
		}
		return "general"
	}
	return "custom"
}

func Root(libraryRoot string) string {
	return filepath.Join(libraryRoot, "knowledge")
}

// List merges built-in articles with the flat, author-managed local library.
// A missing local file does not hide a built-in article; an edited local file
// takes precedence and is never silently overwritten by a release upgrade.
func List(libraryRoot string) ([]Doc, error) {
	local, err := localDocuments(libraryRoot)
	if err != nil {
		return nil, err
	}
	builtins := novel.BuiltinKnowledge()
	out := make([]Doc, 0, len(builtins)+len(local))
	for _, article := range builtins {
		raw := article.Content
		customized := false
		if localRaw, ok := local[article.ID]; ok {
			raw = localRaw
			customized = bodyOf(localRaw) != bodyOf(article.Content)
			delete(local, article.ID)
		}
		doc := newDoc(article.ID, raw, true, customized)
		// An author overlay that drops the front matter inherits the built-in
		// tags, so editing prose never silently un-tags an article.
		builtinTags := tagsFrom(article.Content)
		if len(doc.Stages) == 0 {
			doc.Stages = builtinTags.Stages
		}
		if len(doc.Genres) == 0 && len(doc.Subgenres) == 0 {
			doc.Genres = builtinTags.Genres
			doc.Subgenres = builtinTags.Subgenres
		}
		// 默认注入同样继承：作者只改正文时，不能把这份文章变成可选清单。
		if !doc.Default {
			doc.Default = builtinTags.Default
		}
		// audience=author 也要继承：覆盖文件只改正文时，作者文档不能变成可注入。
		if doc.Audience == "" {
			doc.Audience = builtinTags.Audience
		}
		if doc.Audience == "" {
			doc.Audience = AudienceModel
		}
		out = append(out, doc)
	}
	for id, raw := range local {
		out = append(out, newDoc(id, raw, false, false))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Title == out[j].Title {
			return out[i].ID < out[j].ID
		}
		return out[i].Title < out[j].Title
	})
	return out, nil
}

func newDoc(id, raw string, builtin, customized bool) Doc {
	// 单章字数这类政策数字在文章里写成占位符，展示/注入时统一展开，
	// 免得文章里的数字和硬门里的数字各走各的。
	body := novel.ExpandPolicy(bodyOf(raw))
	tags := tagsFrom(raw)
	return Doc{
		ID: id, Title: titleOf(id, body), Summary: summaryOf(body), Bytes: len(body),
		Category: CategoryOf(id), Builtin: builtin, Customized: customized,
		Stages: tags.Stages, Genres: tags.Genres, Subgenres: tags.Subgenres,
		Default: tags.Default, Audience: tags.Audience,
	}
}

// frontMatter is the optional YAML header of an article:
//
//	---
//	stages: [plan, outline]
//	genres: [悬疑]
//	subgenres: [刑侦探案]
//	default: true
//	---
//
// It carries applicability metadata only; the body is what the author edits and
// what an LLM call receives.
type frontMatter struct {
	Stages    []string `yaml:"stages"`
	Genres    []string `yaml:"genres"`
	Subgenres []string `yaml:"subgenres"`
	// Default marks a 默认注入 article: the pipeline always includes it in that
	// stage's baseline prompt, so it is NOT offered in the picker. Selecting it
	// again would double-inject the same text.
	Default bool `yaml:"default"`
	// Audience separates 给模型的规则 from 给作者的操作说明（平台运营、应用流程、
	// 出处索引）。默认为 model；author 的文章永不进提示词，也不出现在选择器里。
	Audience string `yaml:"audience"`
}

// Audience values. An empty audience is model: that is the common case and the
// safe default, because a new article should be usable rather than invisible.
const (
	AudienceModel  = "model"
	AudienceAuthor = "author"
)

// normalizeAudience clamps the front-matter value to the two known audiences.
func normalizeAudience(in string) string {
	if strings.EqualFold(strings.TrimSpace(in), AudienceAuthor) {
		return AudienceAuthor
	}
	return AudienceModel
}

// Tags is the effective applicability metadata of one article: which steps it
// is offered at, and which book genre it is written for (empty = 公共).
type Tags struct {
	Stages    []string `json:"stages"`
	Genres    []string `json:"genres"`
	Subgenres []string `json:"subgenres"`
	// Default = 默认注入：由 pipeline 自动带上，不进选择器。
	Default bool `json:"default,omitempty"`
	// Audience = model（默认）或 author。author 永不进提示词。
	Audience string `json:"audience,omitempty"`
}

// splitFrontMatter separates the YAML header from the body. Documents without a
// header are returned unchanged so plain Markdown keeps working.
func splitFrontMatter(raw string) (frontMatter, string) {
	var fm frontMatter
	s := strings.TrimPrefix(raw, "\ufeff")
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fm, raw
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return fm, raw
	}
	// A document may open with a horizontal rule rather than a header. Only a
	// block that actually parses as YAML is treated as front matter, so real
	// prose is never swallowed.
	parsed := frontMatter{}
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &parsed); err != nil {
		return fm, raw
	}
	body := strings.Join(lines[end+1:], "\n")
	body = strings.TrimLeft(body, "\n")
	return parsed, body
}

// tagsFrom reads the normalized applicability metadata out of a raw document.
func tagsFrom(raw string) Tags {
	fm, _ := splitFrontMatter(raw)
	return Tags{
		Stages:    NormalizeStages(fm.Stages),
		Genres:    NormalizeGenres(fm.Genres),
		Subgenres: normalizeSubgenres(fm.Subgenres),
		Default:   fm.Default,
		Audience:  normalizeAudience(fm.Audience),
	}
}

func bodyOf(raw string) string {
	_, body := splitFrontMatter(raw)
	return body
}

// compose writes the front matter back in front of the body. Empty tags omit
// the header entirely, so untagged articles stay plain Markdown.
func compose(body string, tags Tags) string {
	var lines []string
	if len(tags.Stages) > 0 {
		lines = append(lines, "stages: ["+strings.Join(tags.Stages, ", ")+"]")
	}
	if len(tags.Genres) > 0 {
		lines = append(lines, "genres: ["+strings.Join(tags.Genres, ", ")+"]")
	}
	if len(tags.Subgenres) > 0 {
		lines = append(lines, "subgenres: ["+strings.Join(tags.Subgenres, ", ")+"]")
	}
	// default 必须能写回：否则作者在知识库页保存一次，默认注入的标记就丢了。
	if tags.Default {
		lines = append(lines, "default: true")
	}
	// audience=author 同理：丢了这一行，作者文档会变成可注入的文章。
	if tags.Audience == AudienceAuthor {
		lines = append(lines, "audience: author")
	}
	if len(lines) == 0 {
		return body
	}
	return "---\n" + strings.Join(lines, "\n") + "\n---\n\n" + strings.TrimLeft(body, "\n")
}

func localDocuments(libraryRoot string) (map[string]string, error) {
	entries, err := os.ReadDir(Root(libraryRoot))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(Root(libraryRoot), entry.Name()))
		if err != nil {
			return nil, err
		}
		out[strings.TrimSuffix(entry.Name(), ".md")] = string(raw)
	}
	return out, nil
}

// Read returns the author's overlay, or the built-in article when no overlay
// exists. Built-ins therefore work with an empty or read-only library root. The
// YAML front matter is stripped: callers get prose, never metadata.
func Read(libraryRoot, id string) (string, error) {
	raw, err := readRaw(libraryRoot, id)
	if err != nil {
		return "", err
	}
	return novel.ExpandPolicy(bodyOf(raw)), nil
}

func readRaw(libraryRoot, id string) (string, error) {
	if !validID(id) {
		return "", os.ErrInvalid
	}
	raw, err := os.ReadFile(filepath.Join(Root(libraryRoot), id+".md"))
	if err == nil {
		return string(raw), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if builtin, ok := novel.BuiltinKnowledgeDocument(id); ok {
		return builtin, nil
	}
	return "", err
}

// TagsOf returns the effective tags: the overlay's when it declares any,
// otherwise the built-in article's. Step and genre tags inherit independently,
// so a body-only overlay keeps both.
func TagsOf(libraryRoot, id string) (Tags, error) {
	raw, err := readRaw(libraryRoot, id)
	if err != nil {
		return Tags{}, err
	}
	tags := tagsFrom(raw)
	builtin, ok := novel.BuiltinKnowledgeDocument(id)
	if !ok {
		return tags, nil
	}
	bt := tagsFrom(builtin)
	if len(tags.Stages) == 0 {
		tags.Stages = bt.Stages
	}
	if len(tags.Genres) == 0 && len(tags.Subgenres) == 0 {
		tags.Genres = bt.Genres
		tags.Subgenres = bt.Subgenres
	}
	// 默认注入是结构属性，不是作者可选项：本地覆盖文件没写就继承内置。
	if !tags.Default {
		tags.Default = bt.Default
	}
	if tags.Audience == "" {
		tags.Audience = bt.Audience
	}
	if tags.Audience == "" {
		tags.Audience = AudienceModel
	}
	return tags, nil
}

// Restore removes only the local overlay, exposing the built-in article again.
func Restore(libraryRoot, id string) (string, error) {
	builtin, ok := novel.BuiltinKnowledgeDocument(id)
	if !ok {
		return "", os.ErrNotExist
	}
	if err := os.Remove(filepath.Join(Root(libraryRoot), id+".md")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	// 与 Read 保持同一种呈现：恢复后作者看到的必须是展开过占位符的正文。
	return novel.ExpandPolicy(bodyOf(builtin)), nil
}

func validID(id string) bool {
	return id != "" && !strings.ContainsAny(id, "/\\") && !strings.Contains(id, "..")
}

// IsCustomized reports whether the author's library holds a body that differs
// from the built-in article (same test List uses for the "已编辑" badge).
// Callers that must know whether to prefer an edit over the shipped default
// should use this instead of byte-comparing raw files, because Read returns the
// body while the embedded article still carries its front matter.
func IsCustomized(libraryRoot, id string) bool {
	if !validID(id) {
		return false
	}
	local, err := os.ReadFile(filepath.Join(Root(libraryRoot), id+".md"))
	if err != nil {
		return false
	}
	builtin, ok := novel.BuiltinKnowledgeDocument(id)
	if !ok {
		return true // 作者自建文章，没有内置版本可比较
	}
	return bodyOf(string(local)) != bodyOf(builtin)
}

// Write saves an article body (author edits own copy; no candidate flow needed
// because knowledge is not canon and never auto-enters context). A nil keep
// means "preserve the current effective tags"; otherwise keep supplies the new
// step and genre tags.
func Write(libraryRoot, id, content string, keep *Tags) error {
	if !validID(id) {
		return os.ErrInvalid
	}
	body := bodyOf(content)
	tags := Tags{}
	if keep != nil {
		tags = *keep
	} else {
		current, err := TagsOf(libraryRoot, id)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		tags = current
	}
	tags = Tags{
		Stages:    NormalizeStages(tags.Stages),
		Genres:    NormalizeGenres(tags.Genres),
		Subgenres: normalizeSubgenres(tags.Subgenres),
		// Default / Audience 不在归一化范围内，必须显式保留：漏掉它们会让
		// 默认注入的文章退化成可勾选项，让作者文档变成可注入文章。
		Default:  tags.Default,
		Audience: normalizeAudience(tags.Audience),
	}
	if err := os.MkdirAll(Root(libraryRoot), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(Root(libraryRoot), id+".md"), []byte(compose(body, tags)), 0o644)
}

// ImportFromDir copies *.md from srcDir into the library, skipping existing
// files (never overwrite author edits). Returns number imported.
func ImportFromDir(libraryRoot, srcDir string) (int, error) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		dst := filepath.Join(Root(libraryRoot), e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			continue
		}
		if err := os.MkdirAll(Root(libraryRoot), 0o755); err != nil {
			return n, err
		}
		if err := os.WriteFile(dst, raw, 0o644); err != nil {
			continue
		}
		n++
	}
	return n, nil
}

func titleOf(id, raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return id
}

func summaryOf(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		r := []rune(s)
		if len(r) > 80 {
			return string(r[:80]) + "…"
		}
		return s
	}
	return ""
}
