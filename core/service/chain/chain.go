package chain

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	"github.com/danmo-ai/danmo-novel/core/service/knowledge"
	"github.com/danmo-ai/danmo-novel/core/service/runs"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
	"github.com/google/uuid"
)

// maxInstructionRunes bounds the author's 追加指令 so a chain snapshot stays
// small and the prompt budget predictable.
const maxInstructionRunes = 2000

// errStop unwinds execute() after an interruption/cancel has been persisted.
var errStop = errors.New("chain stopped")

// maxConcurrentRunWait bounds how long a stage waits for an in-flight manual
// call on the same unit before giving up and asking the author.
const maxConcurrentRunWait = 15 * time.Minute

// Service runs 连续单元重写 chains.
//
// 链本身不构造 prompt：每个阶段都用同一套 TaskSpec 调 runs.Service（与手工
// 点击完全同一条 execute → Pipeline → ContextBuilder 通路），链只负责排序、
// 自动接受、门禁校验和中断/恢复。
type Service struct {
	Store    *sqlite.Store
	Settings *config.Manager
	Runs     *runs.Service

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	subRuns map[string]string // chainID → 当前子 run ID
}

func New(store *sqlite.Store, settings *config.Manager, runSvc *runs.Service) *Service {
	return &Service{
		Store: store, Settings: settings, Runs: runSvc,
		cancels: map[string]context.CancelFunc{},
		subRuns: map[string]string{},
	}
}

