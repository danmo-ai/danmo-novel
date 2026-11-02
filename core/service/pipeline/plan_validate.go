package pipeline

import (
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

func outlineViolations(doc domain.StructuredMarkdown, kind string) []Violation {
	var out []Violation
	if doc.Legacy {
		return []Violation{{Path: "document", Want: "结构化 Markdown v1（含 schema_version）", Got: "legacy Markdown"}}
	}
	for _, d := range doc.Diagnostics {
		out = append(out, Violation{Path: d.Path, Want: d.Message, Got: fmt.Sprintf("第 %d 行", d.Line)})
	}
	for _, f := range doc.Root.Fields {
		if f.Name == "schema_version" || f.Name == "book_id" || f.Name == "volume_id" || f.Name == "title" || f.Name == "logline" || f.Name == "source_arc_id" || f.Name == "chapter_range" || f.Name == "goal" || f.Name == "central_conflict" || f.Name == "stakes" || f.Name == "entry_state" || f.Name == "exit_state" || f.Name == "cast" || f.Name == "deliverables" || f.Name == "open_questions" || f.Name == "next_volume_entry" {
			continue
		}
		out = append(out, Violation{Path: f.Name, Want: "已知根字段", Got: "未知字段"})
	}
	for _, f := range []string{"schema_version"} {
		if strings.TrimSpace(doc.Root.Value(f)) == "" {
			out = append(out, Violation{Path: f, Want: "非空", Got: "空"})
		}
	}
	if kind == "book" {
		for _, f := range []string{"book_id", "title", "logline"} {
			requireRoot(doc, f, &out)
		}
		if len(doc.EntitiesOf("分卷")) == 0 {
			out = append(out, Violation{Path: "volumes", Want: "至少一个分卷实体", Got: "0"})
		}
		for _, e := range doc.EntitiesOf("故事线") {
			requireFields(e, []string{"kind", "description", "opening_state", "ending_state"}, &out)
		}
	} else {
		for _, f := range []string{"volume_id", "title", "source_arc_id", "chapter_range", "goal", "central_conflict", "stakes", "entry_state", "exit_state"} {
			requireRoot(doc, f, &out)
		}
		if len(doc.EntitiesOf("单元")) == 0 {
			out = append(out, Violation{Path: "units", Want: "至少一个单元实体", Got: "0"})
		}
	}
	if kind == "volume" {
		if err := books.ValidateVolumeClockContract(doc); err != nil {
			out = append(out, Violation{Path: "clock", Want: "合法且衔接的单元时间合同", Got: err.Error()})
		}
	}
	for _, e := range doc.Entities {
		path := e.Kind + "[" + e.ID + "]"
		switch e.Kind {
		case "分卷":
			requireFields(e, []string{"title", "chapter_range", "purpose", "dramatic_question", "central_conflict", "stakes", "entry_state", "exit_state", "thread_moves", "deliverables", "open_questions", "next_volume_entry"}, &out)
		case "单元":
			requireFields(e, []string{"title", "chapter_range", "objective", "obstacle", "attempt", "setback", "choice", "cost", "outcome", "entry_cause", "exit_result", "state_changes", "clock_start_day", "clock_end_day", "clock_gap_days", "thread_ids", "next_hook"}, &out)
			from, to := books.ChapterWindow(e.Value("chapter_range"))
			if from <= 0 || to < from || to-from+1 > 10 {
				out = append(out, Violation{Path: path + ".chapter_range", Want: "有效范围且不超过 10 章", Got: e.Value("chapter_range")})
			}
			if strings.TrimSpace(e.Value("state_changes")) == "" {
				out = append(out, Violation{Path: path + ".state_changes", Want: "至少一个实际状态迁移", Got: "空"})
			}
		case "转折", "节拍":
			requireFields(e, []string{"event", "cause", "effect", "chapter_window"}, &out)
			from, to := books.ChapterWindow(e.Value("chapter_window"))
			if from <= 0 || to < from {
				out = append(out, Violation{Path: path + ".chapter_window", Want: "有效章范围", Got: e.Value("chapter_window")})
			}
		case "承诺":
			requireFields(e, []string{"description"}, &out)
		case "问题":
			requireFields(e, []string{"entity_path", "severity", "lens", "evidence", "suggestion"}, &out)
			if e.Value("severity") != "blocking" && e.Value("severity") != "advisory" {
				out = append(out, Violation{Path: path + ".severity", Want: "blocking 或 advisory", Got: e.Value("severity")})
			}
		}
	}
	return out
}

func requireRoot(doc domain.StructuredMarkdown, key string, out *[]Violation) {
	if strings.TrimSpace(doc.Root.Value(key)) == "" {
		*out = append(*out, Violation{Path: key, Want: "非空", Got: "空"})
	}
}

func requireFields(e domain.MarkdownEntity, keys []string, out *[]Violation) {
	for _, key := range keys {
		if strings.TrimSpace(e.Value(key)) == "" {
			*out = append(*out, Violation{Path: e.Kind + "[" + e.ID + "]." + key, Want: "非空", Got: "空"})
		}
	}
}

func validateBookOutline(raw string) (domain.StructuredMarkdown, error) {
	doc := books.ParseStructuredMarkdown(raw, books.BookOutlineProtocol)
	return doc, contractFail("总纲", outlineViolations(doc, "book"))
}

func validateVolumeOutline(raw string) (domain.StructuredMarkdown, error) {
	doc := books.ParseStructuredMarkdown(raw, books.VolumeOutlineProtocol)
	return doc, contractFail("卷纲", outlineViolations(doc, "volume"))
}

// validateVolumeInheritance enforces the fields that the parent has locked.
// A volume may add detail but may not substitute another story under the same ID.
func validateVolumeInheritance(bookDoc, volumeDoc domain.StructuredMarkdown, volumeID string) error {
	arc, ok := bookDoc.Entity("分卷", volumeID)
	if !ok {
		return contractFail("卷纲", []Violation{{Path: "source_arc_id", Want: "总纲已有分卷 " + volumeID, Got: volumeDoc.Root.Value("source_arc_id")}})
	}
	var vs []Violation
	// goal 与 dramatic_question 不是同一字段：前者是本卷要完成的写作目标，
	// 后者是要回答的读者悬念。绑在一起会强迫模型把目标写成问句。
	for _, pair := range [][2]string{{"chapter_range", "chapter_range"}, {"entry_state", "entry_state"}, {"exit_state", "exit_state"}} {
		parent, child := strings.TrimSpace(arc.Value(pair[0])), strings.TrimSpace(volumeDoc.Root.Value(pair[1]))
		if child != parent {
			vs = append(vs, Violation{Path: pair[1], Want: "继承总纲：" + parent, Got: child})
		}
	}
	if got := strings.TrimSpace(volumeDoc.Root.Value("source_arc_id")); got != arc.Value("arc_id") && got != volumeID {
		vs = append(vs, Violation{Path: "source_arc_id", Want: arc.Value("arc_id"), Got: got})
	}
	unitCoverage := map[string]bool{}
	for _, u := range volumeDoc.EntitiesOf("单元") {
		unitCoverage[u.ID] = true
	}
	for _, e := range arc.Fields {
		if e.Name == "deliverables" && strings.TrimSpace(e.Value) == "" {
			vs = append(vs, Violation{Path: "分卷[" + volumeID + "].deliverables", Want: "至少一个必须兑现结果", Got: "空"})
		}
	}
	if len(unitCoverage) == 0 {
		vs = append(vs, Violation{Path: "units", Want: "完整承接本卷的单元种子", Got: "空"})
	}
	return contractFail("卷纲父子合同", vs)
}
