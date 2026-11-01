package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
)

func TestStructureReviewAndRepairRemainCandidateOnly(t *testing.T) {
	llm := &structureFlowLLM{}
	runner := New(llm)
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "x"}, Revision: "before", Files: map[string]string{
		"outline/book_outline.md": testBookOutlineMarkdown(),
		"outline/volumes/v01.md":  testVolumeOutlineMarkdown(),
	}}
	review, err := runner.ReviewStructure(context.Background(), snap, domain.TaskSpec{Kind: domain.TaskStructureReview, TargetPath: "outline/volumes/v01.md", VolumeID: "v01"}, "mock/m")
	if err != nil || len(review.Candidate.Findings) != 1 {
		t.Fatalf("review=%+v err=%v", review, err)
	}
	repaired, err := runner.RepairStructure(context.Background(), snap, domain.TaskSpec{TargetPath: "outline/volumes/v01.md", VolumeID: "v01"}, review.Candidate.Findings, "mock/m")
	if err != nil {
		t.Fatal(err)
	}
	if repaired.Candidate.Replacement == "" || !strings.Contains(repaired.Candidate.Replacement, "获得三天复核许可") {
		t.Fatal("repair must produce a complete document candidate")
	}
	if snap.Files["outline/volumes/v01.md"] != testVolumeOutlineMarkdown() || snap.Revision != "before" {
		t.Fatal("review/repair must not write canon or change revision")
	}
	if len(llm.requests) != 2 {
		t.Fatalf("editor+repair should be bounded to 2 calls, got %d", len(llm.requests))
	}
	if !strings.Contains(llm.requests[1].Messages[1].Content, "causality") {
		t.Fatal("editor finding was not fed into repair")
	}
}

type structureFlowLLM struct{ requests []port.LLMChatRequest }

func (l *structureFlowLLM) Chat(_ context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	l.requests = append(l.requests, req)
	payload, _ := contextsvc.DecodeUserPayload(req.Messages[1].Content)
	if payload.Task == "review_structure" {
		return port.LLMChatResponse{Content: `# 结构审校 · v01
- schema_version: 1
- source_revision: before

## 问题 · v01-U1-causality-1
- entity_path: units[v01-U1].outcome
- severity: blocking
- lens: causality
- evidence: 结果需要进一步核对选择
- suggestion: 保留已经明确的申请复核选择
`, Done: true}, nil
	}
	return port.LLMChatResponse{Content: testVolumeOutlineMarkdown(), Done: true}, nil
}

func TestAutoStructureReviewIsBoundedAndProducesCandidate(t *testing.T) {
	llm := &autoStructureLLM{}
	runner := New(llm)
	runner.AutoStructureReview = true
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "x"}, Files: map[string]string{"outline/book_outline.md": testBookOutlineMarkdown()}}
	res, err := runner.RunPlanMode(context.Background(), snap, 1, "mock/m", domain.PlanModeVolume, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Candidate.Status != domain.CandidateOpen || res.Candidate.PlanMarkdown["volume_outline"] == "" {
		t.Fatal("auto review must leave an open candidate")
	}
	if len(llm.requests) != 3 || res.Packet.Policy.MaxCalls != 4 {
		t.Fatalf("generate+editor+repair budget mismatch: calls=%d policy=%d", len(llm.requests), res.Packet.Policy.MaxCalls)
	}
}

type autoStructureLLM struct{ requests []port.LLMChatRequest }

func (l *autoStructureLLM) Chat(_ context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	l.requests = append(l.requests, req)
	payload, _ := contextsvc.DecodeUserPayload(req.Messages[1].Content)
	switch payload.Task {
	case "review_structure":
		return port.LLMChatResponse{Content: "# 结构审校\n- schema_version: 1\n\n## 问题 · one\n- entity_path: units[v01-U1].outcome\n- severity: blocking\n- lens: causality\n- evidence: 需要核对\n- suggestion: 保留明确选择\n", Done: true}, nil
	case "repair_structure":
		return port.LLMChatResponse{Content: testVolumeOutlineMarkdown(), Done: true}, nil
	default:
		body, _ := json.Marshal(map[string]string{"volumeOutlineMarkdown": testVolumeOutlineMarkdown()})
		return port.LLMChatResponse{Content: string(body), Done: true}, nil
	}
}

func TestVolumePlanRejectsChangedParentState(t *testing.T) {
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "x"}, Files: map[string]string{"outline/book_outline.md": testBookOutlineMarkdown()}}
	bad := strings.Replace(testVolumeOutlineMarkdown(), "exit_state: 已完成", "exit_state: 另一个故事", 1)
	body, _ := json.Marshal(map[string]string{"volumeOutlineMarkdown": bad})
	llm := &scriptedLLM{raw: string(body)}
	_, err := New(llm).RunPlanMode(context.Background(), snap, 1, "mock/m", domain.PlanModeVolume, "")
	if err == nil || !strings.Contains(err.Error(), "继承总纲") {
		t.Fatalf("expected parent contract violation, got %v", err)
	}
}
