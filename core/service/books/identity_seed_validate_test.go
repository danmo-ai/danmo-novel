package books

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 身份段：u1 试用，u2 起正式。转换点是 u1→u2 边界。
func seedIdentityFixture() []domain.IdentitySegment {
	return []domain.IdentitySegment{
		{Stem: "wen", RoleTitle: "新警", FromUnit: "v01-U1", ToUnit: "v01-U1"},
		{Stem: "wen", RoleTitle: "试用刑警", FromUnit: "v01-U2", ToUnit: "v01-U2"},
		{Stem: "wen", RoleTitle: "正式刑警", FromUnit: "v01-U3"},
	}
}

// 转换写在生效的那个单元（u3）：合法。
// from 是「单元前状态」= 上一段（试用刑警），to 是本单元生效身份（正式刑警）。
func TestSeedIdentityAllowsBoundaryTransition(t *testing.T) {
	segs := seedIdentityFixture()
	if err := ValidateSeedIdentityChanges(segs, "v01-U3", "wen.role_title=试用刑警->正式刑警"); err != nil {
		t.Fatalf("边界转换应通过: %v", err)
	}
}

// 转换写早了（u2 内并不会转正）：必须拒绝。
//
// 这类种子会让细纲永远无法通过：父级继承要求逐字含这条迁移，
// 角色校验又要求 to 等于本单元生效身份（试用刑警），两者互斥。
func TestSeedIdentityRejectsTransitionWrittenTooEarly(t *testing.T) {
	segs := seedIdentityFixture()
	err := ValidateSeedIdentityChanges(segs, "v01-U2", "wen.role_title=试用刑警->正式刑警")
	if err == nil {
		t.Fatal("把 u3 的转换写进 u2 必须被拒绝")
	}
	msg := err.Error()
	// 错误必须指出去哪儿改，否则作者只能猜。
	if !strings.Contains(msg, "v01-U3") {
		t.Fatalf("错误应指出正确归口单元 v01-U3，实际: %s", msg)
	}
}

// 单元内身份不变时写 from==to 是合法的（冗余但无害）。
func TestSeedIdentityAllowsUnchangedRole(t *testing.T) {
	segs := seedIdentityFixture()
	if err := ValidateSeedIdentityChanges(segs, "v01-U2", "wen.role_title=试用刑警->试用刑警"); err != nil {
		t.Fatalf("恒等迁移应通过: %v", err)
	}
}

// from 必须能对上「上一段」或「本单元」，随便编一个都不行。
func TestSeedIdentityRejectsUnknownFrom(t *testing.T) {
	segs := seedIdentityFixture()
	err := ValidateSeedIdentityChanges(segs, "v01-U3", "wen.role_title=市局刑警->正式刑警")
	if err == nil || !strings.Contains(err.Error(), "市局刑警") {
		t.Fatalf("from 对不上任何一段时必须拒绝，实际: %v", err)
	}
}

// 非 role_title 的迁移不参与身份校验（位置/资源等归账本）。
func TestSeedIdentityIgnoresOtherFields(t *testing.T) {
	segs := seedIdentityFixture()
	if err := ValidateSeedIdentityChanges(segs, "v01-U2", "wen.location=派出所->市局；wen.resources=无->工资"); err != nil {
		t.Fatalf("非身份字段不应被拦: %v", err)
	}
}

// 完全没有覆盖本单元的身份段：必须报出来，而不是静默放行。
func TestSeedIdentityRejectsMissingSegment(t *testing.T) {
	segs := seedIdentityFixture()
	err := ValidateSeedIdentityChanges(segs, "v01-U1", "other.role_title=甲->乙")
	if err == nil || !strings.Contains(err.Error(), "other") {
		t.Fatalf("无生效段时必须拒绝，实际: %v", err)
	}
}

