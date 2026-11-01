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
	target := books.WordTargetPerChapter(unit)
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
			fixed, calls, err := r.reviseChapter(ctx, modelID, snap, unit, ch, byChapter[ch.Chapter], baseline)
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
			return Result{Calls: calls, LocalGate: local}, fmt.Errorf("chapter %d: %w", chapters[out.index].Chapter, out.err)
		}
		revised[out.index] = out.fixed
	}

	// Verify untouched chapters are byte-identical to the original.
	for idx, ch := range revised {
		if len(byChapter[ch.Chapter]) > 0 {
			continue
		}
		if chapterHash(ch) != chapterHash(chapters[idx]) {
			return Result{Calls: calls, LocalGate: local},
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

	res := Result{Candidate: cand, Calls: calls, LocalGate: rep}
	res.Packet = contextsvc.BuildReviseChapterPacket(snap, unit, chapters[0], findings, baseline, r.modelMaxOut(modelID))
	// 预算按"实际要修的章数 × 每章最多 2 次"，UI 才不会显示 3/2 这种越界数字。
	touched := 0
	for _, ch := range chapters {
		if len(byChapter[ch.Chapter]) > 0 {
			touched++
		}
	}
	res.Packet.Policy.MaxCalls = touched * 2
	return res, nil
}

// reviseChapter revises a single chapter against the given findings.
func (r *Runner) reviseChapter(ctx context.Context, modelID string, snap domain.BookSnapshot, unit domain.UnitInfo, ch domain.ChapterDraft, findings []domain.ReviewFinding, baseline contextsvc.ReviewBaseline) (domain.ChapterDraft, []domain.CallRecord, error) {
	packet := contextsvc.BuildReviseChapterPacket(snap, unit, ch, findings, baseline, r.modelMaxOut(modelID))

	var fixed domain.ChapterDraft
	res, err := r.runJSON(ctx, snap, packet, modelID, domain.TaskRevise, unit.UnitID, unit.ProsePath,
		func(raw string, cand *domain.Candidate) error {
			var parsed struct {
				Chapter int    `json:"chapter"`
				Title   string `json:"title"`
				Body    string `json:"body"`
			}
			if err := unmarshalJSON(raw, &parsed); err != nil {
				return err
			}
			// title 缺失时沿用原标题（修订不该丢掉章名）；章号与正文仍严格校验——
			// 章号错会把别的章的内容写回目标章。
			draft := domain.ChapterDraft{Chapter: parsed.Chapter, Title: parsed.Title, Body: parsed.Body}
			if strings.TrimSpace(draft.Title) == "" {
				draft.Title = ch.Title
			}
			if err := validateRevisedChapter(draft, ch.Chapter); err != nil {
				return err
			}
			fixed = draft
			cand.Chapters = []domain.ChapterDraft{fixed}
			return nil
		},
		func(cand domain.Candidate) gates.Report {
			return gates.CheckProseWithRules(cand.Chapters, 0, r.proseRules())
		})
	for i := range res.Calls {
		res.Calls[i].Step = fmt.Sprintf("revise-ch%d", ch.Chapter)
	}
	return fixed, res.Calls, err
}

// chapterHash returns a short hash of a chapter draft for byte-identity checks.
func chapterHash(ch domain.ChapterDraft) string {
	h := sha256.Sum256([]byte(ch.Body))
	return hex.EncodeToString(h[:8])
}
