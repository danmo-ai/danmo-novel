package knowledge

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/resource/novel"
)

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
			customized = raw != article.Content
			delete(local, article.ID)
		}
		out = append(out, newDoc(article.ID, raw, true, customized))
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
	return Doc{
		ID: id, Title: titleOf(id, raw), Summary: summaryOf(raw), Bytes: len(raw),
		Category: CategoryOf(id), Builtin: builtin, Customized: customized,
	}
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
// exists. Built-ins therefore work with an empty or read-only library root.
func Read(libraryRoot, id string) (string, error) {
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

// Restore removes only the local overlay, exposing the built-in article again.
func Restore(libraryRoot, id string) (string, error) {
	builtin, ok := novel.BuiltinKnowledgeDocument(id)
	if !ok {
		return "", os.ErrNotExist
	}
	if err := os.Remove(filepath.Join(Root(libraryRoot), id+".md")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return builtin, nil
}

func validID(id string) bool {
	return id != "" && !strings.ContainsAny(id, "/\\") && !strings.Contains(id, "..")
}

// Write saves an article (author edits own copy; no candidate flow needed
// because knowledge is not canon and never auto-enters context).
func Write(libraryRoot, id, content string) error {
	if !validID(id) {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(Root(libraryRoot), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(Root(libraryRoot), id+".md"), []byte(content), 0o644)
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
