package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/gates"
	"github.com/google/uuid"
)

type Runner struct {
	LLM port.LLMProvider
	// KnowledgeDocs are author-attached reference articles appended to the user
	// prompt as method/style reference (never as canon facts).
	KnowledgeDocs []string
	// ReviewChecklist is the author's edited 25-review-checklist body, already
	// resolved through the knowledge library (overlay beats built-in). Empty
	// means "use the built-in article".
	ReviewChecklist string
	// DeslopRules is the author-resolved 去 AI 味 rule set for local gates
	// (book canon/deslop-rules.yaml > library edit > embedded). Nil = embedded.
	DeslopRules *gates.Ruleset
	// ModelMaxOut returns the assigned model's catalog max_output (0 = unknown).
	ModelMaxOut func(modelID string) int
	// AutoStructureReview enables the bounded editor pass after a structured
	// plan. Manual structure review is always available without this switch.
	AutoStructureReview bool
}

// proseRules returns the effective local-gate rule set.
func (r *Runner) proseRules() *gates.Ruleset {
	if r != nil && r.DeslopRules != nil {
		return r.DeslopRules
	}
	return gates.BuiltinRules()
}

func New(llm port.LLMProvider) *Runner { return &Runner{LLM: llm} }

func (r *Runner) modelMaxOut(modelID string) int {
	if r == nil || r.ModelMaxOut == nil {
		return 0
	}
	return r.ModelMaxOut(modelID)
}

// withKnowledge merges author-attached knowledge into the JSON user payload.
func (r *Runner) withKnowledge(user string) string {
	return contextsvc.MergeKnowledgeIntoUserPayload(user, r.KnowledgeDocs)
}

type Result struct {
	Candidate domain.Candidate
	Calls     []domain.CallRecord
	Packet    domain.ContextPacket
	LocalGate gates.Report
}

func (r *Runner) RunWrite(ctx context.Context, snap domain.BookSnapshot, unit domain.UnitInfo, instruction string, modelID string) (Result, error) {
	if err := contextsvc.ValidateUnitInput(snap, unit); err != nil {
		return Result{}, err
	}
	packet := contextsvc.BuildWritePacket(snap, unit, instruction)
	// Fixed per-chapter budget. Each call generates exactly one chapter.
	from, to := unit.ChapterFrom, unit.ChapterTo
	if from <= 0 || to < from {
		from, to = 1, 1
	}
	cand := domain.Candidate{
		ID: uuid.NewString(), BookID: snap.Summary.ID, Kind: domain.TaskWrite,
		Status: domain.CandidateOpen, BaseRevision: snap.Revision,
		TargetPath: unit.ProsePath, UnitID: unit.UnitID, VolumeID: packet.VolumeID,
		CreatedAt: time.Now().UTC(),
	}
	var calls []domain.CallRecord
	originalExtras := map[int]map[string]any{}
	// 每章目标字数放在"本次只写这一章"的指令里：按章调用时这条是模型最后读到
	// 的、也是唯一针对本次输出的约束，比 payload 里那段通用说明更难忽略。
	target := books.ChapterTarget(unit)
	floor, ceil := domain.ChapterBand(target)
	for ch := from; ch <= to; ch++ {
		// 单章只带本章的场面节拍。节拍按章变化，因此必须进 extra（payload 尾部）
		// 而不是留在 documents：documents 排在 references 之前，一旦中途分叉，
		// 后面的基线块与整份参考知识都无法命中前缀缓存。
		chapterPacket, beats := contextsvc.NarrowBeats(packet, unit, ch)
		extra := map[string]any{
			"chapter": ch,
			"chapterRule": fmt.Sprintf(
				"本次只写第 %d 章，chapters 数组只放这一章。本章为 %d 字（允许 %d–%d 字）：先按场面分配字数，写完收在目标附近，不要靠追加场景或复述把篇幅写长。",
				ch, target, floor, ceil),
		}
		if beats != "" {
			extra[contextsvc.ChapterBeatsKey] = beats
		}
		// 本章伏笔：只给本章要埋/要收的条目与已埋未收清单。
		// 按章变化，所以必须走 extra（payload 尾部），不能留在 documents。
		if foreshadow := contextsvc.ForeshadowForChapter(snap, unit, ch); foreshadow != "" {
			extra[contextsvc.ChapterForeshadowsKey] = foreshadow
		}
		if tail, source := contextsvc.PreviousChapterTail(snap, unit, cand.Chapters, ch); tail != "" {
			extra["previousChapterTail"] = map[string]any{
				"chapter": ch - 1, "body": tail, "source": source,
				"note": "仅供语感与承接，不要复述上一章剧情",
			}
		}
		originalExtras[ch] = extra
		user := contextsvc.MergeExtraIntoUserPayload(r.withKnowledge(chapterPacket.UserPrompt), extra)
		draft, rec, err := r.generateChapter(ctx, modelID, chapterPacket, user, ch, &calls)
		if err != nil {
			return Result{Calls: calls, Packet: packet}, err
		}
		_ = rec
		cand.Chapters = append(cand.Chapters, draft)
	}
	// 按章定向修复：单章硬门不过只重生成该章，其他章不动。
	cand.Chapters = r.repairFailedChapters(ctx, modelID, packet, unit, cand.Chapters, target, &calls, originalExtras)
	rep := gates.CheckProseWithRules(cand.Chapters, target, r.proseRules())
	cand.Findings = append(cand.Findings, rep.Findings...)
	return Result{Candidate: cand, Calls: calls, Packet: packet, LocalGate: rep}, nil
}

