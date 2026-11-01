package pipeline

import (
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
)

// ResolvePlanPreview shares the exact immutable prompt with RunPlanMode.
func (r *Runner) ResolvePlanPreview(snap domain.BookSnapshot, mode string, packet domain.ContextPacket) (domain.ContextPacket, error) {
	if mode == domain.PlanModeInit || mode == domain.PlanModeReinit {
		tmpl, err := r.promptStore().Resolve("plan_book", bookOutlineContract.ID, 1, bookOutlineContract)
		if err != nil {
			return packet, err
		}
		packet.SystemPrompt = tmpl.Text
		packet.EstPromptTokens = (len([]rune(packet.SystemPrompt)) + len([]rune(packet.UserPrompt))) / 2
		return packet, nil
	}
	doc, ok := books.StructuredOutlineFor(snap, "outline/book_outline.md")
	if !ok || !doc.Valid() {
		return packet, fmt.Errorf("旧格式或无效总纲不进入生产链路，请先通过 Agent 批量迁移")
	}
	tmpl, err := r.promptStore().Resolve("plan_volume", volumeOutlineContract.ID, 1, volumeOutlineContract)
	if err != nil {
		return packet, err
	}
	packet.SystemPrompt = tmpl.Text
	return packet, nil
}

// PreviewStructuredTask resolves exactly the same immutable prompt used at
// execution time; only the model call is omitted.
func (r *Runner) PreviewStructuredTask(snap domain.BookSnapshot, spec domain.TaskSpec) (domain.ContextPacket, error) {
	switch spec.Kind {
	case domain.TaskStructureReview:
		tmpl, err := r.promptStore().Resolve("review_structure", structureEditorContract.ID, 1, structureEditorContract)
		if err != nil {
			return domain.ContextPacket{}, err
		}
		packet := contextPacketForStructure(snap, spec)
		packet.SystemPrompt = tmpl.Text
		packet.EstPromptTokens = (len([]rune(packet.SystemPrompt)) + len([]rune(packet.UserPrompt))) / 2
		return packet, nil
	case domain.TaskRewriteEntity:
		if spec.EntityKind != "structure_repair" {
			return domain.ContextPacket{}, fmt.Errorf("not a structure task")
		}
		contract := bookOutlineContract
		if spec.TargetPath != "outline/book_outline.md" {
			contract = volumeOutlineContract
		}
		tmpl, err := r.promptStore().Resolve("repair_structure", bookOutlineContract.ID, 1, bookOutlineContract)
		if err != nil {
			return domain.ContextPacket{}, err
		}
		tmpl.Text = replaceProtocolText(tmpl.Text, bookOutlineContract, contract)
		packet := contextPacketForStructure(snap, spec)
		packet.SystemPrompt = tmpl.Text
		packet.EstPromptTokens = (len([]rune(packet.SystemPrompt)) + len([]rune(packet.UserPrompt))) / 2
		return packet, nil
	default:
		return domain.ContextPacket{}, fmt.Errorf("not a structure task")
	}
}

func replaceProtocolText(text string, from, to OutputProtocol) string {
	return strings.ReplaceAll(text, from.Text(), to.Text())
}

func contextPacketForStructure(snap domain.BookSnapshot, spec domain.TaskSpec) domain.ContextPacket {
	return contextsvc.BuildStructureReviewPacket(snap, spec)
}
