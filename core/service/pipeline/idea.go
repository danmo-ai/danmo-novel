package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/gates"
	"github.com/google/uuid"
)

// ideaMinChars 是「这算不算一份提案」的下限。
//
// 自由输出没有 schema 可校验，唯一能判的是「有没有内容」。门槛定得很低，
// 因为它的用途只是拦下空回答和一句敷衍，而不是替作者评判创意好坏。
const ideaMinChars = 120

// RunIdeaExpand 把立项输入扩成一份创意提案（自由 Markdown 候选）。
//
// 不走 runJSON：那套路径要求一个 JSON codec，而这一步的全部价值就在于不结构化。
// 校验降级为「空输出 / 过短 → 一次定向修复」，其余交给作者判断。
// 候选的 Replacement 是**合并后的完整 outline/idea.md**，不是模型原文：
// 「立项输入」段是作者填的表单，模型无权覆盖。
func (r *Runner) RunIdeaExpand(ctx context.Context, snap domain.BookSnapshot, instruction, modelID string) (Result, error) {
	packet := contextsvc.BuildIdeaPacket(snap, instruction)
	if modelID == "" {
		return Result{}, fmt.Errorf("modelId required")
	}
	user := r.withKnowledge(packet.UserPrompt)
	var calls []domain.CallRecord
	var lastErr error
	max := packet.Policy.MaxCalls
	if max < 1 {
		max = 1
	}
	for attempt := 1; attempt <= max; attempt++ {
		if attempt > 1 && !packet.Policy.AllowOneRepair {
			break
		}
		attemptUser := user
		if attempt > 1 {
			attemptUser = contextsvc.MergeRepairIntoUserPayload(user, lastErr)
		}
		raw, rec, err := r.chat(ctx, modelID, packet.SystemPrompt, attemptUser, packet.Policy)
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
		proposal := strings.TrimSpace(raw)
		if n := len([]rune(proposal)); n < ideaMinChars {
			lastErr = fmt.Errorf("创意提案过短（%d 字）：需要按小节写出推荐主线、主角、对抗力量与开篇钩子", n)
			if attempt < max {
				continue
			}
			return Result{Calls: calls, Packet: packet}, lastErr
		}
		merged := books.MergeIdeaProposal(snap.Files[domain.IdeaPath], proposal)
		cand := domain.Candidate{
			ID:           uuid.NewString(),
			BookID:       snap.Summary.ID,
			Kind:         domain.TaskIdea,
			Status:       domain.CandidateOpen,
			BaseRevision: snap.Revision,
			TargetPath:   domain.IdeaPath,
			Replacement:  merged,
			CreatedAt:    time.Now().UTC(),
		}
		return Result{Candidate: cand, Calls: calls, Packet: packet, LocalGate: gates.Report{OK: true, Counts: map[string]int{}}}, nil
	}
	return Result{Calls: calls, Packet: packet}, fmt.Errorf("budget exhausted: %v", lastErr)
}