func (r *Runner) RunOutline(ctx context.Context, snap domain.BookSnapshot, volume int, unitIDs []string, instruction string, modelID string) (Result, error) {
	for _, id := range unitIDs {
		for _, u := range snap.Units {
			if u.UnitID == id {
				if err := books.ValidateIdentityCoverage(snap.IdentitySegments, snap.Cast, u, outlineOnStage(u.OutlineRaw)); err != nil {
					return Result{}, err
				}
			}
		}
	}
	packet := contextsvc.BuildOutlinePacket(snap, volume, unitIDs, instruction)
	var volCast []string
	for _, v := range snap.Volumes {
		if v.Volume == volume {
			volCast = v.Cast
			break
		}
	}
	return r.runJSON(ctx, snap, packet, modelID, domain.TaskOutline, "", "", func(raw string, cand *domain.Candidate) error {
		units, err := parseOutlineUnits(raw)
		if err != nil {
			return err
		}
		// 章覆盖只是底线；目标/冲突/转折/结果也必须可被写作阶段消费。
		if err := ValidateOutlineContract(units, unitIDs); err != nil {
			return err
		}
		if err := ValidateOutlineInheritance(snap, volume, units); err != nil {
			return err
		}
		cand.UnitOutlines = units
		return nil
	}, func(cand domain.Candidate) gates.Report {
		rep := gates.Report{OK: true, Counts: map[string]int{}}
		for _, u := range cand.UnitOutlines {
			g := gates.CheckUnitOutline(u, volCast)
			if !g.OK {
				rep.OK = false
			}
			rep.Findings = append(rep.Findings, g.Findings...)
		}
		return rep
	})
}

// RunPlanMode runs 立项 / 重新立项 / 重写卷纲.
func (r *Runner) RunPlanMode(ctx context.Context, snap domain.BookSnapshot, volume int, modelID, mode, instruction string, preservePaths ...[]string) (Result, error) {
	if mode == "" {
		if books.HasBookOutline(snap) {
			mode = domain.PlanModeVolume
		} else {
			mode = domain.PlanModeInit
		}
	}
	if mode == domain.PlanModeVolume {
		doc, ok := books.StructuredOutlineFor(snap, "outline/book_outline.md")
		if !ok || doc.Legacy {
			return Result{}, fmt.Errorf("旧格式总纲不进入生产链路，请先通过 Agent 批量迁移")
		}
		if !doc.Valid() {
			return Result{}, fmt.Errorf("总纲结构校验失败：%s", books.FormatOutlineDiagnostics(doc.Diagnostics))
		}
	}
	return r.runStructuredPlan(ctx, snap, volume, modelID, mode, instruction, preservePaths...)
}

func (r *Runner) RunLocalEdit(ctx context.Context, snap domain.BookSnapshot, sel domain.TextSelection, instruction, modelID string) (Result, error) {
	packet := contextsvc.BuildLocalEditPacket(snap, sel, instruction)
	return r.runJSON(ctx, snap, packet, modelID, domain.TaskLocalEdit, "", sel.Path, func(raw string, cand *domain.Candidate) error {
		var parsed struct {
			Replacement string `json:"replacement"`
		}
		if err := unmarshalJSON(raw, &parsed); err != nil {
			return err
		}
		if strings.TrimSpace(parsed.Replacement) == "" {
			return contractFail("局部改写", []Violation{{
				Path: "replacement",
				Want: "改写后的完整替换文本，可直接替换原文选区",
				Got:  "空",
			}})
		}
		cand.Replacement = parsed.Replacement
		return nil
	}, func(cand domain.Candidate) gates.Report {
		return gates.Report{OK: true, Counts: map[string]int{}}
	})
}

