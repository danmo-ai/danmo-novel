package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/gates"
	"github.com/google/uuid"
)

// ReviseOptions bounds how a revision is executed.
type ReviseOptions struct {
	// Concurrency limits parallel per-chapter revise calls (default 3).
	Concurrency int
}

// RunRevise applies selected findings, grouped by chapter, in bounded parallel.
// Chapters with no selected findings are kept byte-identical; the hash check
// below is the contract that makes "minimum change" enforceable in code rather
// than in prompt wording only.
func (r *Runner) RunRevise(ctx context.Context, snap domain.BookSnapshot, unit domain.UnitInfo, findings []domain.ReviewFinding, modelID string) (Result, error) {
	return r.RunReviseWithOptions(ctx, snap, unit, findings, modelID, ReviseOptions{})
}

// RunReviseWithOptions is RunRevise with an explicit concurrency budget.
func (r *Runner) RunReviseWithOptions(ctx context.Context, snap domain.BookSnapshot, unit domain.UnitInfo, findings []domain.ReviewFinding, modelID string, opts ReviseOptions) (Result, error) {
	chapters := gates.ParseProseChapters(unit.ProseRaw)
	if len(chapters) == 0 {
		return Result{}, fmt.Errorf("no chapters to revise")
	}
	target := books.ChapterTarget(unit)
	local := gates.CheckProseWithRules(chapters, target, r.proseRules())
	baseline := contextsvc.BuildReviewBaseline(snap, unit, nil, r.ReviewChecklist)

	byChapter := map[int][]domain.ReviewFinding{}
	for _, f := range findings {
		byChapter[f.Chapter] = append(byChapter[f.Chapter], f)
	}
	// 章号对不上就修不了：以前静默无操作——候选与原稿逐字节相同，作者点「确认
	// 落盘」等于什么都没发生。整单元审校产出的"全单元级"意见（chapter=0）必然
	// 落到这里，所以必须报出来，而不是假装修过了。
	known := map[int]bool{}
	for _, ch := range chapters {
		known[ch.Chapter] = true
	}
	var unmapped []string
	for _, f := range findings {
		if known[f.Chapter] {
			continue
		}
		label := f.Title
		if label == "" {
			label = f.ID
		}
		if f.Chapter > 0 {
			label = fmt.Sprintf("第 %d 章：%s", f.Chapter, label)
		} else {
			label = "（未标章号）" + label
		}
		unmapped = append(unmapped, label)
	}
	if len(unmapped) > 0 {
		return Result{LocalGate: local}, fmt.Errorf(
			"这些审校意见无法定位到具体章节，逐章修订无从下手：%s；请改选带章号的意见，或先按章重新审校",
			strings.Join(unmapped, "；"))
	}

	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 3
	}
	if concurrency > 8 {
		concurrency = 8
	}

	type outcome struct {
		index int
		fixed domain.ChapterDraft
		calls []domain.CallRecord
		err   error
	}
	// 预算在任何调用之前就定下来：run 失败时也要能看到"上限是几次"，
	// 否则 UI 显示"调用预算 0 次"，成本无从判断。补丁 → 补丁修复 → 整章兜底，
	// 每章最多 3 次，按实际要修的章数申报。
	packet := contextsvc.BuildReviseChapterPacket(snap, unit, chapters[0], findings, chapterPrevTail(chapters, 0, baseline), baseline, r.modelMaxOut(modelID))
	touched := 0
	for _, ch := range chapters {
		if len(byChapter[ch.Chapter]) > 0 {
			touched++
		}
	}
	packet.Policy.MaxCalls = touched * 3

	results := make([]outcome, len(chapters))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for idx, ch := range chapters {
		results[idx] = outcome{index: idx, fixed: ch}
		if len(byChapter[ch.Chapter]) == 0 {
			continue // 未涉及的章原样保留，不发起调用
		}
		wg.Add(1)
		go func(idx int, ch domain.ChapterDraft) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fixed, calls, err := r.reviseChapter(ctx, modelID, snap, unit, ch, byChapter[ch.Chapter], chapterPrevTail(chapters, idx, baseline), baseline)
			results[idx].fixed = fixed
			results[idx].calls = calls
			results[idx].err = err
		}(idx, ch)
	}
	wg.Wait()

	revised := make([]domain.ChapterDraft, len(chapters))
	var calls []domain.CallRecord
	for _, out := range results {
		calls = append(calls, out.calls...)
		if out.err != nil {
			return Result{Calls: calls, LocalGate: local, Packet: packet},
				fmt.Errorf("chapter %d: %w", chapters[out.index].Chapter, out.err)
		}
		revised[out.index] = out.fixed
	}

	// Verify untouched chapters are byte-identical to the original.
	for idx, ch := range revised {
		if len(byChapter[ch.Chapter]) > 0 {
			continue
		}
		if chapterHash(ch) != chapterHash(chapters[idx]) {
			return Result{Calls: calls, LocalGate: local, Packet: packet},
				fmt.Errorf("chapter %d was modified but not in scope; refusing candidate", ch.Chapter)
		}
	}

	cand := domain.Candidate{
		ID: uuid.NewString(), BookID: snap.Summary.ID, Kind: domain.TaskRevise,
		Status: domain.CandidateOpen, BaseRevision: snap.Revision,
		TargetPath: unit.ProsePath, UnitID: unit.UnitID,
		// 保留修订前正文：前端据此做 diff，"确认落盘"必须有据可依。
		BaseChapters: append([]domain.ChapterDraft{}, chapters...),
		Chapters:     revised,
		CreatedAt:    time.Now().UTC(),
	}
	// 只报修订后正文的门禁结果。以前把修订前的 local 也拼进去，候选会带着
	// "字数不足 chars=1394" 这种早已修好的旧结论（实际已 2305 字），
	// 作者与自动流程都会据此误判。
	rep := gates.CheckProseWithRules(revised, target, r.proseRules())
	cand.Findings = append(cand.Findings, rep.Findings...)

	res := Result{Candidate: cand, Calls: calls, LocalGate: rep, Packet: packet}
	return res, nil
}

