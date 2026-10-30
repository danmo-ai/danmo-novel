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
		"cast":                []map[string]any{{"stem": "lin", "name": "林晚", "role": "protagonist", "cardMarkdown": "# 林晚\n"}},
		"bookOutlineMarkdown": "# 总纲",
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
	body, _ := json.Marshal(map[string]any{
		"cast": []map[string]any{
			{"stem": "lin", "name": "林晚", "role": "protagonist", "cardMarkdown": "# 林晚\n"},
		},
		"bookOutlineMarkdown": "# 总纲",
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

func TestRunPlanModeVolumeRejectsSettingsCastBook(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"cast":                  []map[string]any{{"stem": "lin", "name": "林晚", "role": "protagonist"}},
		"bookOutlineMarkdown":   "# no",
		"volumeOutlineMarkdown": "# 卷\n\n## 单元索引\n| unit_id | 章范围 | 一句话功能 | 终局边界 | 下一钩类型 |\n|---|---|---|---|---|\n| v01-U1 | 1-2 | a | b | c |\n",
		"settings":              map[string]any{"files": map[string]string{"book-bible.md": "x"}},
	})
	r := New(&scriptedLLM{raw: string(body)})
	_, err := r.RunPlanMode(context.Background(), domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "x"},
		Volumes: []domain.VolumeInfo{{Volume: 1}},
		Files:   map[string]string{"outline/book_outline.md": "# 总纲\n"},
	}, 1, "mock/m", domain.PlanModeVolume, "")
	if err == nil || !strings.Contains(err.Error(), "settings") {
		t.Fatalf("expected settings reject, got %v", err)
	}

	bodyOK, _ := json.Marshal(map[string]any{
		"volumeOutlineMarkdown": "# 卷\n\n## 单元索引\n| unit_id | 章范围 | 一句话功能 | 终局边界 | 下一钩类型 |\n|---|---|---|---|---|\n| v01-U1 | 1-2 | a | b | c |\n",
	})
	llm := &scriptedLLM{raw: string(bodyOK)}
	r = New(llm)
	res, err := r.RunPlanMode(context.Background(), domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "x"},
		Files:   map[string]string{"outline/book_outline.md": "# 总纲\n"},
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
	if err == nil || !strings.Contains(err.Error(), "立项") {
		t.Fatalf("expected book outline gate, got %v", err)
	}
}