// RunRewriteEntity rewrites one canon/setting file via a single bounded call.
// Output is a candidate only; accept writes the normalized content back.
func (r *Runner) RunRewriteEntity(ctx context.Context, snap domain.BookSnapshot, spec domain.TaskSpec, modelID string) (Result, error) {
	packet := contextsvc.BuildRewriteEntityPacket(snap, spec)
	return r.runJSON(ctx, snap, packet, modelID, domain.TaskRewriteEntity, "", spec.TargetPath, func(raw string, cand *domain.Candidate) error {
		var parsed struct {
			Content string `json:"content"`
		}
		if err := unmarshalJSON(raw, &parsed); err != nil {
			return err
		}
		if strings.TrimSpace(parsed.Content) == "" {
			return contractFail("改写设定", []Violation{{
				Path: "content",
				Want: "改写后的完整文件内容（保留 front matter 与原有结构）",
				Got:  "空",
			}})
		}
		cand.Replacement = parsed.Content
		return nil
	}, func(cand domain.Candidate) gates.Report {
		return gates.Report{OK: true, Counts: map[string]int{}}
	})
}

// RunFinalize performs local gates plus bounded actual-ledger reconciliation.
//
// 默认先用一次调用核对整单元账本，失败再退回逐章：前者的产出只有一张表，
// 后者会把同一份单元基线重复携带 N 遍并按 N 份计费思维链。两条路径的判据
// 完全一致（回证必须逐字出自该章正文、每章都必须有一条），所以退回逐章
// 不会放行任何整单元路径拒绝过的内容。
func (r *Runner) RunFinalize(ctx context.Context, snap domain.BookSnapshot, unit domain.UnitInfo, reviewFindings []domain.ReviewFinding, modelID string) (Result, error) {
	chapters := gates.ParseProseChapters(unit.ProseRaw)
	if err := validateChapters("定稿正文", chapters, unit.ChapterFrom, unit.ChapterTo); err != nil {
		return Result{}, err
	}
	local := gates.CheckProseWithRules(chapters, books.ChapterTarget(unit), r.proseRules())
	if len(reviewFindings) > 0 {
		local.Findings = append(local.Findings, reviewFindings...)
	}
	if verdict := domain.ReviewVerdict(reviewFindings); verdict == domain.ReviewBlocking || verdict == domain.ReviewUnfinished {
		local.OK = false
	}
	cand := domain.Candidate{
		ID: uuid.NewString(), BookID: snap.Summary.ID, Kind: domain.TaskFinalize,
		Status: domain.CandidateOpen, BaseRevision: snap.Revision,
		TargetPath: unit.ProsePath, UnitID: unit.UnitID,
		Chapters: chapters, Findings: local.Findings, CreatedAt: time.Now().UTC(),
	}
	var calls []domain.CallRecord
	// 每章共用的单元级基线：与审校/修订同一套块，逐章调用时前缀才可复用。
	baseline := contextsvc.BuildReviewBaseline(snap, unit, nil, "")
	// 整单元一次核对比逐章便宜得多（正文只有一万多字，产出一张表；逐章会把
	// 同一份基线重复携带 N 遍，思维链也按 N 份计费）。代价是一份 JSON 承载
	// 全单元：任何一章不合规就整份作废。所以这里先走便宜的路径，失败就退到
	// 逐章——逐章路径的失败面只有一章，是兜底而不是主路径。
	actuals, unitCalls, unitErr := r.reconcileUnit(ctx, snap, unit, chapters, baseline, modelID)
	calls = append(calls, unitCalls...)
	if unitErr == nil {
		cand.ActualChapters = actuals
		// 预算按实际走过的路径申报：整单元成功就只花了 2 次额度，报 2+N*2 会让
		// 界面显示"调用 1/14 次"，作者无从判断这条路径到底贵不贵。
		return Result{Candidate: cand, Calls: calls, LocalGate: local, Packet: domain.ContextPacket{TaskKind: domain.TaskFinalize, BookID: snap.Summary.ID, UnitID: unit.UnitID, Policy: domain.CallPolicy{MaxCalls: 2, AllowOneRepair: true, TimeoutSec: 600}}}, nil
	}
	for _, ch := range chapters {
		actual, recs, err := r.reconcileChapter(ctx, snap, unit, ch, baseline, modelID)
		calls = append(calls, recs...)
		if err != nil {
			return Result{Candidate: cand, Calls: calls}, fmt.Errorf("整单元核对未通过（%v），逐章重试又失败：%w", unitErr, err)
		}
		cand.ActualChapters = append(cand.ActualChapters, actual)
	}
	return Result{Candidate: cand, Calls: calls, LocalGate: local, Packet: domain.ContextPacket{TaskKind: domain.TaskFinalize, BookID: snap.Summary.ID, UnitID: unit.UnitID, Policy: domain.CallPolicy{MaxCalls: 2 + len(chapters)*2, AllowOneRepair: true, TimeoutSec: 600}}}, nil
}

