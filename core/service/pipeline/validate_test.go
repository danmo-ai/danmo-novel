package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
)

func TestValidateChaptersRejectsGapsAndEmptyBody(t *testing.T) {
	chapters := []domain.ChapterDraft{
		{Chapter: 1, Body: "正文一"},
		{Chapter: 3, Body: "   "},
		{Chapter: 3, Body: "重复章号"},
	}
	err := validateChapters("正文", chapters, 1, 3)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"缺少第 2 章",             // 漏章：静默通过会让摘要与账本游标错位
		"第 3 章重复出现",           // 章号重复
		"正文为空",                // 空章体会被直接写进文件
		"chapters[2].chapter", // 路径要精确到下标，模型才改得动
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error missing %q:\n%s", want, msg)
		}
	}
}

func TestValidateChaptersRejectsOutOfRange(t *testing.T) {
	chapters := []domain.ChapterDraft{
		{Chapter: 1, Body: "a"},
		{Chapter: 2, Body: "b"},
		{Chapter: 7, Body: "c"},
	}
	err := validateChapters("正文", chapters, 1, 2)
	if err == nil || !strings.Contains(err.Error(), "多出第 7 章") {
		t.Fatalf("want out-of-range error, got %v", err)
	}
}

func TestValidateReviewFindingsRejectsUnknownSeverity(t *testing.T) {
	findings := []domain.ReviewFinding{
		{ID: "a-1-1", Severity: "fatal", Chapter: 1, Title: "主角动机断裂", Evidence: "上一章还拒绝，这章主动"},
		{ID: "", Severity: "advisory", Chapter: 1, Title: "", Evidence: ""},
	}
	err := validateReviewFindings(findings, 1)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	msg := err.Error()
	// severity 是下游唯一开关：别名（fatal/high）若被静默当 advisory，
	// 一条真硬伤就被藏起来了，所以必须报错让模型按枚举重写。
	for _, want := range []string{
		"findings[0].severity",
		"blocking 或 advisory",
		"fatal",
		"findings[1].id",
		"findings[1].title",
		"findings[1].evidence",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error missing %q:\n%s", want, msg)
		}
	}
}

// id 重复会让"勾一条"变成"勾多条"：前端按 id 去重显示已选数量，修订又按 id
// 取问题，于是一条勾选会把同 id 的两条意见一起送去改。
func TestValidateReviewFindingsRejectsDuplicateIDs(t *testing.T) {
	findings := []domain.ReviewFinding{
		{ID: "style-1-1", Severity: "advisory", Chapter: 1, Title: "比喻重复", Evidence: "e1"},
		{ID: "style-1-1", Severity: "blocking", Chapter: 1, Title: "另一条问题", Evidence: "e2"},
	}
	err := validateReviewFindings(findings, 1)
	if err == nil {
		t.Fatal("duplicate id must be rejected")
	}
	msg := err.Error()
	if !strings.Contains(msg, "findings[1].id") || !strings.Contains(msg, "重复") {
		t.Fatalf("error should point at the duplicate id: %s", msg)
	}
}

// 章号必填在两条路径上都要成立：整单元审校以前允许 chapter=0，结果那条意见
// 到了「自动修复」这一步才炸（RunRevise 找不到落点），而且一条就废掉整批。
func TestValidateReviewFindingsRequiresChapter(t *testing.T) {
	findings := []domain.ReviewFinding{
		{ID: "structure-1", Severity: "blocking", Title: "全单元节奏拖沓", Evidence: "每章都在原地打转"},
	}
	// wantChapter=0 就是整单元审校的调用方式。
	err := validateReviewFindings(findings, 0)
	if err == nil {
		t.Fatal("findings without a chapter must be rejected even in whole-unit review")
	}
	msg := err.Error()
	if !strings.Contains(msg, "findings[0].chapter") || !strings.Contains(msg, "正整数章号") {
		t.Fatalf("error should demand a chapter: %s", msg)
	}
}