// 卷纲级校验：身份变更必须先生效，再校验单元种子。
//
// 变更实体写在所属单元之后，单遍扫描会拿落盘前的时间线去校验，永远对不上。
func TestVolumeSeedIdentitiesAppliesChangesBeforeValidating(t *testing.T) {
	doc := ParseStructuredMarkdown(`# 卷纲 · v01
- schema_version: 1
- volume_id: v01

## 单元 · v01-U1
- title: 报到
- chapter_range: 1-3
- state_changes: wen.location=家中->派出所

## 单元 · v01-U2
- title: 转正
- chapter_range: 4-6
- state_changes: wen.role_title=新警->试用刑警

### 身份变更 · v01-U2-wen
- stem: wen
- role_title: 试用刑警
- org: 分局
- from_unit: v01-U2
- reason: 转正
`, VolumeOutlineProtocol)

	units := []domain.UnitInfo{{UnitID: "v01-U1"}, {UnitID: "v01-U2"}}
	// 盘上只有 U1 的段：U2 的身份靠本卷的「身份变更」实体声明。
	segs := []domain.IdentitySegment{{Stem: "wen", RoleTitle: "新警", Org: "派出所", FromUnit: "v01-U1", ToUnit: "v01-U1"}}
	if err := ValidateVolumeSeedIdentities(segs, doc, units); err != nil {
		t.Fatalf("本卷声明的身份变更应先于单元校验生效: %v", err)
	}
}

// ── 引用（外键）语义 ──────────────────────────────────────────────────────

// 身份段必须有稳定主键：显式 id，否则 stem@from_unit 派生。
func TestSegmentKeyIsStableAndUnique(t *testing.T) {
	segs := seedIdentityFixture()
	a := segs[1]
	if got := SegmentKey(a); got != "wen@v01-U2" {
		t.Fatalf("派生主键应为 stem@from_unit，实际 %q", got)
	}
	b := a
	b.ID = "ident-wen-trial"
	if got := SegmentKey(b); got != "ident-wen-trial" {
		t.Fatalf("显式 id 应优先，实际 %q", got)
	}
}

// 引用写法：@单元 解析成该单元生效的那一段。
func TestResolveIdentityRefByUnit(t *testing.T) {
	segs := seedIdentityFixture()
	got := ResolveIdentitySegment(segs, "wen", "@v01-U3", "v01-U1")
	if got == nil || got.RoleTitle != "正式刑警" {
		t.Fatalf("@v01-U3 应解析到正式刑警，实际 %+v", got)
	}
}

// 引用写法：完整主键解析成对应段。
func TestResolveIdentityRefByKey(t *testing.T) {
	segs := seedIdentityFixture()
	got := ResolveIdentitySegment(segs, "wen", "wen@v01-U2", "")
	if got == nil || got.RoleTitle != "试用刑警" {
		t.Fatalf("主键引用应解析到试用刑警，实际 %+v", got)
	}
}

// 文案反查必须唯一；重名一律判失败，不猜。
func TestResolveIdentityRefRejectsAmbiguousTitle(t *testing.T) {
	segs := []domain.IdentitySegment{
		{Stem: "wen", RoleTitle: "刑警", FromUnit: "v01-U1", ToUnit: "v01-U1"},
		{Stem: "wen", RoleTitle: "刑警", FromUnit: "v01-U2"},
	}
	if got := ResolveIdentitySegment(segs, "wen", "刑警", ""); got != nil {
		t.Fatalf("重名文案不应猜测，实际解析到 %+v", got)
	}
}

// 核心：种子里用「@单元」引用身份时，不需要跟时间线文案逐字相等。
//
// 这正是摆脱「文案硬编码」的证据——把时间线里的措辞改掉，引用照旧成立。
func TestSeedIdentityAcceptsReferenceInsteadOfLiteral(t *testing.T) {
	segs := seedIdentityFixture()
	// 时间线措辞与引用完全无关，仍应通过。
	if err := ValidateSeedIdentityChanges(segs, "v01-U3", "wen.role_title=@v01-U3"); err != nil {
		t.Fatalf("引用式身份迁移应通过: %v", err)
	}
}

