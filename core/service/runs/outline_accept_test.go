package runs

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/adapter/llm"
	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
	"gopkg.in/yaml.v3"
)

// outlineAcceptFixture 建一本最小书 + 一条待接受的细纲候选。
func outlineAcceptFixture(t *testing.T, cand domain.Candidate) (*Service, string, func()) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("NOVEL_HOME", dir)
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := books.CreateBookFromDraft(dir, books.DraftInput{BookID: "outline-book", Title: "雪夜", Genre: "悬疑"})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertBook(context.Background(), snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}
	cand.BookID = snap.Summary.ID
	cand.BaseRevision = snap.Revision
	if err := db.SaveCandidate(context.Background(), cand); err != nil {
		t.Fatal(err)
	}
	return New(db, settings, llm.NewMock()), root, func() { db.Close() }
}

// 生成期硬门的阻断结论必须拦住接受：带 blocking 的细纲不能靠点「接受」落盘。
func TestAcceptOutlineRefusesBlockingFindings(t *testing.T) {
	svc, _, done := outlineAcceptFixture(t, domain.Candidate{
		ID: "outline-blocked", Kind: domain.TaskOutline, Status: domain.CandidateOpen,
		VolumeID: "v01",
		Findings: []domain.ReviewFinding{{ID: "outline-pov-stage", Severity: "blocking", Source: "local_gate", Title: "pov 必须属于 on_stage"}},
		UnitOutlines: []domain.UnitOutlineDraft{{
			UnitID: "v01-U1", ChapterFrom: 1, ChapterTo: 3,
			StateDeltas: []any{map[string]any{"stem": "lin", "field": "goal", "from": "a", "to": "b"}},
		}},
	})
	defer done()

	_, err := svc.Accept(context.Background(), domain.AcceptCommand{CandidateID: "outline-blocked"})
	if err == nil || !strings.Contains(err.Error(), "细纲仍有阻断问题") {
		t.Fatalf("blocking findings must block accept: %v", err)
	}
}

// 卷号不能靠「单元是否已在盘上」反推：反推失败会 vol=0 并静默跳过父子合同。
func TestAcceptOutlineRequiresVolumeID(t *testing.T) {
	svc, _, done := outlineAcceptFixture(t, domain.Candidate{
		ID: "outline-no-vol", Kind: domain.TaskOutline, Status: domain.CandidateOpen,
		UnitOutlines: []domain.UnitOutlineDraft{{
			UnitID: "v01-U1", ChapterFrom: 1, ChapterTo: 3,
			StateDeltas: []any{map[string]any{"stem": "lin", "field": "goal", "from": "a", "to": "b"}},
		}},
	})
	defer done()

	_, err := svc.Accept(context.Background(), domain.AcceptCommand{CandidateID: "outline-no-vol"})
	if err == nil || !strings.Contains(err.Error(), "无法确定细纲所属卷") {
		t.Fatalf("missing volumeId must refuse accept instead of skipping inheritance: %v", err)
	}
}

func TestVolumeFromTag(t *testing.T) {
	cases := map[string]int{"v01": 1, "v10": 10, "1": 1, "10": 10, "": 0, "vv": 0, "v0": 0}
	for tag, want := range cases {
		if got := volumeFromTag(tag); got != want {
			t.Fatalf("volumeFromTag(%q)=%d want %d", tag, got, want)
		}
	}
}

// 多场面单元的细纲落盘必须是合法 YAML：以前每个场面都写进同一个 mapping，
// 重复键让文件完全无法被严格解析（真实数据里有 8 个单元文件已经坏了）。
func TestSerializeUnitYAMLWritesScenesAsSequence(t *testing.T) {
	raw := serializeUnitYAML(domain.UnitOutlineDraft{
		UnitID: "v01-U1", Title: "开局", ChapterFrom: 1, ChapterTo: 2,
		StoryDay: "1", EndDay: 5,
		Scenes: []map[string]any{
			{"chapter": 1, "goal": "甲", "conflict": "乙", "turn": "丙", "outcome": "丁"},
			{"chapter": 2, "goal": "戊", "conflict": "己", "turn": "庚", "outcome": "辛"},
		},
	})
	var doc struct {
		Scenes []map[string]any `yaml:"scenes"`
	}
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("serialized unit must be valid YAML: %v\n%s", err, raw)
	}
	if len(doc.Scenes) != 2 {
		t.Fatalf("scenes=%d want 2:\n%s", len(doc.Scenes), raw)
	}
	if doc.Scenes[1]["goal"] != "戊" {
		t.Fatalf("second scene lost: %+v", doc.Scenes)
	}
}