// generateChapter produces one chapter, returning the parsed draft.
//
// 单章结构与正文不同：空 body、章号错这类问题靠 repair 一次就能修好，而失败
// 代价极高——它是分章写作路径，前面几章已经花掉的调用会随整单元一起作废。
// 所以这里允许一次定向修复（与整单元 runJSON 同一条规则：只在 policy 允许时），
// 而不是把结构错误直接抛给作者。
func (r *Runner) generateChapter(ctx context.Context, modelID string, packet domain.ContextPacket, user string, ch int, calls *[]domain.CallRecord) (domain.ChapterDraft, domain.CallRecord, error) {
	policy := packet.Policy
	max := 1
	if policy.AllowOneRepair {
		max = 2
	}
	var lastErr error
	var lastRec domain.CallRecord
	for attempt := 1; attempt <= max; attempt++ {
		req := user
		if attempt > 1 {
			req = contextsvc.MergeRepairIntoUserPayload(user, lastErr)
		}
		raw, rec, err := r.chat(ctx, modelID, packet.SystemPrompt, req, policy)
		rec.Index = len(*calls) + 1
		rec.Step = fmt.Sprintf("chapter-%d", ch)
		if attempt > 1 {
			rec.Step = fmt.Sprintf("chapter-%d-repair", ch)
		}
		*calls = append(*calls, rec)
		lastRec = rec
		if err != nil {
			lastErr = err
			continue
		}
		draft, perr := parseChapterDraft(raw, ch)
		if perr == nil {
			return draft, rec, nil
		}
		lastErr = perr
		// 截断只认协议信号（finish_reason=length），不再猜预算。上限由 client
		// 按模型配置决定，上层已经无法"加大预算"，重试能改的只有输出是否紧凑。
		if truncatedOutput(rec) {
			lastErr = truncationRetryError(perr)
		}
	}
	return domain.ChapterDraft{}, lastRec, lastErr
}

