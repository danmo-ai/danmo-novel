package domain

// CharacterSnapshot is one character's state after a unit was finalized.
//
// 只放"随剧情变化"的事实。静态人设（欲望/伤口/三锚点/台词）住在
// canon/cast/<stem>.md，不在这里重复。
type CharacterSnapshot struct {
	RoleTitle string `json:"roleTitle,omitempty" yaml:"role_title,omitempty"` // 本单元生效的职务身份
	Location  string `json:"location,omitempty" yaml:"location,omitempty"`
	Injury    string `json:"injury,omitempty" yaml:"injury,omitempty"`
	Resources string `json:"resources,omitempty" yaml:"resources,omitempty"`
	Goal      string `json:"goal,omitempty" yaml:"goal,omitempty"` // 此刻想要什么
	// Knows 是 POV 知情范围。写作不需要它（会诱导模型提前揭示），
	// 审校需要它来判断「他凭什么知道」。
	Knows []string `json:"knows,omitempty" yaml:"knows,omitempty"`
}

// LoopEntry is one foreshadow thread and its current status.
type LoopEntry struct {
	ID      string `json:"id" yaml:"id"`
	Status  string `json:"status" yaml:"status"` // planted | advanced | paid
	Detail  string `json:"detail,omitempty" yaml:"detail,omitempty"`
	Planted int    `json:"planted,omitempty" yaml:"planted,omitempty"` // 埋点章
	Paid    int    `json:"paid,omitempty" yaml:"paid,omitempty"`       // 回收章
}

// UnitState is the continuity snapshot taken right after one unit was finalized.
//
// 全书状态是「按单元段」的序列，读取时取目标单元之前最近的一段——与
// canon/identity-timeline.yaml 同一约定。因此续写与重写走同一条规则：
// 两者都只看目标单元之前在案的状态，后续单元的结果不会倒灌。
type UnitState struct {
	Unit     string                       `json:"unit" yaml:"unit"`
	Volume   int                          `json:"volume,omitempty" yaml:"volume,omitempty"`
	AfterCh  int                          `json:"afterCh,omitempty" yaml:"after_ch,omitempty"`
	StoryDay int                          `json:"storyDay,omitempty" yaml:"story_day,omitempty"`
	Cast     map[string]CharacterSnapshot `json:"cast,omitempty" yaml:"cast,omitempty"`
	Loops    []LoopEntry                  `json:"loops,omitempty" yaml:"loops,omitempty"`
}

// StateFile is the on-disk shape of continuity/state.yaml.
type StateFile struct {
	Version int         `json:"version" yaml:"version"`
	Units   []UnitState `json:"units" yaml:"units"`
}
