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
	for stem, rows := range byStem {
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

// ValidateFinalizedIdentity compares actual state deltas with the identity
// effective at this unit. A finalizer may confirm, never invent, a new segment.
func ValidateFinalizedIdentity(segments []domain.IdentitySegment, cast []domain.CastCard, unit domain.UnitInfo, outlineRaw string) error {
	if err := ValidateIdentityCoverage(segments, cast, unit, outlineOnStageForBooks(outlineRaw)); err != nil {
		return err
	}
	for _, delta := range parseStateDeltas(outlineRaw) {
		if delta.Field != "role_title" && delta.Field != "identity" && delta.Field != "title" {
			continue
		}
		identity := IdentityAt(segments, delta.Stem, unit.UnitID)
		if identity == nil {
			return fmt.Errorf("%s：%s 缺少本单元生效身份", unit.UnitID, delta.Stem)
		}
		// A role change completed at the end of a unit is a next-boundary
		// transition. The next unit's planning Accept registers it; this unit
		// must still be evaluated against its opening identity.
		if delta.From != "" && delta.From != identity.RoleTitle {
			return fmt.Errorf("%s：%s 的开场身份 %s 与时间线 %s 不一致", unit.UnitID, delta.Stem, delta.From, identity.RoleTitle)
		}
		if delta.To != identity.RoleTitle {
			return fmt.Errorf("%s：%s 的身份 %s 与本单元生效段 %s 不一致；请先调整规划，不允许定稿自动改身份", unit.UnitID, delta.Stem, delta.To, identity.RoleTitle)
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

// ValidateNoIntraUnitChange enforces 单元内部身份恒定：the unit's own
// state_deltas must not declare an identity/role change for on_stage stems.
// Such a change means the unit must be split at the boundary.
func ValidateNoIntraUnitChange(outlineRaw string, onStage []string) (blocking []string) {
	deltas := strings.ToLower(outlineRaw)
	markers := []string{"identity_change", "role_change", "职务转换", "身份转换", "调任", "晋升", "降职"}
	for _, m := range markers {
		if strings.Contains(deltas, m) {
			blocking = append(blocking, "单元内部不得职务/身份转换；请把转换点拆到单元边界（下一单元开场），并登记 identity-timeline")
			break
		}
	}
	return blocking
}
