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
	Segments []domain.IdentitySegment `yaml:"segments"`
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

var _ = strings.TrimSpace