func TestValidateReviewFindingsRejectsWrongChapter(t *testing.T) {
	findings := []domain.ReviewFinding{
		{ID: "a-2-1", Severity: "advisory", Chapter: 2, Title: "t", Evidence: "e"},
	}
	err := validateReviewFindings(findings, 1)
	if err == nil || !strings.Contains(err.Error(), "本次只审这一章") {
		t.Fatalf("want chapter mismatch error, got %v", err)
	}
}

func TestValidateVerdict(t *testing.T) {
	for _, ok := range []string{"pass", "revise", " REVISE "} {
		if err := validateVerdict(ok); err != nil {
			t.Fatalf("%q should pass: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "maybe", "fail"} {
		if err := validateVerdict(bad); err == nil {
			t.Fatalf("%q should fail", bad)
		}
	}
}

func TestValidateOutlineScenesRejectsOutOfRangeAndGap(t *testing.T) {
	units := []domain.UnitOutlineDraft{{
		UnitID: "v01-U1", ChapterFrom: 1, ChapterTo: 3,
		Scenes: []map[string]any{
			{"chapter": 1, "goal": "a"},
			{"chapter": 5, "goal": "越界"},
		},
	}}
	err := validateOutlineScenes(units)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	msg := err.Error()
	// 越界的 scene 会让「本章场面节拍」过滤失效，那一章写作时无节拍可用。
	for _, want := range []string{"units[0].scenes[1].chapter", "缺第 2、3 章的 scene"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error missing %q:\n%s", want, msg)
		}
	}
}

func TestValidatePlanCast(t *testing.T) {
	rows := []map[string]any{
		{"stem": "lin-yuan", "name": "林渊", "role": "protagonist", "status": "candidate", "cardMarkdown": "# 林渊\n\n主角。"},
		{"stem": "Bad Stem", "name": "", "role": "npc", "status": "canon", "cardMarkdown": "林渊"},
	}
	err := validatePlanCast(rows)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"cast[1].stem", "cast[1].name", "cast[1].role", "cast[1].cardMarkdown"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error missing %q:\n%s", want, msg)
		}
	}
}

// 校验失败必须真的把错误喂回模型，而不是只让这次 run 失败。
// 这串住的是「结构化输出 → 校验 → 反馈 → 重试」整条链路。
func TestReviewValidationFeedbackReachesModel(t *testing.T) {
	snap, unit := loadReviewFixture(t)
	fake := &repairProbeLLM{responses: []string{
		// 第一轮：severity 用了别名，缺 evidence。
		`{"verdict":"pass","findings":[{"id":"style-1-1","severity":"fatal","chapter":1,"title":"节奏略平"}]}`,
		// 第二轮：按反馈修正。
		`{"verdict":"pass","findings":[{"id":"style-1-1","severity":"blocking","chapter":1,"title":"节奏略平","evidence":"全章只有一句推进"}]}`,
	}}
	r := New(fake)

	res, err := r.RunReview(context.Background(), snap, unit, "mock/mock-gpt-4", ReviewOptions{Concurrency: 1, Rounds: 1})
	if err != nil {
		t.Fatalf("second attempt should pass: %v", err)
	}
	if fake.count() != 2 {
		t.Fatalf("requests=%d want 2 (one repair round)", fake.count())
	}
	repair := fake.repairPayload(1)
	if repair == "" {
		t.Fatal("second request carries no repair feedback")
	}
	for _, want := range []string{"findings[0].severity", "blocking 或 advisory", "findings[0].evidence"} {
		if !strings.Contains(repair, want) {
			t.Fatalf("repair feedback missing %q:\n%s", want, repair)
		}
	}
	var blocking int
	for _, f := range res.Candidate.Findings {
		if f.Severity == "blocking" {
			blocking++
		}
	}
	if blocking != 1 {
		t.Fatalf("blocking findings=%d want 1", blocking)
	}

	// 重试必须能复用前缀缓存：system prompt 一字不改，user payload 只在尾部
	// 追加 repair。否则每次校验失败都在按全价重算整份正文。
	if fake.systems[0] != fake.systems[1] {
		t.Fatal("system prompt changed between attempts; prefix cache would miss")
	}
	firstBody := fake.bodies[0]
	secondBody := fake.bodies[1]
	if !strings.HasPrefix(secondBody, strings.TrimSuffix(firstBody, "}")) {
		t.Fatalf("retry body diverges before the repair block\nfirst=%s\nsecond=%s", firstBody, secondBody)
	}
}

