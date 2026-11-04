package domain

import "time"

// TaskKind identifies a controlled novel generation action.
type TaskKind string

const (
	TaskPlan            TaskKind = "plan"
	TaskOutline         TaskKind = "outline"
	TaskWrite           TaskKind = "write"
	TaskReview          TaskKind = "review"
	TaskRevise          TaskKind = "revise"
	TaskFinalize        TaskKind = "finalize"
	TaskRewriteEntity   TaskKind = "rewrite_entity" // 单实体优化：bible/人物卡/世界规则等
	TaskLocalEdit       TaskKind = "local_edit"
	TaskContinue        TaskKind = "continue"
	TaskExpand          TaskKind = "expand"
	TaskPolish          TaskKind = "polish"
	TaskRewriteSel      TaskKind = "rewrite_selection"
	TaskStructureReview TaskKind = "structure_review"
	TaskOutlineReview   TaskKind = "outline_review"
	// TaskIdea 是立项之前的自由创意扩写：产出作者可读的创意提案，
	// 不写总纲/设定/人物，也不参与卷纲与单元生产。
	TaskIdea TaskKind = "idea"
)

// 立项阶段（novel-state.yaml 的 stage）。idea / idea_expanded 是本次新增的
// 前置阶段：先有作者输入与确认过的创意提案，才允许进入结构化立项。
const (
	StageIdea         = "idea"
	StageIdeaExpanded = "idea_expanded"
	StagePlanning     = "planning"
)

// IdeaPath 是立项工作台文件：作者输入 + 创意提案。
// 它是作者意图，不是 canon；结构化立项接受时永不覆盖。
const IdeaPath = "outline/idea.md"

// Plan modes for TaskPlan (立项 / 重新立项 / 重写卷纲).
const (
	PlanModeInit   = "init"
	PlanModeReinit = "reinit"
	PlanModeVolume = "volume"
)

type UnitStatus string

const (
	UnitProposed  UnitStatus = "proposed"
	UnitAccepted  UnitStatus = "accepted"
	UnitDrafted   UnitStatus = "drafted"
	UnitReviewed  UnitStatus = "reviewed"
	UnitFinalized UnitStatus = "finalized"
)

type CastStatus string

const (
	CastCandidate CastStatus = "candidate"
	CastCanon     CastStatus = "canon"
)

type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunWaiting   RunStatus = "waiting_accept"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
	// RunRejected means the run produced a candidate and the author discarded it.
	// 与 succeeded 区分开：运行记录里"成功"但候选被拒会掩盖一次返工。
	RunRejected RunStatus = "rejected"
	// RunExpired 表示候选在生成后失效（书籍被改动），既不是作者拒绝也不是
	// 调用失败。它必须是终结态：留在 waiting_accept 会永久占住单元。
	RunExpired RunStatus = "expired"
)

type CandidateStatus string

const (
	CandidateOpen     CandidateStatus = "open"
	CandidateAccepted CandidateStatus = "accepted"
	CandidateRejected CandidateStatus = "rejected"
	CandidateExpired  CandidateStatus = "expired"
)

type StageModelRole string

const (
	StagePlan      StageModelRole = "plan"
	StageOutline   StageModelRole = "outline"
	StageWrite     StageModelRole = "write"
	StageReview    StageModelRole = "review"
	StageLocalEdit StageModelRole = "local_edit"
	// StageFinalize 只服务定稿核对（正文 → 结构化实际账本）。
	//
	// 它不创作也不评判，只是把已经定稿的正文读成一张账本；与审校共用推理强度
	// 会让最机械的一步按最贵的档位计费。单独成一个角色，作者就能给它配便宜的
	// 模型或更低的 effort。未配置时 resolveModel 退回 StageReview。
	StageFinalize StageModelRole = "finalize"
)

// CallPolicy is fixed before a run starts. Tools are always empty.
type CallPolicy struct {
	MaxCalls int `json:"maxCalls"`
	// MaxOutputTokens 已弃用，恒为 0。
	//
	// 输出上限不是上层该管的事：LLM client 会按模型目录里的 max_output 决定，
	// 目录没写就不发这个字段、由 provider 用默认值。上层写死过 8192/32768/65536，
	// 结果是声明 384000 的模型被按 8k 用，思考 tokens 把预算吃满后正文从中间被
	// 截断。是否截断改为看响应协议的 finish_reason。
	MaxOutputTokens int  `json:"maxOutputTokens,omitempty"`
	TimeoutSec      int  `json:"timeoutSec"`
	AllowOneRepair  bool `json:"allowOneRepair"`
}

