package knowledge

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Doc is one author-owned reference article. Knowledge is NOT canon: it is
// method/style/subject reference the author may attach to a call explicitly.
type Doc struct {
	ID       string `json:"id"`      // filename without .md
	Title    string `json:"title"`   // first H1 or id
	Summary  string `json:"summary"` // first non-heading paragraph, clipped
	Bytes    int    `json:"bytes"`
	Category string `json:"category"` // general | genre | custom
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

// List scans library/knowledge/*.md (flat, author-managed).
func List(libraryRoot string) ([]Doc, error) {
	dir := Root(libraryRoot)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []Doc{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Doc
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		raw, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		out = append(out, Doc{
			ID:       id,
			Title:    titleOf(id, string(raw)),
			Summary:  summaryOf(string(raw)),
			Bytes:    len(raw),
			Category: CategoryOf(id),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out, nil
}

// Read returns the full article content for attaching to a packet.
func Read(libraryRoot, id string) (string, error) {
	if id == "" || strings.ContainsAny(id, "/\\..") {
		return "", os.ErrInvalid
	}
	b, err := os.ReadFile(filepath.Join(Root(libraryRoot), id+".md"))
	return string(b), err
}

// Write saves an article (author edits own copy; no candidate flow needed
// because knowledge is not canon and never auto-enters context).
func Write(libraryRoot, id, content string) error {
	if id == "" || strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
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