func TestReviewSalvageNormalizesInsteadOfFailing(t *testing.T) {
	// 截断产物：JSON 不完整，最后一条缺 severity。抢救路径应规范化收下，
	// 而不是把整份结论丢掉——重试由 budget escalation 负责。
	raw := `{"verdict":"pass","findings":[{"id":"a-1-1","severity":"blocking","chapter":1,"title":"主角被动","evidence":"全程被推着走"},{"id":"a-1-2","title":"对白说明化","chapter":1,"evidence":"`
	findings, err := parseReviewResponse(raw, 1, true)
	if err != nil {
		t.Fatalf("salvage should not fail: %v", err)
	}
	if len(findings) < 2 {
		t.Fatalf("findings=%d want salvaged entries + truncation notice", len(findings))
	}
	var sawTruncated bool
	for _, f := range findings {
		switch f.Severity {
		case severityBlocking, severityAdvisory:
		default:
			t.Fatalf("salvaged severity not normalized: %q", f.Severity)
		}
		if f.ID == "" {
			t.Fatal("salvaged finding lost its id")
		}
		if f.ID == "review-truncated" {
			sawTruncated = true
		}
	}
	if !sawTruncated {
		t.Fatal("truncated output must append a budget advisory")
	}
}

func TestReviewStrictPathStillRejectsEmptyVerdict(t *testing.T) {
	_, err := parseReviewResponse(`{"findings":[]}`, 1)
	if err == nil || !strings.Contains(err.Error(), "verdict") {
		t.Fatalf("want verdict violation, got %v", err)
	}
}

// repairProbeLLM replays canned responses and records the decoded payload of
// each request so tests can assert what the model actually saw.
type repairProbeLLM struct {
	mu        sync.Mutex
	cursor    int
	responses []string
	payloads  []*contextsvc.UserPayload
	systems   []string
	bodies    []string
}

func (p *repairProbeLLM) Chat(_ context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	payload, err := contextsvc.DecodeUserPayload(req.Messages[len(req.Messages)-1].Content)
	if err != nil {
		return port.LLMChatResponse{}, err
	}
	p.payloads = append(p.payloads, &payload)
	if len(req.Messages) > 0 {
		p.systems = append(p.systems, req.Messages[0].Content)
	}
	p.bodies = append(p.bodies, req.Messages[len(req.Messages)-1].Content)
	if p.cursor >= len(p.responses) {
		return port.LLMChatResponse{}, fmt.Errorf("no canned response left")
	}
	out := p.responses[p.cursor]
	p.cursor++
	return port.LLMChatResponse{Content: out, Done: true}, nil
}

func (p *repairProbeLLM) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.payloads)
}

func (p *repairProbeLLM) repairPayload(i int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if i >= len(p.payloads) || p.payloads[i] == nil || p.payloads[i].Repair == nil {
		return ""
	}
	b, _ := json.Marshal(p.payloads[i].Repair)
	return string(b)
}

// loadReviewFixture builds a minimal book with one fully written unit so
// RunReview has real prose to audit.
func loadReviewFixture(t *testing.T) (domain.BookSnapshot, domain.UnitInfo) {
	t.Helper()
	snap, unit, _ := loadFixtureUnit(t)
	unit.ProseRaw = "## 第1章 门\n\n" + strings.Repeat("雨落在青石上，他握紧了刀柄。", 140)
	return snap, unit
}