// Start validates the plan, persists the chain, then runs it in the background.
func (s *Service) Start(ctx context.Context, spec domain.ChainSpec) (domain.Chain, error) {
	spec = normalizeSpec(spec)
	if spec.BookID == "" {
		return domain.Chain{}, fmt.Errorf("bookId required")
	}
	root, readOnly, err := s.Store.GetBookRoot(ctx, spec.BookID)
	if err != nil {
		return domain.Chain{}, err
	}
	if readOnly {
		return domain.Chain{}, fmt.Errorf("书籍为只读，无法连续重写")
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		return domain.Chain{}, err
	}
	if !books.HasBookOutline(snap) {
		return domain.Chain{}, fmt.Errorf("缺少全书总纲，请先完成立项")
	}
	volume, err := resolveVolume(snap, spec)
	if err != nil {
		return domain.Chain{}, err
	}
	if strings.TrimSpace(snap.Files[fmt.Sprintf("outline/volumes/v%02d.md", volume)]) == "" {
		return domain.Chain{}, fmt.Errorf("第 %d 卷缺少卷纲，请先生成/接受卷纲", volume)
	}
	units, err := planUnits(snap, volume, spec)
	if err != nil {
		return domain.Chain{}, err
	}
	if err := validateKnowledge(spec, s.libraryRoot()); err != nil {
		return domain.Chain{}, err
	}
	if err := s.preflightModels(spec); err != nil {
		return domain.Chain{}, err
	}
	if active, ok, _ := s.Store.FindActiveChain(ctx, spec.BookID); ok {
		return active, fmt.Errorf("该书已有连续重写在运行：%s", active.ID)
	}

	now := time.Now().UTC()
	ch := domain.Chain{
		ID: uuid.NewString(), BookID: spec.BookID, Volume: volume,
		Status: domain.ChainPending, Stage: domain.ChainStageOutline,
		Spec: spec, CreatedAt: now, UpdatedAt: now,
	}
	for _, u := range units {
		note := "新写"
		if u.HasProse {
			note = "重写"
		}
		ch.Units = append(ch.Units, domain.ChainUnitState{
			UnitID: u.UnitID, Stage: domain.ChainStageOutline,
			Status: domain.ChainUnitPending, Note: note,
		})
	}
	if err := s.Store.SaveChain(ctx, ch); err != nil {
		return domain.Chain{}, err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[ch.ID] = cancel
	s.mu.Unlock()
	go s.execute(runCtx, ch.ID)
	return ch, nil
}

// Get returns one chain.
func (s *Service) Get(ctx context.Context, chainID string) (domain.Chain, error) {
	return s.Store.GetChain(ctx, chainID)
}

// List returns chains of a book (empty bookID = all).
func (s *Service) List(ctx context.Context, bookID string) ([]domain.Chain, error) {
	return s.Store.ListChains(ctx, bookID)
}

// Latest returns the most recently updated chain for a book.
func (s *Service) Latest(ctx context.Context, bookID string) (domain.Chain, bool) {
	ch, ok, err := s.Store.LatestChain(ctx, bookID)
	if err != nil {
		return domain.Chain{}, false
	}
	return ch, ok
}

// RecoverInterrupted moves chains orphaned by a process restart to
// waiting_manual so the author can resume from the interrupted stage instead of
// polling a "running" chain forever. Call once at startup.
func (s *Service) RecoverInterrupted(ctx context.Context) (int, error) {
	list, err := s.Store.ListUnfinishedChains(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, ch := range list {
		s.mu.Lock()
		_, live := s.cancels[ch.ID]
		s.mu.Unlock()
		if live {
			continue
		}
		ch.Status = domain.ChainWaitingManual
		ch.Error = "服务重启导致连续重写中断，可点击继续从中断处恢复"
		if ch.Cursor < len(ch.Units) && ch.Units[ch.Cursor].Status == domain.ChainUnitRunning {
			ch.Units[ch.Cursor].Status = domain.ChainUnitBlocked
			ch.Units[ch.Cursor].Error = ch.Error
		}
		ch.UpdatedAt = time.Now().UTC()
		if err := s.Store.SaveChain(ctx, ch); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Cancel stops the chain and its current child run.
func (s *Service) Cancel(ctx context.Context, chainID string) error {
	ch, err := s.Store.GetChain(ctx, chainID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	cancel := s.cancels[chainID]
	runID := s.subRuns[chainID]
	s.mu.Unlock()
	if runID != "" {
		_ = s.Runs.Cancel(ctx, runID)
	}
	if cancel != nil {
		cancel()
	}
	switch ch.Status {
	case domain.ChainCancelled, domain.ChainCompleted:
		return nil
	}
	ch.Status = domain.ChainCancelled
	ch.Error = "cancelled"
	ch.UpdatedAt = time.Now().UTC()
	return s.Store.SaveChain(ctx, ch)
}

// Resume continues a chain after the author repaired the blocked unit.
//
// 起点按单元的当前状态推导，尊重人工修复结果：
// proposed→细纲、accepted→正文、drafted→审校；已 reviewed 的单元视为人工完成。
func (s *Service) Resume(ctx context.Context, chainID string) (domain.Chain, error) {
	ch, err := s.Store.GetChain(ctx, chainID)
	if err != nil {
		return domain.Chain{}, err
	}
	switch ch.Status {
	case domain.ChainWaitingManual, domain.ChainFailed, domain.ChainCancelled:
	default:
		return ch, fmt.Errorf("当前状态不可继续：%s", ch.Status)
	}
	if active, ok, _ := s.Store.FindActiveChain(ctx, ch.BookID); ok && active.ID != ch.ID {
		return ch, fmt.Errorf("已有连续重写在运行：%s", active.ID)
	}
	if ch.Cursor >= len(ch.Units) {
		ch.Status = domain.ChainCompleted
		ch.UpdatedAt = time.Now().UTC()
		return ch, s.Store.SaveChain(ctx, ch)
	}
	root, _, err := s.Store.GetBookRoot(ctx, ch.BookID)
	if err != nil {
		return domain.Chain{}, err
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		return domain.Chain{}, err
	}
	unitID := ch.Units[ch.Cursor].UnitID
	open, _ := s.Store.ListOpenCandidates(ctx, ch.BookID)
	for _, c := range open {
		if c.UnitID == unitID {
			return ch, fmt.Errorf("单元 %s 还有待审候选，请先接受或拒绝后再继续", unitID)
		}
	}
	unit, ok := findUnit(snap, unitID)
	if !ok {
		return ch, fmt.Errorf("单元不存在：%s", unitID)
	}

	stage := stageForStatus(unit.Status)
	if unit.Status == domain.UnitReviewed {
		ch.Units[ch.Cursor].Status = domain.ChainUnitDone
		ch.Units[ch.Cursor].Stage = domain.ChainStageFinalize
		ch.Units[ch.Cursor].Error = ""
		ch.Cursor++
		if ch.Cursor >= len(ch.Units) {
			ch.Status = domain.ChainCompleted
			ch.Error = ""
			ch.UpdatedAt = time.Now().UTC()
			return ch, s.Store.SaveChain(ctx, ch)
		}
		stage = domain.ChainStageOutline
	}
	ch.Stage = stage
	ch.Units[ch.Cursor].Stage = stage
	ch.Units[ch.Cursor].Status = domain.ChainUnitPending
	ch.Units[ch.Cursor].Error = ""
	ch.Status = domain.ChainPending
	ch.Error = ""
	ch.UpdatedAt = time.Now().UTC()
	if err := s.Store.SaveChain(ctx, ch); err != nil {
		return ch, err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[ch.ID] = cancel
	s.mu.Unlock()
	go s.execute(runCtx, ch.ID)
	return ch, nil
}

// ---- execution ----

func (s *Service) execute(ctx context.Context, chainID string) {
	defer func() {
		s.mu.Lock()
		delete(s.cancels, chainID)
		delete(s.subRuns, chainID)
		s.mu.Unlock()
	}()
	for {
		ch, err := s.Store.GetChain(context.Background(), chainID)
		if err != nil {
			return
		}
		switch ch.Status {
		case domain.ChainCancelled, domain.ChainCompleted, domain.ChainFailed, domain.ChainWaitingManual:
			return
		}
		if ctx.Err() != nil {
			s.cancelChain(chainID)
			return
		}
		if ch.Cursor >= len(ch.Units) {
			ch.Status = domain.ChainCompleted
			ch.UpdatedAt = time.Now().UTC()
			_ = s.Store.SaveChain(context.Background(), ch)
			return
		}
		stage := ch.Stage
		if stage == "" {
			stage = domain.ChainStageOutline
		}
		if err := s.runStage(ctx, &ch, ch.Units[ch.Cursor].UnitID, stage); err != nil {
			return // 中断/取消已持久化
		}
	}
}

// settleActiveRun resolves a pre-existing run of the same kind for this unit.
//
// 两种情形必须区别对待：
//   - 还在跑（pending/running）：那是一次正在进行的手工调用，等它结束即可，
//     不该让整条链因此中断——作者常常是先点审校、再点连续重写。
//   - 停在 waiting_accept：候选不处理就永远占位，只能交回作者明确处理。
//
// 返回 blocked=true 表示链无法继续，why 是给作者看的说明。
func (s *Service) settleActiveRun(ctx context.Context, ch *domain.Chain, stage domain.ChainStage, unitID string) (bool, string) {
	kind := domain.TaskKind(stage)
	active, busy, err := s.Store.FindActiveRun(ctx, ch.BookID, kind, unitID)
	if err != nil || !busy {
		return false, ""
	}
	if active.Status == domain.RunPending || active.Status == domain.RunRunning {
		// 有界等待：调用自身有超时，这里再加一层上限，避免孤儿行无限挂住链。
		waitCtx, cancel := context.WithTimeout(ctx, maxConcurrentRunWait)
		defer cancel()
		if _, werr := s.Runs.WaitRun(waitCtx, active.ID); werr != nil {
			if ctx.Err() != nil {
				return true, "已取消"
			}
			return true, fmt.Sprintf("等待单元 %s 上正在进行的调用超时（%s），请稍后继续",
				unitID, maxConcurrentRunWait)
		}
		// 等完之后状态可能变成待接受，重新判断。
		active, busy, err = s.Store.FindActiveRun(ctx, ch.BookID, kind, unitID)
		if err != nil || !busy {
			return false, ""
		}
	}
	return true, fmt.Sprintf(
		"单元 %s 已有一份待处理的审校结果等作者处理，连续重写无法同时开始新的调用；"+
			"请先在审校页接受或丢弃它，再点继续", unitID)
}

// runStage runs one stage for one unit and persists the progress transition.
func (s *Service) runStage(ctx context.Context, ch *domain.Chain, unitID string, stage domain.ChainStage) error {
	if ctx.Err() != nil {
		return s.cancelChain(ch.ID)
	}
	root, _, err := s.Store.GetBookRoot(ctx, ch.BookID)
	if err != nil {
		return s.interrupt(ch, stage, "", err.Error())
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		return s.interrupt(ch, stage, "", err.Error())
	}
	if _, ok := findUnit(snap, unitID); !ok {
		return s.interrupt(ch, stage, "", "单元不存在："+unitID)
	}

	idx := ch.Cursor
	ch.Units[idx].Stage = stage
	ch.Units[idx].Status = domain.ChainUnitRunning
	ch.Stage = stage
	ch.Status = domain.ChainRunning
	ch.Error = ""
	ch.UpdatedAt = time.Now().UTC()
	if err := s.Store.SaveChain(context.Background(), *ch); err != nil {
		return errStop
	}

	// 同一单元同类型只能有一个调用。作者手点的那次可能还在跑，甚至可能停在
	// 待接受——那会一直占着这个单元，让链"莫名其妙失败"。
	if blocked, why := s.settleActiveRun(ctx, ch, stage, unitID); blocked {
		return s.interrupt(ch, stage, "", why)
	}

	cfg := stageConfig(ch.Spec, stage)
	spec := domain.TaskSpec{
		Kind: domain.TaskKind(stage), BookID: ch.BookID,
		VolumeID:     fmt.Sprintf("v%02d", ch.Volume),
		UnitID:       unitID,
		BaseRevision: snap.Revision,
		ModelID:      ch.Spec.ModelID,
		Instruction:  cfg.Instruction,
		KnowledgeIDs: cfg.KnowledgeIDs,
		// 分章与否与人工路径读同一份设置（settings.review.splitByChapter），
		// 否则同一单元在两条路下会得到不同粒度的 findings。
		SplitByChapter: stage == domain.ChainStageReview && s.reviewSplitByChapter(),
	}
	run, err := s.Runs.Start(ctx, spec)
	if err != nil {
		return s.interrupt(ch, stage, "", err.Error())
	}
	s.mu.Lock()
	s.subRuns[ch.ID] = run.ID
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subRuns, ch.ID)
		s.mu.Unlock()
	}()
	ch.Units[idx].RunID = run.ID
	ch.UpdatedAt = time.Now().UTC()
	_ = s.Store.SaveChain(context.Background(), *ch)

	done, err := s.Runs.WaitRun(ctx, run.ID)
	if err != nil {
		if ctx.Err() != nil {
			return s.cancelChain(ch.ID)
		}
		return s.interrupt(ch, stage, run.ID, err.Error())
	}
	if done.Status != domain.RunWaiting || done.CandidateID == "" {
		msg := done.Error
		if strings.TrimSpace(msg) == "" {
			msg = fmt.Sprintf("运行状态 %s，未产出候选", done.Status)
		}
		return s.interrupt(ch, stage, run.ID, msg)
	}
	ch.Units[idx].CandidateID = done.CandidateID

	cand, err := s.Store.GetCandidate(ctx, done.CandidateID)
	if err != nil {
		return s.interrupt(ch, stage, run.ID, err.Error())
	}
	lastRunID := run.ID
	// 审校阻断先自动定向修订一轮再复审；预算固定（+1 revise、+1 review），不循环。
	if stage == domain.ChainStageReview && len(blockingOf(cand.Findings)) > 0 {
		revised, freshSnap, reviewRunID, rerr := s.autoReviseReview(ctx, ch, unitID, cand)
		if rerr != nil {
			if ctx.Err() != nil {
				return s.cancelChain(ch.ID)
			}
			if reviewRunID != "" {
				lastRunID = reviewRunID
			}
			return s.interrupt(ch, stage, lastRunID, rerr.Error())
		}
		cand, snap, lastRunID = revised, freshSnap, reviewRunID
		ch.Units[idx].CandidateID = cand.ID
		ch.Units[idx].RunID = lastRunID
	}
	if blocking := blockingFindings(cand.Findings); len(blocking) > 0 {
		return s.interrupt(ch, stage, lastRunID, "门禁未通过："+strings.Join(blocking, "；"))
	}
	// 覆盖已有正文前先备份旧稿（新写单元无文件，静默跳过）。
	if stage == domain.ChainStageWrite {
		if u, ok := findUnit(snap, unitID); ok && u.HasProse {
			if _, err := books.ArchiveUnitProse(root, unitID); err != nil {
				return s.interrupt(ch, stage, lastRunID, "备份旧正文失败："+err.Error())
			}
		}
	}
	if _, err := s.Runs.Accept(ctx, domain.AcceptCommand{CandidateID: cand.ID, BaseRevision: snap.Revision}); err != nil {
		return s.interrupt(ch, stage, lastRunID, "接受候选失败："+err.Error())
	}

	if next := domain.NextChainStage(stage); next != "" {
		ch.Stage = next
		ch.Units[idx].Stage = next
	} else {
		ch.Units[idx].Status = domain.ChainUnitDone
		ch.Units[idx].Stage = domain.ChainStageFinalize
		ch.Units[idx].Error = ""
		ch.Cursor++
		ch.Stage = domain.ChainStageOutline
	}
	ch.Status = domain.ChainRunning
	ch.Error = ""
	ch.UpdatedAt = time.Now().UTC()
	if err := s.Store.SaveChain(context.Background(), *ch); err != nil {
		return errStop
	}
	return nil
}

func (s *Service) interrupt(ch *domain.Chain, stage domain.ChainStage, runID, msg string) error {
	// 保留上一条不同的原因：作者常常点几次「继续」才回来看，第一次的
	// "门禁未通过：…" 才是根因，不能被后来的次生错误顶掉。
	if prev := strings.TrimSpace(ch.Error); prev != "" && prev != msg {
		ch.PreviousErrors = append([]string{prev}, ch.PreviousErrors...)
		if len(ch.PreviousErrors) > 3 {
			ch.PreviousErrors = ch.PreviousErrors[:3]
		}
	}
	ch.Status = domain.ChainWaitingManual
	ch.Stage = stage
	ch.Error = msg
	if ch.Cursor < len(ch.Units) {
		ch.Units[ch.Cursor].Stage = stage
		ch.Units[ch.Cursor].Status = domain.ChainUnitBlocked
		ch.Units[ch.Cursor].RunID = runID
		ch.Units[ch.Cursor].Error = msg
	}
	ch.UpdatedAt = time.Now().UTC()
	_ = s.Store.SaveChain(context.Background(), *ch)
	return errStop
}

// autoReviseReview turns a blocked review into one bounded 定向修订 + 复审.
//
// 预算固定：一次阻断最多换来 +1 次 revise 调用和 +1 次 review 调用，不做第二轮；
// 复审仍不过就交回人工。必须等修订跑完再归档旧清单——修订任务是从 open 审校候选
// 里按 issueId 取 findings 的，提前归档会让它拿不到要修的问题。
func (s *Service) autoReviseReview(ctx context.Context, ch *domain.Chain, unitID string, review domain.Candidate) (domain.Candidate, domain.BookSnapshot, string, error) {
	var zero domain.BookSnapshot
	var ids []string
	seen := map[string]bool{}
	for _, f := range blockingOf(review.Findings) {
		if f.ID == "" || seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		ids = append(ids, f.ID)
	}
	if len(ids) == 0 {
		return domain.Candidate{}, zero, "", fmt.Errorf("审校阻断项缺少 id，无法定向修订")
	}

	reviseRun, err := s.Runs.Start(ctx, domain.TaskSpec{
		Kind: domain.TaskRevise, BookID: ch.BookID, UnitID: unitID,
		ModelID: ch.Spec.ModelID, IssueIDs: ids,
	})
	if err != nil {
		return domain.Candidate{}, zero, "", fmt.Errorf("自动修订未能启动：%w", err)
	}
	s.trackRun(ch.ID, reviseRun.ID)
	done, err := s.Runs.WaitRun(ctx, reviseRun.ID)
	if err != nil {
		return domain.Candidate{}, zero, reviseRun.ID, fmt.Errorf("自动修订中断：%w", err)
	}
	if done.Status != domain.RunWaiting || done.CandidateID == "" {
		return domain.Candidate{}, zero, reviseRun.ID, fmt.Errorf("自动修订失败：%s", runFailure(done))
	}
	revCand, err := s.Store.GetCandidate(ctx, done.CandidateID)
	if err != nil {
		return domain.Candidate{}, zero, reviseRun.ID, err
	}

	root, _, err := s.Store.GetBookRoot(ctx, ch.BookID)
	if err != nil {
		return domain.Candidate{}, zero, reviseRun.ID, err
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		return domain.Candidate{}, zero, reviseRun.ID, err
	}
	// 修订会覆盖正文：先按写入阶段的同一规则备份旧稿。
	if u, ok := findUnit(snap, unitID); ok && u.HasProse {
		if _, err := books.ArchiveUnitProse(root, unitID); err != nil {
			return domain.Candidate{}, zero, reviseRun.ID, fmt.Errorf("备份旧正文失败：%w", err)
		}
	}
	if _, err := s.Runs.Accept(ctx, domain.AcceptCommand{CandidateID: revCand.ID, BaseRevision: snap.Revision}); err != nil {
		return domain.Candidate{}, zero, reviseRun.ID, fmt.Errorf("接受修订候选失败：%w", err)
	}
	// 阻断项已按清单修过：归档这份清单，免得它继续挡住 Resume 或被当成待办。
	if _, err := s.Runs.Accept(ctx, domain.AcceptCommand{CandidateID: review.ID, BaseRevision: snap.Revision}); err != nil {
		return domain.Candidate{}, zero, reviseRun.ID, fmt.Errorf("归档审校清单失败：%w", err)
	}

	// 正文已变，重新取快照再分章复审一次；复审通过的候选由调用方正常接受。
	snap, err = books.LoadBook(root, false)
	if err != nil {
		return domain.Candidate{}, zero, reviseRun.ID, err
	}
	reviewRun, err := s.Runs.Start(ctx, domain.TaskSpec{
		Kind: domain.TaskReview, BookID: ch.BookID, UnitID: unitID,
		ModelID: ch.Spec.ModelID, SplitByChapter: s.reviewSplitByChapter(),
		KnowledgeIDs: ch.Spec.Review.KnowledgeIDs,
	})
	if err != nil {
		return domain.Candidate{}, zero, reviseRun.ID, fmt.Errorf("自动复审未能启动：%w", err)
	}
	s.trackRun(ch.ID, reviewRun.ID)
	done, err = s.Runs.WaitRun(ctx, reviewRun.ID)
	if err != nil {
		return domain.Candidate{}, zero, reviewRun.ID, fmt.Errorf("自动复审中断：%w", err)
	}
	if done.Status != domain.RunWaiting || done.CandidateID == "" {
		return domain.Candidate{}, zero, reviewRun.ID, fmt.Errorf("自动复审失败：%s", runFailure(done))
	}
	final, err := s.Store.GetCandidate(ctx, done.CandidateID)
	if err != nil {
		return domain.Candidate{}, zero, reviewRun.ID, err
	}
	return final, snap, reviewRun.ID, nil
}

// trackRun points the chain's cancel handle at the run currently in flight.
func (s *Service) trackRun(chainID, runID string) {
	s.mu.Lock()
	s.subRuns[chainID] = runID
	s.mu.Unlock()
}

func runFailure(run domain.Run) string {
	if strings.TrimSpace(run.Error) != "" {
		return run.Error
	}
	return fmt.Sprintf("运行状态 %s，未产出候选", run.Status)
}

func blockingOf(fs []domain.ReviewFinding) []domain.ReviewFinding {
	var out []domain.ReviewFinding
	for _, f := range fs {
		if f.Severity == "blocking" {
			out = append(out, f)
		}
	}
	return out
}

func (s *Service) cancelChain(chainID string) error {
	ch, err := s.Store.GetChain(context.Background(), chainID)
	if err != nil {
		return errStop
	}
	if ch.Status != domain.ChainCancelled {
		ch.Status = domain.ChainCancelled
		ch.Error = "cancelled"
		ch.UpdatedAt = time.Now().UTC()
		_ = s.Store.SaveChain(context.Background(), ch)
	}
	return errStop
}

// ---- helpers ----

func (s *Service) libraryRoot() string {
	if s.Settings == nil {
		return ""
	}
	return s.Settings.Get().LibraryRoot
}

func normalizeSpec(spec domain.ChainSpec) domain.ChainSpec {
	spec.Outline.Instruction = strings.TrimSpace(limitRunes(spec.Outline.Instruction, maxInstructionRunes))
	spec.Write.Instruction = strings.TrimSpace(limitRunes(spec.Write.Instruction, maxInstructionRunes))
	spec.Outline.KnowledgeIDs = dedupe(spec.Outline.KnowledgeIDs)
	spec.Write.KnowledgeIDs = dedupe(spec.Write.KnowledgeIDs)
	spec.Review.KnowledgeIDs = dedupe(spec.Review.KnowledgeIDs)
	// 审校不接受追加指令：规则来自审校清单与本地门禁，作者的自由文本会与之冲突。
	spec.Review.Instruction = ""
	return spec
}

func limitRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func dedupe(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// stageConfig returns the author extras that apply to a stage.
func stageConfig(spec domain.ChainSpec, stage domain.ChainStage) domain.ChainStageConfig {
	switch stage {
	case domain.ChainStageOutline:
		return spec.Outline
	case domain.ChainStageWrite:
		return spec.Write
	case domain.ChainStageReview:
		// 审校只取参考知识：它没有"追加指令"的语义，规则来自审校清单与本地 gate。
		// 人工审校在确认框里能勾文章，链若不透传，作者以为生效的规则其实没生效。
		return domain.ChainStageConfig{KnowledgeIDs: spec.Review.KnowledgeIDs}
	}
	return domain.ChainStageConfig{}
}

// reviewSplitByChapter mirrors the manual path: 分章与否只看同一份设置。
//
// 人工审校在 runs.Service 里读 s.Settings.Get().Review.SplitByChapter，链必须
// 读同一个来源，否则同一个单元两条路给出不同粒度、不同条数的 findings。
func (s *Service) reviewSplitByChapter() bool {
	if s.Settings == nil {
		return false
	}
	return s.Settings.Get().Review.SplitByChapter
}

// stageForStatus maps an existing unit status to the stage a resumed chain
// should start from (never redoing work the author already fixed).
func stageForStatus(status domain.UnitStatus) domain.ChainStage {
	switch status {
	case domain.UnitAccepted:
		return domain.ChainStageWrite
	case domain.UnitDrafted:
		return domain.ChainStageReview
	}
	return domain.ChainStageOutline
}

func validateKnowledge(spec domain.ChainSpec, libraryRoot string) error {
	ids := append(append(append([]string{}, spec.Outline.KnowledgeIDs...), spec.Write.KnowledgeIDs...), spec.Review.KnowledgeIDs...)
	if len(ids) == 0 {
		return nil
	}
	docs, err := knowledge.List(libraryRoot)
	if err != nil {
		return fmt.Errorf("读取参考知识失败：%w", err)
	}
	known := map[string]bool{}
	for _, d := range docs {
		known[d.ID] = true
	}
	for _, id := range ids {
		if !known[id] {
			return fmt.Errorf("参考知识不存在：%s", id)
		}
	}
	return nil
}

// preflightModels fails fast when a stage has no model assigned, instead of
// discovering it halfway through the volume.
func (s *Service) preflightModels(spec domain.ChainSpec) error {
	stages := []struct {
		kind  domain.TaskKind
		label string
	}{
		{domain.TaskOutline, "细纲"},
		{domain.TaskWrite, "正文"},
		{domain.TaskReview, "审校"},
	}
	for _, st := range stages {
		if s.Runs.ResolveModel(domain.TaskSpec{Kind: st.kind, ModelID: spec.ModelID}) == "" {
			return fmt.Errorf("%s阶段未配置模型（设置 → 创作阶段模型）", st.label)
		}
	}
	return nil
}

func resolveVolume(snap domain.BookSnapshot, spec domain.ChainSpec) (int, error) {
	if v := strings.TrimSpace(spec.VolumeID); v != "" {
		n, err := parseVolumeTag(v)
		if err != nil {
			return 0, err
		}
		return n, nil
	}
	if u, ok := findUnit(snap, strings.TrimSpace(spec.FromUnitID)); ok {
		return u.Volume, nil
	}
	return 0, fmt.Errorf("volumeId 或 fromUnitId 必填")
}

func parseVolumeTag(tag string) (int, error) {
	s := strings.TrimSpace(tag)
	s = strings.TrimLeft(strings.TrimPrefix(s, "v"), "0")
	if s == "" {
		return 0, fmt.Errorf("非法卷号：%s", tag)
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("非法卷号：%s", tag)
	}
	return n, nil
}

// planUnits collects the volume's units from fromUnitId to the volume end.
func planUnits(snap domain.BookSnapshot, volume int, spec domain.ChainSpec) ([]domain.UnitInfo, error) {
	from, ok := findUnit(snap, strings.TrimSpace(spec.FromUnitID))
	if !ok {
		return nil, fmt.Errorf("起始单元不存在：%s", spec.FromUnitID)
	}
	if from.Volume != volume {
		return nil, fmt.Errorf("起始单元 %s 不属于第 %d 卷", from.UnitID, volume)
	}
	toIndex := -1
	if id := strings.TrimSpace(spec.ToUnitID); id != "" {
		to, ok := findUnit(snap, id)
		if !ok {
			return nil, fmt.Errorf("结束单元不存在：%s", id)
		}
		if to.Volume != volume || to.Index < from.Index {
			return nil, fmt.Errorf("结束单元 %s 必须与起始单元同卷且不早于它", id)
		}
		toIndex = to.Index
	}
	var out []domain.UnitInfo
	for _, u := range snap.Units {
		if u.Volume != volume || u.Index < from.Index {
			continue
		}
		if toIndex >= 0 && u.Index > toIndex {
			continue
		}
		out = append(out, u)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("第 %d 卷没有可执行的单元", volume)
	}
	return out, nil
}

func findUnit(snap domain.BookSnapshot, id string) (domain.UnitInfo, bool) {
	for _, u := range snap.Units {
		if u.UnitID == id {
			return u, true
		}
	}
	return domain.UnitInfo{}, false
}

// blockingFindings renders blocking gate results for the interruption message.
func blockingFindings(fs []domain.ReviewFinding) []string {
	var out []string
	for _, f := range fs {
		if f.Severity != "blocking" {
			continue
		}
		msg := f.Title
		if f.Evidence != "" {
			msg += "（" + f.Evidence + "）"
		}
		if f.Chapter > 0 {
			msg = fmt.Sprintf("第 %d 章：%s", f.Chapter, msg)
		}
		out = append(out, msg)
	}
	return out
}
