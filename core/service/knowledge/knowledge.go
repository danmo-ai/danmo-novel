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
		if len(doc.Stages) == 0 {
			doc.Stages = stagesOf(article.Content)
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
	return Doc{
		ID: id, Title: titleOf(id, body), Summary: summaryOf(body), Bytes: len(body),
		Category: CategoryOf(id), Builtin: builtin, Customized: customized,
		Stages: stagesOf(raw),
	}
}

// frontMatter is the optional YAML header of an article:
//
//	---
//	stages: [plan, outline]
//	---
//
// It carries applicability metadata only; the body is what the author edits and
// what an LLM call receives.
type frontMatter struct {
	Stages []string `yaml:"stages"`
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

func stagesOf(raw string) []string {
	fm, _ := splitFrontMatter(raw)
	return NormalizeStages(fm.Stages)
}

func bodyOf(raw string) string {
	_, body := splitFrontMatter(raw)
	return body
}

// compose writes the front matter back in front of the body. Empty stages omit
// the header entirely, so untagged articles stay plain Markdown.
func compose(body string, stages []string) string {
	if len(stages) == 0 {
		return body
	}
	return "---\nstages: [" + strings.Join(stages, ", ") + "]\n---\n\n" + strings.TrimLeft(body, "\n")
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

// StagesOf returns the effective stage tags: the overlay's when it declares
// any, otherwise the built-in article's.
func StagesOf(libraryRoot, id string) ([]string, error) {
	raw, err := readRaw(libraryRoot, id)
	if err != nil {
		return nil, err
	}
	if stages := stagesOf(raw); len(stages) > 0 {
		return stages, nil
	}
	if builtin, ok := novel.BuiltinKnowledgeDocument(id); ok {
		return stagesOf(builtin), nil
	}
	return nil, nil
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
// because knowledge is not canon and never auto-enters context). A nil stages
// keeps the document's current effective tags; a non-nil slice replaces them.
func Write(libraryRoot, id, content string, stages []string) error {
	if !validID(id) {
		return os.ErrInvalid
	}
	body := bodyOf(content)
	tags := stages
	if tags == nil {
		current, err := StagesOf(libraryRoot, id)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		tags = current
	}
	if err := os.MkdirAll(Root(libraryRoot), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(Root(libraryRoot), id+".md"), []byte(compose(body, NormalizeStages(tags))), 0o644)
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