// 模型在候选 JSON 里塞 rawYaml 曾经能直接决定落盘内容，而所有校验读的是
// 结构体字段——验的和写的可以不是同一份文本。字段已删除，这里锁住回归。
func TestSerializeUnitYAMLIgnoresRawYAMLInJSON(t *testing.T) {
	var u domain.UnitOutlineDraft
	if err := json.Unmarshal([]byte(`{"unitId":"v01-U1","title":"起步","rawYaml":"unit_id: HACK\nstatus: accepted\n"}`), &u); err != nil {
		t.Fatal(err)
	}
	raw := serializeUnitYAML(u)
	if strings.Contains(raw, "HACK") {
		t.Fatalf("rawYaml must not reach disk:\n%s", raw)
	}
	encoded, _ := json.Marshal(u)
	if strings.Contains(string(encoded), "rawYaml") {
		t.Fatalf("candidate must not advertise a rawYaml field: %s", encoded)
	}
}

// 接受前的时钟校验必须看「将要落盘的内容」。以前传的是 accept 前的 snap：
// 首次生成时单元还不在盘上，重写时读的是被替换掉的旧 story_day。
func TestClockUnitsWithCandidateUsesCandidateStoryDay(t *testing.T) {
	snap := domain.BookSnapshot{Units: []domain.UnitInfo{
		{UnitID: "v01-U1", Volume: 1, Index: 1, OutlineRaw: "story_day: 1\n"},
		{UnitID: "v01-U2", Volume: 1, Index: 2, OutlineRaw: "story_day: 50\n"},
	}}
	// 候选把 U2 的故事日改到 U1 之前。
	outlines := []domain.UnitOutlineDraft{{
		UnitID: "v01-U2", ChapterFrom: 4, ChapterTo: 6,
		StoryDay: "0", EndDay: 3, GapDays: 0,
	}}

	// 旧快照看不出问题（盘上还是 50）——这正是以前的盲区。
	if len(books.ValidateClockContinuity(snap.Units, 1)) != 0 {
		t.Fatal("sanity: stale snapshot should look clean")
	}
	found := false
	for _, f := range books.ValidateClockContinuity(clockUnitsWithCandidate(snap, 1, outlines), 1) {
		if f.Severity == "blocking" {
			found = true
		}
	}
	if !found {
		t.Fatal("candidate story_day that precedes the previous unit must be blocking")
	}
}

// 新单元还没落盘时也要参与排序校验。
func TestClockUnitsWithCandidateAppendsNewUnit(t *testing.T) {
	snap := domain.BookSnapshot{Units: []domain.UnitInfo{
		{UnitID: "v01-U1", Volume: 1, Index: 1, OutlineRaw: "story_day: 10\n"},
	}}
	outlines := []domain.UnitOutlineDraft{{UnitID: "v01-U2", StoryDay: "3", EndDay: 5}}
	merged := clockUnitsWithCandidate(snap, 1, outlines)
	if len(merged) != 2 {
		t.Fatalf("merged units=%d want 2: %+v", len(merged), merged)
	}
	if merged[1].UnitID != "v01-U2" || merged[1].Volume != 1 || merged[1].Index != 2 {
		t.Fatalf("new unit not placed correctly: %+v", merged[1])
	}
	blocking := false
	for _, f := range books.ValidateClockContinuity(merged, 1) {
		if f.Severity == "blocking" {
			blocking = true
		}
	}
	if !blocking {
		t.Fatal("new unit with earlier story_day must be blocking")
	}
}
