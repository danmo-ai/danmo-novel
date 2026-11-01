package domain

import "time"

// ChainStatus is the lifecycle of a 连续单元重写 task.
type ChainStatus string

const (
	ChainPending       ChainStatus = "pending"
	ChainRunning       ChainStatus = "running"
	ChainWaitingManual ChainStatus = "waiting_manual"
	ChainCompleted     ChainStatus = "completed"
	ChainFailed        ChainStatus = "failed"
	ChainCancelled     ChainStatus = "cancelled"
)

// ChainStage is one step of the per-unit production loop.
type ChainStage string

const (
	ChainStageOutline  ChainStage = "outline"
	ChainStageWrite    ChainStage = "write"
	ChainStageReview   ChainStage = "review"
	ChainStageFinalize ChainStage = "finalize"
)

// NextChainStage returns the stage after s, or "" when the unit is finished.
func NextChainStage(s ChainStage) ChainStage {
	switch s {
	case ChainStageOutline:
		return ChainStageWrite
	case ChainStageWrite:
		return ChainStageReview
	case ChainStageReview:
		return ChainStageFinalize
	}
	return ""
}

// ChainStageConfig holds the author-provided extras for one stage. It applies
// to every unit of the chain for that stage only. Finalize takes none — it is a
// local check with no LLM call.
type ChainStageConfig struct {
	Instruction  string   `json:"instruction,omitempty"`
	KnowledgeIDs []string `json:"knowledgeIds,omitempty"`
}

// ChainSpec starts one chain. 总纲/卷纲/设定 are preconditions, never rewritten.
type ChainSpec struct {
	BookID     string `json:"bookId"`
	VolumeID   string `json:"volumeId"`           // vNN
	FromUnitID string `json:"fromUnitId"`         // inclusive
	ToUnitID   string `json:"toUnitId,omitempty"` // default: last unit of the volume
	ModelID    string `json:"modelId,omitempty"`  // overrides all four stages
	// Outline / Write / Review carry per-stage instruction + reference knowledge.
	// Review carries knowledge only: 审校没有"追加指令"，规则由审校清单与本地 gate 决定。
	Outline ChainStageConfig `json:"outline"`
	Write   ChainStageConfig `json:"write"`
	Review  ChainStageConfig `json:"review"`
}

// ChainUnitStatus is the progress of one unit inside a chain.
type ChainUnitStatus string

const (
	ChainUnitPending ChainUnitStatus = "pending"
	ChainUnitRunning ChainUnitStatus = "running"
	ChainUnitDone    ChainUnitStatus = "done"
	ChainUnitBlocked ChainUnitStatus = "blocked"
)

// ChainUnitState drives the progress list in the UI.
type ChainUnitState struct {
	UnitID      string          `json:"unitId"`
	Stage       ChainStage      `json:"stage"`
	Status      ChainUnitStatus `json:"status"`
	RunID       string          `json:"runId,omitempty"`
	CandidateID string          `json:"candidateId,omitempty"`
	Error       string          `json:"error,omitempty"`
	Note        string          `json:"note,omitempty"` // 新写 | 重写
}

// Chain is one 连续单元重写 run: units are processed strictly in order.
type Chain struct {
	ID     string           `json:"id"`
	BookID string           `json:"bookId"`
	Volume int              `json:"volume"`
	Status ChainStatus      `json:"status"`
	Cursor int              `json:"cursor"` // index into Units
	Stage  ChainStage       `json:"stage"`
	Units  []ChainUnitState `json:"units"`
	Spec   ChainSpec        `json:"spec"`
	Error  string           `json:"error,omitempty"`
	// PreviousErrors 保留此前的中断原因（最近的在前，最多 3 条）。
	// Error 只描述"这一次为什么停"，但作者往往是点了几次「继续」才来看的，
	// 第一次的原因（例如"门禁未通过：连续性…"）才是根因，不能被后来的
	// "单元已被占用"之类的次生错误覆盖掉。
	PreviousErrors []string  `json:"previousErrors,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// Progress returns done/total for display convenience.
func (c Chain) Progress() (done, total int) {
	for _, u := range c.Units {
		if u.Status == ChainUnitDone {
			done++
		}
	}
	return done, len(c.Units)
}