// parseChapterDraft enforces the one-chapter contract.
func parseChapterDraft(raw string, ch int) (domain.ChapterDraft, error) {
	var parsed struct {
		Chapters []domain.ChapterDraft `json:"chapters"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return domain.ChapterDraft{}, fmt.Errorf("chapter %d: %w", ch, err)
	}
	if len(parsed.Chapters) != 1 {
		return domain.ChapterDraft{}, contractFail(fmt.Sprintf("第 %d 章正文", ch), []Violation{{
			Path: "chapters",
			Want: "恰好 1 章（本次只写这一章）",
			Got:  fmt.Sprintf("%d 章", len(parsed.Chapters)),
		}})
	}
	if strings.TrimSpace(parsed.Chapters[0].Body) == "" {
		return domain.ChapterDraft{}, contractFail(fmt.Sprintf("第 %d 章正文", ch), []Violation{{
			Path: "chapters[0].body",
			Want: "非空正文",
			Got:  "空",
		}})
	}
	if parsed.Chapters[0].Chapter != ch {
		return domain.ChapterDraft{}, contractFail(fmt.Sprintf("第 %d 章正文", ch), []Violation{{Path: "chapters[0].chapter", Want: fmt.Sprint(ch), Got: fmt.Sprint(parsed.Chapters[0].Chapter)}})
	}
	return parsed.Chapters[0], nil
}

// repairFailedChapters re-generates only chapters that fail the local hard
// gate, with evidence+suggestion回注。每章最多一次修复，通过的章不动。
//
// target 必须与审校阶段一致（books.ChapterTarget），否则写作阶段会
// 觉得"字数够了"而审校阶段判 blocking，白跑一轮修复。
func (r *Runner) repairFailedChapters(ctx context.Context, modelID string, packet domain.ContextPacket, unit domain.UnitInfo, chapters []domain.ChapterDraft, target int, calls *[]domain.CallRecord, originalExtras map[int]map[string]any) []domain.ChapterDraft {
	rep := gates.CheckProseWithRules(chapters, target, r.proseRules())
	if rep.OK {
		return chapters
	}
	failed := map[int][]domain.ReviewFinding{}
	for _, f := range rep.Findings {
		if f.Severity == "blocking" && f.Chapter > 0 {
			failed[f.Chapter] = append(failed[f.Chapter], f)
		}
	}
	for i, ch := range chapters {
		fs, ok := failed[ch.Chapter]
		if !ok {
			continue
		}
		// 用与生成该章时完全相同的包（同样的 blocks、同样的节拍位置），
		// 修复才能直接命中刚才那次生成留下的前缀缓存；多塞一句诊断只在尾部。
		chapterPacket, beats := contextsvc.NarrowBeats(packet, unit, ch.Chapter)
		extra := map[string]any{}
		for key, value := range originalExtras[ch.Chapter] {
			extra[key] = value
		}
		extra["chapter"] = ch.Chapter
		extra["repairChapter"] = map[string]any{
			"findings": detailedFindings(fs),
			"body":     ch.Body,
			"rule":     repairRule(ch.Chapter, fs, target),
		}
		if beats != "" {
			extra[contextsvc.ChapterBeatsKey] = beats
		}
		user := contextsvc.MergeExtraIntoUserPayload(r.withKnowledge(chapterPacket.UserPrompt), extra)
		// 记录起点：generateChapter 自己也可能重试一次，所以要按本次新增的
		// 调用逐条打标，否则第一次尝试会被错标成生成调用。
		start := len(*calls)
		fixed, _, err := r.generateChapter(ctx, modelID, chapterPacket, user, ch.Chapter, calls)
		for k := start; k < len(*calls); k++ {
			label := fmt.Sprintf("repair-ch%d", ch.Chapter)
			if k > start {
				label = fmt.Sprintf("repair-ch%d-repair", ch.Chapter)
			}
			(*calls)[k].Step = label
		}
		if err == nil && strings.TrimSpace(fixed.Body) != "" {
			chapters[i] = fixed
		}
	}
	return chapters
}

// repairRule 把硬门的阻断结论翻译成这一章该做什么。
//
// 只有 blocking 会走到这里（超长是 advisory，不进重写循环）。字数不足时明确
// 给出区间，并要求"补能改变因果的场面"，否则模型会用同义扩写和天气描写凑字。
func repairRule(chapter int, findings []domain.ReviewFinding, target int) string {
	short := false
	for _, f := range findings {
		if f.ID == fmt.Sprintf("length-ch%d", chapter) {
			short = true
		}
	}
	_, ceil := domain.ChapterBand(target)
	if short {
		return fmt.Sprintf("第 %d 章字数不足，请补写到 %d–%d 字：补能改变因果的场面与动作，不做同义扩写、不注水；只返回这一章 JSON。", chapter, target, ceil)
	}
	return fmt.Sprintf("第 %d 章未通过本地硬门，请定向修复这一章（其他章已合格，不需重写）；只返回这一章 JSON。", chapter)
}

func (r *Runner) runJSON(
	ctx context.Context,
	snap domain.BookSnapshot,
	packet domain.ContextPacket,
	modelID string,
	kind domain.TaskKind,
	unitID, targetPath string,
	parse func(string, *domain.Candidate) error,
	gateFn func(domain.Candidate) gates.Report,
) (Result, error) {
	return r.runJSONWithRecord(ctx, snap, packet, modelID, kind, unitID, targetPath, func(raw string, cand *domain.Candidate, _ domain.CallRecord) error {
		return parse(raw, cand)
	}, gateFn)
}

func (r *Runner) runJSONWithRecord(
	ctx context.Context,
	snap domain.BookSnapshot,
	packet domain.ContextPacket,
	modelID string,
	kind domain.TaskKind,
	unitID, targetPath string,
	parse func(string, *domain.Candidate, domain.CallRecord) error,
	gateFn func(domain.Candidate) gates.Report,
) (Result, error) {
	if modelID == "" && kind != domain.TaskFinalize {
		return Result{}, fmt.Errorf("modelId required")
	}
	var calls []domain.CallRecord
	user := packet.UserPrompt
	var lastRaw string
	var lastErr error
	policy := packet.Policy
	max := policy.MaxCalls
	if max < 1 {
		max = 1
	}
	for attempt := 1; attempt <= max; attempt++ {
		if attempt > 1 && !policy.AllowOneRepair {
			break
		}
		user = r.withKnowledge(packet.UserPrompt)
		if attempt > 1 {
			user = contextsvc.MergeRepairIntoUserPayload(user, lastErr)
		}
		raw, rec, err := r.chat(ctx, modelID, packet.SystemPrompt, user, policy)
		if attempt > 1 {
			rec.Step = "repair"
		} else {
			rec.Step = "generate"
		}
		rec.Index = attempt
		calls = append(calls, rec)
		if err != nil {
			lastErr = err
			if attempt == max {
				return Result{Calls: calls, Packet: packet}, err
			}
			continue
		}
		lastRaw = raw
		cand := domain.Candidate{
			ID:           uuid.NewString(),
			BookID:       snap.Summary.ID,
			Kind:         kind,
			Status:       domain.CandidateOpen,
			BaseRevision: snap.Revision,
			TargetPath:   targetPath,
			UnitID:       unitID,
			VolumeID:     packet.VolumeID,
			CreatedAt:    time.Now().UTC(),
		}
		if err := parse(raw, &cand, rec); err != nil {
			lastErr = err
			truncated := truncatedOutput(rec)
			if truncated {
				lastErr = truncationRetryError(err)
			}
			if attempt < max && policy.AllowOneRepair {
				continue
			}
			if truncated {
				return Result{Calls: calls, Packet: packet}, fmt.Errorf("parse: %w（模型输出达到该模型的最大输出上限被截断）", err)
			}
			return Result{Calls: calls, Packet: packet}, fmt.Errorf("parse: %w", err)
		}
		rep := gateFn(cand)
		cand.Findings = append(cand.Findings, rep.Findings...)
		// Structure-blocking local gates can trigger one 定向修复；advisory never blocks.
		if !rep.OK && attempt < max && packet.Policy.AllowOneRepair {
			lastErr = fmt.Errorf("本地硬门未通过：\n%s", detailedFindings(rep.Findings))
			continue
		}
		return Result{Candidate: cand, Calls: calls, Packet: packet, LocalGate: rep}, nil
	}
	return Result{Calls: calls, Packet: packet}, fmt.Errorf("budget exhausted: %v; last=%s", lastErr, clip(lastRaw, 200))
}

// truncationRetryError 告诉模型「上次是撞到输出上限」，而不是「格式不对」。
// 上层不再持有预算（上限由 client 按模型配置决定），所以重试能改的只有长度：
// 要求更紧凑的 JSON 与更少的复述。
func truncationRetryError(err error) error {
	return fmt.Errorf("%w；上一次输出因达到模型的最大输出上限被截断（思维链与答案共用这份额度），"+
		"内容没写完。这次只返回精简 JSON：不要复述或引用正文，条目与字段文本尽量短，正文按字数目标写完",
		err)
}

// truncatedOutput reports that the model stopped because it hit its output
// ceiling — read straight off the response protocol, never inferred.
//
// 以前上层自己定一个 max_tokens，再用「completion_tokens 是否等于它」反推截断。
// 这个反推两头都错：上层给的值可能远低于模型能力（8k vs 384k），而用量报
// 8191/8192、8189/8192 时严等于又判不出来。现在上限交给 client 按模型配置
// 决定，截断只认 provider 自己说的 finish_reason == "length"。
func truncatedOutput(rec domain.CallRecord) bool {
	return rec.FinishReason == port.FinishReasonLength
}

func (r *Runner) chat(ctx context.Context, modelID, system, user string, policy domain.CallPolicy) (string, domain.CallRecord, error) {
	start := time.Now()
	rec := domain.CallRecord{ModelID: modelID, Step: "chat", StartedAt: start}
	if i := strings.IndexByte(modelID, '/'); i > 0 {
		rec.Provider = modelID[:i]
	}
	req := port.LLMChatRequest{
		Model: modelID,
		Messages: []port.ChatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		// Temperature 0 = omit; model catalog / provider default fills when needed.
		// Kimi K3.x rejects non-1 temperature — dialect strips fixed sampling fields.
		GenParams: &port.ModelGenParams{MaxTokens: policy.MaxOutputTokens},
	}
	resp, err := r.LLM.Chat(ctx, req)
	rec.DurationMs = time.Since(start).Milliseconds()
	// 停止原因先记下来：截断判定只看这个字段，不看我们发出去的额度。
	rec.FinishReason = resp.FinishReason
	if resp.Usage != nil {
		rec.PromptTokens = resp.Usage.PromptTokens
		rec.CompletionTokens = resp.Usage.CompletionTokens
		// 思维链单独记一份：它已经算在 completion 里，但两者量级差一个数量级，
		// 不分开记就解释不了"审校的答案只有几百 token，completion 却上万"。
		rec.ReasoningTokens = resp.Usage.ReasoningTokens
		// 缓存命中量是重试成本的直接指标：repair 追加在 payload 末尾时，
		// 第二次调用应命中首轮 prompt 的绝大部分（各家自动前缀缓存）。
		rec.CacheReadTokens = resp.Usage.CacheReadTokens
		rec.CacheCreationTokens = resp.Usage.CacheCreationTokens
	}
	if err != nil {
		rec.Error = sanitizeErr(err)
		return "", rec, err
	}
	if len(resp.ToolCalls) > 0 {
		rec.Error = "model returned tool calls which are forbidden"
		return "", rec, fmt.Errorf(rec.Error)
	}
	return resp.Content, rec, nil
}

// parseOutlineUnits accepts {"units":[...]} or a bare single-unit object
// (common model drift when rewriting one unit).
func parseOutlineUnits(raw string) ([]domain.UnitOutlineDraft, error) {
	raw = normalizeOutlineJSON(raw)
	var parsed struct {
		Units []domain.UnitOutlineDraft `json:"units"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Units) > 0 {
		return parsed.Units, nil
	}
	var one domain.UnitOutlineDraft
	if err := unmarshalJSON(raw, &one); err == nil && strings.TrimSpace(one.UnitID) != "" {
		return []domain.UnitOutlineDraft{one}, nil
	}
	return nil, contractFail("细纲", []Violation{{
		Path: "units",
		Want: `非空数组 {"units":[...]}（即使只改 1 个单元）`,
		Got:  "0 个单元",
	}})
}

