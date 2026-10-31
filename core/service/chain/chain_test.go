package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	"github.com/danmo-ai/danmo-novel/core/resource/novel"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/llmconfig"
	"github.com/danmo-ai/danmo-novel/core/service/runs"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
)

// fakeLLM answers by task kind so a chain (or a resumed chain) can run any
// number of times without hand-queued steps. It records every request so tests
// can assert which instruction / knowledge reached which stage.
type fakeLLM struct {
	mu       sync.Mutex
	blocking bool
	requests []port.LLMChatRequest
}

func (f *fakeLLM) setBlocking(v bool) {
	f.mu.Lock()
	f.blocking = v
	f.mu.Unlock()
}

func (f *fakeLLM) captured() []port.LLMChatRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]port.LLMChatRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

func proseBody() string {
	return strings.Repeat("他握紧刀柄，没有退缩，只把呼吸压到最慢，等那道石门彻底打开。", 60)
}

func (f *fakeLLM) Chat(_ context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	blocking := f.blocking
	f.mu.Unlock()
	if len(req.Messages) < 2 {
		return port.LLMChatResponse{}, fmt.Errorf("expected system+user messages")
	}
	payload, err := contextsvc.DecodeUserPayload(req.Messages[len(req.Messages)-1].Content)
	if err != nil {
		return port.LLMChatResponse{}, fmt.Errorf("decode payload: %w", err)
	}
	switch domain.TaskKind(payload.Task) {
	case domain.TaskOutline:
		out, _ := json.Marshal(map[string]any{"units": []map[string]any{{
			"unitId": "v01-U1", "status": "accepted", "title": "入门试炼",
			"function": "觉醒与试炼入门", "desire": "活着通过试炼",
			"obstacle": "体弱与名额争夺", "choice": "用代价换一次爆发",
			"payoff": "入门资格", "pleasure": "弱者翻盘", "forbidden": "不揭金手指来源",
			"wordTarget": "2500", "pov": "linchen",
			"chapterFrom": 1, "chapterTo": 3, "onStage": []string{"linchen"},
			"scenes":   []map[string]any{{"id": "S1", "chapter": 1, "beat": "被点名", "who": []string{"linchen"}, "pov": "linchen", "where": "试炼场"}},
			"storyDay": "1", "gapFromPrev": "开场",
		}}})
		return port.LLMChatResponse{Content: string(out), Done: true}, nil
	case domain.TaskWrite:
		ch := 1
		if v, ok := payload.Extra["chapter"].(float64); ok && v > 0 {
			ch = int(v)
		}
		out, _ := json.Marshal(map[string]any{"chapters": []map[string]any{{
			"chapter": ch, "title": fmt.Sprintf("第%d章", ch), "body": proseBody(),
		}}})
		return port.LLMChatResponse{Content: string(out), Done: true}, nil
	case domain.TaskReview:
		findings := []map[string]any{}
		if blocking {
			findings = append(findings, map[string]any{
				"id": "ai-continuity", "severity": "blocking", "source": "ai_review",
				"title": "连续性错误", "evidence": "第 2 章位置跳变", "chapter": 2,
				"suggestion": "补一句移动过程",
			})
		}
		out, _ := json.Marshal(map[string]any{"verdict": "pass", "findings": findings})
		return port.LLMChatResponse{Content: string(out), Done: true}, nil
	}
	return port.LLMChatResponse{}, fmt.Errorf("unexpected task %q", payload.Task)
}