// TaskSpec starts a controlled generation run.
type TaskSpec struct {
	Kind            TaskKind       `json:"kind"`
	BookID          string         `json:"bookId"`
	VolumeID        string         `json:"volumeId,omitempty"`
	UnitID          string         `json:"unitId,omitempty"`
	Chapter         int            `json:"chapter,omitempty"`
	BaseRevision    string         `json:"baseRevision"`
	ModelID         string         `json:"modelId,omitempty"`
	IdempotencyKey  string         `json:"idempotencyKey,omitempty"`
	ExtraContextIDs []string       `json:"extraContextIds,omitempty"`
	KnowledgeIDs    []string       `json:"knowledgeIds,omitempty"` // 作者勾选的参考知识文章
	Selection       *TextSelection `json:"selection,omitempty"`
	Instruction     string         `json:"instruction,omitempty"`
	IssueIDs        []string       `json:"issueIds,omitempty"`
	TargetPath      string         `json:"targetPath,omitempty"`    // rewrite_entity 目标文件
	EntityKind      string         `json:"entityKind,omitempty"`    // bible|cast|volume|world|style|rules|facts
	Mode            string         `json:"mode,omitempty"`          // plan: init|reinit|volume
	PreservePaths   []string       `json:"preservePaths,omitempty"` // reinit 接受时保留的既有设定/人物路径
	ForceRiskAccept bool           `json:"forceRiskAccept,omitempty"`
	// SupersedeReview 表示"这是一次重审"：该单元名下尚未处理的旧审校清单会被归档，
	// 让新审校能启动。重审就是用新结论替换旧结论，不放行就只能靠作者先手动处置，
	// 而审校清单的"处置"按钮（通过/强制通过）在旧方案里根本不存在——所以会打死结。
	SupersedeReview bool `json:"supersedeReview,omitempty"`
}

type TextSelection struct {
	Path   string `json:"path"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Text   string `json:"text"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

type ContextBlock struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Source   string `json:"source"`
	Content  string `json:"content"`
	Bytes    int    `json:"bytes"`
	Optional bool   `json:"optional,omitempty"`
	Omitted  bool   `json:"omitted,omitempty"`
	Reason   string `json:"omitReason,omitempty"`
}

type ContextPacket struct {
	ContractVersion  string            `json:"contractVersion"`
	TaskKind         TaskKind          `json:"taskKind"`
	BookID           string            `json:"bookId"`
	UnitID           string            `json:"unitId,omitempty"`
	VolumeID         string            `json:"volumeId,omitempty"`
	InputHashes      map[string]string `json:"inputHashes"`
	Blocks           []ContextBlock    `json:"blocks"`
	KnowledgeVersion string            `json:"knowledgeVersion"`
	EstPromptTokens  int               `json:"estPromptTokens"`
	Policy           CallPolicy        `json:"policy"`
	SystemPrompt     string            `json:"systemPrompt"`
	UserPrompt       string            `json:"userPrompt"`
}

type ChapterDraft struct {
	Chapter int    `json:"chapter"`
	Title   string `json:"title"`
	Body    string `json:"body"`
}

// ChapterPlan is the basic production contract. Scenes are optional helpers,
// not a second mandatory outline layer.
type ChapterPlan struct {
	Chapter   int         `json:"chapter"`
	Goal      string      `json:"goal"`
	Change    string      `json:"change"`
	CutHook   string      `json:"cutHook"`
	DayOffset int         `json:"dayOffset"`
	Scenes    []ScenePlan `json:"scenes,omitempty"`
	// Plant / Payoff 是本章负责埋下与回收的伏笔 ID。
	//
	// 章级此前没有任何「该埋什么」的契约：48 个产出过 chapters 的单元里，含章级
	// scenes 的 0 个、mustLand 用过 0 次。于是写第 38 章时，第 42 章要兑现的
	// 「张桂兰说出儿子的姓名与住院时间」根本不在上下文里，伏笔只能靠运气。
	// 这两个字段把「本章该埋/该收」变成可校验的计划，正文只需按它执行。
	Plant  []string `json:"plant,omitempty" yaml:"plant,omitempty"`
	Payoff []string `json:"payoff,omitempty" yaml:"payoff,omitempty"`
}