var outlineStringFields = []string{
	"unitId", "status", "title", "function", "sourceSeedId", "attempt", "setback", "cost",
	"desire", "obstacle", "choice", "payoff", "pleasure", "forbidden", "wordTarget",
	"chapterChars", "pov", "storyDay", "gapFromPrev", "infoControl",
}

// normalizeOutlineJSON coerces model drift before strict struct decode:
// storyDay/gapFromPrev as objects or numbers, and chapterFrom/To/endDay/gapDays
// as strings (细纲头与种子里的 clock 值是带引号的 YAML 字符串).
func normalizeOutlineJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	var root any
	if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
		// May be fenced; let unmarshalJSON extract later.
		re := regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")
		if m := re.FindStringSubmatch(trimmed); len(m) > 1 {
			if err := json.Unmarshal([]byte(m[1]), &root); err != nil {
				return raw
			}
			trimmed = m[1]
		} else {
			start := strings.Index(trimmed, "{")
			end := strings.LastIndex(trimmed, "}")
			if start < 0 || end <= start {
				return raw
			}
			if err := json.Unmarshal([]byte(trimmed[start:end+1]), &root); err != nil {
				return raw
			}
		}
	}
	switch r := root.(type) {
	case map[string]any:
		if units, ok := r["units"].([]any); ok {
			for _, u := range units {
				if m, ok := u.(map[string]any); ok {
					coerceOutlineUnitMap(m)
				}
			}
		} else {
			coerceOutlineUnitMap(r)
		}
	default:
		return raw
	}
	b, err := json.Marshal(root)
	if err != nil {
		return raw
	}
	return string(b)
}