type harness struct {
	svc  *Service
	runs *runs.Service
	llm  *fakeLLM
	root string
	book string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("NOVEL_HOME", dir)
	settings, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	_ = settings.Update(func(s *config.Settings) {
		s.ModelAssignments = []domain.ModelAssignment{
			{Role: domain.StagePlan, ModelID: "mock/model"},
			{Role: domain.StageOutline, ModelID: "mock/model"},
			{Role: domain.StageWrite, ModelID: "mock/model"},
			{Role: domain.StageReview, ModelID: "mock/model"},
			{Role: domain.StageLocalEdit, ModelID: "mock/model"},
		}
	})
	db, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	root := filepath.Join(dir, "book")
	copyDir(t, filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan"), root)
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertBook(context.Background(), snap.Summary.ID, root, snap.Summary.Title, snap.Revision, false); err != nil {
		t.Fatal(err)
	}
	models := llmconfig.NewModelRegistry()
	fake := &fakeLLM{}
	runSvc := runs.New(db, settings, fake)
	runSvc.Models = models
	return &harness{
		svc: New(db, settings, runSvc), runs: runSvc, llm: fake,
		root: root, book: snap.Summary.ID,
	}
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (h *harness) start(t *testing.T, spec domain.ChainSpec) domain.Chain {
	t.Helper()
	spec.BookID = h.book
	if spec.VolumeID == "" {
		spec.VolumeID = "v01"
	}
	if spec.FromUnitID == "" {
		spec.FromUnitID = "v01-U1"
	}
	ch, err := h.svc.Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return h.wait(t, ch.ID)
}

func (h *harness) wait(t *testing.T, id string) domain.Chain {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last domain.Chain
	for time.Now().Before(deadline) {
		ch, err := h.svc.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("get chain: %v", err)
		}
		last = ch
		if ch.Status != domain.ChainPending && ch.Status != domain.ChainRunning {
			return ch
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("chain did not settle in time: %+v", last)
	return domain.Chain{}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func TestChainRunsWholeUnit(t *testing.T) {
	h := newHarness(t)
	ch := h.start(t, domain.ChainSpec{
		Outline: domain.ChainStageConfig{Instruction: "细纲追加要求X"},
		Write:   domain.ChainStageConfig{Instruction: "正文追加要求Y"},
	})
	if ch.Status != domain.ChainCompleted {
		t.Fatalf("status=%s err=%s", ch.Status, ch.Error)
	}
	if done, total := ch.Progress(); done != 1 || total != 1 {
		t.Fatalf("progress=%d/%d", done, total)
	}

	prose := readFile(t, filepath.Join(h.root, "units", "v01-U1.md"))
	for _, want := range []string{"## 第1章", "## 第2章", "## 第3章"} {
		if !strings.Contains(prose, want) {
			t.Fatalf("prose missing %q", want)
		}
	}
	yaml := readFile(t, filepath.Join(h.root, "outline", "units", "v01-U1.yaml"))
	if !strings.Contains(yaml, "status: reviewed") {
		t.Fatalf("unit status not reviewed:\n%s", yaml)
	}
	if _, err := os.Stat(filepath.Join(h.root, "continuity", "commits", "v01-U1.md")); err != nil {
		t.Fatalf("commit log missing: %v", err)
	}
	summary := readFile(t, filepath.Join(h.root, "continuity", "summaries", "v01.md"))
	if n := strings.Count(summary, "\n| 1 |"); n != 1 {
		t.Fatalf("summary rows for ch1 = %d\n%s", n, summary)
	}
	// 新写单元不该产生备份。
	if matches, _ := filepath.Glob(filepath.Join(h.root, "backups", "*-chain", "units", "v01-U1.md")); len(matches) != 0 {
		t.Fatalf("unexpected backup for new prose: %v", matches)
	}

	// 链不自建 prompt：每个阶段的系统提示词必须与手工点击同 kind 完全一致。
	for _, req := range h.llm.captured() {
		payload, err := contextsvc.DecodeUserPayload(req.Messages[len(req.Messages)-1].Content)
		if err != nil {
			t.Fatal(err)
		}
		var want string
		switch domain.TaskKind(payload.Task) {
		case domain.TaskOutline:
			want = novel.OutlineSystemPrompt()
		case domain.TaskWrite:
			want = novel.WriteSystemPrompt()
		default:
			continue
		}
		if req.Messages[0].Content != want {
			t.Fatalf("%s system prompt diverged from the manual builder", payload.Task)
		}
		if len(req.Tools) != 0 {
			t.Fatal("tools must stay empty")
		}
	}

	// 分阶段指令只进入各自阶段。
	var sawOutline, sawWrite bool
	for _, req := range h.llm.captured() {
		payload, err := contextsvc.DecodeUserPayload(req.Messages[len(req.Messages)-1].Content)
		if err != nil {
			t.Fatal(err)
		}
		switch domain.TaskKind(payload.Task) {
		case domain.TaskOutline:
			if !strings.Contains(payload.Instruction, "细纲追加要求X") {
				t.Fatalf("outline instruction=%q", payload.Instruction)
			}
			sawOutline = true
		case domain.TaskWrite:
			if !strings.Contains(payload.Instruction, "正文追加要求Y") {
				t.Fatalf("write instruction=%q", payload.Instruction)
			}
			if strings.Contains(payload.Instruction, "细纲追加要求X") {
				t.Fatal("outline instruction leaked into write")
			}
			sawWrite = true
		case domain.TaskReview:
			if strings.Contains(payload.Instruction, "追加要求") {
				t.Fatalf("review must not carry stage instructions: %q", payload.Instruction)
			}
		}
	}
	if !sawOutline || !sawWrite {
		t.Fatalf("stages not exercised: outline=%v write=%v", sawOutline, sawWrite)
	}
}

func TestChainStopsOnBlockingReview(t *testing.T) {
	h := newHarness(t)
	h.llm.setBlocking(true)
	ch := h.start(t, domain.ChainSpec{})
	if ch.Status != domain.ChainWaitingManual {
		t.Fatalf("status=%s", ch.Status)
	}
	if !strings.Contains(ch.Error, "连续性错误") {
		t.Fatalf("error=%q", ch.Error)
	}
	if ch.Cursor != 0 || ch.Units[0].Status != domain.ChainUnitBlocked {
		t.Fatalf("unit state=%+v", ch.Units[0])
	}
	// 正文已落盘，审校候选保持 open 供人工处理。
	if _, err := os.Stat(filepath.Join(h.root, "units", "v01-U1.md")); err != nil {
		t.Fatalf("prose should be accepted before review failure: %v", err)
	}
	open, err := h.svc.Store.ListOpenCandidates(context.Background(), h.book)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].Kind != domain.TaskReview {
		t.Fatalf("open candidates=%+v", open)
	}

	// 还有待审候选时恢复必须被拒绝。
	if _, err := h.svc.Resume(context.Background(), ch.ID); err == nil || !strings.Contains(err.Error(), "待审候选") {
		t.Fatalf("resume should be refused, got %v", err)
	}

	// 人工接受审校候选后，恢复从审校继续并跑完整个单元。
	snap, err := books.LoadBook(h.root, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.runs.Accept(context.Background(), domain.AcceptCommand{CandidateID: open[0].ID, BaseRevision: snap.Revision}); err != nil {
		t.Fatalf("manual accept: %v", err)
	}
	h.llm.setBlocking(false)
	if _, err := h.svc.Resume(context.Background(), ch.ID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	final := h.wait(t, ch.ID)
	if final.Status != domain.ChainCompleted {
		t.Fatalf("status=%s err=%s", final.Status, final.Error)
	}
}

func TestChainRewriteIsIdempotentAndBacksUp(t *testing.T) {
	h := newHarness(t)
	if ch := h.start(t, domain.ChainSpec{}); ch.Status != domain.ChainCompleted {
		t.Fatalf("first run: %s %s", ch.Status, ch.Error)
	}
	if ch := h.start(t, domain.ChainSpec{}); ch.Status != domain.ChainCompleted {
		t.Fatalf("rewrite: %s %s", ch.Status, ch.Error)
	}
	summary := readFile(t, filepath.Join(h.root, "continuity", "summaries", "v01.md"))
	for _, n := range []string{"| 1 |", "| 2 |", "| 3 |"} {
		if got := strings.Count(summary, "\n"+n); got != 1 {
			t.Fatalf("summary rows %q = %d\n%s", n, got, summary)
		}
	}
	matches, _ := filepath.Glob(filepath.Join(h.root, "backups", "*-chain", "units", "v01-U1.md"))
	if len(matches) == 0 {
		t.Fatal("rewrite must back up the previous prose")
	}
	body := readFile(t, matches[0])
	if !strings.Contains(body, "## 第1章") {
		t.Fatalf("backup content looks wrong: %q", body[:60])
	}
}

func TestChainRejectsMissingPreconditions(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.Start(context.Background(), domain.ChainSpec{
		BookID: h.book, VolumeID: "v01", FromUnitID: "v01-U9",
	}); err == nil || !strings.Contains(err.Error(), "起始单元不存在") {
		t.Fatalf("expected missing unit error, got %v", err)
	}
	if _, err := h.svc.Start(context.Background(), domain.ChainSpec{
		BookID: h.book, VolumeID: "v02", FromUnitID: "v01-U1",
	}); err == nil {
		t.Fatal("expected volume/precondition error")
	}
}

// 后端重启会丢掉链的执行 goroutine：启动收尾应把 running/pending 链转到
// waiting_manual（可 Resume），并让当前单元的 running 标记不再悬空。
func TestRecoverInterruptedChainBecomesResumable(t *testing.T) {
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
	defer db.Close()
	svc := New(db, settings, runs.New(db, settings, &fakeLLM{}))

	now := time.Now().UTC()
	ch := domain.Chain{
		ID: "chain-orphan", BookID: "b-origin", Volume: 1,
		Status: domain.ChainRunning, Stage: domain.ChainStageWrite,
		CreatedAt: now, UpdatedAt: now,
		Units: []domain.ChainUnitState{{
			UnitID: "v01-U1", Stage: domain.ChainStageWrite,
			Status: domain.ChainUnitRunning,
		}},
	}
	if err := db.SaveChain(context.Background(), ch); err != nil {
		t.Fatal(err)
	}

	n, err := svc.RecoverInterrupted(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("recovered=%d, want 1", n)
	}
	got, err := db.GetChain(context.Background(), "chain-orphan")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.ChainWaitingManual {
		t.Fatalf("status=%s, want waiting_manual", got.Status)
	}
	if got.Error == "" {
		t.Fatal("recovered chain should explain the restart")
	}
	if got.Units[0].Status != domain.ChainUnitBlocked {
		t.Fatalf("unit status=%s, want blocked", got.Units[0].Status)
	}
	// waiting_manual 是 Resume 允许的状态。
	if got.Status != domain.ChainWaitingManual {
		t.Fatalf("chain cannot be resumed from %s", got.Status)
	}
}
