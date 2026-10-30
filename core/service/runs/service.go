package runs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danmo-ai/danmo-novel/core/config"
	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/knowledge"
	"github.com/danmo-ai/danmo-novel/core/service/llmconfig"
	"github.com/danmo-ai/danmo-novel/core/service/pipeline"
	"github.com/danmo-ai/danmo-novel/core/store/sqlite"
	"github.com/google/uuid"
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

func (s *Service) resolveModel(spec domain.TaskSpec) string {
	if spec.ModelID != "" {
		return spec.ModelID
	}
	role := domain.StageWrite
	switch spec.Kind {
	case domain.TaskPlan, domain.TaskInit:
		role = domain.StagePlan
	case domain.TaskOutline:
		role = domain.StageOutline
	case domain.TaskReview, domain.TaskRevise, domain.TaskFinalize:
		role = domain.StageReview
	case domain.TaskLocalEdit, domain.TaskContinue, domain.TaskExpand, domain.TaskPolish, domain.TaskRewriteSel:
		role = domain.StageLocalEdit
	case domain.TaskRewriteEntity:
		role = domain.StagePlan
	}
	return s.Settings.ModelFor(role)
}

func (s *Service) Start(ctx context.Context, spec domain.TaskSpec) (domain.Run, error) {
	root, readOnly, err := s.Store.GetBookRoot(ctx, spec.BookID)
	if err != nil {
		return domain.Run{}, err
	}
	if readOnly && (spec.Kind == domain.TaskWrite || spec.Kind == domain.TaskOutline || spec.Kind == domain.TaskPlan || spec.Kind == domain.TaskFinalize) {
		// generation creating candidates is OK; accept will refuse writeback on read-only
	}
	if active, ok, _ := s.Store.FindActiveRun(ctx, spec.BookID, spec.Kind, spec.UnitID); ok {
		return active, fmt.Errorf("active run already exists: %s", active.ID)
	}
	snap, err := books.LoadBook(root, readOnly)
	if err != nil {
		return domain.Run{}, err
	}
	if spec.BaseRevision != "" && spec.BaseRevision != snap.Revision {
		return domain.Run{}, fmt.Errorf("baseRevision mismatch: book moved on, refresh and retry")
	}
	modelID := s.resolveModel(spec)
	if modelID == "" {
		return domain.Run{}, fmt.Errorf("no model assigned for this stage; configure Settings → 创作阶段模型")
	}

	run := domain.Run{
		ID: uuid.NewString(), BookID: spec.BookID, Kind: spec.Kind, Status: domain.RunRunning,
		Spec: spec, ModelID: modelID, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := s.Store.SaveRun(ctx, run); err != nil {
		return domain.Run{}, err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[run.ID] = cancel
	s.mu.Unlock()

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
	// 作者勾选的参考知识文章（独立于 unit 的 ExtraContextIDs 语义）。
	s.Pipeline.KnowledgeDocs = s.loadKnowledge(run.Spec.KnowledgeIDs)
	defer func() { s.Pipeline.KnowledgeDocs = nil }()
	switch run.Kind {
	case domain.TaskWrite:
		unit, ok := findUnit(snap, run.Spec.UnitID)
		if !ok {
			err = fmt.Errorf("unit not found: %s", run.Spec.UnitID)
		} else {
			res, err = s.Pipeline.RunWrite(ctx, snap, unit, modelID)
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
		res, err = s.Pipeline.RunOutline(ctx, snap, vnum, ids, modelID)
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
				if u.Volume == vnum && u.Status != domain.UnitReviewed {
					allDone = false
					break
				}
			}
			if allDone && len(snap.Units) > 0 {
				vnum++
			}
		}
		res, err = s.Pipeline.RunPlanMode(ctx, snap, vnum, modelID, mode, run.Spec.Instruction, run.Spec.PreservePaths)
	case domain.TaskReview:
		unit, ok := findUnit(snap, run.Spec.UnitID)
		if !ok {
			err = fmt.Errorf("unit not found")
		} else {
			res, err = s.Pipeline.RunReview(ctx, snap, unit, modelID)
		}
	case domain.TaskLocalEdit, domain.TaskRewriteSel, domain.TaskExpand, domain.TaskPolish, domain.TaskContinue:
		if run.Spec.Selection == nil {
			err = fmt.Errorf("selection required")
		} else {
			res, err = s.Pipeline.RunLocalEdit(ctx, snap, *run.Spec.Selection, run.Spec.Instruction, modelID)
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
			res, err = s.Pipeline.RunRevise(ctx, snap, unit, findings, modelID)
		}
	case domain.TaskFinalize:
		unit, ok := findUnit(snap, run.Spec.UnitID)
		if !ok {
			err = fmt.Errorf("unit not found")
		} else {
			res, err = s.Pipeline.RunFinalize(ctx, snap, unit)
		}
	case domain.TaskRewriteEntity:
		if run.Spec.TargetPath == "" {
			err = fmt.Errorf("targetPath required for rewrite_entity")
		} else {
			res, err = s.Pipeline.RunRewriteEntity(ctx, snap, run.Spec, modelID)
		}
	default:
		err = fmt.Errorf("unsupported task kind: %s", run.Kind)
	}

	run.UpdatedAt = time.Now().UTC()
	run.Calls = res.Calls
	run.CallsUsed = len(res.Calls)
	run.Policy = res.Packet.Policy
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
func (s *Service) PreviewPacket(_ context.Context, spec domain.TaskSpec) (domain.ContextPacket, error) {
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
		return contextsvc.BuildWritePacket(snap, unit, s.modelMaxOut(s.resolveModel(spec))), nil
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
		return contextsvc.BuildOutlinePacket(snap, vnum, ids, s.modelMaxOut(s.resolveModel(spec))), nil
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
		return contextsvc.BuildPlanPacketWithMode(snap, vnum, mode, spec.Instruction, s.modelMaxOut(s.resolveModel(spec))), nil
	case domain.TaskReview:
		unit, ok := findUnit(snap, spec.UnitID)
		if !ok {
			return domain.ContextPacket{}, fmt.Errorf("unit not found")
		}
		return contextsvc.BuildReviewPacket(snap, unit, nil, s.modelMaxOut(s.resolveModel(spec))), nil
	case domain.TaskFinalize:
		unit, ok := findUnit(snap, spec.UnitID)
		if !ok {
			return domain.ContextPacket{}, fmt.Errorf("unit not found: %s", spec.UnitID)
		}
		if !unit.HasProse {
			return domain.ContextPacket{}, fmt.Errorf("单元尚无正文，无法定稿")
		}
		return contextsvc.BuildFinalizePacket(snap, unit), nil
	case domain.TaskRewriteEntity:
		if spec.TargetPath == "" {
			return domain.ContextPacket{}, fmt.Errorf("targetPath required")
		}
		return contextsvc.BuildRewriteEntityPacket(snap, spec, s.modelMaxOut(s.resolveModel(spec))), nil
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

func (s *Service) Accept(ctx context.Context, cmd domain.AcceptCommand) (domain.Candidate, error) {
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
	if cmd.BaseRevision != "" && cmd.BaseRevision != snap.Revision {
		cand.Status = domain.CandidateExpired
		_ = s.Store.SaveCandidate(ctx, cand)
		return domain.Candidate{}, fmt.Errorf("baseRevision expired; re-compare and regenerate if needed")
	}
	if cand.BaseRevision != snap.Revision {
		cand.Status = domain.CandidateExpired
		_ = s.Store.SaveCandidate(ctx, cand)
		return domain.Candidate{}, fmt.Errorf("book changed since generation")
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
		if err := books.WriteUnitProse(root, cand.UnitID, cand.Chapters); err != nil {
			return domain.Candidate{}, err
		}
		if cand.Kind == domain.TaskWrite {
			if err := books.SetUnitStatus(root, cand.UnitID, domain.UnitDrafted); err != nil {
				return domain.Candidate{}, err
			}
		}
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
		// 剧情时钟连续性：故事日倒退（非闪回）blocking。
		vol := 0
		if len(cand.UnitOutlines) > 0 {
			if u, ok := findUnit(snap, cand.UnitOutlines[0].UnitID); ok {
				vol = u.Volume
			}
		}
		for _, f := range books.ValidateClockContinuity(snap.Units, vol) {
			if f.Severity == "blocking" && !cmd.ForceRiskAccept {
				return domain.Candidate{}, fmt.Errorf("剧情时钟冲突：%s（%s）", f.Title, f.Evidence)
			}
		}
		// 单元内部身份恒定：合同声明身份转换则必须拆单元。
		for _, u := range cand.UnitOutlines {
			if blocking := books.ValidateNoIntraUnitChange(u.RawYAML, u.OnStage); len(blocking) > 0 {
				return domain.Candidate{}, fmt.Errorf("单元 %s 含身份转换，须拆到单元边界：%s", u.UnitID, strings.Join(blocking, "; "))
			}
			path := filepath.Join(root, "outline", "units", u.UnitID+".yaml")
			raw := u.RawYAML
			if raw == "" {
				raw = serializeUnitYAML(u)
			}
			if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
				return domain.Candidate{}, err
			}
			_ = books.SetUnitStatus(root, u.UnitID, domain.UnitAccepted)
			// 临时人物机制：on_stage 新 stem 播种 candidate 卡（不自动晋升 canon）。
			for _, stem := range u.OnStage {
				if err := books.SeedCandidateCast(root, stem, nil); err != nil {
					return domain.Candidate{}, err
				}
			}
		}
	case domain.TaskPlan:
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
		// 整文件规范化重写，接受才落盘。
		if cand.TargetPath == "" || strings.TrimSpace(cand.Replacement) == "" {
			return domain.Candidate{}, fmt.Errorf("missing rewrite content")
		}
		full := filepath.Join(root, cand.TargetPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return domain.Candidate{}, err
		}
		if err := os.WriteFile(full, []byte(cand.Replacement), 0o644); err != nil {
			return domain.Candidate{}, err
		}
	case domain.TaskReview:
		// accepting review findings alone never finalizes
	case domain.TaskFinalize:
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
		// 唯一写回路径：状态、facts.md、Cast snapshot、伏笔、卷摘要、定稿账本。
		if err := books.ApplyFinalizationLedger(root, unit, cand.Chapters, cand.Findings); err != nil {
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
		// update committed cursor from last chapter
		if len(cand.Chapters) > 0 {
			last := cand.Chapters[len(cand.Chapters)-1].Chapter
			statePath := filepath.Join(root, "novel-state.yaml")
			if raw, err := os.ReadFile(statePath); err == nil {
				text := updateStateScalar(string(raw), "last_committed_ch", fmt.Sprintf("%d", last))
				_ = os.WriteFile(statePath, []byte(text), 0o644)
			}
		}
	}

	now := time.Now().UTC()
	cand.Status = domain.CandidateAccepted
	cand.AcceptedAt = &now
	if err := s.Store.SaveCandidate(ctx, cand); err != nil {
		return domain.Candidate{}, err
	}
	if run, err := s.Store.GetRun(ctx, cand.RunID); err == nil {
		run.Status = domain.RunSucceeded
		run.UpdatedAt = now
		_ = s.Store.SaveRun(ctx, run)
	}
	_ = root
	return cand, nil
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
	if run, err := s.Store.GetRun(ctx, cand.RunID); err == nil {
		run.Status = domain.RunSucceeded
		run.Error = "rejected: " + reason
		run.UpdatedAt = time.Now().UTC()
		_ = s.Store.SaveRun(ctx, run)
	}
	return nil
}

func (s *Service) loadKnowledge(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	root := s.Settings.Get().LibraryRoot
	var docs []string
	for _, id := range ids {
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

func serializeUnitYAML(u domain.UnitOutlineDraft) string {
	var b strings.Builder
	fmt.Fprintf(&b, "unit_id: %s\n", u.UnitID)
	fmt.Fprintf(&b, "status: accepted\n")
	fmt.Fprintf(&b, "title_working: %q\n", u.Title)
	fmt.Fprintf(&b, "function: %q\n", u.Function)
	fmt.Fprintf(&b, "desire: %q\n", u.Desire)
	fmt.Fprintf(&b, "obstacle: %q\n", u.Obstacle)
	fmt.Fprintf(&b, "choice: %q\n", u.Choice)
	fmt.Fprintf(&b, "payoff: %q\n", u.Payoff)
	fmt.Fprintf(&b, "pleasure: %q\n", u.Pleasure)
	fmt.Fprintf(&b, "forbidden: %q\n", u.Forbidden)
	fmt.Fprintf(&b, "word_target: %q\n", u.WordTarget)
	fmt.Fprintf(&b, "pov: %q\n", u.POV)
	fmt.Fprintf(&b, "chapter_range: [%d, %d]\n", u.ChapterFrom, u.ChapterTo)
	fmt.Fprintf(&b, "on_stage: [%s]\n", strings.Join(quoteAll(u.OnStage), ", "))
	// 完整保留结构化 scenes（写作与审校的场面节拍来源）。
	if len(u.Scenes) > 0 {
		b.WriteString("scenes:\n")
		for _, s := range u.Scenes {
			writeYAMLMap(&b, s, "  ")
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
	if u.GapFromPrev != "" {
		fmt.Fprintf(&b, "gap_from_prev: %q\n", u.GapFromPrev)
	}
	if u.Flashback {
		b.WriteString("flashback: true\n")
	}
	if u.InfoControl != "" {
		fmt.Fprintf(&b, "info_control: %q\n", u.InfoControl)
	}
	if u.StateDeltas != nil {
		encoded, _ := json.Marshal(u.StateDeltas)
		fmt.Fprintf(&b, "state_deltas_json: %q\n", string(encoded))
	}
	return b.String()
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