func coerceOutlineUnitMap(m map[string]any) {
	for _, k := range outlineStringFields {
		if v, ok := m[k]; ok {
			m[k] = coerceToOutlineString(v)
		}
	}
	// 时钟与章号必须是 JSON 数字：细纲头和卷纲种子里的 clock_end_day /
	// clock_gap_days 是字符串（"90"），模型会原样照抄成 "endDay":"90"，
	// 严格 struct decode 直接失败——连父子合同校验都到不了。这里统一转回数字。
	for _, k := range []string{"chapterFrom", "chapterTo", "endDay", "gapDays"} {
		if v, ok := m[k]; ok {
			m[k] = coerceToOutlineInt(v)
		}
	}
	// nextHook 落盘是 {type,out} 对象；模型常漂移成一句话字符串。
	if v, ok := m["nextHook"]; ok {
		m["nextHook"] = coerceOutlineNextHook(v)
	}
}

// coerceOutlineNextHook 把字符串/字符串数组形态的 nextHook 归一成
// {out: "..."} 对象，避免整份细纲因类型不匹配而 decode 失败。
func coerceOutlineNextHook(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil
		}
		return map[string]any{"out": s}
	case []any:
		parts := make([]string, 0, len(t))
		for _, x := range t {
			if s := strings.TrimSpace(coerceToOutlineString(x)); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) == 0 {
			return nil
		}
		return map[string]any{"out": strings.Join(parts, "；")}
	case map[string]any:
		return t
	default:
		return v
	}
}

func coerceToOutlineString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case json.Number:
		return t.String()
	case bool:
		return fmt.Sprintf("%v", t)
	case map[string]any:
		for _, key := range []string{"day", "value", "text", "label", "storyDay"} {
			if x, ok := t[key]; ok {
				s := coerceToOutlineString(x)
				if s == "" {
					continue
				}
				if key == "day" || key == "value" {
					if lab, ok := t["label"]; ok {
						if ls := coerceToOutlineString(lab); ls != "" && ls != s {
							return strings.TrimSpace(s + " " + ls)
						}
					}
				}
				return s
			}
		}
		b, _ := json.Marshal(t)
		return string(b)
	case []any:
		b, _ := json.Marshal(t)
		return string(b)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func coerceToOutlineInt(v any) any {
	switch t := v.(type) {
	case float64:
		return int(t)
	case json.Number:
		i, err := t.Int64()
		if err == nil {
			return int(i)
		}
		return v
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err == nil {
			return n
		}
		return v
	default:
		return v
	}
}

func unmarshalJSON(raw string, dest any) error {
	raw = strings.TrimSpace(raw)
	try := func(data []byte) error {
		data = []byte(normalizeChapterNumbers(string(data)))
		dec := json.NewDecoder(strings.NewReader(string(data)))
		dec.UseNumber()
		return dec.Decode(dest)
	}
	if err := try([]byte(raw)); err == nil {
		return nil
	}
	re := regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")
	if m := re.FindStringSubmatch(raw); len(m) > 1 {
		if err := try([]byte(m[1])); err == nil {
			return nil
		}
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		return try([]byte(raw[start : end+1]))
	}
	return fmt.Errorf("no JSON object in model output")
}

