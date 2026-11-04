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
		if delta.Field != "role_title" && delta.Field != "identity" && delta.Field != "title" {
			continue
		}
		identity := IdentityAt(segments, delta.Stem, unit.UnitID)
		if identity == nil {
			return identityFinding(unit, delta, "", "缺少本单元生效身份"), fmt.Errorf("%s：%s 缺少本单元生效身份", unit.UnitID, delta.Stem)
		}
		// to 必须是本单元生效身份：定稿只确认时间线，不允许它顺手改写身份。
		if delta.To != identity.RoleTitle {
			return identityFinding(unit, delta, identity.RoleTitle,
					fmt.Sprintf("state_deltas 的 to=%s 与本单元生效段 %s 不一致；请先调整规划，不允许落盘自动改身份", delta.To, identity.RoleTitle)),
				fmt.Errorf("%s：%s 的身份 %s 与本单元生效段 %s 不一致；请先调整规划，不允许定稿自动改身份", unit.UnitID, delta.Stem, delta.To, identity.RoleTitle)
		}
		// from 是「单元前状态」，可以是本单元身份（单元内不变），也可以是上一段
		// 身份（边界转换）。两者都不匹配才说明 state_deltas 写错了。
		if delta.From == "" || delta.From == identity.RoleTitle {
			continue
		}
		prev := prevRoleLabel(segments, delta.Stem, unit.UnitID)
		if prev != "（无）" && prev == delta.From {
			continue
		}
		msg := fmt.Sprintf("state_deltas 的 from=%s 既不等于上一段 %s，也不等于本单元 %s；请修正 state_deltas 或身份时间线",
			delta.From, prev, identity.RoleTitle)
		return identityFinding(unit, delta, identity.RoleTitle, msg),
			fmt.Errorf("%s：%s 的单元前身份 %s 既不等于上一段 %s，也不等于本单元 %s；请修正 state_deltas 或身份时间线",
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