type ScenePlan struct {
	ID          string   `json:"id,omitempty"`
	Chapter     int      `json:"chapter"`
	Goal        string   `json:"goal"`
	Conflict    string   `json:"conflict"`
	Turn        string   `json:"turn"`
	Outcome     string   `json:"outcome"`
	MustLand    []string `json:"mustLand,omitempty"`
	EvidenceIn  []string `json:"evidenceIn,omitempty"`
	EvidenceOut []string `json:"evidenceOut,omitempty"`
	DayOffset   *int     `json:"dayOffset,omitempty"`
}

type UnitOutlineDraft struct {
	UnitID     string     `json:"unitId"`
	Status     UnitStatus `json:"status"`
	Title      string     `json:"title"`
	Function   string     `json:"function"`
	Desire     string     `json:"desire"`
	Obstacle   string     `json:"obstacle"`
	Choice     string     `json:"choice"`
	Payoff     string     `json:"payoff"`
	Pleasure   string     `json:"pleasure"`
	Forbidden  string     `json:"forbidden"`
	WordTarget string     `json:"wordTarget"`
	// ChapterChars 是单章目标字数（首选写法）；WordTarget 是旧格式的单元总字数，
	// 仅供兼容读取。两者都由 books.ChapterTarget 归一，硬门只认归一后的值。
	ChapterChars string           `json:"chapterChars,omitempty"`
	OnStage      []string         `json:"onStage"`
	POV          string           `json:"pov"`
	ChapterFrom  int              `json:"chapterFrom"`
	ChapterTo    int              `json:"chapterTo"`
	Scenes       []map[string]any `json:"scenes,omitempty"`
	Chapters     []ChapterPlan    `json:"chapters,omitempty"`
	EndDay       int              `json:"endDay"`
	GapDays      int              `json:"gapDays"`
	TimelineID   string           `json:"timelineId,omitempty"`
	NextHook     map[string]any   `json:"nextHook,omitempty"`
	StoryDay     string           `json:"storyDay,omitempty"`
	GapFromPrev  string           `json:"gapFromPrev,omitempty"`
	Flashback    bool             `json:"flashback,omitempty"`
	InfoControl  string           `json:"infoControl,omitempty"`
	StateDeltas  any              `json:"stateDeltas,omitempty"`
	// 这里曾经有一个 RawYAML（json:"rawYaml"）：模型可以直接把一份 YAML 原文
	// 塞进候选，Accept 时优先写它。但所有校验（父子合同、时钟、身份、stateDeltas）
	// 都读结构体字段——校验的和写盘的可以不是同一份文本，等于开了个后门。它从未
	// 被任何生产代码赋值，只是一条没人用却一直敞着的通路，已删除。落盘只走
	// serializeUnitYAML。
	SourceSeedID string `json:"sourceSeedId,omitempty"`
	Attempt      string `json:"attempt,omitempty"`
	Setback      string `json:"setback,omitempty"`
	Cost         string `json:"cost,omitempty"`
}