// normalizeChapterNumbers handles providers that serialize chapter as "1"
// instead of 1 without discarding otherwise valid findings.
func normalizeChapterNumbers(raw string) string {
	re := regexp.MustCompile(`("chapter"\s*:\s*)"(\d+)"`)
	return re.ReplaceAllString(raw, "$1$2")
}

var reviewVerdictRe = regexp.MustCompile(`"verdict"\s*:\s*"([^"]*)"`)

// salvageReviewJSON interprets a review response the strict parser rejected.
//
// 它只做两件"结构上无法避免"的事：跳过 JSON 前面的分析文字，认出被预算切断的
// 输出。被切断时只保留完整的 finding 对象——半条结论不算结论，交给 runJSON 用
// 更大预算重试，而不是把猜测补成一条看似成立的问题。
// truncated 让调用方既能重试、也能在最终失败时如实说"被截断"而不是"没有 JSON"。
func salvageReviewJSON(raw string) (verdict string, findings []domain.ReviewFinding, truncated bool, err error) {
	if m := reviewVerdictRe.FindStringSubmatch(raw); len(m) == 2 {
		verdict = m[1]
	}
	complete, truncated := scanFindingObjects(raw)
	findings = make([]domain.ReviewFinding, 0, len(complete))
	for _, obj := range complete {
		if f, ok := decodeFinding(obj); ok {
			findings = append(findings, f)
		}
	}
	// 输出被切断，且一条完整结论都没救回来：这不是"审校没问题"，
	// 而是"没审完"，必须报错让重试提高预算。
	if truncated && len(findings) == 0 {
		return "", nil, true, fmt.Errorf("model output truncated before any finding was completed")
	}
	if len(findings) == 0 && verdict == "" {
		start := strings.Index(raw, "{")
		end := strings.LastIndex(raw, "}")
		if start >= 0 && end > start {
			var parsed struct {
				Verdict  string                 `json:"verdict"`
				Findings []domain.ReviewFinding `json:"findings"`
			}
			if e := unmarshalJSON(raw[start:end+1], &parsed); e == nil {
				return parsed.Verdict, parsed.Findings, false, nil
			}
		}
	}
	if len(findings) == 0 && verdict == "" {
		if truncated {
			return "", nil, true, fmt.Errorf("model output truncated before any finding completed")
		}
		return "", nil, false, fmt.Errorf("no JSON object in model output")
	}
	return verdict, findings, truncated, nil
}

func decodeFinding(obj string) (domain.ReviewFinding, bool) {
	var f domain.ReviewFinding
	if err := json.Unmarshal([]byte(normalizeChapterNumbers(obj)), &f); err != nil {
		return domain.ReviewFinding{}, false
	}
	if strings.TrimSpace(f.Title) == "" && f.ID == "" {
		return domain.ReviewFinding{}, false
	}
	return f, true
}

// scanFindingObjects walks the findings array and returns the objects that were
// complete; truncated reports that the array never closed (output cut off).
// 半条 finding 不算结论：宁可交给重试，也不把猜测塞进审校清单。
func scanFindingObjects(raw string) (complete []string, truncated bool) {
	key := strings.Index(raw, `"findings"`)
	if key < 0 {
		return nil, false
	}
	lb := strings.IndexByte(raw[key:], '[')
	if lb < 0 {
		return nil, false
	}
	start := key + lb + 1
	objStart := -1
	depth := 0
	inStr, esc := false, false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			if depth == 0 {
				objStart = i
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
				if depth == 0 && objStart >= 0 {
					complete = append(complete, raw[objStart:i+1])
					objStart = -1
				}
			}
		case ']':
			if depth == 0 {
				return complete, false
			}
		}
	}
	return complete, true
}

// detailedFindings renders blocking findings with evidence + suggestion so the
// model knows 错在哪、差多少、怎么改（not just 错了）.
func detailedFindings(fs []domain.ReviewFinding) string {
	var b strings.Builder
	for _, f := range fs {
		if f.Severity != "blocking" {
			continue
		}
		fmt.Fprintf(&b, "- 【%s】%s", f.Title, f.Lens)
		if f.Chapter > 0 {
			fmt.Fprintf(&b, "（第 %d 章）", f.Chapter)
		}
		if f.Evidence != "" {
			fmt.Fprintf(&b, "：%s", f.Evidence)
		}
		if f.Suggestion != "" {
			fmt.Fprintf(&b, "；修复建议：%s", f.Suggestion)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func sanitizeErr(err error) string {
	msg := err.Error()
	msg = regexp.MustCompile(`(?i)(sk-[a-zA-Z0-9_-]+|Bearer\s+\S+|api[_-]?key[=:]\s*\S+)`).ReplaceAllString(msg, "[redacted]")
	return msg
}
