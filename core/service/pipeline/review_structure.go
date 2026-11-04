package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/prompt"
	"github.com/google/uuid"
)

var structureEditorContract = prompt.NewContract(books.StructureEditorProtocol, "markdown", `只输出一个结构审校 Markdown 文档。根字段包含 schema_version 和 source_revision。
用「## 问题 · 稳定唯一ID」声明问题；每项必须有 entity_path/severity/lens/evidence/suggestion。
severity 只能 blocking 或 advisory；没有问题则不写问题实体。`, `# 结构审校 · {{target}}
- schema_version: 1
- source_revision: {{revision}}

## 问题 · {{unit-or-volume}}-causality-1
- entity_path: {{units[v01-U1].outcome}}
- severity: {{blocking|advisory}}
- lens: {{inheritance|causality|payoff|state|continuity|repetition|reveal|agency}}
- evidence: {{可核验的问题证据}}
- suggestion: {{可执行的最小修复建议}}`)

// ReviewStructure audits planning artifacts, never prose. It returns a review
// candidate and does not mutate the source Markdown.
func (r *Runner) ReviewStructure(ctx context.Context, snap domain.BookSnapshot, spec domain.TaskSpec, modelID string) (Result, error) {
	path := spec.TargetPath
	raw := strings.TrimSpace(snap.Files[path])
	if raw == "" {
		return Result{}, fmt.Errorf("结构审校目标不存在: %s", path)
	}
	return r.reviewStructureDocument(ctx, snap, spec, raw, modelID)
}

func (r *Runner) reviewStructureDocument(ctx context.Context, snap domain.BookSnapshot, spec domain.TaskSpec, raw, modelID string) (Result, error) {
	path := spec.TargetPath
	protocol := books.BookOutlineProtocol
	if path != "outline/book_outline.md" {
		protocol = books.VolumeOutlineProtocol
	}
	doc := books.ParseStructuredMarkdown(raw, protocol)
	if !doc.Valid() {
		return Result{}, fmt.Errorf("请先修复 Markdown 结构：%s", books.FormatOutlineDiagnostics(doc.Diagnostics))
	}
	tmpl, err := r.promptStore().Resolve("review_structure", structureEditorContract.ID, 1, structureEditorContract)
	if err != nil {
		return Result{}, err
	}
	blocks := contextsvc.BuildStructureReviewPacket(snap, spec).Blocks
	payload := contextsvc.UserPayload{Task: "review_structure", Book: map[string]any{"id": snap.Summary.ID}, Documents: contextsvc.DocumentsFromBlocks(blocks), Instruction: spec.Instruction}
	res, err := ExecuteTask(ctx, &TaskRunner{LLM: r.LLM, ModelMaxOut: r.modelMaxOut, Knowledge: r.KnowledgeDocs}, Invocation[[]domain.ReviewFinding]{
		TaskID: "review_structure", StaticPrompt: tmpl.Text, DynamicPayload: payload, OutputContract: structureEditorContract,
		Policy: domain.CallPolicy{MaxCalls: 2, AllowOneRepair: true, TimeoutSec: 600}, ModelID: modelID,
		Definition: TaskDefinition[[]domain.ReviewFinding]{ID: "review_structure", Version: 1, Codec: structureReviewCodec{contract: structureEditorContract}, Candidate: func(findings []domain.ReviewFinding, in TaskInput) domain.Candidate {
			return domain.Candidate{ID: uuid.NewString(), Kind: domain.TaskStructureReview, Status: domain.CandidateOpen, TargetPath: in.Spec.TargetPath, VolumeID: in.Spec.VolumeID, Findings: findings, CreatedAt: time.Now().UTC()}
		}}, Input: TaskInput{Snapshot: snap, Spec: spec},
	})
	return Result{Candidate: res.Candidate, Calls: res.Calls, Packet: res.Packet}, err
}

type structureReviewCodec struct{ contract OutputProtocol }

func (c structureReviewCodec) Protocol() OutputProtocol { return c.contract }
func (c structureReviewCodec) Decode(raw string) ([]domain.ReviewFinding, error) {
	return ParseStructureEditor(raw)
}

// reviewAndRepairStructuredCandidate performs exactly one editor call and at
// most one semantic repair. It never accepts the repaired document.
func reviewAndRepairStructuredCandidate[T any](r *Runner, ctx context.Context, snap domain.BookSnapshot, generated TaskResult[T], path, modelID, instruction string) (TaskResult[T], error) {
	candidate := generated.Candidate
	key := "book_outline"
	if path != "outline/book_outline.md" {
		key = "volume_outline"
	}
	raw := candidate.PlanMarkdown[key]
	files := map[string]string{}
	for p, body := range snap.Files {
		files[p] = body
	}
	files[path] = raw
	snap.Files = files
	spec := domain.TaskSpec{Kind: domain.TaskStructureReview, TargetPath: path, VolumeID: candidate.VolumeID, Instruction: instruction}
	reviewed, err := r.reviewStructureDocument(ctx, snap, spec, raw, modelID)
	generated.Calls = append(generated.Calls, reviewed.Calls...)
	if err != nil {
		return generated, err
	}
	candidate.Findings = append(candidate.Findings, reviewed.Candidate.Findings...)
	var blocking []domain.ReviewFinding
	for _, f := range reviewed.Candidate.Findings {
		if f.Severity == "blocking" {
			blocking = append(blocking, f)
		}
	}
	if len(blocking) == 0 {
		generated.Candidate = candidate
		generated.Packet.Policy.MaxCalls = generated.Packet.Policy.MaxCalls + 2
		return generated, nil
	}
	repaired, err := r.RepairStructure(ctx, snap, domain.TaskSpec{TargetPath: path, VolumeID: candidate.VolumeID, Instruction: instruction}, blocking, modelID)
	generated.Calls = append(generated.Calls, repaired.Calls...)
	if err != nil {
		return generated, err
	}
	candidate.PlanMarkdown[key] = repaired.Candidate.Replacement
	// A repair is a new document revision. Historical findings remain visible
	// for audit, but must not be treated as a fresh review of the repair.
	candidate.Findings = nil
	generated.Candidate = candidate
	generated.Packet.Policy.MaxCalls = generated.Packet.Policy.MaxCalls + 2
	return generated, nil
}

