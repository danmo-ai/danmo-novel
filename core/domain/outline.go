package domain

// SourceRange identifies a field in its Markdown source. It lets the UI edit a
// single field without turning the whole document into a second data format.
type SourceRange struct {
	Line       int `json:"line"`
	Column     int `json:"column"`
	EndLine    int `json:"endLine"`
	EndColumn  int `json:"endColumn"`
	ByteOffset int `json:"byteOffset"`
	ByteEnd    int `json:"byteEnd"`
}

type OutlineDiagnostic struct {
	Path     string `json:"path"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
	Line     int    `json:"line,omitempty"`
}

type MarkdownField struct {
	Name  string      `json:"name"`
	Value string      `json:"value"`
	Range SourceRange `json:"range"`
}

type MarkdownEntity struct {
	Kind   string          `json:"kind"`
	ID     string          `json:"id"`
	Fields []MarkdownField `json:"fields"`
	Range  SourceRange     `json:"range"`
}

func (e MarkdownEntity) Field(name string) (MarkdownField, bool) {
	for _, f := range e.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return MarkdownField{}, false
}

func (e MarkdownEntity) Value(name string) string {
	f, _ := e.Field(name)
	return f.Value
}

// StructuredMarkdown is an in-memory projection. Markdown remains the only
// persisted source; this object is never written back as a parallel document.
type StructuredMarkdown struct {
	Protocol    string              `json:"protocol"`
	Version     int                 `json:"version"`
	Title       string              `json:"title"`
	Root        MarkdownEntity      `json:"root"`
	Entities    []MarkdownEntity    `json:"entities"`
	Diagnostics []OutlineDiagnostic `json:"diagnostics,omitempty"`
	Legacy      bool                `json:"legacy"`
}

func (d StructuredMarkdown) Valid() bool {
	return !d.Legacy && len(d.Diagnostics) == 0
}

func (d StructuredMarkdown) Entity(kind, id string) (MarkdownEntity, bool) {
	for _, e := range d.Entities {
		if e.Kind == kind && e.ID == id {
			return e, true
		}
	}
	return MarkdownEntity{}, false
}

func (d StructuredMarkdown) EntitiesOf(kind string) []MarkdownEntity {
	out := []MarkdownEntity{}
	for _, e := range d.Entities {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}