type ReviewFinding struct {
	ID       string `json:"id"`
	Severity string `json:"severity"` // blocking | advisory
	Source   string `json:"source"`   // local_gate | ai_review
	Lens     string `json:"lens,omitempty"`
	Title    string `json:"title"`
	Evidence string `json:"evidence,omitempty"`
	// Anchor 是问题所在处的逐字原文（10–40 字），用于跨轮去重与定位。
	//
	// 与 Evidence 分工不同：Evidence 是给作者看的说明，允许转述；Anchor 必须
	// 逐字可命中，否则无法把两轮指同一处的意见合并。实测单轮审校只覆盖约 31%
	// 的问题，多轮取并集是唯一提覆盖率的办法，而去重的键就是这个字段。
	// 它不参与严格校验：审校是诊断，定位失败只降级该条，不重试整轮。
	Anchor string `json:"anchor,omitempty"`
	// RoundsHit 是被多少轮审校独立检出的次数（1 = 只有一轮看到）。
	//
	// 实测同一份正文跑 4 轮，并集 128 处里只有 19% 被至少两轮抓到、0 处被四轮
	// 全中。所以命中多次的条目更可能是可复现的硬伤，排序与「是否阻断通过」都
	// 应该看这个数字，而不是看单轮判定的 severity。
	RoundsHit  int    `json:"roundsHit,omitempty"`
	Chapter    int    `json:"chapter,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
	EntityPath string `json:"entityPath,omitempty"`
	VolumeID   string `json:"volumeId,omitempty"`
	UnitID     string `json:"unitId,omitempty"`
}

// 这两条 finding 不是"意见"，是"这一章根本没审完"的标记：输出被预算截断，
// 或分章审校里该章调用失败。它们必须是 advisory——截断的产物判不出严重度——
// 但绝不能因此被当成"没有阻断，可以过"。
const (
	FindingReviewTruncated  = "review-truncated"
	FindingReviewIncomplete = "review-incomplete"
)

// ReviewVerdict 是一份审校清单的结论分类，落盘/通过的唯一判据。
const (
	ReviewClean      = "clean"      // 无阻断且每章都审完了
	ReviewBlocking   = "blocking"   // 有必须修的硬伤
	ReviewUnfinished = "unfinished" // 有章没审完：沉默不是证据
)

// ReviewVerdict classifies one review list.
//
// blocking 优先于 unfinished：已知的硬伤是可执行的，审校不完整只是一个缺口。
// 两者都不是 clean，所以"0 条 blocking"不能单独作为放行条件。
//
// 单轮审校的判定不稳定，所以 blocking 还要看复现次数：同一章两次调用（同为
// high effort）曾给出完全不同的 blocking，实测同一份正文跑 4 轮、并集 128 处
// 里 0 处被四轮全部抓到。若单轮即可阻断，清单会永远关不掉——修完一条又冒出
// 另一批同样"看起来是 blocking"的新条目。
//
// 因此 RoundsHit>=2（至少两轮独立检出的同一处）才阻断；单轮 blocking 记为
// 待确认，仍留在清单里给作者看，但不卡流程。RoundsHit 为 0 表示这份清单不是
// 多轮并集产的（比如旧数据或本地硬门），此时按原语义处理，保持兼容。
func ReviewVerdict(findings []ReviewFinding) string {
	unfinished := false
	for _, f := range findings {
		switch f.ID {
		case FindingReviewTruncated, FindingReviewIncomplete:
			unfinished = true
			continue
		}
		if f.Severity != "blocking" {
			continue
		}
		if f.RoundsHit == 0 || f.RoundsHit >= 2 {
			return ReviewBlocking
		}
	}
	if unfinished {
		return ReviewUnfinished
	}
	return ReviewClean
}

type CanonChange struct {
	Path        string `json:"path"`
	Description string `json:"description"`
	Before      string `json:"before,omitempty"`
	After       string `json:"after,omitempty"`
	Locked      bool   `json:"locked,omitempty"`
}

type ForeshadowAssignment struct {
	ID            string `json:"id"`
	PlantUnit     string `json:"plantUnit,omitempty"`
	PayoffUnit    string `json:"payoffUnit,omitempty"`
	PlantChapter  int    `json:"plantChapter,omitempty"`
	PayoffChapter int    `json:"payoffChapter,omitempty"`
}

type ChapterActualDelta struct {
	Stem      string `json:"stem"`
	Field     string `json:"field"`
	From      string `json:"from"`
	To        string `json:"to"`
	Evidence  string `json:"evidence"`
	Confirmed bool   `json:"confirmed"`
}

type ChapterActual struct {
	Chapter                  int                  `json:"chapter"`
	StoryDay                 int                  `json:"storyDay,omitempty"`
	EndDay                   int                  `json:"endDay,omitempty"`
	Summary                  string               `json:"summary"`
	StateDeltas              []ChapterActualDelta `json:"stateDeltas,omitempty"`
	Plant                    []string             `json:"plant,omitempty"`
	Payoff                   []string             `json:"payoff,omitempty"`
	PlannedForeshadowHandled []string             `json:"plannedForeshadowHandled,omitempty"`
}

type Candidate struct {
	ID           string          `json:"id"`
	RunID        string          `json:"runId"`
	BookID       string          `json:"bookId"`
	Kind         TaskKind        `json:"kind"`
	Status       CandidateStatus `json:"status"`
	BaseRevision string          `json:"baseRevision"`
	TargetPath   string          `json:"targetPath,omitempty"`
	UnitID       string          `json:"unitId,omitempty"`
	VolumeID     string          `json:"volumeId,omitempty"`
	Chapters     []ChapterDraft  `json:"chapters,omitempty"`
	// BaseChapters is the pre-revision prose for a revise candidate, kept so the
	// author can diff before accepting ("确认落盘" must not be a blind signature).
	BaseChapters          []ChapterDraft         `json:"baseChapters,omitempty"`
	UnitOutlines          []UnitOutlineDraft     `json:"unitOutlines,omitempty"`
	Mode                  string                 `json:"mode,omitempty"`          // plan: init|reinit|volume
	PreservePaths         []string               `json:"preservePaths,omitempty"` // reinit 接受时保留的既有路径
	PlanMarkdown          map[string]string      `json:"planMarkdown,omitempty"`
	PlanSettings          map[string]string      `json:"planSettings,omitempty"` // relative path → content (init/reinit)
	ForeshadowAssignments []ForeshadowAssignment `json:"foreshadowAssignments,omitempty"`
	ActualChapters        []ChapterActual        `json:"actualChapters,omitempty"`
	Replacement           string                 `json:"replacement,omitempty"`
	Findings              []ReviewFinding        `json:"findings,omitempty"`
	CanonChanges          []CanonChange          `json:"canonChanges,omitempty"`
	CreatedAt             time.Time              `json:"createdAt"`
	AcceptedAt            *time.Time             `json:"acceptedAt,omitempty"`
}

type AcceptCommand struct {
	CandidateID      string   `json:"candidateId"`
	BaseRevision     string   `json:"baseRevision"`
	AcceptFindingIDs []string `json:"acceptFindingIds,omitempty"`
	ForceRiskAccept  bool     `json:"forceRiskAccept,omitempty"`
}

type CallRecord struct {
	Index            int    `json:"index"`
	ModelID          string `json:"modelId"`
	Provider         string `json:"provider"`
	Step             string `json:"step"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
	// CacheReadTokens 是这次调用命中 provider 前缀缓存的 prompt token 数。
	// 重试把 repair 追加在 payload 末尾，所以它应接近首轮的 prompt 量——
	// 这个数字就是"重试几乎免费"的证据，也是缓存被破坏时的告警。
	CacheReadTokens     int `json:"cacheReadTokens,omitempty"`
	CacheCreationTokens int `json:"cacheCreationTokens,omitempty"`
	// FinishReason 是 provider 报的停止原因；"length" 表示输出被预算截断。
	// 记下来才能在日志里分辨"模型答歪了"和"我们给的预算不够"。
	FinishReason string `json:"finishReason,omitempty"`
	// ReasoningTokens 是 CompletionTokens 里属于思维链的部分（已包含在
	// completionTokens 内，不额外相加）。审校这类"答案很短、思考很长"的调用，
	// 不分开记就无法解释 completion 为什么上万。
	ReasoningTokens int       `json:"reasoningTokens,omitempty"`
	DurationMs      int64     `json:"durationMs"`
	Error           string    `json:"error,omitempty"`
	StartedAt       time.Time `json:"startedAt"`
}

