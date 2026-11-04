package contextsvc

import (
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/service/books"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// BuildStructureReviewPacket is the fixed data assembler for structure review.
// Protocol text is resolved by the task layer; this packet only carries data.
func BuildStructureReviewPacket(snap domain.BookSnapshot, spec domain.TaskSpec) domain.ContextPacket {
	if spec.TargetPath != "outline/book_outline.md" && !strings.HasPrefix(spec.TargetPath, "outline/volumes/") {
		return domain.ContextPacket{}
	}
	path := spec.TargetPath
	blocks := []domain.ContextBlock{}
	if path != "outline/book_outline.md" {
		if parent := snap.Files["outline/book_outline.md"]; parent != "" {
			blocks = append(blocks, domain.ContextBlock{ID: "parent", Label: "父级总纲", Source: "outline/book_outline.md", Content: parent, Bytes: len(parent)})
		}
	}
	raw := snap.Files[path]
	blocks = append(blocks, domain.ContextBlock{ID: "target", Label: "待审结构", Source: path, Content: raw, Bytes: len(raw)})
	add := func(id, label, source, content string) {
		if content = strings.TrimSpace(content); content != "" {
			blocks = append(blocks, domain.ContextBlock{ID: id, Label: label, Source: source, Content: content, Bytes: len(content)})
		}
	}
	add("world", "世界与能力约束", "canon/world.md", snap.Files["canon/world.md"])
	add("goldfinger", "能力上限", "canon/goldfinger.md", snap.Files["canon/goldfinger.md"])
	add("foreshadows", "伏笔登记", books.ForeshadowSchedulePath, snap.Files[books.ForeshadowSchedulePath])
	add("continuity", "已定稿事实", books.StatePath, ExtractContinuityLatest(snap, nil, ContinuityForReview))
	for _, c := range snap.Cast {
		add("cast-"+c.Stem, "人物·"+c.Stem, "canon/cast/"+c.Stem+".md", c.Raw)
	}
	hashes := map[string]string{}
	for _, b := range blocks {
		hashes[b.ID+"|"+b.Source] = shortHash(b.Content)
	}
	user := BuildUserPrompt("review_structure", "", map[string]any{"id": snap.Summary.ID}, spec.Instruction, blocks, nil)
	return domain.ContextPacket{ContractVersion: "structure-editor/v1", TaskKind: domain.TaskStructureReview, BookID: snap.Summary.ID, VolumeID: spec.VolumeID, InputHashes: hashes, Blocks: blocks, Policy: domain.CallPolicy{MaxCalls: 2, AllowOneRepair: true, TimeoutSec: 600}, UserPrompt: user, EstPromptTokens: len([]rune(user)) / 2, SystemPrompt: fmt.Sprintf("任务 review_structure；协议由 PromptStore 注入。")}
}
