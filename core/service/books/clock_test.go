package books

import (
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func TestValidateClockContinuity(t *testing.T) {
	mk := func(id string, vol, idx int, day, flashback string) domain.UnitInfo {
		raw := "unit_id: " + id + "\n"
		if day != "" {
			raw += "story_day: " + day + "\n"
		}
		if flashback != "" {
			raw += "flashback: " + flashback + "\n"
		}
		return domain.UnitInfo{UnitID: id, Volume: vol, Index: idx, OutlineRaw: raw}
	}
	units := []domain.UnitInfo{
		mk("v01-U1", 1, 1, "3", ""),
		mk("v01-U2", 1, 2, "5", ""),
		mk("v01-U3", 1, 3, "9", ""),
	}
	// 正常递增：无 blocking，可有缺失提示
	f := ValidateClockContinuity(units, 1)
	for _, x := range f {
		if x.Severity == "blocking" {
			t.Fatalf("unexpected blocking: %+v", x)
		}
	}

	// 故事日倒退（非闪回）→ blocking
	bad := append([]domain.UnitInfo{}, units...)
	bad = append(bad, mk("v01-U4", 1, 4, "2", ""))
	found := false
	for _, x := range ValidateClockContinuity(bad, 1) {
		if x.Severity == "blocking" && x.Lens == "clock" {
			found = true
		}
	}
	if !found {
		t.Fatal("regressing story_day should be blocking")
	}

	// 倒退但标了闪回 → 不 blocking
	fb := append([]domain.UnitInfo{}, units...)
	fb = append(fb, mk("v01-U4", 1, 4, "2", "true"))
	for _, x := range ValidateClockContinuity(fb, 1) {
		if x.Severity == "blocking" {
			t.Fatalf("flashback regress should pass: %+v", x)
		}
	}

	// 缺 story_day → advisory 提示回填
	missing := []domain.UnitInfo{mk("v01-U1", 1, 1, "", "")}
	hasAdvisory := false
	for _, x := range ValidateClockContinuity(missing, 1) {
		if x.Severity == "advisory" {
			hasAdvisory = true
		}
	}
	if !hasAdvisory {
		t.Fatal("missing story_day should be advisory")
	}
}