type Run struct {
	ID             string       `json:"id"`
	BookID         string       `json:"bookId"`
	Kind           TaskKind     `json:"kind"`
	Status         RunStatus    `json:"status"`
	Spec           TaskSpec     `json:"spec"`
	Policy         CallPolicy   `json:"policy"`
	ModelID        string       `json:"modelId"`
	CallsUsed      int          `json:"callsUsed"`
	Calls          []CallRecord `json:"calls"`
	CandidateID    string       `json:"candidateId,omitempty"`
	ContextSummary []string     `json:"contextSummary,omitempty"`
	Error          string       `json:"error,omitempty"`
	// Warnings 记录"跑成了但走了退路"的事实：定稿的整单元核对被拒、退回逐章，
	// 或某条优化路径没生效。没有它，作者只看到调用数变多，不知道代价花在哪。
	Warnings  []string  `json:"warnings,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ModelAssignment struct {
	Role    StageModelRole `json:"role"`
	ModelID string         `json:"modelId"`
}

type BookSummary struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Genre           string `json:"genre"`
	Subgenre        string `json:"subgenre"`
	Stage           string `json:"stage"`
	LastCommittedCh int    `json:"lastCommittedCh"`
	NextAction      string `json:"nextAction"`
	TimeSystem      string `json:"timeSystem,omitempty"`
	UnitCount       int    `json:"unitCount"`
	DraftedCount    int    `json:"draftedCount"`
	ReviewedCount   int    `json:"reviewedCount"`
	OpenCandidates  int    `json:"openCandidates"`
	RootPath        string `json:"rootPath"`
	ReadOnly        bool   `json:"readOnly"`
}

type VolumeInfo struct {
	Volume     int                 `json:"volume"`
	FileName   string              `json:"fileName"`
	Cast       []string            `json:"cast"`
	Raw        string              `json:"raw,omitempty"`
	Structured *StructuredMarkdown `json:"structured,omitempty"`
}

type UnitInfo struct {
	UnitID      string     `json:"unitId"`
	Volume      int        `json:"volume"`
	Index       int        `json:"index"`
	Status      UnitStatus `json:"status"`
	Title       string     `json:"title"`
	ChapterFrom int        `json:"chapterFrom"`
	ChapterTo   int        `json:"chapterTo"`
	HasProse    bool       `json:"hasProse"`
	OutlinePath string     `json:"outlinePath"`
	ProsePath   string     `json:"prosePath"`
	// ChapterTarget 是派生出来的单章目标字数（books.ChapterTarget）。
	// UI 直接用它显示，不再自己解析细纲的 word_target——那个字段是单元总数，
	// 曾被标成「字/章」，把 30000/6 显示成 30000 字每章。
	ChapterTarget int    `json:"chapterTarget"`
	OutlineRaw    string `json:"outlineRaw,omitempty"`
	ProseRaw      string `json:"proseRaw,omitempty"`
}

type CastCard struct {
	Stem    string     `json:"stem"`
	Name    string     `json:"name"`              // 纯姓名，唯一来源；不含括号说明
	Tagline string     `json:"tagline,omitempty"` // 一句话身份说明，与姓名分离
	Status  CastStatus `json:"status"`
	Role    string     `json:"role"`
	Raw     string     `json:"raw,omitempty"`
}

// BookSnapshot is the parsed on-disk book used by pipelines and the UI.
type BookSnapshot struct {
	Summary    BookSummary       `json:"summary"`
	StateRaw   string            `json:"stateRaw,omitempty"`
	Volumes    []VolumeInfo      `json:"volumes"`
	Units      []UnitInfo        `json:"units"`
	Cast       []CastCard        `json:"cast"`
	Files      map[string]string `json:"files,omitempty"` // relative path → content
	Revision   string            `json:"revision"`
	ImportedAt time.Time         `json:"importedAt"`
	// IdentitySegments 人物职务身份时间线（canon/identity-timeline.yaml）。
	// 按 stem + from_unit 排序；单元内部身份恒定，转换只在单元边界。
	IdentitySegments []IdentitySegment `json:"identitySegments,omitempty"`
	// StructuredOutlines are projections of the same Markdown in Files. They
	// carry no independent state and are safe to discard and reparse.
	StructuredOutlines map[string]*StructuredMarkdown `json:"structuredOutlines,omitempty"`
}

// IdentitySegment 一段恒定身份区间（与单元边界对齐）。
type IdentitySegment struct {
	Stem      string `json:"stem" yaml:"stem"`
	RoleTitle string `json:"roleTitle" yaml:"role_title"`
	Org       string `json:"org,omitempty" yaml:"org,omitempty"`
	FromUnit  string `json:"fromUnit" yaml:"from_unit"`
	ToUnit    string `json:"toUnit,omitempty" yaml:"to_unit,omitempty"`
	Note      string `json:"note,omitempty" yaml:"note,omitempty"`
}

// ProviderPublic is the API DTO without API key plaintext.
type ProviderPublic struct {
	ID                     string          `json:"id"`
	Provider               LLMProviderType `json:"provider"`
	Name                   string          `json:"name"`
	BaseURL                string          `json:"baseUrl,omitempty"`
	DefaultProtocolDialect string          `json:"defaultProtocolDialect,omitempty"`
	HasAPIKey              bool            `json:"hasApiKey"`
	Models                 []LLMModelRef   `json:"models,omitempty"`
	CreatedAt              string          `json:"createdAt"`
	UpdatedAt              string          `json:"updatedAt"`
}

func (c LLMProviderConfig) Public() ProviderPublic {
	return ProviderPublic{
		ID:                     c.ID,
		Provider:               c.Provider,
		Name:                   c.Name,
		BaseURL:                c.BaseURL,
		DefaultProtocolDialect: c.DefaultProtocolDialect,
		HasAPIKey:              c.APIKey != "",
		Models:                 c.Models,
		CreatedAt:              c.CreatedAt,
		UpdatedAt:              c.UpdatedAt,
	}
}
