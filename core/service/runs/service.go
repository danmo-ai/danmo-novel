package runs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	"github.com/danmo-ai/danmo-novel/core/runtimepaths"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/gates"
	"github.com/danmo-ai/danmo-novel/core/service/knowledge"
	"github.com/danmo-ai/danmo-novel/core/service/llmconfig"
	"github.com/danmo-ai/danmo-novel/core/service/pipeline"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

type Service struct {
	Store    *sqlite.Store
	Settings *config.Manager
	LLM      port.LLMProvider
	Models   *llmconfig.ModelRegistry
	Pipeline *pipeline.Runner
	mu       sync.Mutex
	cancels  map[string]context.CancelFunc
}

func New(store *sqlite.Store, settings *config.Manager, llm port.LLMProvider) *Service {
	s := &Service{
		Store: store, Settings: settings, LLM: llm,
		Pipeline: pipeline.New(llm),
		cancels:  map[string]context.CancelFunc{},
	}
	s.Pipeline.ModelMaxOut = func(modelID string) int {
		if s.Models == nil {
			return 0
		}
		return s.Models.MaxOutputTokens(modelID)
	}
	return s
}

func (s *Service) modelMaxOut(modelID string) int {
	if s == nil || s.Models == nil {
		return 0
	}
	return s.Models.MaxOutputTokens(modelID)
}

// resolveModel 把 spec 映射到阶段角色再取绑定模型；显式 ModelID 优先。
func (s *Service) resolveModel(spec domain.TaskSpec) string {
	if spec.ModelID != "" {
		return spec.ModelID
	}
	role := domain.StageWrite
	switch spec.Kind {
	case domain.TaskPlan, domain.TaskIdea:
		role = domain.StagePlan
	case domain.TaskOutlineReview:
		role = domain.StageReview
	case domain.TaskOutline:
		role = domain.StageOutline
	case domain.TaskReview, domain.TaskRevise:
		role = domain.StageReview
	case domain.TaskFinalize:
		// 定稿是"照抄式提取"：把已经写好的正文读成一张结构化账本，不创作、
		// 不评判文笔。它跟审校共用一套推理强度是浪费——实测 6 章定稿只产出
		// 几十行 JSON，completion 却有 3–9k/章，绝大部分是思维链。
		// 所以给它独立角色；没单独配置时退回审校，老配置的行为不变。
		if s.Settings.ModelFor(domain.StageFinalize) != "" {
			role = domain.StageFinalize
		} else {
			role = domain.StageReview
		}
	case domain.TaskLocalEdit, domain.TaskContinue, domain.TaskExpand, domain.TaskPolish, domain.TaskRewriteSel:
		role = domain.StageLocalEdit
	case domain.TaskRewriteEntity:
		role = domain.StagePlan
	}
	return s.Settings.ModelFor(role)
}

func (s *Service) Start(ctx context.Context, spec domain.TaskSpec) (domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startLocked(ctx, spec)
}

