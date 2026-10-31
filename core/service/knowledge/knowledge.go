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

// GenreFamily is one of the closed book-genre families used to decide whether
// a genre-specific article is offered. The names are the 检索标题 from
// `07-genre-platform`; a book's `genre` is expected to be one of them, and
// `GenreFamilies` maps common variations onto them.
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

var genreOrder = func() map[string]int {
	m := make(map[string]int, len(GenreFamiliesList))
	for i, g := range GenreFamiliesList {
		m[g.ID] = i
	}
	return m
}()

// genreAliases maps the spellings that appear in book metadata (platform main
// categories, reading tags) onto families. It only widens matching; an unknown
// name still falls back to a substring scan over the family names.
var genreAliases = map[string][]string{
	"玄幻": {"玄幻"}, "奇幻": {"玄幻"}, "东方玄幻": {"玄幻"}, "西方奇幻": {"玄幻"},
	"仙侠": {"仙侠"}, "修真": {"仙侠"}, "武侠": {"仙侠"}, "凡人修仙": {"仙侠"}, "古典仙侠": {"仙侠"},
	"都市": {"都市"}, "现实": {"都市"}, "都市日常": {"都市"}, "都市高武": {"都市"}, "战神赘婿": {"都市"},
	"悬疑": {"悬疑"}, "推理": {"悬疑"}, "刑侦": {"悬疑"}, "灵异": {"悬疑"}, "悬疑灵异": {"悬疑"},
	"言情": {"现代言情", "古代言情"},
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
}

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
			if strings.EqualFold(strings.TrimSpace(s), sub) {
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
	body := bodyOf(raw)
	tags := tagsFrom(raw)
	return Doc{
		ID: id, Title: titleOf(id, body), Summary: summaryOf(body), Bytes: len(body),
		Category: CategoryOf(id), Builtin: builtin, Customized: customized,
		Stages: tags.Stages, Genres: tags.Genres, Subgenres: tags.Subgenres,
	}
}

// frontMatter is the optional YAML header of an article:
//
//	---
//	stages: [plan, outline]
//	genres: [悬疑]
//	subgenres: [刑侦探案]
//	---
//
// It carries applicability metadata only; the body is what the author edits and
// what an LLM call receives.
type frontMatter struct {
	Stages    []string `yaml:"stages"`
	Genres    []string `yaml:"genres"`
	Subgenres []string `yaml:"subgenres"`
}

// Tags is the effective applicability metadata of one article: which steps it
// is offered at, and which book genre it is written for (empty = 公共).
type Tags struct {
	Stages    []string `json:"stages"`
	Genres    []string `json:"genres"`
	Subgenres []string `json:"subgenres"`
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
	return bodyOf(raw), nil
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
	return bodyOf(builtin), nil
}

func validID(id string) bool {
	return id != "" && !strings.ContainsAny(id, "/\\") && !strings.Contains(id, "..")
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