// RepairStructure applies one bounded set of findings to a candidate. Accept
// remains the only operation allowed to write the result.
func (r *Runner) RepairStructure(ctx context.Context, snap domain.BookSnapshot, spec domain.TaskSpec, findings []domain.ReviewFinding, modelID string) (Result, error) {
	contract := bookOutlineContract
	if spec.TargetPath != "outline/book_outline.md" {
		contract = volumeOutlineContract
	}
	resolvedContract := bookOutlineContract
	tmpl, err := r.promptStore().Resolve("repair_structure", resolvedContract.ID, 1, resolvedContract)
	if err != nil {
		return Result{}, err
	}
	if spec.TargetPath != "outline/book_outline.md" {
		tmpl.Text = replaceProtocolText(tmpl.Text, bookOutlineContract, volumeOutlineContract)
	}
	blocks := contextsvc.BuildStructureReviewPacket(snap, spec).Blocks
	for i := range blocks {
		if blocks[i].ID == "target" {
			blocks[i].ID, blocks[i].Label = "current", "当前文档"
		}
	}
	if parent := snap.Files["outline/book_outline.md"]; parent != "" && spec.TargetPath != "outline/book_outline.md" {
		blocks = append(blocks, domain.ContextBlock{ID: "parent", Label: "不可修改的父级合同", Source: "outline/book_outline.md", Content: parent, Bytes: len(parent)})
	}
	var feedback strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&feedback, "- %s [%s] %s：%s；建议：%s\n", f.EntityPath, f.Severity, f.Lens, f.Evidence, f.Suggestion)
	}
	payload := contextsvc.UserPayload{Task: "repair_structure", Book: map[string]any{"id": snap.Summary.ID}, Documents: contextsvc.DocumentsFromBlocks(blocks), Instruction: spec.Instruction, Extra: map[string]any{"findings": feedback.String()}}
	res, err := ExecuteTask(ctx, &TaskRunner{LLM: r.LLM, ModelMaxOut: r.modelMaxOut, Knowledge: r.KnowledgeDocs}, Invocation[string]{
		TaskID: "repair_structure", StaticPrompt: tmpl.Text, DynamicPayload: payload, OutputContract: contract,
		Policy: domain.CallPolicy{MaxCalls: 2, AllowOneRepair: true, TimeoutSec: 600}, ModelID: modelID,
		Definition: TaskDefinition[string]{ID: "repair_structure", Version: 1, Codec: structureRepairCodec{contract: contract}, Validate: func(body string, in TaskInput) []Violation {
			doc := books.ParseStructuredMarkdown(body, contract.ID)
			if contract.ID == books.VolumeOutlineProtocol {
				parent, _ := books.StructuredOutlineFor(in.Snapshot, "outline/book_outline.md")
				if err := validateVolumeInheritance(parent, doc, in.Spec.VolumeID); err != nil {
					return []Violation{{Path: "parent_contract", Want: "继承父级合同", Got: err.Error()}}
				}
			}
			return nil
		}, Candidate: func(body string, in TaskInput) domain.Candidate {
			return domain.Candidate{ID: uuid.NewString(), Kind: domain.TaskRewriteEntity, Status: domain.CandidateOpen, TargetPath: in.Spec.TargetPath, VolumeID: in.Spec.VolumeID, Replacement: body, Findings: findings, CreatedAt: time.Now().UTC()}
		}}, Input: TaskInput{Snapshot: snap, Spec: spec},
	})
	return Result{Candidate: res.Candidate, Calls: res.Calls, Packet: res.Packet}, err
}

type structureRepairCodec struct{ contract OutputProtocol }

func (c structureRepairCodec) Protocol() OutputProtocol { return c.contract }
func (c structureRepairCodec) Decode(raw string) (string, error) {
	body, err := decodeFullMarkdown(raw)
	if err != nil {
		return "", err
	}
	_, err = c.validate(body)
	return body, err
}
func (c structureRepairCodec) validate(body string) (domain.StructuredMarkdown, error) {
	if c.contract.ID == books.BookOutlineProtocol {
		return validateBookOutline(body)
	}
	return validateVolumeOutline(body)
}
