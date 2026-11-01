package runs

import (
	"fmt"
	"strings"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// taskKindLabel is the author-facing name of a run kind.
//
// 这条信息只出现在冲突提示里：作者看到一个 UUID 无法判断"到底是谁占着这个单元"，
// 所以提示必须说人话。
func taskKindLabel(kind domain.TaskKind) string {
	switch kind {
	case domain.TaskInit:
		return "立项"
	case domain.TaskPlan:
		return "卷纲"
	case domain.TaskOutline:
		return "细纲"
	case domain.TaskWrite:
		return "正文"
	case domain.TaskReview:
		return "审校"
	case domain.TaskRevise:
		return "修订"
	case domain.TaskFinalize:
		return "定稿"
	case domain.TaskRewriteEntity:
		return "重写设定"
	case domain.TaskLocalEdit, domain.TaskContinue, domain.TaskExpand, domain.TaskPolish, domain.TaskRewriteSel:
		return "局部编辑"
	}
	return string(kind)
}

// activeRunConflict explains why a new run cannot start for this unit and what
// the author can do about it.
//
// 规则本身是对的：同一本书同一类型同一单元只能有一个在跑的调用，否则两份候选
// 会互相覆盖。但旧实现只回一句 `active run already exists: <uuid>`——作者既
// 不知道是谁占着，也不知道该等还是该去点哪里，连续重写因此看起来"莫名其妙失败"。
func activeRunConflict(active domain.Run, spec domain.TaskSpec) error {
	label := taskKindLabel(spec.Kind)
	unit := strings.TrimSpace(spec.UnitID)
	target := unit
	if target == "" {
		target = "全书"
	}

	elapsed := ""
	if !active.CreatedAt.IsZero() {
		if d := time.Since(active.CreatedAt); d > time.Second {
			elapsed = fmt.Sprintf("，已运行 %s", humanDuration(d))
		}
	}

	switch active.Status {
	case domain.RunWaiting:
		// waiting_accept 才是真正会一直挡住的那个：候选不处理就永远占着。
		return fmt.Errorf(
			"单元 %s 已有一份待处理的%s结果（%s，候选 %s），接受或拒绝它之后才能再发起%s",
			target, label, diffStatus(active.Status), shortID(active.CandidateID), label)
	case domain.RunRunning, domain.RunPending:
		return fmt.Errorf(
			"单元 %s 的%s调用还在进行中（%s%s），等它结束或取消后再发起%s",
			target, label, diffStatus(active.Status), elapsed, label)
	}
	return fmt.Errorf(
		"单元 %s 已有一个%s调用（%s）占用，处理完才能发起新的%s",
		target, label, diffStatus(active.Status), label)
}

// diffStatus maps a run status to a phrase that reads naturally in 中文.
func diffStatus(s domain.RunStatus) string {
	switch s {
	case domain.RunPending:
		return "排队中"
	case domain.RunRunning:
		return "运行中"
	case domain.RunWaiting:
		return "待接受"
	}
	return string(s)
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d 分钟", int(d.Minutes()))
	}
	return fmt.Sprintf("%.1f 小时", d.Hours())
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
