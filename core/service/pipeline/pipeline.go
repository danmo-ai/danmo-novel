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
	packet := contextsvc.BuildWritePacket(snap, unit, instruction, r.modelMaxOut(modelID))
	if !packet.Policy.SplitByChapter {
		return r.runJSON(ctx, snap, packet, modelID, domain.TaskWrite, unit.UnitID, unit.ProsePath, func(raw string, cand *domain.Candidate) error {
			var parsed struct {
				Chapters []domain.ChapterDraft `json:"chapters"`
			}
			if err := unmarshalJSON(raw, &parsed); err != nil {
				return err
			}
			// 章号与章体是下游一切计算的基础；漏章尤其危险——它不会报错，
			// 只会让摘要、账本游标和下次续写全部错位。
			if err := validateChapters("正文", parsed.Chapters, unit.ChapterFrom, unit.ChapterTo); err != nil {
				return err
			}
			cand.Chapters = parsed.Chapters
			return nil
		}, func(cand domain.Candidate) gates.Report {
			// 与审校共用同一个每章目标（books.ChapterTarget）：写作阶段与审校
			// 阶段的字数判定不可能再分叉。
			return gates.CheckProseWithRules(cand.Chapters, books.ChapterTarget(unit), r.proseRules())
		})
	}

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
	for ch := from; ch <= to; ch++ {
		extra := map[string]any{
			"chapter":     ch,
			"chapterRule": fmt.Sprintf("本次只写第 %d 章，chapters 数组只放这一章。", ch),
		}
		if len(cand.Chapters) > 0 {
			prev := cand.Chapters[len(cand.Chapters)-1]
			extra["previousChapter"] = map[string]any{"chapter": prev.Chapter, "body": prev.Body, "note": "仅供承接，不要重复输出"}
		}
		// 单章只带本章的场面节拍：整单元 20+ 个场面每章重复注入一遍纯属浪费。
		chapterPacket := contextsvc.NarrowBeats(packet, unit, ch)
		user := contextsvc.MergeExtraIntoUserPayload(r.withKnowledge(chapterPacket.UserPrompt), extra)
		draft, rec, err := r.generateChapter(ctx, modelID, chapterPacket, user, ch, &calls)
		if err != nil {
			return Result{Calls: calls, Packet: packet}, err
		}
		_ = rec
		cand.Chapters = append(cand.Chapters, draft)
	}
	// 按章定向修复：单章硬门不过只重生成该章，其他章不动。
	target := books.ChapterTarget(unit)
	cand.Chapters = r.repairFailedChapters(ctx, modelID, packet, cand.Chapters, target, &calls)
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
	packet := contextsvc.BuildOutlinePacket(snap, volume, unitIDs, instruction, r.modelMaxOut(modelID))
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
		if volumeDoc, ok := books.StructuredOutlineFor(snap, fmt.Sprintf("outline/volumes/v%02d.md", volume)); ok && volumeDoc.Valid() {
			for _, unit := range units {
				seed, found := volumeDoc.Entity("单元", unit.UnitID)
				if !found {
					return contractFail("细纲父子合同", []Violation{{Path: "unitId", Want: "卷纲已有单元种子", Got: unit.UnitID}})
				}
				if err := ValidateUnitSeedInheritance(unit, seed); err != nil {
					return err
				}
			}
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

func (r *Runner) RunPlan(ctx context.Context, snap domain.BookSnapshot, volume int, modelID string) (Result, error) {
	return r.RunPlanMode(ctx, snap, volume, modelID, domain.PlanModeVolume, "")
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
	packet := contextsvc.BuildRewriteEntityPacket(snap, spec, r.modelMaxOut(modelID))
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

// RunFinalize performs local gates only — no extra LLM review loop.
func (r *Runner) RunFinalize(_ context.Context, snap domain.BookSnapshot, unit domain.UnitInfo) (Result, error) {
	chapters := gates.ParseProseChapters(unit.ProseRaw)
	local := gates.CheckProseWithRules(chapters, books.ChapterTarget(unit), r.proseRules())
	cand := domain.Candidate{
		ID: uuid.NewString(), BookID: snap.Summary.ID, Kind: domain.TaskFinalize,
		Status: domain.CandidateOpen, BaseRevision: snap.Revision,
		TargetPath: unit.ProsePath, UnitID: unit.UnitID,
		Chapters: chapters, Findings: local.Findings, CreatedAt: time.Now().UTC(),
	}
	return Result{Candidate: cand, LocalGate: local}, nil
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
		// 截断是唯一能靠"改预算"修掉的失败：原样重试必然在同一处再截断。
		if truncatedOutput(rec, policy) && attempt < max {
			policy.MaxOutputTokens = escalateOutputBudget(policy.MaxOutputTokens, r.modelMaxOut(modelID))
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
	// 章号由调用方决定：这是"一次一章"的调用，模型给错号说明它理解偏差，
	// 但正文仍属本章，覆盖比丢弃更安全（丢弃会让整单元重写）。
	parsed.Chapters[0].Chapter = ch
	return parsed.Chapters[0], nil
}

// repairFailedChapters re-generates only chapters that fail the local hard
// gate, with evidence+suggestion回注。每章最多一次修复，通过的章不动。
//
// target 必须与审校阶段一致（books.ChapterTarget），否则写作阶段会
// 觉得"字数够了"而审校阶段判 blocking，白跑一轮修复。
func (r *Runner) repairFailedChapters(ctx context.Context, modelID string, packet domain.ContextPacket, chapters []domain.ChapterDraft, target int, calls *[]domain.CallRecord) []domain.ChapterDraft {
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
		user := contextsvc.MergeExtraIntoUserPayload(r.withKnowledge(packet.UserPrompt), map[string]any{
			"chapter": ch.Chapter,
			"repairChapter": map[string]any{
				"findings": detailedFindings(fs),
				"body":     ch.Body,
				"rule":     fmt.Sprintf("第 %d 章未通过本地硬门，请定向修复这一章（其他章已合格，不需重写）；只返回这一章 JSON。", ch.Chapter),
			},
		})
		// 记录起点：generateChapter 自己也可能重试一次，所以要按本次新增的
		// 调用逐条打标，否则第一次尝试会被错标成生成调用。
		start := len(*calls)
		fixed, _, err := r.generateChapter(ctx, modelID, packet, user, ch.Chapter, calls)
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
	if modelID == "" {
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
		if err := parse(raw, &cand); err != nil {
			lastErr = err
			truncated := truncatedOutput(rec, policy)
			if truncated {
				// 预算用完是唯一能靠"改预算"修掉的失败：原样重试必然在同一处再截断。
				lastErr = fmt.Errorf("%w；上一次输出在 %d tokens 预算处被截断（思维链与答案共用这份预算），"+
					"这次只返回精简 JSON：不要复述或引用正文，条目与字段文本尽量短", err, policy.MaxOutputTokens)
			}
			if attempt < max && policy.AllowOneRepair {
				if truncated {
					policy.MaxOutputTokens = escalateOutputBudget(policy.MaxOutputTokens, r.modelMaxOut(modelID))
				}
				continue
			}
			if truncated {
				return Result{Calls: calls, Packet: packet}, fmt.Errorf("parse: %w（输出在 %d tokens 预算处被截断）", err, policy.MaxOutputTokens)
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

// truncatedOutput reports that the call used up the whole output budget, so the
// response was cut off rather than finished (thinking tokens share that budget
// on Chat Completions dialects that enable reasoning).
func truncatedOutput(rec domain.CallRecord, policy domain.CallPolicy) bool {
	return policy.MaxOutputTokens > 0 && rec.CompletionTokens >= policy.MaxOutputTokens
}

// escalateOutputBudget doubles the answer budget for a retry, never beyond what
// the model itself accepts. Truncation is the one failure a retry can actually
// fix, and only if the retry differs in the dimension that failed.
func escalateOutputBudget(current, modelMax int) int {
	if current <= 0 {
		return current
	}
	next := current * 2
	if modelMax > 0 && next > modelMax {
		next = modelMax
	}
	return next
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
	if resp.Usage != nil {
		rec.PromptTokens = resp.Usage.PromptTokens
		rec.CompletionTokens = resp.Usage.CompletionTokens
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
	"unitId", "status", "title", "function", "desire", "obstacle", "choice", "payoff",
	"pleasure", "forbidden", "wordTarget", "chapterChars", "pov", "storyDay", "gapFromPrev", "infoControl",
}

// normalizeOutlineJSON coerces model drift: storyDay/gapFromPrev as objects or
// numbers, chapterFrom/To as strings, before strict struct decode.
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
	for _, k := range []string{"chapterFrom", "chapterTo"} {
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

func joinFindings(fs []domain.ReviewFinding) string {
	var b strings.Builder
	for _, f := range fs {
		if f.Severity != "blocking" {
			continue
		}
		fmt.Fprintf(&b, "%s; ", f.Title)
	}
	return b.String()
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
