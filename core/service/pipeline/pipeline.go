package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
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
	// ModelMaxOut returns the assigned model's catalog max_output (0 = unknown).
	ModelMaxOut func(modelID string) int
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

func (r *Runner) RunWrite(ctx context.Context, snap domain.BookSnapshot, unit domain.UnitInfo, modelID string) (Result, error) {
	packet := contextsvc.BuildWritePacket(snap, unit, 0)
	if !packet.Policy.SplitByChapter {
		return r.runJSON(ctx, snap, packet, modelID, domain.TaskWrite, unit.UnitID, unit.ProsePath, func(raw string, cand *domain.Candidate) error {
			var parsed struct {
				Chapters []domain.ChapterDraft `json:"chapters"`
			}
			if err := unmarshalJSON(raw, &parsed); err != nil {
				return err
			}
			if len(parsed.Chapters) == 0 {
				return fmt.Errorf("empty chapters")
			}
			cand.Chapters = parsed.Chapters
			return nil
		}, func(cand domain.Candidate) gates.Report {
			return gates.CheckProse(cand.Chapters, 2000)
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
		user := contextsvc.MergeExtraIntoUserPayload(r.withKnowledge(packet.UserPrompt), extra)
		draft, rec, err := r.generateChapter(ctx, modelID, packet, user, ch, &calls)
		if err != nil {
			return Result{Calls: calls, Packet: packet}, err
		}
		_ = rec
		cand.Chapters = append(cand.Chapters, draft)
	}
	// 按章定向修复：单章硬门不过只重生成该章，其他章不动。
	cand.Chapters = r.repairFailedChapters(ctx, modelID, packet, cand.Chapters, &calls)
	rep := gates.CheckProse(cand.Chapters, 2000)
	cand.Findings = append(cand.Findings, rep.Findings...)
	return Result{Candidate: cand, Calls: calls, Packet: packet, LocalGate: rep}, nil
}

func (r *Runner) RunOutline(ctx context.Context, snap domain.BookSnapshot, volume int, unitIDs []string, modelID string) (Result, error) {
	packet := contextsvc.BuildOutlinePacket(snap, volume, unitIDs)
	var volCast []string
	for _, v := range snap.Volumes {
		if v.Volume == volume {
			volCast = v.Cast
			break
		}
	}
	return r.runJSON(ctx, snap, packet, modelID, domain.TaskOutline, "", "", func(raw string, cand *domain.Candidate) error {
		var parsed struct {
			Units []domain.UnitOutlineDraft `json:"units"`
		}
		if err := unmarshalJSON(raw, &parsed); err != nil {
			return err
		}
		if len(parsed.Units) == 0 {
			return fmt.Errorf("empty units")
		}
		cand.UnitOutlines = parsed.Units
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
	var preserve []string
	if len(preservePaths) > 0 {
		preserve = preservePaths[0]
	}
	if mode == "" {
		if books.HasBookOutline(snap) {
			mode = domain.PlanModeVolume
		} else {
			mode = domain.PlanModeInit
		}
	}
	full := mode == domain.PlanModeInit || mode == domain.PlanModeReinit
	if !full {
		if volume < 1 {
			volume = 1
		}
		if !books.HasBookOutline(snap) {
			return Result{}, fmt.Errorf("生成卷纲前需先完成立项（缺少 outline/book_outline.md）")
		}
	}
	packet := contextsvc.BuildPlanPacketWithMode(snap, volume, mode, instruction, r.modelMaxOut(modelID))
	return r.runJSON(ctx, snap, packet, modelID, domain.TaskPlan, "", "", func(raw string, cand *domain.Candidate) error {
		var parsed struct {
			Cast                  []map[string]any `json:"cast"`
			BookOutlineMarkdown   string           `json:"bookOutlineMarkdown"`
			VolumeOutlineMarkdown string           `json:"volumeOutlineMarkdown"`
			Settings              *struct {
				Files map[string]string `json:"files"`
			} `json:"settings"`
		}
		if err := unmarshalJSON(raw, &parsed); err != nil {
			return err
		}
		cand.Mode = mode
		if mode == domain.PlanModeReinit && len(preserve) > 0 {
			cand.PreservePaths = append([]string(nil), preserve...)
		}
		cand.PlanMarkdown = map[string]string{}
		if full {
			if strings.TrimSpace(parsed.BookOutlineMarkdown) == "" {
				return fmt.Errorf("empty book outline")
			}
			cand.PlanMarkdown["book_outline"] = parsed.BookOutlineMarkdown
			b, _ := json.Marshal(parsed.Cast)
			cand.PlanMarkdown["cast_json"] = string(b)
			if parsed.Settings == nil || len(parsed.Settings.Files) == 0 {
				return fmt.Errorf("立项必须输出 settings.files")
			}
			cand.PlanSettings = map[string]string{}
			for _, path := range books.PlanSettingsPaths {
				body := strings.TrimSpace(parsed.Settings.Files[path])
				if body == "" {
					return fmt.Errorf("缺少设定文件: %s", path)
				}
				cand.PlanSettings[path] = parsed.Settings.Files[path]
			}
			for path := range parsed.Settings.Files {
				if !books.IsPlanSettingsPath(path) {
					return fmt.Errorf("不允许的设定路径: %s", path)
				}
			}
			// Ignore accidental volume output from init/reinit.
		} else {
			if strings.TrimSpace(parsed.VolumeOutlineMarkdown) == "" {
				return fmt.Errorf("empty volume outline")
			}
			if parsed.Settings != nil && len(parsed.Settings.Files) > 0 {
				return fmt.Errorf("重写卷纲不得输出 settings")
			}
			if len(parsed.Cast) > 0 {
				return fmt.Errorf("重写卷纲不得输出 cast")
			}
			if strings.TrimSpace(parsed.BookOutlineMarkdown) != "" {
				return fmt.Errorf("重写卷纲不得输出 bookOutlineMarkdown")
			}
			cand.PlanMarkdown["volume_outline"] = parsed.VolumeOutlineMarkdown
			cand.VolumeID = packet.VolumeID
		}
		return nil
	}, func(cand domain.Candidate) gates.Report {
		rep := gates.Report{OK: true, Counts: map[string]int{}}
		if full {
			if strings.TrimSpace(cand.PlanMarkdown["book_outline"]) == "" {
				rep.OK = false
				rep.Findings = append(rep.Findings, domain.ReviewFinding{ID: "plan-book", Severity: "blocking", Source: "local_gate", Title: "缺少全书总纲"})
			}
			for _, path := range books.PlanSettingsPaths {
				if strings.TrimSpace(cand.PlanSettings[path]) == "" {
					rep.OK = false
					rep.Findings = append(rep.Findings, domain.ReviewFinding{ID: "plan-setting-" + path, Severity: "blocking", Source: "local_gate", Title: "缺少设定: " + path})
				}
			}
			if strings.TrimSpace(cand.PlanMarkdown["cast_json"]) == "[]" || strings.TrimSpace(cand.PlanMarkdown["cast_json"]) == "" {
				rep.OK = false
				rep.Findings = append(rep.Findings, domain.ReviewFinding{ID: "plan-cast", Severity: "blocking", Source: "local_gate", Title: "未生成人物卡"})
			} else if !castHasProtagonist(cand.PlanMarkdown["cast_json"]) {
				rep.OK = false
				rep.Findings = append(rep.Findings, domain.ReviewFinding{ID: "plan-protagonist", Severity: "blocking", Source: "local_gate", Title: "缺少 protagonist"})
			}
		} else if strings.TrimSpace(cand.PlanMarkdown["volume_outline"]) == "" {
			rep.OK = false
			rep.Findings = append(rep.Findings, domain.ReviewFinding{ID: "plan-volume", Severity: "blocking", Source: "local_gate", Title: "缺少卷纲"})
		}
		return rep
	})
}

func castHasProtagonist(castJSON string) bool {
	var cast []map[string]any
	if err := json.Unmarshal([]byte(castJSON), &cast); err != nil {
		return false
	}
	for _, c := range cast {
		role, _ := c["role"].(string)
		if strings.EqualFold(strings.TrimSpace(role), "protagonist") {
			return true
		}
	}
	return false
}

func (r *Runner) RunReview(ctx context.Context, snap domain.BookSnapshot, unit domain.UnitInfo, modelID string) (Result, error) {
	chapters := gates.ParseProseChapters(unit.ProseRaw)
	local := gates.CheckProse(chapters, 2000)
	packet := contextsvc.BuildReviewPacket(snap, unit, local.Findings)
	res, err := r.runJSON(ctx, snap, packet, modelID, domain.TaskReview, unit.UnitID, unit.ProsePath, func(raw string, cand *domain.Candidate) error {
		var parsed struct {
			Verdict  string                 `json:"verdict"`
			Findings []domain.ReviewFinding `json:"findings"`
		}
		if err := unmarshalJSON(raw, &parsed); err != nil {
			return err
		}
		for i := range parsed.Findings {
			if parsed.Findings[i].Source == "" {
				parsed.Findings[i].Source = "ai_review"
			}
		}
		cand.Findings = append(local.Findings, parsed.Findings...)
		cand.Chapters = chapters
		return nil
	}, func(cand domain.Candidate) gates.Report {
		return local
	})
	res.LocalGate = local
	return res, err
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
			return fmt.Errorf("empty replacement")
		}
		cand.Replacement = parsed.Replacement
		return nil
	}, func(cand domain.Candidate) gates.Report {
		return gates.Report{OK: true, Counts: map[string]int{}}
	})
}

// RunRevise applies selected findings in one bounded revision call.
func (r *Runner) RunRevise(ctx context.Context, snap domain.BookSnapshot, unit domain.UnitInfo, findings []domain.ReviewFinding, modelID string) (Result, error) {
	chapters := gates.ParseProseChapters(unit.ProseRaw)
	local := gates.CheckProse(chapters, 2000)
	packet := contextsvc.BuildReviewPacket(snap, unit, append(local.Findings, findings...))
	packet.TaskKind = domain.TaskRevise
	packet.Policy = contextsvc.DefaultPolicy(domain.TaskRevise, 0)
	packet.SystemPrompt = "你是小说修订助手。只针对列出的审校问题改正文，返回 JSON {\"chapters\":[...],\"canonChanges\":[]}。不要新增自由审稿轮次。"
	packet.UserPrompt = contextsvc.MergeExtraIntoUserPayload(packet.UserPrompt, map[string]any{
		"reviseNote": "请定向修订并返回完整 chapters。",
	})
	return r.runJSON(ctx, snap, packet, modelID, domain.TaskRevise, unit.UnitID, unit.ProsePath, func(raw string, cand *domain.Candidate) error {
		var parsed struct {
			Chapters     []domain.ChapterDraft `json:"chapters"`
			CanonChanges []domain.CanonChange  `json:"canonChanges"`
		}
		if err := unmarshalJSON(raw, &parsed); err != nil {
			return err
		}
		if len(parsed.Chapters) == 0 {
			return fmt.Errorf("empty revised chapters")
		}
		cand.Chapters = parsed.Chapters
		cand.CanonChanges = parsed.CanonChanges
		return nil
	}, func(cand domain.Candidate) gates.Report {
		return gates.CheckProse(cand.Chapters, 2000)
	})
}

// RunRewriteEntity rewrites one canon/setting file via a single bounded call.
// Output is a candidate only; accept writes the normalized content back.
func (r *Runner) RunRewriteEntity(ctx context.Context, snap domain.BookSnapshot, spec domain.TaskSpec, modelID string) (Result, error) {
	packet := contextsvc.BuildRewriteEntityPacket(snap, spec, 0)
	return r.runJSON(ctx, snap, packet, modelID, domain.TaskRewriteEntity, "", spec.TargetPath, func(raw string, cand *domain.Candidate) error {
		var parsed struct {
			Content string `json:"content"`
		}
		if err := unmarshalJSON(raw, &parsed); err != nil {
			return err
		}
		if strings.TrimSpace(parsed.Content) == "" {
			return fmt.Errorf("empty rewritten content")
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
	local := gates.CheckProse(chapters, 2000)
	cand := domain.Candidate{
		ID: uuid.NewString(), BookID: snap.Summary.ID, Kind: domain.TaskFinalize,
		Status: domain.CandidateOpen, BaseRevision: snap.Revision,
		TargetPath: unit.ProsePath, UnitID: unit.UnitID,
		Chapters: chapters, Findings: local.Findings, CreatedAt: time.Now().UTC(),
	}
	return Result{Candidate: cand, LocalGate: local}, nil
}

// generateChapter produces one chapter, returning the parsed draft.
func (r *Runner) generateChapter(ctx context.Context, modelID string, packet domain.ContextPacket, user string, ch int, calls *[]domain.CallRecord) (domain.ChapterDraft, domain.CallRecord, error) {
	raw, rec, err := r.chat(ctx, modelID, packet.SystemPrompt, user, packet.Policy)
	rec.Index = len(*calls) + 1
	rec.Step = fmt.Sprintf("chapter-%d", ch)
	*calls = append(*calls, rec)
	if err != nil {
		return domain.ChapterDraft{}, rec, err
	}
	var parsed struct {
		Chapters []domain.ChapterDraft `json:"chapters"`
	}
	if err := unmarshalJSON(raw, &parsed); err != nil {
		return domain.ChapterDraft{}, rec, fmt.Errorf("chapter %d: %w", ch, err)
	}
	if len(parsed.Chapters) != 1 || strings.TrimSpace(parsed.Chapters[0].Body) == "" {
		return domain.ChapterDraft{}, rec, fmt.Errorf("chapter %d: expected one non-empty chapter", ch)
	}
	parsed.Chapters[0].Chapter = ch
	return parsed.Chapters[0], rec, nil
}

// repairFailedChapters re-generates only chapters that fail the local hard
// gate, with evidence+suggestion回注。每章最多一次修复，通过的章不动。
func (r *Runner) repairFailedChapters(ctx context.Context, modelID string, packet domain.ContextPacket, chapters []domain.ChapterDraft, calls *[]domain.CallRecord) []domain.ChapterDraft {
	rep := gates.CheckProse(chapters, 2000)
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
		fixed, rec, err := r.generateChapter(ctx, modelID, packet, user, ch.Chapter, calls)
		rec.Step = fmt.Sprintf("repair-ch%d", ch.Chapter)
		(*calls)[len(*calls)-1] = rec
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
	max := packet.Policy.MaxCalls
	if max < 1 {
		max = 1
	}
	for attempt := 1; attempt <= max; attempt++ {
		if attempt > 1 && !packet.Policy.AllowOneRepair {
			break
		}
		user = r.withKnowledge(packet.UserPrompt)
		if attempt > 1 {
			user = contextsvc.MergeRepairIntoUserPayload(user, lastErr)
		}
		raw, rec, err := r.chat(ctx, modelID, packet.SystemPrompt, user, packet.Policy)
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
			if attempt < max && packet.Policy.AllowOneRepair {
				continue
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