// chapterPrevTail returns the "previous chapter tail" a chapter's packet carries.
//
// 审校与修订必须用同一个值：blocks 里「上一章末 / 最近章摘要」的选择依赖它，
// 两处不一致就会让同一章的两份包在那一块分叉，前缀缓存白丢。逻辑与审校路径
// 逐字相同（首章退到 baseline.PrevTail，其余带上一章末 800 字）。
func chapterPrevTail(chapters []domain.ChapterDraft, idx int, baseline contextsvc.ReviewBaseline) string {
	if idx <= 0 {
		return baseline.PrevTail
	}
	if idx >= len(chapters) {
		return baseline.PrevTail
	}
	return tailOf(chapters[idx-1].Body, 800)
}

// reviseChapter revises a single chapter against the given findings.
//
// 三段一条线：补丁 → 补丁修复 → 整章兜底。
//
// 三次调用共用同一个 system prompt 与同一批 blocks，只有 instruction 里的输出
// 契约与尾部 repair 不同，所以三次都命中审校留下的前缀缓存——包括最后的整章
// 兜底。prevTail 必须与审校那条路径传同一个值，否则那边块选择一变，前缀就散了。
func (r *Runner) reviseChapter(ctx context.Context, modelID string, snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, findings []domain.ReviewFinding, prevTail string, baseline contextsvc.ReviewBaseline) (domain.ChapterDraft, []domain.CallRecord, error) {
	patchPacket := contextsvc.BuildReviseChapterPacket(snap, unit, ch, findings, prevTail, baseline, r.modelMaxOut(modelID))
	fallbackPacket := contextsvc.BuildReviseChapterFallbackPacket(snap, unit, ch, findings, prevTail, baseline, r.modelMaxOut(modelID))

	max := patchPacket.Policy.MaxCalls
	if max < 1 {
		max = 1
	}

	var calls []domain.CallRecord
	var lastRaw string
	var lastErr error
	var attemptErrs []string
	var fixed domain.ChapterDraft
	done := false

	for attempt := 1; attempt <= max; attempt++ {
		// 最后一次走整章兜底：整章输出必然能落盘，是补丁路径失败后的唯一出口。
		isFallback := attempt == max && max > 1
		packet := patchPacket
		step := fmt.Sprintf("revise-ch%d", ch.Chapter)
		if isFallback {
			packet = fallbackPacket
			step = fmt.Sprintf("revise-ch%d-fallback", ch.Chapter)
		}
		user := r.withKnowledge(packet.UserPrompt)
		// 只有补丁轮的第二次才注入 repair：它携带的是锚点定位失败的具体原因，
		// 整章兜底看到那个原因没有意义，反而会把正文往"重写"方向推。
		if !isFallback && attempt > 1 {
			user = contextsvc.MergeRepairIntoUserPayload(user, lastErr)
			step = fmt.Sprintf("revise-ch%d-repair", ch.Chapter)
		}

		raw, rec, err := r.chat(ctx, modelID, packet.SystemPrompt, user, packet.Policy)
		rec.Step = step
		rec.Index = len(calls) + 1
		calls = append(calls, rec)
		if err != nil {
			lastErr = err
			attemptErrs = append(attemptErrs, fmt.Sprintf("%s: %v", step, err))
			continue
		}
		lastRaw = raw

		var draft domain.ChapterDraft
		var perr error
		if isFallback {
			draft, perr = parseRevisedChapterBody(raw, ch)
		} else {
			draft, perr = parseChapterPatch(raw, ch)
		}
		if perr != nil {
			lastErr = perr
			// 截断只认协议信号：把"撞输出上限"说成"格式不对"，模型只会越改越短。
			if truncatedOutput(rec) {
				perr = truncationRetryError(perr)
			}
			attemptErrs = append(attemptErrs, fmt.Sprintf("%s: %v", step, perr))
			continue
		}

		rep := gates.CheckProseWithRules([]domain.ChapterDraft{draft}, 0, r.proseRules())
		if !rep.OK && attempt < max {
			lastErr = fmt.Errorf("本地硬门未通过：\n%s", detailedFindings(rep.Findings))
			attemptErrs = append(attemptErrs, fmt.Sprintf("%s: %v", step, lastErr))
			continue
		}
		fixed = draft
		done = true
		break
	}

	if !done {
		// 逐次保留原因：只报最后一次会把"章号写错"这种可直接修的原因埋在
		// 兜底轮的 JSON 解析失败后面，作者与模型都看不到真正该改的东西。
		return domain.ChapterDraft{}, calls, fmt.Errorf("budget exhausted: %s; last=%s",
			clip(strings.Join(attemptErrs, "；"), 600), clip(lastRaw, 200))
	}
	return fixed, calls, nil
}