// 引用也挡得住「写早了」：@v01-U3 写在 v01-U2 里就必须拒绝。
func TestSeedIdentityRejectsReferenceToFutureUnit(t *testing.T) {
	segs := seedIdentityFixture()
	if err := ValidateSeedIdentityChanges(segs, "v01-U2", "wen.role_title=@v01-U3"); err == nil {
		t.Fatal("引用到未来单元的身份必须被拒绝")
	}
}

// 父级继承不再要求复制 role_title：身份归时间线所有，卷纲只留规划说明。
//
// 这条是死锁的结构性修复。以前 ContractStateDeltaKeys 会把 role_title 也算进
// 必须逐字继承的清单，于是「继承父级」和「等于本单元生效身份」互斥。
func TestContractStateDeltaKeysExcludesIdentity(t *testing.T) {
	keys := ContractStateDeltaKeys("wen.role_title=甲->乙；wen.location=家中->派出所")
	for _, k := range keys {
		if strings.Contains(k, "role_title") {
			t.Fatalf("role_title 归身份时间线所有，不应要求细纲继承: %v", keys)
		}
	}
	if len(keys) != 1 || !strings.Contains(keys[0], "location") {
		t.Fatalf("非身份迁移必须保留: %v", keys)
	}
}

// 走真实保存路径（ValidateStructuredOutlineFile）：卷纲里用引用写身份，必须通过。
//
// 这条覆盖的是作者/模型实际提交的那条链路，而不只是校验函数本身。
func TestVolumeSaveAcceptsIdentityReference(t *testing.T) {
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "demo"},
		IdentitySegments: []domain.IdentitySegment{
			{Stem: "wen", RoleTitle: "新警", Org: "派出所", FromUnit: "v01-U1", ToUnit: "v01-U1"},
			{Stem: "wen", RoleTitle: "试用刑警", Org: "分局", FromUnit: "v01-U2", ToUnit: "v01-U2"},
			{Stem: "wen", RoleTitle: "正式刑警", Org: "分局", FromUnit: "v01-U3"},
		},
		Cast:  []domain.CastCard{{Stem: "wen", Name: "温某", Status: domain.CastCanon}},
		Units: []domain.UnitInfo{{UnitID: "v01-U1"}, {UnitID: "v01-U2"}, {UnitID: "v01-U3"}},
		Files: map[string]string{"outline/book_outline.md": `# 全书总纲
- schema_version: 1
- book_id: demo
- title: 样本
- logline: 一句话

## 故事线 · career
- kind: main
- description: 职业线
- opening_state: 入职
- ending_state: 立足

## 分卷 · v01
- arc_id: arc-v01
- title: 第一卷
- chapter_range: 1-9
- purpose: 立住
- dramatic_question: 能否留下
- central_conflict: 资历不够
- stakes: 失去岗位
- entry_state: 报到
- exit_state: 转正
- thread_moves: career=入职->立足
- deliverables: 首案告破
- open_questions: 无
- next_volume_entry: 进入重案

### 转折 · e-01
- event: 首案
- cause: 报到
- effect: 转正
- chapter_window: 1-9
`},
	}
	volume := `# 卷纲 · v01
- schema_version: 1
- volume_id: v01
- title: 第一卷
- source_arc_id: arc-v01
- chapter_range: 1-9
- goal: 立住
- dramatic_question: 能否留下
- central_conflict: 资历不够
- stakes: 失去岗位
- entry_state: 报到
- exit_state: 转正
- thread_moves: career=入职->立足
- deliverables: 首案告破
- cast: wen

## 节拍 · e-01
- event: 首案
- cause: 报到
- effect: 转正
- chapter_window: 1-9

## 单元 · v01-U1
- title: 报到
- chapter_range: 1-3
- objective: 留下
- obstacle: 资历
- attempt: 抢案
- setback: 被退回
- choice: 自证
- cost: 得罪人
- outcome: 进入首案
- entry_cause: 报到
- exit_result: 拿到任务
- state_changes: wen.role_title=@v01-U1
- clock_start_day: 1
- clock_end_day: 3
- clock_gap_days: 0
- thread_ids: career
- event_ids: e-01
- next_hook: 信息缺口 | 卷宗少页

## 单元 · v01-U2
- title: 试用
- chapter_range: 4-6
- objective: 转试用
- obstacle: 议论
- attempt: 复查
- setback: 证据不足
- choice: 按程序
- cost: 欠人情
- outcome: 取得试用
- entry_cause: 拿到任务
- exit_result: 转试用
- state_changes: wen.role_title=@v01-U2
- clock_start_day: 4
- clock_end_day: 6
- clock_gap_days: 0
- thread_ids: career
- event_ids: e-01
- next_hook: 信息缺口 | 旧案重启

## 单元 · v01-U3
- title: 转正
- chapter_range: 7-9
- objective: 转正
- obstacle: 名额
- attempt: 破案
- setback: 反复
- choice: 承担
- cost: 树敌
- outcome: 转正
- entry_cause: 转试用
- exit_result: 站稳
- state_changes: wen.role_title=@v01-U3
- clock_start_day: 7
- clock_end_day: 9
- clock_gap_days: 0
- thread_ids: career
- event_ids: e-01
- next_hook: 信息缺口 | 卷末钩子
`
	if err := ValidateStructuredOutlineFile(snap, "outline/volumes/v01.md", volume); err != nil {
		t.Fatalf("引用式卷纲应能保存: %v", err)
	}
}

