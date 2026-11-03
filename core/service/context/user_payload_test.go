package contextsvc

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 输出上限不由上层决定：builder 不再接受任何预算参数，packet 里的 MaxOutputTokens
// 恒为 0，交给 LLM client 按模型配置处理。任何一处又把数字写回 packet，这里就会红。
func TestPacketsDeferOutputBudgetToModelConfig(t *testing.T) {
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "demo"}}
	packets := map[string]domain.ContextPacket{
		"plan-init":   BuildPlanPacketWithMode(snap, 1, domain.PlanModeInit, ""),
		"plan-reinit": BuildPlanPacketWithMode(snap, 1, domain.PlanModeReinit, ""),
		"plan-volume": BuildPlanPacketWithMode(snap, 1, domain.PlanModeVolume, ""),
		"outline":     BuildOutlinePacket(snap, 1, nil, ""),
		"review":      BuildReviewPacket(snap, domain.UnitInfo{UnitID: "v01-U1", ChapterFrom: 1, ChapterTo: 1}, nil, ""),
		"local-edit":  BuildLocalEditPacket(snap, domain.TextSelection{Text: "原文"}, "改一下"),
	}
	for name, p := range packets {
		if p.Policy.MaxOutputTokens != 0 {
			t.Errorf("%s: maxOutputTokens=%d, want 0 (client reads the model catalog)", name, p.Policy.MaxOutputTokens)
		}
	}
	// 调用预算仍然受控：这是固定任务契约的一部分。
	reinit := packets["plan-reinit"]
	if reinit.Policy.MaxCalls < 2 || !reinit.Policy.AllowOneRepair {
		t.Fatalf("reinit still needs one structure repair: %+v", reinit.Policy)
	}
}

func TestBuildPlanPacketUserIsJSONWithoutSystemTemplate(t *testing.T) {
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "demo", Genre: "悬疑"},
		Files:   map[string]string{"book-bible.md": "# bible\n"},
	}
	packet := BuildPlanPacketWithMode(snap, 1, domain.PlanModeInit, "自定义指令")
	if !strings.Contains(packet.SystemPrompt, "你是小说主编") {
		t.Fatal("system missing template")
	}
	p, err := DecodeUserPayload(packet.UserPrompt)
	if err != nil {
		t.Fatal(err)
	}
	if p.Task != "plan" || p.Mode != domain.PlanModeInit {
		t.Fatalf("payload=%+v", p)
	}
	if p.Instruction != "自定义指令" {
		t.Fatalf("instruction=%q", p.Instruction)
	}
	if strings.Contains(packet.UserPrompt, "你是小说主编") || strings.Contains(packet.UserPrompt, "输出严格 JSON") {
		t.Fatal("system rules leaked into user JSON")
	}
	foundBible := false
	for _, d := range p.Documents {
		if d.Path == "book-bible.md" {
			foundBible = true
		}
	}
	if !foundBible {
		t.Fatalf("documents=%+v", p.Documents)
	}
}

func TestMergeKnowledgeAndRepairStayJSON(t *testing.T) {
	raw := BuildUserPrompt("write", "", map[string]any{"id": "x"}, "写", nil, nil)
	withRef := MergeKnowledgeIntoUserPayload(raw, []string{"风格参考 A"})
	p, err := DecodeUserPayload(withRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.References) != 1 || p.References[0].Content != "风格参考 A" {
		t.Fatalf("refs=%+v", p.References)
	}
	repaired := MergeRepairIntoUserPayload(withRef, errString("缺字段"))
	p2, err := DecodeUserPayload(repaired)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Repair == nil {
		t.Fatal("missing repair")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
