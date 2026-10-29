package domain

import "time"

// TaskKind identifies a controlled novel generation action.
type TaskKind string

const (
	TaskInit       TaskKind = "init"
	TaskPlan       TaskKind = "plan"
	TaskOutline    TaskKind = "outline"
	TaskWrite      TaskKind = "write"
	TaskReview     TaskKind = "review"
	TaskRevise     TaskKind = "revise"
	TaskFinalize   TaskKind = "finalize"
	TaskRewriteEntity TaskKind = "rewrite_entity" // 单实体重写优化：bible/人物卡/卷纲/世界规则等
	TaskLocalEdit  TaskKind = "local_edit"
	TaskContinue   TaskKind = "continue"
	TaskExpand     TaskKind = "expand"
	TaskPolish     TaskKind = "polish"
	TaskRewriteSel TaskKind = "rewrite_selection"
)

type UnitStatus string

const (
	UnitProposed UnitStatus = "proposed"
	UnitAccepted UnitStatus = "accepted"
	UnitDrafted  UnitStatus = "drafted"
	UnitReviewed UnitStatus = "reviewed"
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
	StagePlan     StageModelRole = "plan"
	StageOutline  StageModelRole = "outline"
	StageWrite    StageModelRole = "write"
	StageReview   StageModelRole = "review"
	StageLocalEdit StageModelRole = "local_edit"
)

// CallPolicy is fixed before a run starts. Tools are always empty.
type CallPolicy struct {
	MaxCalls         int           `json:"maxCalls"`
	MaxOutputTokens  int           `json:"maxOutputTokens"`
	TimeoutSec       int           `json:"timeoutSec"`
	AllowOneRepair   bool          `json:"allowOneRepair"`
	SplitByChapter   bool          `json:"splitByChapter,omitempty"`
}

// TaskSpec starts a controlled generation run.
type TaskSpec struct {
	Kind            TaskKind          `json:"kind"`
	BookID          string            `json:"bookId"`
	VolumeID        string            `json:"volumeId,omitempty"`
	UnitID          string            `json:"unitId,omitempty"`
	Chapter         int               `json:"chapter,omitempty"`
	BaseRevision    string            `json:"baseRevision"`
	ModelID         string            `json:"modelId,omitempty"`
	IdempotencyKey  string            `json:"idempotencyKey,omitempty"`
	ExtraContextIDs []string          `json:"extraContextIds,omitempty"`
	KnowledgeIDs    []string          `json:"knowledgeIds,omitempty"` // 作者勾选的参考知识文章
	Selection       *TextSelection    `json:"selection,omitempty"`
	Instruction     string            `json:"instruction,omitempty"`
	IssueIDs        []string          `json:"issueIds,omitempty"`
	TargetPath      string            `json:"targetPath,omitempty"` // rewrite_entity 目标文件
	EntityKind      string            `json:"entityKind,omitempty"` // bible|cast|volume|world|style|rules|facts
	ForceRiskAccept bool              `json:"forceRiskAccept,omitempty"`
}