// 同一份卷纲，把引用换成「写早一年」的文案，保存路径必须拒绝。
func TestVolumeSaveRejectsIdentityLiteralForWrongUnit(t *testing.T) {
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "demo"},
		IdentitySegments: []domain.IdentitySegment{
			{Stem: "wen", RoleTitle: "新警", Org: "派出所", FromUnit: "v01-U1"},
			{Stem: "wen", RoleTitle: "正式刑警", Org: "分局", FromUnit: "v01-U3"},
		},
		Cast:  []domain.CastCard{{Stem: "wen", Name: "温某", Status: domain.CastCanon}},
		Units: []domain.UnitInfo{{UnitID: "v01-U1"}, {UnitID: "v01-U3"}},
	}
	doc := `# 卷纲 · v01
- schema_version: 1
- volume_id: v01
- title: 第一卷
- source_arc_id: arc-v01
- chapter_range: 1-6
- goal: 立住
- dramatic_question: 能否留下
- central_conflict: 资历
- stakes: 失去岗位
- entry_state: 报到
- exit_state: 转正
- thread_moves: career=入职->立足
- deliverables: 首案
- cast: wen

## 节拍 · e-01
- event: 首案
- cause: 报到
- effect: 转正
- chapter_window: 1-6

## 单元 · v01-U1
- title: 报到
- chapter_range: 1-3
- objective: 留下
- obstacle: 资历
- attempt: 抢案
- setback: 被退回
- choice: 自证
- cost: 得罪人
- outcome: 进入首案
- entry_cause: 报到
- exit_result: 拿到任务
- state_changes: wen.role_title=正式刑警
- clock_start_day: 1
- clock_end_day: 3
- clock_gap_days: 0
- thread_ids: career
- event_ids: e-01
- next_hook: 信息缺口 | 卷宗少页

## 单元 · v01-U3
- title: 转正
- chapter_range: 4-6
- objective: 转正
- obstacle: 名额
- attempt: 破案
- setback: 反复
- choice: 承担
- cost: 树敌
- outcome: 转正
- entry_cause: 拿到任务
- exit_result: 站稳
- state_changes: wen.role_title=@v01-U3
- clock_start_day: 4
- clock_end_day: 6
- clock_gap_days: 0
- thread_ids: career
- event_ids: e-01
- next_hook: 信息缺口 | 卷末钩子
`
	if err := ValidateStructuredOutlineFile(snap, "outline/volumes/v01.md", doc); err == nil {
		t.Fatal("把 v01-U3 的身份写进 v01-U1 必须被拒绝")
	}
}
