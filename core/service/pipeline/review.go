package pipeline

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
	// Rounds is how many independent review passes each chapter gets (default 3).
	//
	// 单轮覆盖约 31%，是抽样不确定性而不是审校深度：同一份正文跑 4 轮，并集
	// 128 处里 0 处被四轮全中。多轮取并集是唯一能提覆盖率的办法。轮内串行以
	// 复用前缀缓存，跨章仍并行。
	Rounds int
}

type reviewOutcome struct {
	chapter  int
	findings []domain.ReviewFinding
	calls    []domain.CallRecord
	err      error
}

// hasBlockingFinding reports whether any finding is a blocking one.
//
// 本地硬门的 OK 由 findings 反推，避免两处各判一套：加了新的本地检查（身份一致性
// 等）之后，忘了同步 OK 会让 gate 显示通过而清单里躺着 blocking。
func hasBlockingFinding(fs []domain.ReviewFinding) bool {
	for _, f := range fs {
		if f.Severity == severityBlocking {
			return true
		}
	}
	return false
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
	// 身份一致性是确定性的本地判定，必须在审校清单里就能看见。
	// 只在定稿查的代价是：正文全写完了才发现规划与时间线冲突，而那时"修哪个"已经很难。
	local.Findings = append(local.Findings, books.IdentityDeltaFindings(snap.IdentitySegments, unit, unit.OutlineRaw)...)
	if hasBlockingFinding(local.Findings) {
		local.OK = false
	}
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
			fs, calls, err := r.reviewChapter(ctx, modelID, packet, ch.Chapter, opts.Rounds, ch.Body)
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
	// 预算是"每章 × 每轮最多 2 次"（一次审校 + 一次结构修复），不是单章的总和；
	// 按章数记会让 UI 出现 6/2、7/3 这种越界数字。多轮时也必须乘上去，否则
	// 3 轮跑完的调用数会显示成严重越界。
	rounds := opts.Rounds
	if rounds < 1 {
		rounds = 1
	}
	res.Packet.Policy.MaxCalls = len(chapters) * rounds * 2

	if len(failed) > 0 {
		cand.Findings = append(cand.Findings, domain.ReviewFinding{
			ID: domain.FindingReviewIncomplete, Severity: severityAdvisory, Source: "local_gate", Lens: "coverage",
			Title:      fmt.Sprintf("审校未覆盖第 %v 章", failed),
			Evidence:   fmt.Sprintf("%d/%d 章审校失败，其余章结论已保存。", len(failed), len(chapters)),
			Suggestion: "重新审校整个单元（会先归档旧清单，再按章重审一遍）。",
		})
		res.Candidate = cand
		if firstErr != nil {
			return res, fmt.Errorf("chapters %v failed: %w", failed, firstErr)
		}
		return res, fmt.Errorf("chapters %v failed", failed)
	}
	return res, nil
}

// reviewChapter reviews a single chapter across Rounds independent rounds and
// returns the union of their findings.
//
// 多轮是必须的，不是保险：实测同一份正文跑 4 轮，并集 128 处里单轮平均只看到
// 25.4%，0 处被四轮全部抓到；3 轮并集能到约 75%。这不是审校深度问题，而是
// 每轮注意力落点不同（temperature=0 也固定不住）。
//
// 串行而不是并行：每轮拿到的是同一份 blocks + 同一个 system，只有 instruction
// 里的轮次说明不同，所以第 2 轮起几乎全部命中前缀缓存。并行会让每轮都冷启动，
// 成本从 +35% 变成 +97%。
func (r *Runner) reviewChapter(ctx context.Context, modelID string, packet domain.ContextPacket, chapter int, rounds int, body string) ([]domain.ReviewFinding, []domain.CallRecord, error) {
	if rounds < 1 {
		rounds = 1
	}
	var perRound [][]domain.ReviewFinding
	var calls []domain.CallRecord
	var lastErr error

	for round := 1; round <= rounds; round++ {
		// 单轮的包复制一份改 instruction：除这一句外与审校基准包逐字相同，
		// 缓存前缀因此仍然有效。
		p := packet
		if rounds > 1 {
			p.UserPrompt = withRoundInstruction(packet.UserPrompt, round, rounds)
			p.EstPromptTokens = (len([]rune(p.SystemPrompt)) + len([]rune(p.UserPrompt))) / 2
		}
		fs, cs, err := r.reviewChapterOnce(ctx, modelID, p, chapter, round, rounds)
		calls = append(calls, cs...)
		if err != nil {
			lastErr = err
			// 某轮失败不放弃其余轮：并集的目的是覆盖率，少一轮仍比整章丢失好。
			// 但如果一轮都没成功，调用方必须知道这次审校没有产出结论。
			continue
		}
		perRound = append(perRound, fs)
	}

	if len(perRound) == 0 {
		if lastErr != nil {
			return nil, calls, lastErr
		}
		return nil, calls, fmt.Errorf("审校未产出任何结论")
	}
	if lastErr != nil {
		// 部分轮次失败：结论仍然可用，但要让下游知道这份清单不完整，
		// 否则"少跑一轮"会被读成"这一章没问题"。
		merged := rankFindings(unionFindings(perRound, body))
		return append(merged, incompleteRoundFinding(chapter, rounds, len(perRound))), calls, nil
	}
	return rankFindings(unionFindings(perRound, body)), calls, nil
}

