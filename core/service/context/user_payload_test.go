package contextsvc

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func TestCapMaxOutput(t *testing.T) {
	if CapMaxOutput(32768, 0) != 32768 {
		t.Fatal("unknown model max should keep desired")
	}
	if CapMaxOutput(32768, 128000) != 32768 {
		t.Fatal("desired under model max should keep desired")
	}
	if CapMaxOutput(32768, 8192) != 8192 {
		t.Fatal("desired above model max should clamp")
	}
	if CapMaxOutput(0, 4096) != 4096 {
		t.Fatal("zero desired should default then clamp")
	}
}

func TestBuildPlanPacketInitOutputBudget(t *testing.T) {
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "demo"}}
	full := BuildPlanPacketWithMode(snap, 1, domain.PlanModeInit, "", 0)
	if full.Policy.MaxOutputTokens != planInitMaxOutputTokens {
		t.Fatalf("init budget=%d want %d", full.Policy.MaxOutputTokens, planInitMaxOutputTokens)
	}
	clamped := BuildPlanPacketWithMode(snap, 1, domain.PlanModeReinit, "", 8192)
	if clamped.Policy.MaxOutputTokens != 8192 {
		t.Fatalf("reinit clamped=%d", clamped.Policy.MaxOutputTokens)
	}
	vol := BuildPlanPacketWithMode(snap, 1, domain.PlanModeVolume, "", 4096)
	if vol.Policy.MaxOutputTokens != 4096 {
		t.Fatalf("volume clamped=%d", vol.Policy.MaxOutputTokens)
	}
}

func TestBuildPlanPacketUserIsJSONWithoutSystemTemplate(t *testing.T) {
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "demo", Genre: "悬疑"},
		Files:   map[string]string{"book-bible.md": "# bible\n"},
	}
	packet := BuildPlanPacketWithMode(snap, 1, domain.PlanModeInit, "自定义指令", 0)
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
