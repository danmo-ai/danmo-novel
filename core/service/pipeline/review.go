package pipeline

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/gates"
	"github.com/google/uuid"
)

// ReviewOptions configures a unit review run.
type ReviewOptions struct {
	// SplitByChapter runs one LLM call per chapter instead of one unit-wide call.
	// The baseline context is built once; each chapter call only carries that
	// chapter's prose plus the previous chapter's tail.
	SplitByChapter bool
	// Concurrency bounds how many chapter calls run in parallel (default 3).
	Concurrency int
}

type reviewOutcome struct {
	chapter  int
	findings []domain.ReviewFinding
	calls    []domain.CallRecord
	err      error
}

// RunReviewByChapter runs one review call per chapter, in bounded parallel.
// 单章失败不会带走其他章的结论：它们仍随候选一起落盘，另附一条「审校未覆盖」
// 提示，下游据此判成"审校不完整"，不会被当成"没有问题"放行。
func (r *Runner) RunReviewByChapter(ctx context.Context, snap domain.BookSnapshot, unit domain.UnitInfo, modelID string, opts ReviewOptions) (Result, error) {
	chapters := gates.ParseProseChapters(unit.ProseRaw)
	if len(chapters) == 0 {
		return Result{}, fmt.Errorf("no chapters to review")
	}
	target := books.ChapterTarget(unit)
	local := gates.CheckProseWithRules(chapters, target, r.proseRules())
	baseline := contextsvc.BuildReviewBaseline(snap, unit, local.Findings, r.ReviewChecklist)

	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 3
	}
	if concurrency > 8 {
		concurrency = 8
	}

	results := make([]reviewOutcome, len(chapters))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for idx, ch := range chapters {
		results[idx] = reviewOutcome{chapter: ch.Chapter}
		wg.Add(1)
		go func(idx int, ch domain.ChapterDraft) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			prevTail := baseline.PrevTail
			if idx > 0 {
				prevTail = tailOf(chapters[idx-1].Body, 800)
			}
			packet := contextsvc.BuildChapterReviewPacket(snap, unit, ch, prevTail, baseline, r.modelMaxOut(modelID))
			fs, calls, err := r.reviewChapter(ctx, modelID, packet, ch.Chapter)
			results[idx].findings = fs
			results[idx].calls = calls
			results[idx].err = err
		}(idx, ch)
	}
	wg.Wait()

	cand := domain.Candidate{
		ID: uuid.NewString(), BookID: snap.Summary.ID, Kind: domain.TaskReview,
		Status: domain.CandidateOpen, BaseRevision: snap.Revision,
		TargetPath: unit.ProsePath, UnitID: unit.UnitID,
		Chapters: chapters, Findings: append([]domain.ReviewFinding{}, local.Findings...),
		CreatedAt: time.Now().UTC(),
	}

	var allCalls []domain.CallRecord
	var failed []int
	var firstErr error
	for _, out := range results {
		allCalls = append(allCalls, out.calls...)
		if out.err != nil {
			failed = append(failed, out.chapter)
			if firstErr == nil {
				firstErr = out.err
			}
			continue
		}
		cand.Findings = append(cand.Findings, out.findings...)
	}
	sort.SliceStable(allCalls, func(i, j int) bool { return allCalls[i].Index < allCalls[j].Index })

	res := Result{Candidate: cand, Calls: allCalls, LocalGate: local}
	// 让 run.Policy / ContextSummary 有真实值，否则 UI 显示"调用预算 0 次"。
	res.Packet = contextsvc.BuildChapterReviewPacket(snap, unit, chapters[0], baseline.PrevTail, baseline, r.modelMaxOut(modelID))
	// 分章预算是"每章最多 2 次"（生成 + 一次结构修复），不是单章的 2 次总和；
	// 按章数记会让 UI 出现 6/2、7/3 这种越界数字。
	res.Packet.Policy.MaxCalls = len(chapters) * 2

	if len(failed) > 0 {
		cand.Findings = append(cand.Findings, domain.ReviewFinding{
			ID: domain.FindingReviewIncomplete, Severity: severityAdvisory, Source: "local_gate", Lens: "coverage",
			Title:      fmt.Sprintf("审校未覆盖第 %v 章", failed),
			Evidence:   fmt.Sprintf("%d/%d 章审校失败，其余章结论已保存。", len(failed), len(chapters)),
			Suggestion: "先按失败章重审整个单元（重审会重新审所有章，旧清单随之归档）。",
		})
		res.Candidate = cand
		if firstErr != nil {
			return res, fmt.Errorf("chapters %v failed: %w", failed, firstErr)
		}
		return res, fmt.Errorf("chapters %v failed", failed)
	}
	return res, nil
}

