package books

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"gopkg.in/yaml.v3"
)

type identityFile struct {
	Version  int                      `yaml:"version"`
	Segments []domain.IdentitySegment `yaml:"segments"`
}

const IdentityTimelinePath = "canon/identity-timeline.yaml"

// SaveIdentityTimeline is the sole identity-segment write primitive. Planning
// Accept and the manual editor both use it; finalization never calls it.
func SaveIdentityTimeline(root string, segments []domain.IdentitySegment) error {
	if err := ValidateIdentityTimeline(segments, nil); err != nil {
		return err
	}
	sort.SliceStable(segments, func(i, j int) bool {
		if segments[i].Stem != segments[j].Stem {
			return segments[i].Stem < segments[j].Stem
		}
		return UnitOrderKey(segments[i].FromUnit) < UnitOrderKey(segments[j].FromUnit)
	})
	body, err := yaml.Marshal(identityFile{Version: 1, Segments: segments})
	if err != nil {
		return err
	}
	path := filepath.Join(root, IdentityTimelinePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func MarshalIdentityTimeline(segments []domain.IdentitySegment) ([]byte, error) {
	if err := ValidateIdentityTimeline(segments, nil); err != nil {
		return nil, err
	}
	return yaml.Marshal(identityFile{Version: 1, Segments: segments})
}

// sortedSegmentStems 给出身份时间线校验的稳定遍历顺序。
func sortedSegmentStems(byStem map[string][]domain.IdentitySegment) []string {
	stems := make([]string, 0, len(byStem))
	for stem := range byStem {
		stems = append(stems, stem)
	}
	sort.Strings(stems)
	return stems
}

func ValidateIdentityTimeline(segments []domain.IdentitySegment, units []domain.UnitInfo) error {
	byStem := map[string][]domain.IdentitySegment{}
	validUnits := map[string]bool{}
	for _, u := range units {
		validUnits[u.UnitID] = true
	}
	for i, s := range segments {
		if !castStemRe.MatchString(s.Stem) {
			return fmt.Errorf("segments[%d].stem：非法人物 ID %s", i, s.Stem)
		}
		if strings.TrimSpace(s.RoleTitle) == "" || strings.TrimSpace(s.Org) == "" {
			return fmt.Errorf("%s：职务与机构不能为空", s.Stem)
		}
		if UnitOrderKey(s.FromUnit) == 0 || (len(validUnits) > 0 && !validUnits[s.FromUnit]) {
			return fmt.Errorf("%s.from_unit：单元不存在 %s", s.Stem, s.FromUnit)
		}
		if s.ToUnit != "" && (UnitOrderKey(s.ToUnit) == 0 || UnitOrderKey(s.ToUnit) < UnitOrderKey(s.FromUnit)) {
			return fmt.Errorf("%s.to_unit：结束单元必须不早于起始单元", s.Stem)
		}
		byStem[s.Stem] = append(byStem[s.Stem], s)
	}
	// 遍历顺序决定报错文本：map 顺序随机，同一份时间线两次校验会给出
	// 不同的错误（那段文本会进 repair 提示词），所以按 stem 排序后再比。
	for _, stem := range sortedSegmentStems(byStem) {
		rows := byStem[stem]
		sort.SliceStable(rows, func(i, j int) bool { return UnitOrderKey(rows[i].FromUnit) < UnitOrderKey(rows[j].FromUnit) })
		for i := 1; i < len(rows); i++ {
			if rows[i-1].ToUnit == "" || UnitOrderKey(rows[i].FromUnit) <= UnitOrderKey(rows[i-1].ToUnit) {
				return fmt.Errorf("%s：%s 与 %s 的身份段重叠，请先结束前一段", stem, rows[i-1].FromUnit, rows[i].FromUnit)
			}
		}
	}
	return nil
}

// ApplyIdentityChange appends a planned identity segment at a unit boundary.
// It closes the preceding open segment immediately before the new boundary.
func ApplyIdentityChange(segments []domain.IdentitySegment, change domain.IdentitySegment, units []domain.UnitInfo) ([]domain.IdentitySegment, error) {
	if UnitOrderKey(change.FromUnit) == 0 {
		return nil, fmt.Errorf("身份变更必须落在单元边界")
	}
	if current := IdentityAt(segments, change.Stem, change.FromUnit); current != nil && current.RoleTitle == change.RoleTitle && current.Org == change.Org {
		return nil, fmt.Errorf("%s 在 %s 已是该身份，无需重复创建", change.Stem, change.FromUnit)
	}
	out := append([]domain.IdentitySegment(nil), segments...)
	hasPrevious := false
	for _, s := range out {
		if s.Stem == change.Stem {
			hasPrevious = true
			break
		}
	}
	for _, s := range out {
		if s.Stem == change.Stem && UnitOrderKey(s.FromUnit) >= UnitOrderKey(change.FromUnit) {
			return nil, fmt.Errorf("%s 已有从 %s 开始的身份段，不能在其之前插入或覆盖历史", change.Stem, s.FromUnit)
		}
	}
	for i := range out {
		if out[i].Stem != change.Stem || out[i].ToUnit != "" {
			continue
		}
		if UnitOrderKey(out[i].FromUnit) >= UnitOrderKey(change.FromUnit) {
			return nil, fmt.Errorf("%s 已有从 %s 开始的身份段，不能覆盖未来规划", change.Stem, out[i].FromUnit)
		}
		prevUnit := previousUnitID(out[i].FromUnit, change.FromUnit, units)
		if hasPrevious && prevUnit == "" {
			return nil, fmt.Errorf("%s：%s 与 %s 之间存在单元断档，请先补齐单元索引", change.Stem, out[i].FromUnit, change.FromUnit)
		}
		out[i].ToUnit = prevUnit
	}
	out = append(out, change)
	if err := ValidateIdentityTimeline(out, units); err != nil {
		return nil, err
	}
	return out, nil
}

func previousUnitID(from, boundary string, units []domain.UnitInfo) string {
	candidates := []domain.UnitInfo{}
	for _, u := range units {
		if UnitOrderKey(u.UnitID) >= UnitOrderKey(from) && UnitOrderKey(u.UnitID) < UnitOrderKey(boundary) {
			candidates = append(candidates, u)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.SliceStable(candidates, func(i, j int) bool { return UnitOrderKey(candidates[i].UnitID) < UnitOrderKey(candidates[j].UnitID) })
	last := candidates[len(candidates)-1]
	// If both IDs are real units, the preceding actual unit must be contiguous
	// in the same volume, or be the final unit of the preceding volume.
	if last.Volume == 0 {
		return last.UnitID
	}
	return last.UnitID
}

func ValidateIdentityCoverage(segments []domain.IdentitySegment, cast []domain.CastCard, unit domain.UnitInfo, onStage []string) error {
	for _, stem := range onStage {
		isCandidate := false
		for _, c := range cast {
			if c.Stem == stem && c.Status == domain.CastCandidate {
				isCandidate = true
			}
		}
		if isCandidate {
			continue
		}
		if IdentityAt(segments, stem, unit.UnitID) == nil {
			return fmt.Errorf("%s：%s 缺少生效的职务身份段，请在人物卡中登记", unit.UnitID, stem)
		}
	}
	blocking, _ := ValidateUnitIdentity(segments, onStage, unit.UnitID)
	if len(blocking) > 0 {
		return fmt.Errorf("%s", strings.Join(blocking, "；"))
	}
	return nil
}

// LoadIdentityTimeline reads canon/identity-timeline.yaml (empty if absent).
func LoadIdentityTimeline(root string) ([]domain.IdentitySegment, error) {
	raw, err := os.ReadFile(filepath.Join(root, "canon", "identity-timeline.yaml"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f identityFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("identity-timeline.yaml: %w", err)
	}
	sort.SliceStable(f.Segments, func(i, j int) bool {
		if f.Segments[i].Stem != f.Segments[j].Stem {
			return f.Segments[i].Stem < f.Segments[j].Stem
		}
		return UnitOrderKey(f.Segments[i].FromUnit) < UnitOrderKey(f.Segments[j].FromUnit)
	})
	return f.Segments, nil
}

// UnitOrderKey converts "v03-U12" to a sortable int (volume*1000+index).
func UnitOrderKey(unitID string) int {
	m := regexp.MustCompile(`(?i)^v(\d+)-U(\d+)$`).FindStringSubmatch(unitID)
	if m == nil {
		return 0
	}
	v, _ := strconv.Atoi(m[1])
	u, _ := strconv.Atoi(m[2])
	return v*1000 + u
}

// IdentityAt returns the segment effective for stem at unitID, or nil.
// A segment applies when from_unit <= unitID <= to_unit (to_unit empty = open).
func IdentityAt(segments []domain.IdentitySegment, stem, unitID string) *domain.IdentitySegment {
	key := UnitOrderKey(unitID)
	var best *domain.IdentitySegment
	for i := range segments {
		s := &segments[i]
		if s.Stem != stem {
			continue
		}
		from := UnitOrderKey(s.FromUnit)
		if from > key {
			continue
		}
		if s.ToUnit != "" && UnitOrderKey(s.ToUnit) < key {
			continue
		}
		if best == nil || from >= UnitOrderKey(best.FromUnit) {
			best = s
		}
	}
	return best
}

// ValidateUnitIdentity checks every on_stage stem has exactly one effective
// identity segment at the unit. Returns advisory findings for stems with no
// declared segment (fall back to the static card) and blocking for ambiguous
// overlapping segments (data error that must be fixed by the author).
func ValidateUnitIdentity(segments []domain.IdentitySegment, onStage []string, unitID string) (blocking []string, advisory []string) {
	if len(segments) == 0 {
		return nil, nil // 无时间线时全靠静态卡，不约束
	}
	for _, stem := range onStage {
		key := UnitOrderKey(unitID)
		count := 0
		for _, s := range segments {
			if s.Stem != stem {
				continue
			}
			if UnitOrderKey(s.FromUnit) <= key && (s.ToUnit == "" || UnitOrderKey(s.ToUnit) >= key) {
				count++
			}
		}
		if count > 1 {
			blocking = append(blocking, fmt.Sprintf("%s 在 %s 有 %d 段重叠身份，请修正 identity-timeline", stem, unitID, count))
		}
	}
	return blocking, advisory
}

// ValidateStateDeltasIdentity checks a unit's state_deltas against the identity
// timeline **without** requiring the unit to already have prose.
//
// 这是 ValidateFinalizedIdentity 的可提前调用版本：细纲生成/接受时就能跑。定稿才
// 发现这类冲突的代价极大——正文已写完、只能返工规划；而 state_deltas 与时间线
// 都是本地数据，判定零成本。
func ValidateStateDeltasIdentity(segments []domain.IdentitySegment, cast []domain.CastCard, unit domain.UnitInfo, outlineRaw string) error {
	if err := ValidateIdentityCoverage(segments, cast, unit, outlineOnStageForBooks(outlineRaw)); err != nil {
		return err
	}
	_, err := stateDeltaIdentityConflict(segments, unit, outlineRaw)
	return err
}

// ValidateFinalizedIdentity compares actual state deltas with the identity
// effective at this unit. A finalizer may confirm, never invent, a new segment.
//
// `from` / `to` 的语义是「单元前状态 / 单元后状态」（见细纲契约的 stateDeltas
// 说明），不是「本单元开场身份」。所以 role_title 这一类 delta 只有 `to` 必须
// 等于本单元生效身份；`from` 允许是**上一段身份**——身份转换正是在单元边界发生，
// 「内勤新警 → 刑侦线新警」这种写法本身就是对的。
//
// 以前这里要求 from 也等于本单元生效身份，等于要求 from == to，把契约允许的
// 边界转换判成硬伤。实测代价很大：整本 v01-U2 的正文都写完了，到定稿才被拦下，
// 而报错信息又指向"修时间线"，作者以为要改已定稿的设定。
func ValidateFinalizedIdentity(segments []domain.IdentitySegment, cast []domain.CastCard, unit domain.UnitInfo, outlineRaw string) error {
	if err := ValidateIdentityCoverage(segments, cast, unit, outlineOnStageForBooks(outlineRaw)); err != nil {
		return err
	}
	_, err := stateDeltaIdentityConflict(segments, unit, outlineRaw)
	return err
}

// stateDeltaIdentityConflict returns the first conflicting finding, if any.
//
// 返回 finding 而不是只返回 error：同一条规则要在三处生效——定稿拒绝接受、
// 细纲接受拒绝落盘、审校清单里作为本地硬门显示。三处共用一份判定，措辞才不会分叉。
func stateDeltaIdentityConflict(segments []domain.IdentitySegment, unit domain.UnitInfo, outlineRaw string) (domain.ReviewFinding, error) {
	for _, delta := range parseStateDeltas(outlineRaw) {
		// 字段归属只问词表，不枚举别名：「identity / title」这类同义写法是
		// 迁移期的产物，只在 normalize 里处理一次；运行期判断必须只有一条规则。
		if !domain.OwnedByIdentityTimeline(delta.Field) {
			continue
		}
		identity := IdentityAt(segments, delta.Stem, unit.UnitID)
		if identity == nil {
			return identityFinding(unit, delta, "", "缺少本单元生效身份"), fmt.Errorf("%s：%s 缺少本单元生效身份", unit.UnitID, delta.Stem)
		}
		// to 必须解析到「本单元这一段」：定稿只确认时间线，不允许它顺手改写身份。
		// 比较的是段主键而不是文案——作者改措辞、模型写引用都不该让判定翻面。
		if target := ResolveIdentitySegment(segments, delta.Stem, delta.To, unit.UnitID); target == nil || SegmentKey(*target) != SegmentKey(*identity) {
			want := SegmentKey(*identity) + "（" + identity.RoleTitle + "）"
			return identityFinding(unit, delta, want,
					fmt.Sprintf("state_deltas 的 to=%s 不对应本单元生效段 %s；请先调整规划，不允许落盘自动改身份", delta.To, want)),
				fmt.Errorf("%s：%s 的身份 %s 不对应本单元生效段 %s；请先调整规划，不允许定稿自动改身份",
					unit.UnitID, delta.Stem, delta.To, identity.RoleTitle)
		}
		// from 是「单元前状态」，可以是本单元身份（单元内不变），也可以是上一段
		// 身份（边界转换）。两者都不匹配才说明 state_deltas 写错了。
		if strings.TrimSpace(delta.From) == "" {
			continue
		}
		if src := ResolveIdentitySegment(segments, delta.Stem, delta.From, unit.UnitID); src != nil {
			prev := identityBefore(segments, delta.Stem, unit.UnitID)
			if SegmentKey(*src) == SegmentKey(*identity) || (prev != nil && SegmentKey(*src) == SegmentKey(*prev)) {
				continue
			}
		}
		prev := prevRoleLabel(segments, delta.Stem, unit.UnitID)
		msg := fmt.Sprintf("state_deltas 的 from=%s 既不对应上一段 %s，也不对应本单元 %s；请修正 state_deltas 或身份时间线",
			delta.From, prev, identity.RoleTitle)
		return identityFinding(unit, delta, identity.RoleTitle, msg),
			fmt.Errorf("%s：%s 的单元前身份 %s 既不对应上一段 %s，也不对应本单元 %s；请修正 state_deltas 或身份时间线",
				unit.UnitID, delta.Stem, delta.From, prev, identity.RoleTitle)
	}
	return domain.ReviewFinding{}, nil
}

// identityFinding renders the same conflict as a blocking local-gate finding, so
// it shows up in the review checklist instead of only at finalize.
func identityFinding(unit domain.UnitInfo, delta stateDelta, want, msg string) domain.ReviewFinding {
	return domain.ReviewFinding{
		ID:         fmt.Sprintf("identity-%s-%s", unit.UnitID, delta.Stem),
		Severity:   "blocking",
		Source:     "local_gate",
		Lens:       "identity",
		Title:      "state_deltas 与身份时间线冲突",
		Evidence:   fmt.Sprintf("%s：%s（from=%s, to=%s，时间线=%s）", unit.UnitID, delta.Stem, delta.From, delta.To, want),
		Suggestion: msg,
		EntityPath: IdentityTimelinePath,
		UnitID:     unit.UnitID,
	}
}

// IdentityDeltaFindings exposes the identity check as review findings.
//
// 审校清单是修问题最便宜的阶段：那时规划刚定、正文可能还没写。把它做成本地硬门
// 而不是让 AI 审校去"感觉"身份不对——这条判定是确定性的，无需模型参与。
//
// 它原来只检查 state_deltas 里的 role_title 是否与时间线一致。身份副本从账本
// 移除后，那份 delta 仍是「本单元切了身份」的声明，所以这条检查保留；同时补上
// 更常见的一类：on_stage 里有人在本单元没有生效身份段。那本来只在细纲接受与
// 定稿时才查，而那时正文已经写完。
func IdentityDeltaFindings(segments []domain.IdentitySegment, unit domain.UnitInfo, outlineRaw string) []domain.ReviewFinding {
	if f, err := stateDeltaIdentityConflict(segments, unit, outlineRaw); err != nil {
		return []domain.ReviewFinding{f}
	}
	onStage := outlineOnStageForBooks(outlineRaw)
	var missing []string
	for _, stem := range onStage {
		if IdentityAt(segments, stem, unit.UnitID) == nil {
			missing = append(missing, stem)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []domain.ReviewFinding{{
		ID: "identity-missing-" + unit.UnitID, Severity: "blocking", Source: "local_gate", Lens: "identity",
		Title:    "在场人物缺少生效身份",
		Evidence: unit.UnitID + " 的 " + strings.Join(missing, "、") + " 没有覆盖该单元的身份段",
		Suggestion: "在 canon/identity-timeline.yaml 补登记，或在卷纲里写「### 身份变更」；" +
			"身份只由时间线拥有，不要在 state_deltas 里自行创造",
	}}
}

// identityBefore returns the segment effective immediately before the one that
// applies at unitID — 即本单元的「单元前状态」。没有更早的段时返回 nil。
func identityBefore(segments []domain.IdentitySegment, stem, unitID string) *domain.IdentitySegment {
	cur := IdentityAt(segments, stem, unitID)
	if cur == nil {
		return nil
	}
	curFrom := UnitOrderKey(cur.FromUnit)
	var best *domain.IdentitySegment
	for i := range segments {
		s := &segments[i]
		if s.Stem != stem {
			continue
		}
		if UnitOrderKey(s.FromUnit) >= curFrom {
			continue
		}
		if best == nil || UnitOrderKey(s.FromUnit) >= UnitOrderKey(best.FromUnit) {
			best = s
		}
	}
	return best
}

// prevRoleLabel renders the previous segment's role for error messages.
func prevRoleLabel(segments []domain.IdentitySegment, stem, unitID string) string {
	if prev := identityBefore(segments, stem, unitID); prev != nil {
		return prev.RoleTitle
	}
	return "（无）"
}

// SegmentKey 返回身份段的主键。
//
// 显式写了 id 就用 id；否则用「stem@from_unit」派生——它天然唯一（同一个人在同一
// 单元边界只能有一次身份切换，ApplyIdentityChange 会拒绝重复），所以不需要迁移
// 现有数据就有一份稳定主键。
func SegmentKey(seg domain.IdentitySegment) string {
	if id := strings.TrimSpace(seg.ID); id != "" {
		return id
	}
	if seg.Stem == "" || seg.FromUnit == "" {
		return ""
	}
	return seg.Stem + "@" + seg.FromUnit
}

// IdentityRef 判断 state_deltas 里的 role_title 值是不是一个引用，而非一段文案。
//
// 支持三种写法（新写法优先，旧数据继续可用）：
//
//	@v02-U3                 该单元生效的那一段（最常用：只关心"这时候是谁"）
//	wen-nianci@v02-U3       段主键
//	重案队刑警              角色文案；解析为"该 stem 名下唯一使用此文案的段"
//
// 引用与文案的区别是**判断方式**：引用按主键查找，文案按唯一性反查。
// 反查命中 0 段或命中多段都报错，绝不猜——猜错比报错更难查。
type IdentityRef struct {
	Raw    string
	Unit   string // `@vNN-Un` 形式
	Key    string // 完整主键形式
	IsRef  bool
	IsByID bool
}

func ParseIdentityRef(raw string) IdentityRef {
	v := strings.TrimSpace(raw)
	if v == "" {
		return IdentityRef{}
	}
	if strings.HasPrefix(v, "@") {
		unit := strings.TrimSpace(strings.TrimPrefix(v, "@"))
		return IdentityRef{Raw: v, Unit: unit, IsRef: true, IsByID: true}
	}
	if stem, unit, ok := strings.Cut(v, "@"); ok && strings.TrimSpace(stem) != "" && strings.TrimSpace(unit) != "" {
		return IdentityRef{Raw: v, Key: v, IsRef: true, IsByID: true}
	}
	return IdentityRef{Raw: v}
}

// ResolveIdentitySegment 把一个引用/文案解析成具体身份段。nil 表示解析不到。
//
// 这是「外键查找」的唯一入口：所有拿 role_title 做判断的地方都走这里，
// 而不是各自比字符串。以前三处各写一套比较（细纲接受、定稿校验、审校硬门），
// 措辞一改就三处结论不一致。
func ResolveIdentitySegment(segments []domain.IdentitySegment, stem, raw, atUnit string) *domain.IdentitySegment {
	ref := ParseIdentityRef(raw)
	switch {
	case ref.IsRef && ref.Unit != "":
		// `@单元`：解析成该单元生效的那一段（与传入的 atUnit 无关——这正是
		// 它有用的地方：种子可以点名"转换发生在哪个单元"）。
		return IdentityAt(segments, stem, ref.Unit)
	case ref.IsRef && ref.Key != "":
		for i := range segments {
			if segments[i].Stem == stem && SegmentKey(segments[i]) == ref.Key {
				return &segments[i]
			}
		}
		return nil
	}
	if ref.Raw == "" {
		return nil
	}
	// 文案反查：必须唯一命中，否则视为解析失败（由调用方给出可操作的报错）。
	var hit *domain.IdentitySegment
	for i := range segments {
		s := &segments[i]
		if s.Stem != stem || s.RoleTitle != ref.Raw {
			continue
		}
		if hit != nil {
			return nil // 有重名，无法判断指的是哪一段
		}
		hit = s
	}
	if hit == nil && atUnit != "" {
		// 文案对不上任何段，但恰好等于本单元生效身份时仍按本单元处理：
		// 旧数据里大量 from/to 写的是"当前身份"，不必强制迁移。
		if cur := IdentityAt(segments, stem, atUnit); cur != nil && cur.RoleTitle == ref.Raw {
			return cur
		}
	}
	return hit
}

// unitStartingRole 返回「从哪个单元开始生效」的单元 ID，用于错误信息指出正确归口。
func unitStartingRole(segments []domain.IdentitySegment, stem, roleTitle string) string {
	if seg := ResolveIdentitySegment(segments, stem, roleTitle, ""); seg != nil {
		return seg.FromUnit
	}
	return ""
}

// ValidateSeedIdentityChanges 校验卷纲单元种子的 state_changes 里身份条目与身份时间线一致。
//
// 为什么必须在卷纲阶段查：细纲的身份声明要同时满足两条规则——
//
//	A. 必须逐字继承卷纲 state_changes 的父级迁移（ValidateStateDeltaInheritance）
//	B. 必须等于本单元生效身份（stateDeltaIdentityConflict）
//
// 当种子写的是「本单元内并不会发生的身份转换」时，A 和 B 互相排斥：无论模型
// 怎么写都会被拒。实测 mindvoice-cop 的 v02-U2 种子写着
// 「重案队试用刑警->重案队刑警」，而时间线里「重案队刑警」从 v02-U3 才生效，
// 细纲连续 3 次撞死、烧掉 6 次调用，最终整单元被跳过、下一个单元在断链状态开写。
//
// 判据是「解析出的身份段是否就是本单元的那一段」——按主键/引用判断，不比文案。
func ValidateSeedIdentityChanges(segments []domain.IdentitySegment, unitID, stateChanges string) error {
	for _, row := range stateDeltasFromMarkdown(stateChanges) {
		if !domain.OwnedByIdentityTimeline(row["field"]) {
			continue
		}
		stem, from, to := row["stem"], row["from"], row["to"]
		cur := IdentityAt(segments, stem, unitID)
		if cur == nil {
			return fmt.Errorf("%s：%s 的 role_title=%s->%s 找不到生效身份段（身份时间线没有覆盖本单元的段）",
				unitID, stem, from, to)
		}
		// to 必须指向「本单元这一段」。用引用解析而不是字面量比较：
		// 作者改措辞、模型换同义词都不该让规划失效。
		target := ResolveIdentitySegment(segments, stem, to, unitID)
		if target == nil || SegmentKey(*target) != SegmentKey(*cur) {
			hint := ""
			if at := unitStartingRole(segments, stem, to); at != "" && at != unitID {
				hint = fmt.Sprintf("；「%s」从 %s 起生效，这条迁移应写在该单元的种子里", targetRoleLabel(target, to), at)
			}
			return fmt.Errorf("%s：%s 的 role_title to=%s 不是本单元生效段 %s。本单元内身份恒定，身份转换只能写在转换生效的那个单元%s",
				unitID, stem, to, SegmentKey(*cur), hint)
		}
		// from 是「单元前状态」：等于本单元身份（单元内不变）或上一段身份（边界转换）。
		if strings.TrimSpace(from) == "" {
			continue
		}
		if src := ResolveIdentitySegment(segments, stem, from, unitID); src != nil {
			prev := identityBefore(segments, stem, unitID)
			sameAsCur := SegmentKey(*src) == SegmentKey(*cur)
			sameAsPrev := prev != nil && SegmentKey(*src) == SegmentKey(*prev)
			if sameAsCur || sameAsPrev {
				continue
			}
		}
		return fmt.Errorf("%s：%s 的 role_title from=%s 既不指向上一段 %s，也不指向本单元 %s",
			unitID, stem, from, prevRoleLabel(segments, stem, unitID), cur.RoleTitle)
	}
	return nil
}

// targetRoleLabel 给错误信息用：引用显示为解析后的文案，文案原样显示。
func targetRoleLabel(seg *domain.IdentitySegment, raw string) string {
	if seg != nil {
		return seg.RoleTitle
	}
	return raw
}

// ApplyVolumeIdentityChanges 应用卷纲文档里声明的全部「### 身份变更」，
// 返回「本卷落盘后」的身份时间线副本。
//
// 抽成函数是因为必须被两处共用：卷纲生成时的合同校验与接受时的落盘校验。
// 各写一份迟早会分叉——分叉的表现就是「生成通过、接受失败」，而作者已经
// 为那次生成付过钱了。
func ApplyVolumeIdentityChanges(segments []domain.IdentitySegment, doc domain.StructuredMarkdown, units []domain.UnitInfo) ([]domain.IdentitySegment, error) {
	out := append([]domain.IdentitySegment(nil), segments...)
	for _, e := range doc.EntitiesOf("身份变更") {
		var err error
		out, err = ApplyIdentityChange(out, domain.IdentitySegment{
			Stem:      e.Value("stem"),
			RoleTitle: e.Value("role_title"),
			Org:       e.Value("org"),
			FromUnit:  e.Value("from_unit"),
			Note:      e.Value("reason"),
		}, units)
		if err != nil {
			return nil, fmt.Errorf("身份变更[%s]：%w", e.ID, err)
		}
	}
	return out, nil
}

// ValidateVolumeSeedIdentities 对整份卷纲的单元种子做身份一致性校验。
func ValidateVolumeSeedIdentities(segments []domain.IdentitySegment, doc domain.StructuredMarkdown, units []domain.UnitInfo) error {
	planned, err := ApplyVolumeIdentityChanges(segments, doc, units)
	if err != nil {
		return err
	}
	for _, e := range doc.EntitiesOf("单元") {
		if err := ValidateSeedIdentityChanges(planned, e.ID, e.Value("state_changes")); err != nil {
			return err
		}
	}
	return nil
}

func OnStageFromOutline(raw string) []string {
	return outlineOnStageForBooks(raw)
}

func outlineOnStageForBooks(raw string) []string {
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "on_stage:") {
			continue
		}
		value := strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "on_stage:")), "[]")
		var out []string
		for _, stem := range strings.Split(value, ",") {
			if stem = strings.Trim(strings.TrimSpace(stem), `"'`); stem != "" {
				out = append(out, stem)
			}
		}
		return out
	}
	return nil
}

// 以前这里有一个 ValidateNoIntraUnitChange：扫全文找「晋升」「调任」「身份转换」
// 等 marker，命中就判单元内部发生了身份转换。已删除，理由三条：
//
//  1. 它是按关键词猜语义，会误杀合法内容。实测 gaojishu-zhuixiong/v01-U13 的
//     state_deltas 写着「不推晋升、不给借调」，rule-loophole/v01-U9 的 function
//     写着「每晋升一层淘汰率 50%」，都会被判成身份转换。
//  2. 它检查的是「有没有这个词」，而不是「身份到底变没变」，与真正的规则
//     （单元内部身份恒定）不是同一件事。
//  3. 精确版本已经存在：stateDeltaIdentityConflict 按 state_deltas 的
//     role_title/identity 行逐条比对身份段，并允许 from = 上一段身份这种
//     单元边界转换的合法写法。同一条规则两份实现，只会分叉。
