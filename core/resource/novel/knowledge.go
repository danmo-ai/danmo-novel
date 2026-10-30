package novel

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

// These coordinates identify the upstream import. The documents are adapted
// for Danmo Novel in this repository; a release does not need Danmo Work or
// any files on the author's machine to provide its built-in knowledge base.
const (
	KnowledgeSourceRepository = "danmo-ai/danmo-work"
	KnowledgeSourceCommit     = "5d3e8283530fd396e3e74c04b9ba33016a0d579b"
)

// KnowledgeDoc is an immutable document shipped in the application binary.
type KnowledgeDoc struct {
	ID      string
	Content string
}

//go:embed knowledge/*.md
var knowledgeFiles embed.FS

// BuiltinKnowledge returns all built-in articles in stable filename order.
func BuiltinKnowledge() []KnowledgeDoc {
	entries, err := fs.ReadDir(knowledgeFiles, "knowledge")
	if err != nil {
		panic(err)
	}
	docs := make([]KnowledgeDoc, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		raw, err := knowledgeFiles.ReadFile("knowledge/" + entry.Name())
		if err != nil {
			panic(err)
		}
		docs = append(docs, KnowledgeDoc{
			ID:      strings.TrimSuffix(entry.Name(), ".md"),
			Content: string(raw),
		})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
	return docs
}

// BuiltinKnowledgeDocument looks up a built-in article without touching the
// author's library. Local files, when present, are overlays of these contents.
func BuiltinKnowledgeDocument(id string) (string, bool) {
	if id == "" || strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return "", false
	}
	raw, err := knowledgeFiles.ReadFile("knowledge/" + id + ".md")
	if err != nil {
		return "", false
	}
	return string(raw), true
}