// parseRevisedChapterBody enforces the whole-chapter fallback contract.
func parseRevisedChapterBody(raw string, ch domain.ChapterDraft) (domain.ChapterDraft, error) {
	var parsed struct {
		Chapter int    `json:"chapter"`
		Title   string `json:"title"`
		Body    string `json:"body"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return domain.ChapterDraft{}, err
	}
	// 章号缺省按本次调用的章号补齐（与补丁路径一致）；写错章号必须拒绝——
	// 那会把别的章的内容写回目标章。
	if parsed.Chapter == 0 {
		parsed.Chapter = ch.Chapter
	}
	// title 缺失时沿用原标题（修订不该丢掉章名）。
	draft := domain.ChapterDraft{Chapter: parsed.Chapter, Title: strings.TrimSpace(parsed.Title), Body: parsed.Body}
	if draft.Title == "" {
		draft.Title = ch.Title
	}
	if err := validateRevisedChapter(draft, ch.Chapter); err != nil {
		return domain.ChapterDraft{}, err
	}
	return draft, nil
}

// chapterHash returns a short hash of a chapter draft for byte-identity checks.
func chapterHash(ch domain.ChapterDraft) string {
	h := sha256.Sum256([]byte(ch.Body))
	return hex.EncodeToString(h[:8])
}