type TextSelection struct {
	Path      string `json:"path"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
	Text      string `json:"text"`
	Before    string `json:"before,omitempty"`
	After     string `json:"after,omitempty"`
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
	ContractVersion  string         `json:"contractVersion"`
	TaskKind         TaskKind       `json:"taskKind"`
	BookID           string         `json:"bookId"`
	UnitID           string         `json:"unitId,omitempty"`
	VolumeID         string         `json:"volumeId,omitempty"`
	InputHashes      map[string]string `json:"inputHashes"`
	Blocks           []ContextBlock `json:"blocks"`
	KnowledgeVersion string         `json:"knowledgeVersion"`
	EstPromptTokens  int            `json:"estPromptTokens"`
	Policy           CallPolicy     `json:"policy"`
	SystemPrompt     string         `json:"systemPrompt"`
	UserPrompt       string         `json:"userPrompt"`
}

type ChapterDraft struct {
	Chapter int    `json:"chapter"`
	Title   string `json:"title"`
	Body    string `json:"body"`
}

type UnitOutlineDraft struct {
	UnitID        string         `json:"unitId"`
	Status        UnitStatus     `json:"status"`
	Title         string         `json:"title"`
	Function      string         `json:"function"`
	Desire        string         `json:"desire"`
	Obstacle      string         `json:"obstacle"`
	Choice        string         `json:"choice"`
	Payoff        string         `json:"payoff"`
	Pleasure      string         `json:"pleasure"`
	Forbidden     string         `json:"forbidden"`
	WordTarget    string         `json:"wordTarget"`
	OnStage       []string       `json:"onStage"`
	POV           string         `json:"pov"`
	ChapterFrom   int            `json:"chapterFrom"`
	ChapterTo     int            `json:"chapterTo"`
	Scenes        []map[string]any `json:"scenes,omitempty"`
	NextHook      map[string]any  `json:"nextHook,omitempty"`
	StoryDay      string          `json:"storyDay,omitempty"`
	GapFromPrev   string          `json:"gapFromPrev,omitempty"`
	Flashback     bool            `json:"flashback,omitempty"`
	InfoControl   string          `json:"infoControl,omitempty"`
	StateDeltas   any             `json:"stateDeltas,omitempty"`
	RawYAML       string          `json:"rawYaml,omitempty"`
}

type ReviewFinding struct {
	ID         string `json:"id"`
	Severity   string `json:"severity"` // blocking | advisory
	Source     string `json:"source"`   // local_gate | ai_review
	Lens       string `json:"lens,omitempty"`
	Title      string `json:"title"`
	Evidence   string `json:"evidence,omitempty"`
	Chapter    int    `json:"chapter,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

type CanonChange struct {
	Path        string `json:"path"`
	Description string `json:"description"`
	Before      string `json:"before,omitempty"`
	After       string `json:"after,omitempty"`
	Locked      bool   `json:"locked,omitempty"`
}

type Candidate struct {
	ID            string          `json:"id"`
	RunID         string          `json:"runId"`
	BookID        string          `json:"bookId"`
	Kind          TaskKind        `json:"kind"`
	Status        CandidateStatus `json:"status"`
	BaseRevision  string          `json:"baseRevision"`
	TargetPath    string          `json:"targetPath,omitempty"`
	UnitID        string          `json:"unitId,omitempty"`
	VolumeID      string          `json:"volumeId,omitempty"`
	Chapters      []ChapterDraft  `json:"chapters,omitempty"`
	UnitOutlines  []UnitOutlineDraft `json:"unitOutlines,omitempty"`
	PlanMarkdown  map[string]string  `json:"planMarkdown,omitempty"`
	Replacement   string          `json:"replacement,omitempty"`
	Findings      []ReviewFinding `json:"findings,omitempty"`
	CanonChanges  []CanonChange   `json:"canonChanges,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
	AcceptedAt    *time.Time      `json:"acceptedAt,omitempty"`
}

type AcceptCommand struct {
	CandidateID     string   `json:"candidateId"`
	BaseRevision    string   `json:"baseRevision"`
	AcceptFindingIDs []string `json:"acceptFindingIds,omitempty"`
	ForceRiskAccept bool     `json:"forceRiskAccept,omitempty"`
}

type CallRecord struct {
	Index            int       `json:"index"`
	ModelID          string    `json:"modelId"`
	Provider         string    `json:"provider"`
	Step             string    `json:"step"`
	PromptTokens     int       `json:"promptTokens"`
	CompletionTokens int       `json:"completionTokens"`
	DurationMs       int64     `json:"durationMs"`
	Error            string    `json:"error,omitempty"`
	StartedAt        time.Time `json:"startedAt"`
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
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`
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
	Volume   int      `json:"volume"`
	FileName string   `json:"fileName"`
	Cast     []string `json:"cast"`
	Raw      string   `json:"raw,omitempty"`
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
	OutlineRaw  string     `json:"outlineRaw,omitempty"`
	ProseRaw    string     `json:"proseRaw,omitempty"`
}

type CastCard struct {
	Stem    string     `json:"stem"`
	Name    string     `json:"name"`    // 纯姓名，唯一来源；不含括号说明
	Tagline string     `json:"tagline,omitempty"` // 一句话身份说明，与姓名分离
	Status  CastStatus `json:"status"`
	Role    string     `json:"role"`
	Raw     string     `json:"raw,omitempty"`
}

// BookSnapshot is the parsed on-disk book used by pipelines and the UI.
type BookSnapshot struct {
	Summary    BookSummary          `json:"summary"`
	StateRaw   string               `json:"stateRaw,omitempty"`
	Volumes    []VolumeInfo         `json:"volumes"`
	Units      []UnitInfo           `json:"units"`
	Cast       []CastCard           `json:"cast"`
	Files      map[string]string    `json:"files,omitempty"` // relative path → content
	Revision   string               `json:"revision"`
	ImportedAt time.Time            `json:"importedAt"`
	// IdentitySegments 人物职务身份时间线（canon/identity-timeline.yaml）。
	// 按 stem + from_unit 排序；单元内部身份恒定，转换只在单元边界。
	IdentitySegments []IdentitySegment `json:"identitySegments,omitempty"`
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
	ID                     string        `json:"id"`
	Provider               LLMProviderType `json:"provider"`
	Name                   string        `json:"name"`
	BaseURL                string        `json:"baseUrl,omitempty"`
	DefaultProtocolDialect string        `json:"defaultProtocolDialect,omitempty"`
	HasAPIKey              bool          `json:"hasApiKey"`
	Models                 []LLMModelRef `json:"models,omitempty"`
	CreatedAt              string        `json:"createdAt"`
	UpdatedAt              string        `json:"updatedAt"`
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