// reviewChapterOnce runs one review round through runJSON.
func (r *Runner) reviewChapterOnce(ctx context.Context, modelID string, packet domain.ContextPacket, chapter, round, rounds int) ([]domain.ReviewFinding, []domain.CallRecord, error) {
	var findings []domain.ReviewFinding
	// 抢救路径能解析出"半份结论"，但那不是结论：第一次截断必须交给 runJSON
	// 用「更紧凑输出」重试，第二次仍截断才收下，并保留 budget advisory 让下游
	// 判成"审校不完整"而不是"没有问题"。输出上限本身由模型配置决定。
	attempt := 0
	res, err := r.runJSON(ctx, domain.BookSnapshot{}, packet, modelID, domain.TaskReview, packet.UnitID, packet.UnitID+"#"+fmt.Sprint(chapter),
		func(raw string, cand *domain.Candidate) error {
			attempt++
			fs, err := parseReviewResponse(raw, chapter)
			if err != nil {
				return err
			}
			if attempt == 1 && hasTruncationFinding(fs) {
				return fmt.Errorf("审校输出达到模型的最大输出上限被截断，只返回了部分结论")
			}
			findings = fs
			return nil
		},
		func(cand domain.Candidate) gates.Report {
			// 审校只读正文、改不了稿：把它当成"门禁通过"以免 runJSON 再重试一次。
			return gates.Report{OK: true, Counts: map[string]int{}}
		})
	for i := range res.Calls {
		step := fmt.Sprintf("chapter-%d", chapter)
		if rounds > 1 {
			// 多轮时按轮次打标，否则三次调用的成本无从归属。
			step = fmt.Sprintf("chapter-%d-r%d", chapter, round)
		}
		res.Calls[i].Step = step
	}
	return findings, res.Calls, err
}

// withRoundInstruction appends the round marker to the payload's instruction.
//
// 只动 instruction 这一个字段（它排在 documents 之后），所以前几轮的字节前缀
// 不变，缓存照常命中。同时明确告诉模型"这是第几轮"，避免它以为在重试同一个
// 失败的输出而收敛到上一次的结论。
func withRoundInstruction(userPrompt string, round, rounds int) string {
	p, err := contextsvc.DecodeUserPayload(userPrompt)
	if err != nil {
		return userPrompt
	}
	note := fmt.Sprintf("\n\n这是本章独立审校的第 %d/%d 轮：请按你自己的判断从头审一遍。", round, rounds)
	if !strings.Contains(p.Instruction, note) {
		p.Instruction += note
	}
	return contextsvc.EncodeUserPayload(p)
}

// incompleteRoundFinding marks a review whose rounds did not all succeed.
//
// 必须把它当成"审校不完整"而不是"没有更多问题"：少跑的那一轮里可能有硬伤，
// 沉默不是证据。
func incompleteRoundFinding(chapter, rounds, ok int) domain.ReviewFinding {
	return domain.ReviewFinding{
		ID: domain.FindingReviewIncomplete, Severity: severityAdvisory, Source: "local_gate", Lens: "coverage",
		Chapter:    chapter,
		Title:      fmt.Sprintf("第 %d 章有 %d/%d 轮审校失败", chapter, rounds-ok, rounds),
		Evidence:   fmt.Sprintf("只合并了 %d 轮的结论，缺的那几轮可能还有问题。", ok),
		Suggestion: "重跑本章审校，或检查是否个别轮次撞到输出上限。",
	}
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
