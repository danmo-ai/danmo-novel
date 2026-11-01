package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
)

func testBookOutlineMarkdown() string {
	return `# 全书总纲 · 测试
- schema_version: 1
- book_id: x
- title: 测试
- logline: 主角在困境中选择行动

## 故事线 · main
- kind: main
- description: 主线
- opening_state: 未开始
- ending_state: 已完成

## 分卷 · v01
- arc_id: arc-v01
- title: 第一卷
- chapter_range: 1-6
- purpose: 建立目标
- dramatic_question: 能否完成目标
- central_conflict: 目标与阻碍
- stakes: 失败代价
- entry_state: 未开始
- exit_state: 已完成
- thread_moves: main=未开始->已完成
- deliverables: 完成第一次行动
- open_questions: 下一步是什么
- reveal_ids: R1
- forbidden_reveal_ids: R2
- next_volume_entry: 继续行动

### 转折 · event-1
- event: 主角采取行动
- cause: 目标受到阻碍
- effect: 获得新的位置
- chapter_window: 1-6
`
}

type scriptedLLM struct {
	raw string
	req port.LLMChatRequest
}

func (s *scriptedLLM) Chat(ctx context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	s.req = req
	return port.LLMChatResponse{Content: s.raw, Done: true}, nil
}

func TestRunPlanModeInitRequiresSettings(t *testing.T) {
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "x", Genre: "悬疑"}, Files: map[string]string{"book-bible.md": "# b\n"}}
	body, _ := json.Marshal(map[string]any{
		"cast":                []map[string]any{{"stem": "lin", "name": "林晚", "role": "protagonist", "initialIdentity": map[string]any{"roleTitle": "见习调查员", "org": "青江分局", "fromUnit": "v01-U1", "note": "初始身份"}, "cardMarkdown": "# 林晚\n"}},
		"bookOutlineMarkdown": testBookOutlineMarkdown(),
	})
	r := New(&scriptedLLM{raw: string(body)})
	_, err := r.RunPlanMode(context.Background(), snap, 1, "mock/m", domain.PlanModeInit, "")
	if err == nil || !strings.Contains(err.Error(), "settings") {
		t.Fatalf("expected settings error, got %v", err)
	}
}

func TestRunPlanModeInitAcceptsBookOnlyPayload(t *testing.T) {
	files := map[string]string{}
	for _, p := range books.PlanSettingsPaths {
		files[p] = "# " + p + "\nok\n"
	}
	files["canon/locked-terms.yaml"] = "locked_until: {}\ncompliance: []\naliases: {}\n"
	files["canon/reveal-schedule.md"] = "# 揭示计划\n- schema_version: 1\n"
	body, _ := json.Marshal(map[string]any{
		"cast": []map[string]any{
			{"stem": "lin", "name": "林晚", "role": "protagonist", "initialIdentity": map[string]any{"roleTitle": "见习调查员", "org": "青江分局", "fromUnit": "v01-U1", "note": "初始身份"}, "cardMarkdown": "# 林晚\n"},
		},
		"bookOutlineMarkdown": testBookOutlineMarkdown(),
		"settings":            map[string]any{"files": files},
	})
	llm := &scriptedLLM{raw: string(body)}
	r := New(llm)
	res, err := r.RunPlanMode(context.Background(), domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "x", Genre: "悬疑"},
		Files:   map[string]string{"book-bible.md": "# b\n"},
	}, 1, "mock/m", domain.PlanModeInit, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Candidate.Mode != domain.PlanModeInit {
		t.Fatalf("mode=%s", res.Candidate.Mode)
	}
	if len(res.Candidate.PlanSettings) != len(books.PlanSettingsPaths) {
		t.Fatalf("settings=%d", len(res.Candidate.PlanSettings))
	}
	if strings.TrimSpace(res.Candidate.PlanMarkdown["volume_outline"]) != "" {
		t.Fatal("init must not keep volume outline")
	}
	if len(llm.req.Messages) != 2 || llm.req.Messages[0].Role != "system" || llm.req.Messages[1].Role != "user" {
		t.Fatalf("messages=%+v", llm.req.Messages)
	}
	payload, err := contextsvc.DecodeUserPayload(llm.req.Messages[1].Content)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Task != "plan" || payload.Mode != domain.PlanModeInit {
		t.Fatalf("payload=%+v", payload)
	}
	if strings.Contains(llm.req.Messages[1].Content, "你是小说主编") {
		t.Fatal("system template leaked into user payload")
	}
}