// reviewChapter reviews a single chapter, returning its findings and the real
// call records (token usage, latency) so the run keeps accurate accounting.
func (r *Runner) reviewChapter(ctx context.Context, modelID string, packet domain.ContextPacket, chapter int) ([]domain.ReviewFinding, []domain.CallRecord, error) {
	var findings []domain.ReviewFinding
	// 抢救路径能解析出"半份结论"，但那不是结论：第一次截断必须交给 runJSON
	// 用翻倍预算重试（截断是唯一能靠改预算修掉的失败），第二次仍截断才收下，
	// 并保留 budget advisory 让下游判成"审校不完整"而不是"没有问题"。
	attempt := 0
	res, err := r.runJSON(ctx, domain.BookSnapshot{}, packet, modelID, domain.TaskReview, packet.UnitID, packet.UnitID+"#"+fmt.Sprint(chapter),
		func(raw string, cand *domain.Candidate) error {
			attempt++
			fs, err := parseReviewResponse(raw, chapter)
			if err != nil {
				return err
			}
			if attempt == 1 && hasTruncationFinding(fs) {
				return fmt.Errorf("审校输出在 %d tokens 预算处被截断", packet.Policy.MaxOutputTokens)
			}
			findings = fs
			return nil
		},
		func(cand domain.Candidate) gates.Report {
			// 审校只读正文、改不了稿：把它当成"门禁通过"以免 runJSON 再重试一次。
			return gates.Report{OK: true, Counts: map[string]int{}}
		})
	for i := range res.Calls {
		res.Calls[i].Step = fmt.Sprintf("chapter-%d", chapter)
	}
	return findings, res.Calls, err
}

// hasTruncationFinding reports whether the salvaged conclusions carry the
// truncation marker, i.e. the model was cut off rather than finished.
func hasTruncationFinding(fs []domain.ReviewFinding) bool {
	for _, f := range fs {
		if f.ID == domain.FindingReviewTruncated {
			return true
		}
	}
	return false
}

// parseReviewResponse interprets a review response and enforces its contract.
//
// 严格路径（JSON 解析成功）必须完全合规：verdict 是枚举，每条 finding 的
// id/severity/title/evidence 必填——severity 未知值不能静默降级成 advisory，
// 那会把一条真硬伤藏起来。违规会作为错误喂回模型重试。
//
// 抢救路径（输出被输出预算截断）只做安全规范化：截断的产物判不出 severity，
// 也补不出 id，所以按 advisory 收下并追加一条"已截断"提示，而不是丢掉整份
// 结论——重试由 budget-escalation 负责，不是靠丢弃。
func parseReviewResponse(raw string, wantChapter int) ([]domain.ReviewFinding, error) {
	var parsed struct {
		Verdict  string                 `json:"verdict"`
		Findings []domain.ReviewFinding `json:"findings"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		_, findings, truncated, serr := salvageReviewJSON(raw)
		if serr != nil {
			// 原始解析错误信息量更大（no JSON object / 截断位置）。
			return nil, err
		}
		for i := range findings {
			normalizeSalvagedFinding(&findings[i], wantChapter, i)
		}
		if truncated {
			findings = append(findings, domain.ReviewFinding{
				ID: domain.FindingReviewTruncated, Severity: severityAdvisory, Source: "local_gate", Lens: "budget",
				Chapter:    wantChapter,
				Title:      fmt.Sprintf("审校输出被截断，仅保留已解析的 %d 条", len(findings)),
				Evidence:   fmt.Sprintf("模型输出在输出预算处中断（约 %d 字）。", len([]rune(raw))),
				Suggestion: "重跑审校，或降低思考强度 / 换用输出上限更高的模型；不要直接定稿。",
			})
		}
		return findings, nil
	}

	if err := validateVerdict(parsed.Verdict); err != nil {
		return nil, err
	}
	for i := range parsed.Findings {
		if parsed.Findings[i].Source == "" {
			parsed.Findings[i].Source = "ai_review"
		}
		// chapter=0 视为"没写"，按本次审校的章号补上（调用范围已限定在这一章）；
		// 写了别的章号才是矛盾，交给校验报错。
		if parsed.Findings[i].Chapter == 0 && wantChapter > 0 {
			parsed.Findings[i].Chapter = wantChapter
		}
	}
	if err := validateReviewFindings(parsed.Findings, wantChapter); err != nil {
		return nil, err
	}
	return parsed.Findings, nil
}

// tailOf returns the last n runes of s, for cross-chapter continuity context.
func tailOf(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// RunReview keeps the unit-wide review path (used when SplitByChapter is off).
func (r *Runner) RunReview(ctx context.Context, snap domain.BookSnapshot, unit domain.UnitInfo, modelID string) (Result, error) {
	chapters := gates.ParseProseChapters(unit.ProseRaw)
	target := books.ChapterTarget(unit)
	local := gates.CheckProseWithRules(chapters, target, r.proseRules())
	packet := contextsvc.BuildReviewPacket(snap, unit, local.Findings, r.modelMaxOut(modelID), r.ReviewChecklist)
	res, err := r.runJSON(ctx, snap, packet, modelID, domain.TaskReview, unit.UnitID, unit.ProsePath, func(raw string, cand *domain.Candidate) error {
		findings, err := parseReviewResponse(raw, 0)
		if err != nil {
			return err
		}
		// 整单元审校没有"本次只审这一章"的天然约束，必须对着本单元的章号表校验，
		// 否则一条越界章号会让后续「自动修复」整批报无法定位。
		if err := validateFindingsWithinUnit(findings, chapters); err != nil {
			return err
		}
		cand.Findings = append(append([]domain.ReviewFinding{}, local.Findings...), findings...)
		cand.Chapters = chapters
		return nil
	}, func(cand domain.Candidate) gates.Report {
		return gates.Report{OK: true, Counts: map[string]int{}}
	})
	res.LocalGate = local
	return res, err
}
