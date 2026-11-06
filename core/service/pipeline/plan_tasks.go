package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/prompt"
	"github.com/google/uuid"
)

// sortedKeys 解决「把 map 遍历成提示词/校验文本」的顺序问题。
// Go 的 map 迭代顺序随机，同一输入两次构建会得到不同字节序——前缀缓存与
// 重试的可复现性都会失效。任何要进入模型上下文的 map 都必须先过这里。
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (r *Runner) promptStore() *prompt.Store {
	return prompt.NewStore(promptOverlayRoot())
}

func (r *Runner) runStructuredPlan(ctx context.Context, snap domain.BookSnapshot, volume int, modelID, mode, instruction string, preservePaths ...[]string) (Result, error) {
	if mode != domain.PlanModeVolume && mode != domain.PlanModeInit && mode != domain.PlanModeReinit {
		return Result{}, fmt.Errorf("未知 plan mode %q", mode)
	}
	if mode == domain.PlanModeVolume {
		return r.runStructuredVolumePlan(ctx, snap, volume, modelID, instruction)
	}
	return r.runStructuredBookPlan(ctx, snap, modelID, mode, instruction, preservePaths...)
}

func (r *Runner) runStructuredBookPlan(ctx context.Context, snap domain.BookSnapshot, modelID, mode, instruction string, preservePaths ...[]string) (Result, error) {
	contract := bookOutlineContract
	tmpl, err := r.promptStore().Resolve("plan_book", contract.ID, 1, contract)
	if err != nil {
		return Result{}, err
	}
	codec := planBookCodec{markdownCodec{contract: contract, kind: "book"}}
	def := TaskDefinition[planBookPayload]{ID: "plan_book", Version: 1, PromptKey: "plan_book", Codec: codec,
		Validate: func(p planBookPayload, in TaskInput) []Violation {
			var vs []Violation
			normalizePlanCast(p.Cast)
			if err := validatePlanCast(p.Cast); err != nil {
				vs = append(vs, Violation{Path: "cast", Want: "合法人物卡", Got: err.Error()})
			}
			if p.Settings == nil {
				return append(vs, Violation{Path: "settings.files", Want: "完整设定文件", Got: "缺失"})
			}
			for _, path := range books.PlanSettingsPaths {
				if strings.TrimSpace(p.Settings.Files[path]) == "" {
					vs = append(vs, Violation{Path: "settings.files." + path, Want: "非空", Got: "空"})
				}
			}
			// 校验违规会逐条拼进 repair 提示词，所以顺序必须稳定：
			// 直接遍历 map 会让同一次失败在两次重试里给出不同文本。
			for _, path := range sortedKeys(p.Settings.Files) {
				if !books.IsPlanSettingsPath(path) {
					vs = append(vs, Violation{Path: "settings.files." + path, Want: "允许的设定路径", Got: "不允许"})
				}
			}
			if err := books.ValidateForeshadowSchedule(p.Settings.Files[books.ForeshadowSchedulePath]); err != nil {
				vs = append(vs, Violation{Path: "settings.files." + books.ForeshadowSchedulePath, Want: "有效伏笔登记表", Got: err.Error()})
			}
			return vs
		},
		Candidate: func(p planBookPayload, in TaskInput) domain.Candidate {
			cast, _ := jsonMarshal(p.Cast)
			settings := map[string]string{}
			for path, body := range p.Settings.Files {
				settings[path] = body
			}
			c := domain.Candidate{ID: uuid.NewString(), Kind: domain.TaskPlan, Status: domain.CandidateOpen, Mode: in.Spec.Mode, PlanMarkdown: map[string]string{"book_outline": p.BookOutlineMarkdown, "cast_json": cast}, PlanSettings: settings, CreatedAt: time.Now().UTC()}
			if in.Spec.Mode == domain.PlanModeReinit {
				c.PreservePaths = in.Spec.PreservePaths
			}
			return c
		}}
	spec := domain.TaskSpec{Kind: domain.TaskPlan, Mode: mode, Instruction: instruction}
	if len(preservePaths) > 0 {
		spec.PreservePaths = preservePaths[0]
	}
	input := TaskInput{Snapshot: snap, Spec: spec}
	packet := contextsvc.BuildPlanPacketWithMode(snap, 1, mode, instruction)
	payload, err := contextsvc.DecodeUserPayload(packet.UserPrompt)
	if err != nil {
		return Result{}, err
	}
	res, err := ExecuteTask(ctx, &TaskRunner{LLM: r.LLM, ModelMaxOut: r.modelMaxOut, Knowledge: r.KnowledgeDocs}, Invocation[planBookPayload]{
		TaskID: def.ID, StaticPrompt: tmpl.Text, DynamicPayload: payload, OutputContract: contract,
		Policy: packet.Policy, ModelID: modelID, Definition: def, Input: input,
	})
	if err == nil && r.AutoStructureReview {
		res, err = reviewAndRepairStructuredCandidate(r, ctx, snap, res, "outline/book_outline.md", modelID, instruction)
	}
	return Result{Candidate: res.Candidate, Calls: res.Calls, Packet: res.Packet}, err
}

