package books

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func TestIdentityEditorRejectsRetconOfWrittenUnits(t *testing.T) {
	segments := []domain.IdentitySegment{{Stem: "lin", RoleTitle: "内勤", Org: "派出所", FromUnit: "v01-U1"}}
	units := []domain.UnitInfo{{UnitID: "v01-U1", HasProse: true}}
	_, err := UpdateIdentitySegment(segments, 0, domain.IdentitySegment{Stem: "lin", RoleTitle: "专家", Org: "省厅", FromUnit: "v01-U1"}, units)
	if err == nil || !strings.Contains(err.Error(), "已有正文") {
		t.Fatalf("written identity must require explicit impact handling: %v", err)
	}
}

func TestIdentityEditorAllowsInitialAndFutureChanges(t *testing.T) {
	segments := []domain.IdentitySegment{{Stem: "lin", RoleTitle: "内勤", Org: "派出所", FromUnit: "v01-U1"}}
	units := []domain.UnitInfo{{UnitID: "v01-U1"}, {UnitID: "v01-U2"}}
	updated, err := UpdateIdentitySegment(segments, 0, domain.IdentitySegment{Stem: "lin", RoleTitle: "内勤新警", Org: "双桥派出所", FromUnit: "v01-U1"}, units)
	if err != nil || updated[0].RoleTitle != "内勤新警" {
		t.Fatalf("safe correction rejected: %+v %v", updated, err)
	}
	closed, err := CloseIdentitySegment(updated, 0, "v01-U1", units)
	if err != nil || closed[0].ToUnit != "v01-U1" {
		t.Fatalf("close failed: %+v %v", closed, err)
	}
}

func TestIdentityEditorProtectsOpenFuture(t *testing.T) {
	segments := []domain.IdentitySegment{{Stem: "lin", RoleTitle: "内勤", Org: "派出所", FromUnit: "v01-U1"}}
	units := []domain.UnitInfo{{UnitID: "v01-U1"}, {UnitID: "v01-U2", HasProse: true}}
	if _, err := CloseIdentitySegment(segments, 0, "v01-U1", units); err == nil {
		t.Fatal("closing an identity used by later prose must be rejected")
	}
}