func testVolumeOutlineMarkdown() string {
	return `# 卷纲 · v01
- schema_version: 1
- volume_id: v01
- title: 第一卷
- source_arc_id: arc-v01
- chapter_range: 1-6
- goal: 能否完成目标
- central_conflict: 目标与阻碍
- stakes: 失败代价
- entry_state: 未开始
- exit_state: 已完成
- cast: lin

## 节拍 · event-1
- event: 主角采取行动
- cause: 目标受到阻碍
- effect: 获得新的位置
- chapter_window: 1-6

## 单元 · v01-U1
- title: 起步
- chapter_range: 1-6
- objective: 获得行动资格
- obstacle: 尚未获得许可
- attempt: 查找旧材料
- setback: 材料不足
- choice: 按程序申请复核
- cost: 承担复核责任
- outcome: 获得三天复核许可
- entry_cause: 开卷目标受阻
- exit_result: 获得许可
- state_changes: lin.role=新人->复核人
- clock_start_day: 1
- clock_end_day: 6
- clock_gap_days: 0
- timeline_id: main
- thread_ids: main
- event_ids: event-1
- reveal_ids: R1
- forbidden_reveal_ids: R2
- next_hook: 信息缺口 | 签名不一致
`
}

func TestRunPlanModeVolumeRejectsSettingsCastBook(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"cast":                  []map[string]any{{"stem": "lin", "name": "林晚", "role": "protagonist", "initialIdentity": map[string]any{"roleTitle": "见习调查员", "org": "青江分局", "fromUnit": "v01-U1", "note": "初始身份"}}},
		"bookOutlineMarkdown":   "# no",
		"volumeOutlineMarkdown": testVolumeOutlineMarkdown(),
		"settings":              map[string]any{"files": map[string]string{"book-bible.md": "x"}},
	})
	r := New(&scriptedLLM{raw: string(body)})
	_, err := r.RunPlanMode(context.Background(), domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "x"},
		Volumes: []domain.VolumeInfo{{Volume: 1}},
		Files:   map[string]string{"outline/book_outline.md": testBookOutlineMarkdown()},
	}, 1, "mock/m", domain.PlanModeVolume, "")
	if err == nil || !strings.Contains(err.Error(), "settings") {
		t.Fatalf("expected settings reject, got %v", err)
	}

	bodyOK, _ := json.Marshal(map[string]any{
		"volumeOutlineMarkdown": testVolumeOutlineMarkdown(),
	})
	llm := &scriptedLLM{raw: string(bodyOK)}
	r = New(llm)
	res, err := r.RunPlanMode(context.Background(), domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "x"},
		Files:   map[string]string{"outline/book_outline.md": testBookOutlineMarkdown()},
	}, 1, "mock/m", domain.PlanModeVolume, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Candidate.VolumeID != "v01" {
		t.Fatalf("volumeId=%s", res.Candidate.VolumeID)
	}
	payload, err := contextsvc.DecodeUserPayload(llm.req.Messages[1].Content)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Mode != domain.PlanModeVolume {
		t.Fatalf("mode=%s", payload.Mode)
	}
}

func TestRunPlanModeVolumeRequiresBookOutline(t *testing.T) {
	r := New(&scriptedLLM{raw: `{"volumeOutlineMarkdown":"x"}`})
	_, err := r.RunPlanMode(context.Background(), domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "x"},
		Files:   map[string]string{},
	}, 1, "mock/m", domain.PlanModeVolume, "")
	if err == nil || !strings.Contains(err.Error(), "迁移") {
		t.Fatalf("expected explicit migration gate, got %v", err)
	}
}