func (r *Runner) runStructuredVolumePlan(ctx context.Context, snap domain.BookSnapshot, volume int, modelID, instruction string) (Result, error) {
	bookDoc, ok := books.StructuredOutlineFor(snap, "outline/book_outline.md")
	if !ok || !bookDoc.Valid() {
		return Result{}, fmt.Errorf("总纲需要先升级为结构化 Markdown")
	}
	contract := volumeOutlineContract
	tmpl, err := r.promptStore().Resolve("plan_volume", contract.ID, 1, contract)
	if err != nil {
		return Result{}, err
	}
	codec := planVolumeCodec{markdownCodec{contract: contract, kind: "volume"}}
	def := TaskDefinition[planVolumePayload]{ID: "plan_volume", Version: 1, PromptKey: "plan_volume", Codec: codec,
		Validate: func(p planVolumePayload, in TaskInput) []Violation {
			var vs []Violation
			if p.Settings != nil && len(p.Settings.Files) > 0 {
				vs = append(vs, Violation{Path: "settings", Want: "卷纲不得输出 settings", Got: "非空"})
			}
			if len(p.Cast) > 0 {
				vs = append(vs, Violation{Path: "cast", Want: "卷纲不得输出 cast", Got: "非空"})
			}
			if strings.TrimSpace(p.BookOutlineMarkdown) != "" {
				vs = append(vs, Violation{Path: "bookOutlineMarkdown", Want: "卷纲不得改总纲", Got: "非空"})
			}
			if len(vs) > 0 {
				return vs
			}
			doc := books.ParseStructuredMarkdown(p.VolumeOutlineMarkdown, contract.ID)
			if err := validateVolumeInheritance(bookDoc, doc, fmt.Sprintf("v%02d", in.Spec.Chapter)); err != nil {
				return []Violation{{Path: "parent_contract", Want: "继承总纲卷索引", Got: err.Error()}}
			}
			// 单元种子的 role_title 迁移必须与身份时间线一致，否则细纲阶段会陷入
			// 死锁：父级继承要求逐字包含这条迁移，角色校验又要求 to 等于本单元生效
			// 身份——两边不可能同时满足。放在这里（而不是只放在 Accept）是为了让
			// 模型能在结构修复轮里自己改对，而不是等作者付完这次生成的钱才失败。
			if err := books.ValidateVolumeSeedIdentities(snap.IdentitySegments, doc, volumeUnits(snap, doc)); err != nil {
				return []Violation{{Path: "unit.state_changes", Want: "role_title 与身份时间线一致", Got: err.Error()}}
			}
			return nil
		},
		Candidate: func(p planVolumePayload, in TaskInput) domain.Candidate {
			return domain.Candidate{ID: uuid.NewString(), Kind: domain.TaskPlan, Status: domain.CandidateOpen, Mode: domain.PlanModeVolume, VolumeID: fmt.Sprintf("v%02d", in.Spec.Chapter), PlanMarkdown: map[string]string{"volume_outline": p.VolumeOutlineMarkdown}, ForeshadowAssignments: p.ForeshadowAssignments, CreatedAt: time.Now().UTC()}
		}}
	input := TaskInput{Snapshot: snap, Spec: domain.TaskSpec{Kind: domain.TaskPlan, Mode: domain.PlanModeVolume, Chapter: volume, Instruction: instruction}}
	packet := contextsvc.BuildPlanPacketWithMode(snap, volume, domain.PlanModeVolume, instruction)
	payload, err := contextsvc.DecodeUserPayload(packet.UserPrompt)
	if err != nil {
		return Result{}, err
	}
	res, err := ExecuteTask(ctx, &TaskRunner{LLM: r.LLM, ModelMaxOut: r.modelMaxOut, Knowledge: r.KnowledgeDocs}, Invocation[planVolumePayload]{
		TaskID: def.ID, StaticPrompt: tmpl.Text, DynamicPayload: payload, OutputContract: contract,
		Policy: packet.Policy, ModelID: modelID, Definition: def, Input: input,
	})
	if err == nil && r.AutoStructureReview {
		res, err = reviewAndRepairStructuredCandidate(r, ctx, snap, res, fmt.Sprintf("outline/volumes/v%02d.md", volume), modelID, instruction)
	}
	return Result{Candidate: res.Candidate, Calls: res.Calls, Packet: res.Packet}, err
}

func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// volumeUnits 返回「本卷文档里的单元 + 盘上已有的单元」的去重并集。
//
// 身份变更的边界计算（previousUnitID / ValidateIdentityTimeline）要靠单元索引
// 算出「上一段到哪儿结束」。只给盘上的单元会漏掉本卷新增的单元，得到假的断档；
// 只给文档里的单元则会让跨卷的上一段无处落点。两个来源都要。
func volumeUnits(snap domain.BookSnapshot, doc domain.StructuredMarkdown) []domain.UnitInfo {
	out := append([]domain.UnitInfo(nil), snap.Units...)
	seen := make(map[string]bool, len(out))
	for _, u := range out {
		seen[u.UnitID] = true
	}
	for _, e := range doc.EntitiesOf("单元") {
		if !seen[e.ID] {
			out = append(out, domain.UnitInfo{UnitID: e.ID})
			seen[e.ID] = true
		}
	}
	return out
}