func (s *Service) startLocked(ctx context.Context, spec domain.TaskSpec) (domain.Run, error) {
	root, readOnly, err := s.Store.GetBookRoot(ctx, spec.BookID)
	if err != nil {
		return domain.Run{}, err
	}
	if readOnly && (spec.Kind == domain.TaskWrite || spec.Kind == domain.TaskOutline || spec.Kind == domain.TaskPlan || spec.Kind == domain.TaskFinalize) {
		// generation creating candidates is OK; accept will refuse writeback on read-only
	}
	snap, err := books.LoadBook(root, readOnly)
	if err != nil {
		return domain.Run{}, err
	}
	if spec.Kind == domain.TaskWrite {
		unit, ok := findUnit(snap, spec.UnitID)
		if !ok || (unit.Status != domain.UnitAccepted && unit.Status != domain.UnitDrafted && unit.Status != domain.UnitReviewed && unit.Status != domain.UnitFinalized) {
			return domain.Run{}, fmt.Errorf("单元 %s 必须先接受完整细纲，才能写正文", spec.UnitID)
		}
		// 「在原文上优化」没有原文就无从优化：提前拒绝，而不是让模型收到一份
		// 写着"基于底稿"却没有底稿的指令。
		if domain.NormalizeRewriteMode(spec.RewriteMode) == domain.RewriteRefine && !unit.HasProse {
			return domain.Run{}, fmt.Errorf("单元 %s 还没有正文，不能在原文上优化；请直接用「重新生成」", spec.UnitID)
		}
	}
	if spec.Kind == domain.TaskReview || spec.Kind == domain.TaskRevise || spec.Kind == domain.TaskFinalize {
		unit, ok := findUnit(snap, spec.UnitID)
		if !ok || !unit.HasProse || unit.Status == domain.UnitProposed || unit.Status == domain.UnitAccepted {
			return domain.Run{}, fmt.Errorf("单元 %s 必须先接受正文草稿，才能进入 %s", spec.UnitID, spec.Kind)
		}
	}
	// revision 校验必须在归档之前：否则一次过期请求会先把旧清单作废，再报错，
	// 等于"什么都没跑成，结论却没了"。
	if spec.BaseRevision != "" && spec.BaseRevision != snap.Revision {
		return domain.Run{}, fmt.Errorf("baseRevision mismatch: book moved on, refresh and retry")
	}
	if modelID := s.resolveModel(spec); modelID == "" {
		return domain.Run{}, fmt.Errorf("no model assigned for this stage; configure Settings → 创作阶段模型")
	}
	// 重审先用新结论替换旧结论：归档该单元未处理的审校清单，否则那条
	// waiting_accept 的 run 会一直占着单元，重审永远起不来。
	if spec.Kind == domain.TaskReview && spec.SupersedeReview && spec.UnitID != "" {
		if cerr := s.archiveUnitReviews(ctx, spec.BookID, spec.UnitID); cerr != nil {
			return domain.Run{}, fmt.Errorf("归档旧审校清单失败: %w", cerr)
		}
	}
	active, blocked, err := s.blockingRun(ctx, spec)
	if err != nil {
		return domain.Run{}, err
	}
	if blocked {
		return active, activeRunConflict(active, spec)
	}
	modelID := s.resolveModel(spec)

	run := domain.Run{
		ID: uuid.NewString(), BookID: spec.BookID, Kind: spec.Kind, Status: domain.RunRunning,
		Spec: spec, ModelID: modelID, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := s.Store.SaveRun(ctx, run); err != nil {
		return domain.Run{}, err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.cancels[run.ID] = cancel

	go s.execute(runCtx, run, snap, modelID)
	return run, nil
}

func (s *Service) execute(ctx context.Context, run domain.Run, snap domain.BookSnapshot, modelID string) {
	defer func() {
		s.mu.Lock()
		delete(s.cancels, run.ID)
		s.mu.Unlock()
	}()

	var res pipeline.Result
	var err error
	// 每次 run 用一份自己的 Runner 值拷贝：KnowledgeDocs / ReviewChecklist /
	// DeslopRules 是"本次调用"的设置，写回共享的 s.Pipeline 会与并发的另一个 run
	// 互相覆盖（-race 已复现），严重时另一个 run 会在中途丢掉审校清单/去AI味规则。
	// 其余字段（LLM / ModelMaxOut）只读，值拷贝是安全的。
	runner := *s.Pipeline
	// 心跳：每次 LLM 调用后把 callsUsed 与 updatedAt 写进库里。一次写盘几十微秒，
	// 换来的是「这个任务还活着吗」有确切答案——否则长任务与死任务在库里完全一样。
	// 计数留在本地：Runner 是按值拷贝的，带锁字段会被 vet 判为非法拷贝。
	var callCount atomic.Int64
	runner.OnCall = func() { s.touchRun(ctx, run, int(callCount.Add(1))) }
	// 作者勾选的参考知识文章（独立于 unit 的 ExtraContextIDs 语义）。
	runner.KnowledgeDocs = s.loadKnowledge(run.Spec.KnowledgeIDs)
	// 审校清单按作者在知识库里的编辑生效；未编辑时回退内置文章。
	runner.ReviewChecklist = s.loadReviewChecklist()
	// 本地硬门规则同样允许作者覆盖（本书 > 知识库 > 内置）。
	// 正则写错时必须让作者看到，而不是静默退回内置继续"正常"跑——
	// 所以这里先记下错误，switch 跑完后覆盖，而不是被 case 里的赋值冲掉。
	var rulesErr error
	if rules, rerr := s.loadDeslopRules(snap.Summary.RootPath); rerr != nil {
		rulesErr = fmt.Errorf("去 AI 味规则无法解析：%w", rerr)
		runner.DeslopRules = gates.BuiltinRules()
	} else {
		runner.DeslopRules = rules
	}
	switch run.Kind {
	case domain.TaskWrite:
		unit, ok := findUnit(snap, run.Spec.UnitID)
		if !ok {
			err = fmt.Errorf("unit not found: %s", run.Spec.UnitID)
		} else {
			res, err = runner.RunWriteWithMode(ctx, snap, unit, run.Spec.Instruction, run.Spec.RewriteMode, modelID)
		}
	case domain.TaskOutlineReview:
		unit, ok := findUnit(snap, run.Spec.UnitID)
		if !ok {
			err = fmt.Errorf("unit not found: %s", run.Spec.UnitID)
		} else {
			res, err = runner.RunOutlineReview(ctx, snap, unit, modelID)
		}
	case domain.TaskOutline:
		ids := run.Spec.ExtraContextIDs
		if len(ids) == 0 && run.Spec.UnitID != "" {
			ids = []string{run.Spec.UnitID}
		}
		vnum := 1
		if run.Spec.VolumeID != "" {
			vnum, _ = strconv.Atoi(strings.TrimLeft(strings.TrimPrefix(run.Spec.VolumeID, "v"), "0"))
			if vnum == 0 {
				vnum = 1
			}
		}
		if run.Spec.UnitID != "" {
			if u, ok := findUnit(snap, run.Spec.UnitID); ok {
				vnum = u.Volume
			}
		}
		res, err = runner.RunOutline(ctx, snap, vnum, ids, run.Spec.Instruction, modelID)
	case domain.TaskIdea:
		res, err = runner.RunIdeaExpand(ctx, snap, run.Spec.Instruction, modelID)
	case domain.TaskPlan:
		mode := strings.TrimSpace(run.Spec.Mode)
		if mode == "" {
			if books.HasBookOutline(snap) {
				mode = domain.PlanModeVolume
			} else {
				mode = domain.PlanModeInit
			}
		}
		vnum := 1
		if run.Spec.VolumeID != "" {
			vnum, _ = strconv.Atoi(strings.TrimLeft(strings.TrimPrefix(run.Spec.VolumeID, "v"), "0"))
			if vnum == 0 {
				vnum = 1
			}
		} else if mode == domain.PlanModeVolume && len(snap.Volumes) > 0 {
			vnum = snap.Volumes[len(snap.Volumes)-1].Volume
			allDone := true
			for _, u := range snap.Units {
				if u.Volume == vnum && u.Status != domain.UnitReviewed && u.Status != domain.UnitFinalized {
					allDone = false
					break
				}
			}
			if allDone && len(snap.Units) > 0 {
				vnum++
			}
		}
		res, err = runner.RunPlanMode(ctx, snap, vnum, modelID, mode, run.Spec.Instruction, run.Spec.PreservePaths)
	case domain.TaskReview:
		unit, ok := findUnit(snap, run.Spec.UnitID)
		if !ok {
			err = fmt.Errorf("unit not found")
		} else {
			res, err = runner.RunReview(ctx, snap, unit, modelID, pipeline.ReviewOptions{
				Concurrency: s.Settings.Get().Review.Concurrency,
				Rounds:      s.Settings.Get().Review.Rounds,
			})
		}
	case domain.TaskLocalEdit, domain.TaskRewriteSel, domain.TaskExpand, domain.TaskPolish, domain.TaskContinue:
		if run.Spec.Selection == nil {
			err = fmt.Errorf("selection required")
		} else {
			res, err = runner.RunLocalEdit(ctx, snap, *run.Spec.Selection, run.Spec.Instruction, modelID)
		}
	case domain.TaskRevise:
		unit, ok := findUnit(snap, run.Spec.UnitID)
		if !ok {
			err = fmt.Errorf("unit not found")
		} else {
			// findings selected from open review candidates
			var findings []domain.ReviewFinding
			open, _ := s.Store.ListOpenCandidates(ctx, snap.Summary.ID)
			for _, c := range open {
				if c.Kind == domain.TaskReview && c.UnitID == unit.UnitID {
					for _, f := range c.Findings {
						if len(run.Spec.IssueIDs) == 0 || contains(run.Spec.IssueIDs, f.ID) {
							findings = append(findings, f)
						}
					}
				}
			}
			res, err = runner.RunRevise(ctx, snap, unit, findings, modelID)
		}
	case domain.TaskFinalize:
		unit, ok := findUnit(snap, run.Spec.UnitID)
		if !ok {
			err = fmt.Errorf("unit not found")
		} else if !unit.HasProse {
			err = fmt.Errorf("单元尚无正文，无法定稿")
		} else {
			open, _ := s.Store.ListOpenCandidates(ctx, snap.Summary.ID)
			var reviewFindings []domain.ReviewFinding
			for _, candidate := range open {
				if candidate.Kind == domain.TaskReview && candidate.UnitID == unit.UnitID {
					reviewFindings = append(reviewFindings, candidate.Findings...)
				}
			}
			res, err = runner.RunFinalize(ctx, snap, unit, reviewFindings, modelID)
		}
	case domain.TaskStructureReview:
		res, err = runner.ReviewStructure(ctx, snap, run.Spec, modelID)
	case domain.TaskRewriteEntity:
		if run.Spec.TargetPath == "" {
			err = fmt.Errorf("targetPath required for rewrite_entity")
		} else if run.Spec.EntityKind == "structure_repair" {
			var findings []domain.ReviewFinding
			open, _ := s.Store.ListOpenCandidates(ctx, snap.Summary.ID)
			for _, c := range open {
				if c.Kind == domain.TaskStructureReview && c.TargetPath == run.Spec.TargetPath {
					for _, f := range c.Findings {
						if len(run.Spec.IssueIDs) == 0 || contains(run.Spec.IssueIDs, f.ID) {
							findings = append(findings, f)
						}
					}
				}
			}
			res, err = runner.RepairStructure(ctx, snap, run.Spec, findings, modelID)
		} else {
			res, err = runner.RunRewriteEntity(ctx, snap, run.Spec, modelID)
		}
	default:
		err = fmt.Errorf("unsupported task kind: %s", run.Kind)
	}
	// 规则解析失败优先于任务结果：跑出来的结论用的是错规则，不可信。
	if rulesErr != nil {
		err = rulesErr
	}

	run.UpdatedAt = time.Now().UTC()
	run.Calls = res.Calls
	run.CallsUsed = len(res.Calls)
	run.Policy = res.Packet.Policy
	run.Warnings = res.Warnings
	for _, b := range res.Packet.Blocks {
		run.ContextSummary = append(run.ContextSummary, b.Label)
	}
	if ctx.Err() != nil {
		run.Status = domain.RunCancelled
		run.Error = "cancelled"
		_ = s.Store.SaveRun(context.Background(), run)
		return
	}
	if err != nil {
		run.Status = domain.RunFailed
		run.Error = err.Error()
		_ = s.Store.SaveRun(context.Background(), run)
		// 部分完成：仍把已审出的结论落成候选，作者不必从头再跑。
		if res.Candidate.Kind != "" && len(res.Candidate.Findings) > 0 {
			res.Candidate.RunID = run.ID
			_ = s.Store.SaveCandidate(context.Background(), res.Candidate)
			run.CandidateID = res.Candidate.ID
			_ = s.Store.SaveRun(context.Background(), run)
		}
		return
	}
	res.Candidate.RunID = run.ID
	_ = s.Store.SaveCandidate(context.Background(), res.Candidate)
	run.CandidateID = res.Candidate.ID
	run.Status = domain.RunWaiting
	_ = s.Store.SaveRun(context.Background(), run)
}

// PreviewPacket builds (without calling the LLM) the context packet a task
// would send. Used by the confirm dialog to show exactly what goes to the model.
// PreviewPacket builds the packet the model would actually receive.
//
// 知识文章在这之前是漏的：真实调用每个生成路径都会把作者勾选的参考知识
// 追加进 references（write/outline/plan/idea/revise/outline_review 全走
// Runner.withKnowledge），但预览只给 review 和 finalize 补了这一手。结果是
// 作者在"预览"里看到的 prompt 偏小、estPromptTokens 偏低，正好在作者拿预览
// 判断成本的时候给出一个错的数字。唯一不追加的是结构审校（它另走一条
// 不经过 withKnowledge 的路径），所以这里也按同一判据排除。
func (s *Service) PreviewPacket(ctx context.Context, spec domain.TaskSpec) (domain.ContextPacket, error) {
	packet, err := s.previewPacket(ctx, spec)
	if err != nil {
		return packet, err
	}
	if spec.Kind == domain.TaskStructureReview {
		return packet, nil
	}
	if docs := s.loadKnowledge(spec.KnowledgeIDs); len(docs) > 0 {
		packet.UserPrompt = contextsvc.MergeKnowledgeIntoUserPayload(packet.UserPrompt, docs)
		packet.EstPromptTokens = (len([]rune(packet.SystemPrompt)) + len([]rune(packet.UserPrompt))) / 2
	}
	return packet, nil
}

func (s *Service) previewPacket(_ context.Context, spec domain.TaskSpec) (domain.ContextPacket, error) {
	root, readOnly, err := s.Store.GetBookRoot(context.Background(), spec.BookID)
	if err != nil {
		return domain.ContextPacket{}, err
	}
	snap, err := books.LoadBook(root, readOnly)
	if err != nil {
		return domain.ContextPacket{}, err
	}
	switch spec.Kind {
	case domain.TaskWrite:
		unit, ok := findUnit(snap, spec.UnitID)
		if !ok {
			return domain.ContextPacket{}, fmt.Errorf("unit not found: %s", spec.UnitID)
		}
		// 用真实首章包，而不是基础包：预览要能让作者核对"上一章尾部带没带"。
		// 基础包只含 documents + 通用 instruction，按章变化的那几项全在 extra 里。
		return contextsvc.WritePreviewPacketFor(snap, unit, spec.Instruction, spec.RewriteMode), nil
	case domain.TaskOutlineReview:
		unit, ok := findUnit(snap, spec.UnitID)
		if !ok {
			return domain.ContextPacket{}, fmt.Errorf("unit not found")
		}
		return contextsvc.BuildOutlineReviewPacket(snap, unit), nil
	case domain.TaskOutline:
		vnum := 1
		if spec.VolumeID != "" {
			vnum, _ = strconv.Atoi(strings.TrimLeft(strings.TrimPrefix(spec.VolumeID, "v"), "0"))
		}
		if spec.UnitID != "" {
			if u, ok := findUnit(snap, spec.UnitID); ok {
				vnum = u.Volume
			}
		}
		ids := spec.ExtraContextIDs
		if len(ids) == 0 && spec.UnitID != "" {
			ids = []string{spec.UnitID}
		}
		return contextsvc.BuildOutlinePacket(snap, vnum, ids, spec.Instruction), nil
	case domain.TaskIdea:
		return contextsvc.BuildIdeaPacket(snap, spec.Instruction), nil
	case domain.TaskPlan:
		mode := strings.TrimSpace(spec.Mode)
		if mode == "" {
			if books.HasBookOutline(snap) {
				mode = domain.PlanModeVolume
			} else {
				mode = domain.PlanModeInit
			}
		}
		vnum := 1
		if spec.VolumeID != "" {
			vnum, _ = strconv.Atoi(strings.TrimLeft(strings.TrimPrefix(spec.VolumeID, "v"), "0"))
			if vnum == 0 {
				vnum = 1
			}
		} else if mode == domain.PlanModeVolume && len(snap.Volumes) > 0 {
			vnum = snap.Volumes[len(snap.Volumes)-1].Volume
		}
		packet := contextsvc.BuildPlanPacketWithMode(snap, vnum, mode, spec.Instruction)
		return s.Pipeline.ResolvePlanPreview(snap, mode, packet)
	case domain.TaskReview:
		unit, ok := findUnit(snap, spec.UnitID)
		if !ok {
			return domain.ContextPacket{}, fmt.Errorf("unit not found")
		}
		chapters := gates.ParseProseChapters(unit.ProseRaw)
		if len(chapters) == 0 {
			return domain.ContextPacket{}, fmt.Errorf("no chapters to review")
		}
		rules, err := s.loadDeslopRules(snap.Summary.RootPath)
		if err != nil {
			return domain.ContextPacket{}, err
		}
		local := gates.CheckProseWithRules(chapters, books.ChapterTarget(unit), rules)
		local.Findings = append(local.Findings, books.IdentityDeltaFindings(snap.IdentitySegments, unit, unit.OutlineRaw)...)
		local.Findings = append(local.Findings, s.Pipeline.StaleCharacterFindings(snap, unit)...)
		baseline := contextsvc.BuildReviewBaseline(snap, unit, local.Findings, s.loadReviewChecklist())
		packet := contextsvc.BuildChapterReviewPacket(snap, unit, chapters[0], baseline.PrevTail, baseline)
		rounds := s.Settings.Get().Review.Rounds
		if rounds < 1 {
			rounds = 1
		}
		packet.Policy.MaxCalls = len(chapters) * rounds * 2
		// 参考知识统一在 PreviewPacket 外层追加，避免两处各加一遍变成重复引用。
		return packet, nil
	case domain.TaskFinalize:
		unit, ok := findUnit(snap, spec.UnitID)
		if !ok {
			return domain.ContextPacket{}, fmt.Errorf("unit not found: %s", spec.UnitID)
		}
		if !unit.HasProse {
			return domain.ContextPacket{}, fmt.Errorf("单元尚无正文，无法定稿")
		}
		// 预览必须是"真正会发出去的那一份"：定稿先走整单元一次核对，所以这里
		// 也展示整单元包，而不是旧的单元级概要包。逐章兜底是失败后的退路，预览
		// 不展示它（它不是默认路径）。
		chapters := gates.ParseProseChapters(unit.ProseRaw)
		if len(chapters) == 0 {
			return domain.ContextPacket{}, fmt.Errorf("单元正文为空，无法定稿")
		}
		baseline := contextsvc.BuildReviewBaseline(snap, unit, nil, s.loadReviewChecklist())
		packet := contextsvc.BuildFinalizeUnitPacket(snap, unit, chapters, baseline)
		return packet, nil
	case domain.TaskStructureReview:
		return s.Pipeline.PreviewStructuredTask(snap, spec)
	case domain.TaskRewriteEntity:
		if spec.EntityKind == "structure_repair" {
			return s.Pipeline.PreviewStructuredTask(snap, spec)
		}
		if spec.TargetPath == "" {
			return domain.ContextPacket{}, fmt.Errorf("targetPath required")
		}
		return contextsvc.BuildRewriteEntityPacket(snap, spec), nil
	default:
		return domain.ContextPacket{}, fmt.Errorf("preview not supported for kind: %s", spec.Kind)
	}
}

func (s *Service) Cancel(ctx context.Context, runID string) error {
	s.mu.Lock()
	cancel, ok := s.cancels[runID]
	s.mu.Unlock()
	if ok {
		cancel()
	}
	run, err := s.Store.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status == domain.RunRunning || run.Status == domain.RunPending {
		run.Status = domain.RunCancelled
		run.UpdatedAt = time.Now().UTC()
		return s.Store.SaveRun(ctx, run)
	}
	return nil
}

// RecoverInterrupted fails runs left pending/running by a previous process.
// Call it once at startup, before serving requests: execute() runs in-process,
// so after a restart nothing will ever advance those rows, the UI polls them
// forever, and FindActiveRun keeps rejecting new tasks for the same
// book/kind/unit.
func (s *Service) RecoverInterrupted(ctx context.Context) (int, error) {
	list, err := s.Store.ListUnfinishedRuns(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, run := range list {
		s.mu.Lock()
		_, live := s.cancels[run.ID]
		s.mu.Unlock()
		if live {
			continue
		}
		run.Status = domain.RunFailed
		run.Error = "服务重启导致任务中断，请重新发起"
		run.UpdatedAt = time.Now().UTC()
		if err := s.Store.SaveRun(ctx, run); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// touchRun 记录一次进度心跳（调用数 + 更新时间）。
//
// 只写这两个字段，不覆盖别的：run 的其余字段此刻还只在内存里（execute 结束时
// 才一起写回），所以这里必须重新读一次再改，避免把已经开始执行的事实冲掉。
func (s *Service) touchRun(ctx context.Context, run domain.Run, callsUsed int) {
	cur, err := s.Store.GetRun(ctx, run.ID)
	if err != nil {
		return
	}
	if cur.Status != domain.RunRunning && cur.Status != domain.RunPending {
		// 已经被取消/回收：不要再把状态改回 running。
		return
	}
	cur.CallsUsed = callsUsed
	cur.UpdatedAt = time.Now().UTC()
	_ = s.Store.SaveRun(ctx, cur)
}

// ReapOrphans 回收「状态还写着运行中、但本进程里已经没有任何 goroutine 在推进」
// 的 run。
//
// 判据只有一条：`s.cancels` 里有没有它。execute 在结束时必然执行 defer 删除该
// 条目，所以不在表里的 running 必然是孤儿（进程内没有任何东西会再改它）。它留着
// 的后果是 blockingRun 会让同一单元的所有新任务都被拒，只能靠重启进程解开——
// 这条路径以前就真实发生过几次。
//
// 注意不能按 updatedAt 的年龄判：心跳之前的实现里 updatedAt 全程不变，一个合法
// 的长任务（6 章正文 ≈ 40 分钟）和死任务看起来一模一样。用内存里的活体表才准。
func (s *Service) ReapOrphans(ctx context.Context) (int, error) {
	list, err := s.Store.ListUnfinishedRuns(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, run := range list {
		// 给启动留一点余量：startLocked 登记 cancels 与这里读表可能同拍。
		if time.Since(run.CreatedAt) < 30*time.Second {
			continue
		}
		s.mu.Lock()
		_, live := s.cancels[run.ID]
		s.mu.Unlock()
		if live {
			continue
		}
		run.Status = domain.RunFailed
		run.Error = "任务在执行中途失去推进（进程内已无对应执行体），已自动回收；请重新发起"
		run.UpdatedAt = time.Now().UTC()
		if err := s.Store.SaveRun(ctx, run); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Service) Accept(ctx context.Context, cmd domain.AcceptCommand) (domain.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acceptLocked(ctx, cmd)
}

func (s *Service) acceptLocked(ctx context.Context, cmd domain.AcceptCommand) (domain.Candidate, error) {
	cand, err := s.Store.GetCandidate(ctx, cmd.CandidateID)
	if err != nil {
		return domain.Candidate{}, err
	}
	if cand.Status != domain.CandidateOpen {
		return domain.Candidate{}, fmt.Errorf("candidate not open: %s", cand.Status)
	}
	root, readOnly, err := s.Store.GetBookRoot(ctx, cand.BookID)
	if err != nil {
		return domain.Candidate{}, err
	}
	if readOnly {
		return domain.Candidate{}, fmt.Errorf("book is read-only; enable writeback/migration first")
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		return domain.Candidate{}, err
	}
	// 审校候选只是问题清单，接受=归档确认（不写盘）。正文被修订后旧结论仍然可归档，
	// 所以不校验 baseRevision；否则「定向修订→接受」之后旧审校候选会报过期错误、
	// 只能靠拒绝清掉，等于分不清「已复核」和「被丢弃」。
	if cand.Kind != domain.TaskReview && cand.Kind != domain.TaskStructureReview && cand.Kind != domain.TaskOutlineReview {
		if cmd.BaseRevision != "" && cmd.BaseRevision != snap.Revision {
			s.expireCandidate(ctx, cand, "书籍在生成后被修改，候选已过期")
			return domain.Candidate{}, fmt.Errorf("baseRevision expired; re-compare and regenerate if needed")
		}
		if cand.BaseRevision != snap.Revision {
			s.expireCandidate(ctx, cand, "书籍在生成后被修改，候选已过期")
			return domain.Candidate{}, fmt.Errorf("book changed since generation")
		}
	}

	switch cand.Kind {
	case domain.TaskWrite, domain.TaskRevise:
		if len(cand.Chapters) == 0 {
			return domain.Candidate{}, fmt.Errorf("cannot accept empty chapters")
		}
		empty := 0
		for _, ch := range cand.Chapters {
			if strings.TrimSpace(ch.Body) == "" {
				empty++
			}
		}
		if empty == len(cand.Chapters) {
			return domain.Candidate{}, fmt.Errorf("cannot accept all-empty chapters")
		}
		// 这里曾经先调一次 books.WriteUnitProse 把正文写盘，再做后面的校验。
		// 于是「被拒绝的接受」照样改了磁盘：一次带阻断门禁的候选会先覆盖正文，
		// 然后返回错误、状态不推进、候选留在 open。后果有两层：
		//   1. 单元变成「有正文但状态还是 accepted」，下一次运行看到 hasProse 就
		//      跳过写作，而那条 open 候选又挡住了审校——流水线永久卡死。
		//   2. 修订候选被拒时，已定稿的正文其实已经被换掉了，属于静默数据损坏。
		// 校验全部前置；落盘只走下面的 RenderProseAndStatus + AtomicWriteFiles，
		// 那里本来就把正文与状态一起原子写入，不需要任何提前落盘。
		unit, ok := findUnit(snap, cand.UnitID)
		validWrite := ok && (unit.Status == domain.UnitAccepted || unit.Status == domain.UnitDrafted || unit.Status == domain.UnitReviewed || unit.Status == domain.UnitFinalized)
		validRevise := ok && (unit.Status == domain.UnitDrafted || unit.Status == domain.UnitReviewed || unit.Status == domain.UnitFinalized)
		if (cand.Kind == domain.TaskWrite && !validWrite) || (cand.Kind == domain.TaskRevise && !validRevise) {
			return domain.Candidate{}, fmt.Errorf("细纲未接受或单元状态已变化，不能写正文")
		}
		if err := pipeline.ValidateAcceptedChapters(unit, cand.Chapters); err != nil {
			return domain.Candidate{}, err
		}
		if !cmd.ForceRiskAccept {
			for _, f := range cand.Findings {
				if f.Severity == "blocking" {
					return domain.Candidate{}, fmt.Errorf("正文仍有阻断门禁：%s", f.Title)
				}
			}
		}
		status := domain.UnitDrafted
		if cand.Kind == domain.TaskRevise {
			status = domain.UnitReviewed
		}
		writes, err := books.RenderProseAndStatus(root, unit, cand.Chapters, status)
		if err != nil {
			return domain.Candidate{}, err
		}
		if err := books.AtomicWriteFiles(root, writes); err != nil {
			return domain.Candidate{}, err
		}
		if cand.Kind == domain.TaskRevise {
			s.archiveReviewsForProseChange(ctx, cand.BookID, cand.UnitID)
		}
		// 修订落盘后，把这次真正修掉的问题从审校清单里摘掉。
		//
		// 不摘的后果就是清单永远是原来那 81 条：已修的 5 条还留在里面，作者再点
		// 「自动修复」会把它们再修一遍，而「还剩多少要修」这个数字永远不动。
		// 只摘本次 IssueIDs 命中的条目：没勾的那些仍未修，必须继续可操作。
		for _, ch := range cand.CanonChanges {
			if ch.Locked && !cmd.ForceRiskAccept {
				continue // locked canon needs explicit forceRiskAccept
			}
			if ch.Path != "" {
				full := filepath.Join(root, ch.Path)
				_ = os.MkdirAll(filepath.Dir(full), 0o755)
				_ = os.WriteFile(full, []byte(ch.After), 0o644)
			}
		}
	case domain.TaskOutline:
		// Accept 必须复验完整契约；结构错误不属于可强制承担的语义风险。
		var ids []string
		for _, u := range cand.UnitOutlines {
			ids = append(ids, u.UnitID)
			report := gates.CheckUnitOutline(u, nil)
			if !report.OK {
				return domain.Candidate{}, fmt.Errorf("细纲 %s 结构未通过：%s", u.UnitID, report.Findings[0].Title)
			}
		}
		if err := pipeline.ValidateOutlineContract(cand.UnitOutlines, ids); err != nil {
			return domain.Candidate{}, err
		}
		for _, f := range cand.Findings {
			if f.Severity == "blocking" && f.Source == "local_gate" {
				return domain.Candidate{}, fmt.Errorf("细纲仍有结构阻断问题：%s", f.Title)
			}
		}
		// 卷号只认候选自带的 volumeId。以前用 findUnit 反推，单元不在盘上时
		// vol=0，父子合同校验会去读 v00 并不存在而**静默跳过整条继承校验**。
		vol := volumeFromTag(cand.VolumeID)
		if vol < 1 && len(cand.UnitOutlines) > 0 {
			if u, ok := findUnit(snap, cand.UnitOutlines[0].UnitID); ok {
				vol = u.Volume
			}
		}
		if vol < 1 {
			return domain.Candidate{}, fmt.Errorf("无法确定细纲所属卷（volumeId=%q），拒绝接受以免绕过父子合同；请重新生成本单元细纲", cand.VolumeID)
		}
		if err := pipeline.ValidateOutlineInheritance(snap, vol, cand.UnitOutlines); err != nil {
			return domain.Candidate{}, err
		}
		// Accept 复验场面归属与章覆盖：生成时拦过，但候选可能绕过（旧缓存、
		// 并发 Accept），结构底线必须在写盘前再确认一遍。
		if err := pipeline.ValidateOutlineScenes(cand.UnitOutlines); err != nil {
			return domain.Candidate{}, err
		}
		for _, f := range books.ValidateClockContinuity(clockUnitsWithCandidate(snap, vol, cand.UnitOutlines), vol) {
			if f.Severity == "blocking" && !cmd.ForceRiskAccept {
				return domain.Candidate{}, fmt.Errorf("剧情时钟冲突：%s（%s）", f.Title, f.Evidence)
			}
		}
		// 单元内部身份恒定：合同声明身份转换则必须拆单元。
		priorStates, _ := books.LoadState(root)
		var assignments []domain.ForeshadowAssignment
		for _, u := range cand.UnitOutlines {
			for _, ch := range u.Chapters {
				for _, id := range ch.Plant {
					assignments = append(assignments, domain.ForeshadowAssignment{ID: id, PlantUnit: u.UnitID, PlantChapter: ch.Chapter})
				}
				for _, id := range ch.Payoff {
					assignments = append(assignments, domain.ForeshadowAssignment{ID: id, PayoffUnit: u.UnitID, PayoffChapter: ch.Chapter})
				}
			}
		}
		foreshadowRaw, err := books.RenderForeshadowAssignments(root, assignments)
		if err != nil {
			return domain.Candidate{}, fmt.Errorf("细纲伏笔章级细化失败：%w", err)
		}
		writes := map[string][]byte{}
		if len(assignments) > 0 {
			writes[books.ForeshadowSchedulePath] = foreshadowRaw
		}
		for _, u := range cand.UnitOutlines {
			// 单元还没落盘时也要校验身份：身份段的判定只依赖 unitId，不是文件。
			// 以前整段包在 findUnit 成功分支里，单元不在盘上就静默跳过。
			current, ok := findUnit(snap, u.UnitID)
			if !ok {
				current = domain.UnitInfo{UnitID: u.UnitID, Volume: vol}
			}
			if err := books.ValidateIdentityCoverage(snap.IdentitySegments, snap.Cast, current, u.OnStage); err != nil {
				return domain.Candidate{}, err
			}
			// 连续性词表与生命周期：未识别的 field 名在落盘前就要拦下，
			// 否则它会被 StateFromOutline 静默跳过（过去 70% 的写入就是这样丢的）。
			prior, _ := books.StateAt(priorStates, u.UnitID)
			if err := books.ValidateStateDeltas(snap.Cast, current, serializeUnitYAML(u), prior, u.OnStage); err != nil {
				return domain.Candidate{}, err
			}
			if u.StateDeltas == nil {
				return domain.Candidate{}, fmt.Errorf("单元 %s 缺少结构化 stateDeltas，定稿无法回写状态迁移", u.UnitID)
			}
			// 落盘文本只有一份，由结构体字段序列化而来：校验和写盘必须针对
			// 同一份内容，否则「验过的」和「写下去的」可以不是一回事。
			raw := serializeUnitYAML(u)
			// 单元内部身份恒定由 state_deltas 的结构化比对来保证（见下）。
			// 关键词扫全文的旧检查已删除：它会把「不推晋升」这类合法口径判成硬伤。
			// state_deltas 与身份时间线的一致性必须在细纲落盘时就查：
			// 这是纯本地判定，而定稿才发现意味着正文已经写完、只能返工规划。
			if err := books.ValidateStateDeltasIdentity(snap.IdentitySegments, snap.Cast, current, raw); err != nil {
				return domain.Candidate{}, fmt.Errorf("细纲的 state_deltas 与身份时间线冲突，不能接受：%w", err)
			}
			// 细纲只拥有逐章合同与场面；揭示边界、故事线归属、前因后果这些
			// 父级字段必须从卷纲种子回填，否则接受一次就永久丢失。
			raw = books.CarrySeedOwnedFields(snap, vol, u.UnitID, raw)
			writes["outline/units/"+u.UnitID+".yaml"] = []byte(raw)
			// 临时人物机制：on_stage 新 stem 播种 candidate 卡（不自动晋升 canon）。
			for _, stem := range u.OnStage {
				rel, body, create, err := books.CandidateCastFile(root, stem)
				if err != nil {
					return domain.Candidate{}, err
				}
				if create {
					writes[rel] = body
				}
			}
		}
		if err := books.AtomicWriteFiles(root, writes); err != nil {
			return domain.Candidate{}, err
		}
	case domain.TaskIdea:
		// 创意提案只写 outline/idea.md：它是作者意图，不是 canon。
		if strings.TrimSpace(cand.Replacement) == "" {
			return domain.Candidate{}, fmt.Errorf("missing idea proposal")
		}
		if cand.TargetPath == "" {
			cand.TargetPath = domain.IdeaPath
		}
		if cand.TargetPath != domain.IdeaPath {
			return domain.Candidate{}, fmt.Errorf("创意扩写只能写 %s", domain.IdeaPath)
		}
		full := filepath.Join(root, filepath.FromSlash(cand.TargetPath))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return domain.Candidate{}, err
		}
		if err := os.WriteFile(full, []byte(cand.Replacement), 0o644); err != nil {
			return domain.Candidate{}, err
		}
		// 有提案才允许结构化立项；next_action 直接指向 plan。
		if err := books.UpdateState(root, map[string]string{"stage": domain.StageIdeaExpanded, "next_action": "plan"}); err != nil {
			return domain.Candidate{}, err
		}
	case domain.TaskPlan:
		if !cmd.ForceRiskAccept {
			for _, f := range cand.Findings {
				if f.Severity == "blocking" {
					return domain.Candidate{}, fmt.Errorf("结构审校仍有阻断项：%s；请先修复", f.Title)
				}
			}
		}
		if run, rerr := s.Store.GetRun(ctx, cand.RunID); rerr == nil {
			if cand.Mode == "" {
				cand.Mode = run.Spec.Mode
			}
			if len(cand.PreservePaths) == 0 && len(run.Spec.PreservePaths) > 0 {
				cand.PreservePaths = append([]string(nil), run.Spec.PreservePaths...)
			}
		}
		if err := books.AcceptPlan(root, cand); err != nil {
			return domain.Candidate{}, fmt.Errorf("accept plan: %w", err)
		}
	case domain.TaskLocalEdit, domain.TaskRewriteSel, domain.TaskExpand, domain.TaskPolish, domain.TaskContinue:
		if cand.TargetPath == "" {
			return domain.Candidate{}, fmt.Errorf("missing target path")
		}
		full := filepath.Join(root, cand.TargetPath)
		raw, err := os.ReadFile(full)
		if err != nil {
			return domain.Candidate{}, err
		}
		// best-effort: if selection text present in run, replace once
		run, _ := s.Store.GetRun(ctx, cand.RunID)
		if run.Spec.Selection != nil && run.Spec.Selection.Text != "" {
			updated := strings.Replace(string(raw), run.Spec.Selection.Text, cand.Replacement, 1)
			if err := os.WriteFile(full, []byte(updated), 0o644); err != nil {
				return domain.Candidate{}, err
			}
		}
	case domain.TaskRewriteEntity:
		// 结构修复后仍带 blocking 的结果不是可接受候选；作者可另行重写或直改。
		if !cmd.ForceRiskAccept {
			for _, f := range cand.Findings {
				if f.Severity == "blocking" {
					return domain.Candidate{}, fmt.Errorf("结构审校仍有阻断项：%s；请先修复", f.Title)
				}
			}
		}
		// 整文件规范化重写，接受才落盘。
		if cand.TargetPath == books.IdentityTimelinePath {
			var file struct {
				Segments []domain.IdentitySegment `yaml:"segments"`
			}
			if err := yaml.Unmarshal([]byte(cand.Replacement), &file); err != nil {
				return domain.Candidate{}, fmt.Errorf("身份时间线格式错误: %w", err)
			}
			if err := books.ValidateIdentityTimeline(file.Segments, snap.Units); err != nil {
				return domain.Candidate{}, err
			}
			for _, unit := range snap.Units {
				if !unit.HasProse {
					continue
				}
				for _, stem := range books.OnStageFromOutline(unit.OutlineRaw) {
					oldIdentity := books.IdentityAt(snap.IdentitySegments, stem, unit.UnitID)
					newIdentity := books.IdentityAt(file.Segments, stem, unit.UnitID)
					if !cmd.ForceRiskAccept && ((oldIdentity == nil) != (newIdentity == nil) || (oldIdentity != nil && newIdentity != nil && (oldIdentity.RoleTitle != newIdentity.RoleTitle || oldIdentity.Org != newIdentity.Org))) {
						return domain.Candidate{}, fmt.Errorf("%s 已有正文，身份候选需先进行影响确认", unit.UnitID)
					}
				}
			}
		}
		if cand.TargetPath == "" || strings.TrimSpace(cand.Replacement) == "" {
			return domain.Candidate{}, fmt.Errorf("missing rewrite content")
		}
		if err := books.ValidateStructuredOutlineFile(snap, cand.TargetPath, cand.Replacement); err != nil {
			return domain.Candidate{}, fmt.Errorf("accept outline: %w", err)
		}
		full := filepath.Join(root, cand.TargetPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return domain.Candidate{}, err
		}
		if err := os.WriteFile(full, []byte(cand.Replacement), 0o644); err != nil {
			return domain.Candidate{}, err
		}
	case domain.TaskReview, domain.TaskStructureReview, domain.TaskOutlineReview:
		// 接受审校候选只做归档：问题清单不外写、不推进单元状态。
		// 真正改正文的是 review → revise → 接受 revise 候选。
	case domain.TaskFinalize:
		if len(cand.ActualChapters) != len(cand.Chapters) {
			return domain.Candidate{}, fmt.Errorf("定稿缺少逐章实际账本核对，不能直接接受")
		}
		// 定稿前再校验剧情时钟（防止接受了带时钟冲突的细纲一路写到定稿）。
		if !cmd.ForceRiskAccept {
			if unit, ok := findUnit(snap, cand.UnitID); ok {
				for _, f := range books.ValidateClockContinuity(snap.Units, unit.Volume) {
					if f.Severity == "blocking" {
						return domain.Candidate{}, fmt.Errorf("剧情时钟冲突，须先修正：%s（%s）", f.Title, f.Evidence)
					}
				}
			}
			for _, f := range cand.Findings {
				if f.Severity == "blocking" {
					return domain.Candidate{}, fmt.Errorf("blocking gate remains: %s; revise or forceRiskAccept", f.Title)
				}
			}
		}
		unit, ok := findUnit(snap, cand.UnitID)
		if !ok {
			return domain.Candidate{}, fmt.Errorf("unit not found: %s", cand.UnitID)
		}
		if err := books.ValidateFinalizedIdentity(snap.IdentitySegments, snap.Cast, unit, unit.OutlineRaw); err != nil {
			return domain.Candidate{}, err
		}
		// 唯一写回路径：state.yaml 全量快照（人物状态 + 伏笔 loops + 时钟）、
		// 卷摘要、定稿账本。facts.md 已退役，不再写入。
		if err := books.ApplyFinalizationLedger(root, unit, cand.Chapters, cand.ActualChapters, cand.Findings, snap.IdentitySegments); err != nil {
			return domain.Candidate{}, err
		}
		for _, ch := range cand.CanonChanges {
			if ch.Locked && !cmd.ForceRiskAccept {
				continue
			}
			if ch.Path != "" {
				full := filepath.Join(root, ch.Path)
				_ = os.MkdirAll(filepath.Dir(full), 0o755)
				_ = os.WriteFile(full, []byte(ch.After), 0o644)
			}
		}
	}

	now := time.Now().UTC()
	cand.Status = domain.CandidateAccepted
	cand.AcceptedAt = &now
	run, runErr := s.Store.GetRun(ctx, cand.RunID)
	if runErr == nil {
		run.Status = domain.RunSucceeded
		run.UpdatedAt = now
	}
	if err := s.Store.CompleteAcceptance(ctx, cand, run); err != nil {
		return domain.Candidate{}, err
	}

	// 交付流程到此结束：审校（多轮取并集）→ 勾选 → 一次修订 → diff → 落盘。
	// 落盘之后不再自动起审校——实测（同一单元连续 6 轮）复审报出的"新问题"有
	// 96%+ 落在这次根本没改过的原文上，是前几轮没扫到的旧账，不是修复引入的
	// 回归。真要重来就重新走一遍完整审校（重新审校会先归档旧清单）。
	_ = root
	return cand, nil
}

// ReviewGate reports the combined verdict of a unit's open review lists and how
// many lists are open. 没有清单时返回 ("", 0)。
//
// 用于把"下一步"从审校推进到定稿：一份 0 阻断、每章都审完的清单不该让作者
// 再点一次审校——那会重跑一遍完整审校，结论几乎必然不同，然后又回到"要不要
// 再修一轮"的循环。
func (s *Service) ReviewGate(ctx context.Context, bookID, unitID string) (string, int) {
	open, err := s.Store.ListOpenCandidates(ctx, bookID)
	if err != nil {
		return "", 0
	}
	verdict := ""
	n := 0
	for _, c := range open {
		if c.Kind != domain.TaskReview || c.UnitID != unitID {
			continue
		}
		n++
		switch domain.ReviewVerdict(c.Findings) {
		case domain.ReviewBlocking:
			// 任一清单有阻断，整体就是 blocking，后面的不用再看。
			return domain.ReviewBlocking, n
		case domain.ReviewUnfinished:
			verdict = domain.ReviewUnfinished
		default:
			if verdict == "" {
				verdict = domain.ReviewClean
			}
		}
	}
	return verdict, n
}

// archiveUnitReviews archives every open review list of one unit.
//
// 唯一用途是「重新审校前先归档旧清单」：不归档的话那条 waiting_accept 的 run
// 会一直占着单元，新的审校永远起不来。
//
// 归档必须走 Accept 而不是直接改状态：Accept 会顺带把对应 run 从 waiting_accept
// 收尾，否则 run 会继续挡住同一单元的下一次审校。
//
// 「通过清单」这个动作已经没有了：审校面板只有「自动修复」和「拒绝」两个出口，
// 定稿是写作页的独立步骤。所以这里不再判 verdict，也不再顺带起定稿。
func (s *Service) archiveUnitReviews(ctx context.Context, bookID, unitID string) error {
	open, err := s.Store.ListOpenCandidates(ctx, bookID)
	if err != nil {
		return err
	}
	for _, c := range open {
		if c.Kind != domain.TaskReview || c.UnitID != unitID {
			continue
		}
		if _, err := s.acceptLocked(ctx, domain.AcceptCommand{CandidateID: c.ID}); err != nil {
			return fmt.Errorf("归档 %s 失败: %w", c.ID, err)
		}
	}
	return nil
}

// Review findings expire when their source prose is replaced.
//
// 正文一改，旧清单里的定位（anchor / 引文）就全部失效：按旧正文修，补的是
// 已经不存在的句子。所以落盘一次就整份归档，勾没勾都一样——"哪些还没修"
// 不能靠 IssueIDs 之外的猜测保留下来（IssueIDs 只能说明勾了哪些，不能说明
// 改没改成）。保留下来的 findings 只供历史回看，作者要新的待办就重新审校。
//
// 任何一步失败都只跳过：正文已经落盘，不能让清单整理把这次接受退回去。
func (s *Service) archiveReviewsForProseChange(ctx context.Context, bookID, unitID string) {
	open, err := s.Store.ListOpenCandidates(ctx, bookID)
	if err != nil {
		return
	}
	for _, c := range open {
		if c.Kind != domain.TaskReview || c.UnitID != unitID {
			continue
		}
		// 正文版本已改变，旧定位与结论只供历史回看，不能标成已修复。
		c.Status = domain.CandidateExpired
		_ = s.Store.SaveCandidate(ctx, c)
		s.closeRun(ctx, c.RunID, domain.RunExpired, "正文已修订；旧审校清单已归档，未解决问题需按新正文复核")
	}
}

func (s *Service) Reject(ctx context.Context, candidateID, reason string) error {
	cand, err := s.Store.GetCandidate(ctx, candidateID)
	if err != nil {
		return err
	}
	cand.Status = domain.CandidateRejected
	if err := s.Store.SaveCandidate(ctx, cand); err != nil {
		return err
	}
	// 只有已经产出候选的 run 才需要收尾；被拒绝的候选不能记成"成功"，
	// 否则运行记录会显示一切正常，掩盖这一次返工。
	if run, err := s.Store.GetRun(ctx, cand.RunID); err == nil {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			reason = "候选被拒绝"
		}
		run.Status = domain.RunRejected
		run.Error = reason
		run.UpdatedAt = time.Now().UTC()
		_ = s.Store.SaveRun(ctx, run)
	}
	return nil
}

// blockingRun returns the active run that must be resolved before spec can
// start, if any. 同一单元同时只保留一条活跃调用，否则两份候选会互相覆盖。
//
// 定向修订是唯一的例外：它的输入就是该单元待接受的审校清单（审校页的
// 「自动修复」）。那条 waiting_accept 的审校 run 在界面上没有「接受」这个
// 出口，如果把它当成占用，自动修复对任何有结论的单元都永远起不来。
// 审校还在跑/排队时没有结论可修，仍然算冲突；其他任务类型互不干扰的规则不变。
//
// 顺带自愈：waiting_accept 的 run 只有靠候选择才能收尾。若候选早已终结
// （过期/已拒绝/已接受）而 run 还占着，单元就永久卡死——作者看不到那条
// 候选，也就没有任何按钮能解开。这里补一次收尾再继续判断。
func (s *Service) blockingRun(ctx context.Context, spec domain.TaskSpec) (domain.Run, bool, error) {
	runs, err := s.Store.ListActiveRuns(ctx, spec.BookID, spec.UnitID)
	if err != nil {
		return domain.Run{}, false, err
	}
	for _, active := range runs {
		if s.reapIfCandidateClosed(ctx, active) {
			continue
		}
		// 修订的输入正是这份清单：它不算占用。
		if spec.Kind == domain.TaskRevise && active.Kind == domain.TaskReview && active.Status == domain.RunWaiting {
			continue
		}
		return active, true, nil
	}
	return domain.Run{}, false, nil
}

// reapIfCandidateClosed closes a waiting_accept run whose candidate is no longer
// open (expired / rejected / accepted). Returns true when it closed something.
//
// 这是给历史遗留死锁兜底的一道修复：进程崩在「候选已终结、run 未收尾」的
// 中间态时，数据里会永久留一条占位 run。作者看到的只是「已有一份待处理的
// 结果」，而那条结果在界面上已经不存在。
func (s *Service) reapIfCandidateClosed(ctx context.Context, run domain.Run) bool {
	if run.Status != domain.RunWaiting || run.CandidateID == "" {
		return false
	}
	cand, err := s.Store.GetCandidate(ctx, run.CandidateID)
	if err != nil {
		// 候选记录消失（被清理）也算无人认领。
		s.closeRun(ctx, run.ID, domain.RunExpired, "候选已不存在，运行占位已释放")
		return true
	}
	if cand.Status == domain.CandidateOpen {
		return false
	}
	status := domain.RunRejected
	reason := "候选已被拒绝"
	switch cand.Status {
	case domain.CandidateExpired:
		status = domain.RunExpired
		reason = "候选已过期（书籍在生成后被修改），运行占位已释放"
	case domain.CandidateAccepted:
		status = domain.RunSucceeded
		reason = ""
	}
	s.closeRun(ctx, run.ID, status, reason)
	return true
}

// expireCandidate closes a candidate whose base no longer matches the book and,
// crucially, closes its run too.
//
// 以前只把候选标成 expired：候选从「待审」列表里消失，而 run 永远停在
// waiting_accept，单元就被永久占住——作者既看不到那条候选，也无法接受或拒绝
// 它，再点生成只会收到「已有一份待处理的细纲结果」。终结候选就必须同时终结
// run，否则终结态只兑现了一半。
func (s *Service) expireCandidate(ctx context.Context, cand domain.Candidate, reason string) {
	cand.Status = domain.CandidateExpired
	_ = s.Store.SaveCandidate(ctx, cand)
	s.closeRun(ctx, cand.RunID, domain.RunExpired, reason)
}

// closeRun moves a run out of the active states. Failure is ignored on purpose:
// the caller is already handling a user-visible error, and a stuck run is
// repaired by the self-heal path in Start.
func (s *Service) closeRun(ctx context.Context, runID string, status domain.RunStatus, reason string) {
	if runID == "" {
		return
	}
	run, err := s.Store.GetRun(ctx, runID)
	if err != nil {
		return
	}
	run.Status = status
	run.Error = strings.TrimSpace(reason)
	run.UpdatedAt = time.Now().UTC()
	_ = s.Store.SaveRun(ctx, run)
}

// loadDeslopRules resolves the local-gate rule table: 本书 > 作者机器 > 内置。
// Both overrides are plain files, never knowledge articles, so the table can
// never be picked into a prompt.
// A broken override surfaces as an error rather than silently falling back:
// the author must know the checks they think are running are not.
func (s *Service) loadDeslopRules(root string) (*gates.Ruleset, error) {
	bookRaw := ""
	if b, err := os.ReadFile(filepath.Join(root, "canon", "deslop-rules.yaml")); err == nil {
		bookRaw = string(b)
	}
	authorRaw := ""
	authorPath := filepath.Join(runtimepaths.Home(), "gates", "deslop-rules.yaml")
	if b, err := os.ReadFile(authorPath); err == nil {
		authorRaw = string(b)
	}
	return gates.ResolveRules(bookRaw, authorRaw)
}

// loadReviewChecklist returns the author's edited 25-review-checklist body, so
// edits made on the Knowledge page reach the next review. Empty means "not
// customized" and the built-in article is used.
func (s *Service) loadReviewChecklist() string {
	root := s.Settings.Get().LibraryRoot
	if !knowledge.IsCustomized(root, "25-review-checklist") {
		return ""
	}
	raw, err := knowledge.Read(root, "25-review-checklist")
	if err != nil {
		return ""
	}
	return raw
}

// loadKnowledge reads the author-selected reference articles for one call.
// author-only documents are skipped: they are operating notes for the human
// (平台运营、应用流程、出处索引), and letting one into a prompt wastes tokens and
// invites the model to treat bookkeeping as rules. The picker already hides
// them; this is the enforcement half.
func (s *Service) loadKnowledge(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	root := s.Settings.Get().LibraryRoot
	// 按 id 排序后再拼 references：作者在确认框里调整勾选顺序不该改变
	// payload 字节序，否则前缀缓存会因为"同一批文章换了个顺序"整份失效。
	// 实测两个 run 只差这一项，命中量就从 9472 掉到 7040 token。
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	var docs []string
	for _, id := range sorted {
		tags, err := knowledge.TagsOf(root, id)
		if err != nil || tags.Audience == knowledge.AudienceAuthor {
			continue
		}
		raw, err := knowledge.Read(root, id)
		if err == nil && strings.TrimSpace(raw) != "" {
			docs = append(docs, raw)
		}
	}
	return docs
}

func findUnit(snap domain.BookSnapshot, id string) (domain.UnitInfo, bool) {
	for _, u := range snap.Units {
		if u.UnitID == id {
			return u, true
		}
	}
	return domain.UnitInfo{}, false
}

// chapterCharsSource 把草稿里的两个字数键拼成 ChapterTarget 能读的最小 YAML。
// chapter_chars 优先，word_target 只是老格式的输入。
func chapterCharsSource(u domain.UnitOutlineDraft) string {
	var b strings.Builder
	if v := strings.TrimSpace(u.ChapterChars); v != "" {
		fmt.Fprintf(&b, "chapter_chars: %s\n", strings.Trim(v, `"'`))
	}
	if v := strings.TrimSpace(u.WordTarget); v != "" {
		fmt.Fprintf(&b, "word_target: %s\n", strings.Trim(v, `"'`))
	}
	return b.String()
}

// volumeFromTag 把 v01 / 1 这类标签解成卷号；解不出返回 0。
func volumeFromTag(tag string) int {
	s := strings.TrimSpace(tag)
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimLeft(strings.TrimPrefix(strings.ToLower(s), "v"), "0"))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// clockUnitsWithCandidate merges the candidate's unit contracts into the
// on-disk unit list so the pre-accept clock check sees what will actually be
// written.
//
// 以前这里直接传 snap.Units：那是接受前从盘上读到的文件，候选自己新写的
// story_day 根本不在里面。首次生成时单元还不在盘上（整条「故事日倒退」检查
// 空转），重写时校验的是被替换掉的旧值——两种情况下校验都和落盘内容无关。
func clockUnitsWithCandidate(snap domain.BookSnapshot, volume int, outlines []domain.UnitOutlineDraft) []domain.UnitInfo {
	pending := map[string]string{}
	for _, u := range outlines {
		pending[u.UnitID] = serializeUnitYAML(u)
	}
	out := make([]domain.UnitInfo, 0, len(snap.Units)+len(pending))
	for _, u := range snap.Units {
		if raw, ok := pending[u.UnitID]; ok {
			u.OutlineRaw = raw
			delete(pending, u.UnitID)
		}
		out = append(out, u)
	}
	for _, u := range outlines {
		raw, ok := pending[u.UnitID]
		if !ok {
			continue // 已在盘上的单元，上面替换过了
		}
		delete(pending, u.UnitID)
		key := books.UnitOrderKey(u.UnitID)
		vol, idx := key/1000, key%1000
		if vol < 1 {
			vol, idx = volume, key%1000
		}
		out = append(out, domain.UnitInfo{UnitID: u.UnitID, Volume: vol, Index: idx, OutlineRaw: raw})
	}
	// 校验按切片顺序比较「上一单元 → 本单元」，顺序必须稳定，不能受 map 影响。
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Volume != b.Volume {
			return a.Volume < b.Volume
		}
		if a.Index != b.Index {
			return a.Index < b.Index
		}
		return a.UnitID < b.UnitID
	})
	return out
}

func serializeUnitYAML(u domain.UnitOutlineDraft) string {
	var b strings.Builder
	fmt.Fprintf(&b, "unit_id: %s\n", u.UnitID)
	fmt.Fprintf(&b, "status: accepted\n")
	fmt.Fprintf(&b, "title_working: %q\n", u.Title)
	fmt.Fprintf(&b, "function: %q\n", u.Function)
	if u.SourceSeedID != "" {
		fmt.Fprintf(&b, "source_seed_id: %s\n", u.SourceSeedID)
	}
	for _, pair := range []struct{ key, value string }{{"attempt", u.Attempt}, {"setback", u.Setback}, {"cost", u.Cost}} {
		if pair.value != "" {
			fmt.Fprintf(&b, "%s: %q\n", pair.key, pair.value)
		}
	}
	fmt.Fprintf(&b, "desire: %q\n", u.Desire)
	fmt.Fprintf(&b, "obstacle: %q\n", u.Obstacle)
	fmt.Fprintf(&b, "choice: %q\n", u.Choice)
	fmt.Fprintf(&b, "payoff: %q\n", u.Payoff)
	fmt.Fprintf(&b, "pleasure: %q\n", u.Pleasure)
	fmt.Fprintf(&b, "forbidden: %q\n", u.Forbidden)
	// 字数只写一个可执行的口径：单章目标（chapter_chars），word_target 由它派生。
	// 模型自带的 wordTarget 不再直接落盘——曾经 30000/6 章变成 5000 字/章。
	target := books.OutlineChapterTarget(u.ChapterFrom, u.ChapterTo, chapterCharsSource(u))
	chapters := u.ChapterTo - u.ChapterFrom + 1
	if chapters < 1 {
		chapters = 1
	}
	fmt.Fprintf(&b, "chapter_chars: %d\n", target)
	fmt.Fprintf(&b, "word_target: %d\n", target*chapters)
	fmt.Fprintf(&b, "pov: %q\n", u.POV)
	fmt.Fprintf(&b, "chapter_range: [%d, %d]\n", u.ChapterFrom, u.ChapterTo)
	fmt.Fprintf(&b, "on_stage: [%s]\n", strings.Join(quoteAll(u.OnStage), ", "))
	// 章合同是生产的基本输入；场面只是复杂章的可选辅助。
	if len(u.Chapters) > 0 {
		b.WriteString("chapters:\n")
		for _, ch := range u.Chapters {
			fmt.Fprintf(&b, "  - chapter: %d\n", ch.Chapter)
			fmt.Fprintf(&b, "    goal: %q\n", ch.Goal)
			fmt.Fprintf(&b, "    change: %q\n", ch.Change)
			fmt.Fprintf(&b, "    cut_hook: %q\n", ch.CutHook)
			fmt.Fprintf(&b, "    day_offset: %d\n", ch.DayOffset)
			if t := strings.TrimSpace(ch.StartTime); t != "" {
				fmt.Fprintf(&b, "    start_time: %q\n", t)
			}
			if len(ch.Plant) > 0 {
				fmt.Fprintf(&b, "    plant: [%s]\n", strings.Join(quoteAll(ch.Plant), ", "))
			}
			if len(ch.Payoff) > 0 {
				fmt.Fprintf(&b, "    payoff: [%s]\n", strings.Join(quoteAll(ch.Payoff), ", "))
			}
			if len(ch.Scenes) > 0 {
				b.WriteString("    scenes:\n")
				for _, scene := range ch.Scenes {
					writeSceneYAML(&b, scene)
				}
			}
		}
	}
	if len(u.Scenes) > 0 {
		b.WriteString("scenes:\n")
		for _, s := range u.Scenes {
			writeYAMLListItem(&b, s, "  ")
		}
	}
	// 完整保留 next_hook / 故事时钟 / 信息控制。
	if u.NextHook != nil {
		b.WriteString("next_hook:\n")
		writeYAMLMap(&b, u.NextHook, "  ")
	}
	if u.StoryDay != "" && u.StoryDay != "null" {
		fmt.Fprintf(&b, "story_day: %s\n", u.StoryDay)
	}
	fmt.Fprintf(&b, "end_day: %d\n", u.EndDay)
	fmt.Fprintf(&b, "gap_days: %d\n", u.GapDays)
	if u.TimelineID != "" {
		fmt.Fprintf(&b, "timeline_id: %s\n", u.TimelineID)
	}
	if u.GapFromPrev != "" {
		fmt.Fprintf(&b, "gap_from_prev: %q\n", u.GapFromPrev)
	}
	if u.Flashback {
		b.WriteString("flashback: true\n")
	}
	if u.InfoControl != "" {
		fmt.Fprintf(&b, "info_control: %q\n", u.InfoControl)
	}
	// 结构化 YAML 列表，与读方一致。旧实现写 state_deltas_json 字符串，
	// 而读取方找 state_deltas，导致 state_deltas 从未真正进入账本
	// （fallback 扫全文只能碰运气）。
	if u.StateDeltas != nil {
		if rows, ok := stateDeltaRows(u.StateDeltas); ok {
			b.WriteString("state_deltas:\n")
			for _, row := range rows {
				b.WriteString("  - ")
				b.WriteString(row)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func writeSceneYAML(b *strings.Builder, scene domain.ScenePlan) {
	fmt.Fprintf(b, "      - chapter: %d\n", scene.Chapter)
	if scene.ID != "" {
		fmt.Fprintf(b, "        id: %s\n", scene.ID)
	}
	fmt.Fprintf(b, "        goal: %q\n", scene.Goal)
	fmt.Fprintf(b, "        conflict: %q\n", scene.Conflict)
	fmt.Fprintf(b, "        turn: %q\n", scene.Turn)
	fmt.Fprintf(b, "        outcome: %q\n", scene.Outcome)
	// 可选定位与信息边界：只在填写时落盘，不输出空字段制造「已确认事实」的假象。
	if len(scene.Who) > 0 {
		fmt.Fprintf(b, "        who: [%s]\n", strings.Join(quoteAll(scene.Who), ", "))
	}
	if scene.Where != "" {
		fmt.Fprintf(b, "        where: %q\n", scene.Where)
	}
	for _, pair := range []struct {
		key   string
		items []string
	}{{"reveal", scene.Reveal}, {"withhold", scene.Withhold}, {"must_land", scene.MustLand}, {"evidence_in", scene.EvidenceIn}, {"evidence_out", scene.EvidenceOut}} {
		if len(pair.items) > 0 {
			fmt.Fprintf(b, "        %s: [%s]\n", pair.key, strings.Join(quoteAll(pair.items), ", "))
		}
	}
	if scene.DayOffset != nil {
		fmt.Fprintf(b, "        day_offset: %d\n", *scene.DayOffset)
	}
}

// stateDeltaRows flattens the model's stateDeltas into inline YAML mappings.
// Anything the model returned in an unexpected shape is dropped rather than
// serialized blindly: a malformed ledger is worse than a short one.
func stateDeltaRows(v any) ([]string, bool) {
	list, ok := v.([]any)
	if !ok || len(list) == 0 {
		return nil, false
	}
	var out []string
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var parts []string
		for _, key := range []string{"stem", "field", "from", "to"} {
			raw, ok := m[key]
			if !ok || raw == nil {
				continue
			}
			text := strings.TrimSpace(fmt.Sprintf("%v", raw))
			if text == "" {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s: %s", key, yamlQuote(text)))
		}
		if len(parts) == 0 {
			continue
		}
		out = append(out, "{"+strings.Join(parts, ", ")+"}")
	}
	return out, len(out) > 0
}

// yamlQuote emits a double-quoted scalar safe for inline flow mappings.
func yamlQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", " ")
	return `"` + s + `"`
}

// writeYAMLListItem 把一个 map 写成 YAML 列表项（`- key: value`）。
//
// 以前 scenes 直接调 writeYAMLMap：多场面单元会在同一个 mapping 里重复写出
// chapter/goal/… 这些键，产出根本不是合法 YAML——真实数据里有 8 个单元文件
// 因此无法被严格解析器读取。读取方目前都走 yaml.Node 或局部解码，所以还没炸，
// 但文件已经坏了。交给 yaml.Marshal 生成后再加列表前缀，键序也是稳定的。
func writeYAMLListItem(b *strings.Builder, m map[string]any, indent string) {
	raw, err := yaml.Marshal(m)
	if err != nil {
		return
	}
	for i, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if i == 0 {
			fmt.Fprintf(b, "%s- %s\n", indent, line)
			continue
		}
		fmt.Fprintf(b, "%s  %s\n", indent, line)
	}
}

func writeYAMLMap(b *strings.Builder, m map[string]any, indent string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// stable order: id/chapter/beat/who/pov/where first, then rest
	preferred := []string{"id", "scene", "chapter", "beat", "who", "pov", "where"}
	seen := map[string]bool{}
	for _, k := range preferred {
		if v, ok := m[k]; ok {
			writeYAMLValue(b, indent, k, v)
			seen[k] = true
		}
	}
	for _, k := range keys {
		if seen[k] {
			continue
		}
		writeYAMLValue(b, indent, k, m[k])
	}
}

func writeYAMLValue(b *strings.Builder, indent, key string, v any) {
	switch t := v.(type) {
	case []any:
		fmt.Fprintf(b, "%s%s:\n", indent, key)
		for _, item := range t {
			fmt.Fprintf(b, "%s  - %v\n", indent, item)
		}
	case map[string]any:
		fmt.Fprintf(b, "%s%s:\n", indent, key)
		writeYAMLMap(b, t, indent+"  ")
	default:
		fmt.Fprintf(b, "%s%s: %v\n", indent, key, t)
	}
}

func quoteAll(xs []string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = fmt.Sprintf("%q", x)
	}
	return out
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func updateStateScalar(raw, key, value string) string {
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*.*$`)
	if re.MatchString(raw) {
		return re.ReplaceAllString(raw, key+": "+value)
	}
	return raw + "\n" + key + ": " + value + "\n"
}
