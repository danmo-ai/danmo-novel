package domain

import "strings"

// CharacterSnapshot is one character's state after a unit was finalized.
//
// 只放"随剧情变化"的事实。静态人设（欲望/伤口/三锚点/台词）住在
// canon/cast/<stem>.md，不在这里重复。
//
// 它是人物卡的**可变镜像**：用 stem 与 canon/cast/<stem>.md 一一对应，卡回答
// 「这个人是谁」，快照回答「他现在怎么样」。因此每定稿一个单元都要写出**全量**
// 快照，读取时只看最后一段即可携带完整状态——不需要读时合并，也不会因为
// 某段只记了变化项而丢掉更早的事实。
//
// 这里**不存职务身份**。身份由 canon/identity-timeline.yaml 独占：它是按单元
// 边界切换的公开身份，写作与审校都直接从时间线取（identityLabel）。曾经这里也
// 存过一份 role_title 副本，实测 7 段里只有 1 段有值，且人工修正时间线后副本
// 不会跟着变——同一份提示词里同一个人出现两个身份，正是「跨文件副本漂移」。
type CharacterSnapshot struct {
	Location string `json:"location,omitempty" yaml:"location,omitempty"`
	// Health 是身体状态，取代原来的 Injury：既能写「刀伤未愈」也能写「已愈」。
	// 字段失效必须显式写终值，否则后续单元无从得知它已经结束。
	Health    string `json:"health,omitempty" yaml:"health,omitempty"`
	Resources string `json:"resources,omitempty" yaml:"resources,omitempty"`
	Goal      string `json:"goal,omitempty" yaml:"goal,omitempty"` // 此刻想要什么
	// Relation 是当前关系状态（如「决裂」「正式恋人」）。人物卡里的「## 关系」
	// 表是基础关系与稳定底色，这里只放会随剧情推进而改变的那一层。
	Relation string `json:"relation,omitempty" yaml:"relation,omitempty"`
	// Life 是生命状态：alive / missing / dead。此前无法表达，于是全书十四个
	// 单元写过角色死亡却一条记录都没有，死者照样能被写进后续 onStage。
	Life string `json:"life,omitempty" yaml:"life,omitempty"`
	// Knows 是 POV 知情范围。写作不需要它（会诱导模型提前揭示），
	// 审校需要它来判断「他凭什么知道」。
	Knows []string `json:"knows,omitempty" yaml:"knows,omitempty"`
}

// CanonicalLife 把 life 归一成 alive / missing / dead；空值保持为空（未声明）。
func CanonicalLife(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "null":
		return ""
	case "alive", "存活", "在世":
		return "alive"
	case "missing", "失踪", "下落不明":
		return "missing"
	case "dead", "死亡", "已死", "身亡":
		return "dead"
	default:
		return strings.ToLower(strings.TrimSpace(v))
	}
}

// LifeStages 是 life 的合法取值与流转顺序。身份/生命都是单向的：
// 复活不是本产品支持的叙事（需要时另开一条伏笔，而不是回退字段）。
var LifeStages = []string{"alive", "missing", "dead"}

// StateDeltaFields 是 state_deltas.field 的唯一合法取值。
//
// 这份常量是**唯一真源**：提示词里的词表、StateFromOutline 的识别分支、
// 校验器的白名单全部引用它。此前三者各写一份，提示词说
// role_title|location|injury|resources|goal|knowledge，识别分支只认其中六个、
// 没有 default 分支，于是 772 个单元里 399 条 state_deltas 有 281 条（70%）
// 被静默丢弃，丢弃的 field 名多达 114 种。
//
// 不做别名兼容：旧写法由迁移一次性改名（见 books.NormalizeStateDeltaField），
// 运行期只认这八个名字，写错就报错、进修复轮。
var StateDeltaFields = []string{
	"role_title", "location", "health", "resources", "goal", "knows", "relation", "life",
}

// 字段名常量：下面这些地方要按名字分支（而不是当普通字符串存放）。
//
// 之前到处都是裸字符串 "role_title" / "life"，于是「哪些字段归谁所有」这件事
// 只存在于注释里。写成常量后，"身份由时间线独占"这条规则才有唯一的落点。
const (
	StateFieldRoleTitle = "role_title"
	StateFieldLocation  = "location"
	StateFieldHealth    = "health"
	StateFieldResources = "resources"
	StateFieldGoal      = "goal"
	StateFieldKnows     = "knows"
	StateFieldRelation  = "relation"
	StateFieldLife      = "life"
)

// OwnedByIdentityTimeline 表示该字段的唯一真源不是账本，而是别的实体表。
//
// role_title 的取值由 canon/identity-timeline.yaml 独占，账本只保存"声明"，
// 因此在连续性快照里不落副本（见 books.StateFromOutline）。
func OwnedByIdentityTimeline(field string) bool {
	return CanonicalStateField(field) == StateFieldRoleTitle
}

// CanonicalStateField 返回规范字段名；不在词表内时返回空串。
func CanonicalStateField(field string) string {
	f := strings.ToLower(strings.TrimSpace(field))
	for _, allowed := range StateDeltaFields {
		if allowed == f {
			return f
		}
	}
	return ""
}

// StateDeltaFieldList 是给提示词用的词表文本（如 a|b|c）。
func StateDeltaFieldList() string {
	return strings.Join(StateDeltaFields, "|")
}

// LoopEntry is one foreshadow thread and its current status.
type LoopEntry struct {
	ID      string `json:"id" yaml:"id"`
	Status  string `json:"status" yaml:"status"` // planned | planted | advanced | paid | dropped
	Detail  string `json:"detail,omitempty" yaml:"detail,omitempty"`
	Planted int    `json:"planted,omitempty" yaml:"planted,omitempty"` // 埋点章
	Paid    int    `json:"paid,omitempty" yaml:"paid,omitempty"`       // 回收章
}

// UnitState is the continuity snapshot taken right after one unit was finalized.
//
// 全书状态是「按单元段」的序列，读取时取目标单元之前最近的一段——与
// canon/identity-timeline.yaml 同一约定。因此续写与重写走同一条规则：
// 两者都只看目标单元之前在案的状态，后续单元的结果不会倒灌。
//
// 每段都是**全量快照**（上一段 + 本单元 state_deltas 合并后落盘），所以
// 「取最近一段」就等于「携带完整状态」，读取侧不需要任何合并逻辑。
type UnitState struct {
	Unit     string                       `json:"unit" yaml:"unit"`
	Volume   int                          `json:"volume,omitempty" yaml:"volume,omitempty"`
	AfterCh  int                          `json:"afterCh,omitempty" yaml:"after_ch,omitempty"`
	StoryDay int                          `json:"storyDay,omitempty" yaml:"story_day,omitempty"`
	EndDay   int                          `json:"endDay,omitempty" yaml:"end_day,omitempty"`
	Cast     map[string]CharacterSnapshot `json:"cast,omitempty" yaml:"cast,omitempty"`
	Loops    []LoopEntry                  `json:"loops,omitempty" yaml:"loops,omitempty"`
}

// StateFile is the on-disk shape of continuity/state.yaml.
type StateFile struct {
	Version int         `json:"version" yaml:"version"`
	Units   []UnitState `json:"units" yaml:"units"`
}
