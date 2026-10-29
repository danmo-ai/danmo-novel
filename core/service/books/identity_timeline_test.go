package books

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func TestIdentityAt(t *testing.T) {
	segments := []domain.IdentitySegment{
		{Stem: "wen-nianci", RoleTitle: "内勤新警", FromUnit: "v01-U1", ToUnit: "v01-U7"},
		{Stem: "wen-nianci", RoleTitle: "重案队试用", FromUnit: "v01-U8", ToUnit: "v02-U2"},
		{Stem: "wen-nianci", RoleTitle: "特聘专家", FromUnit: "v02-U3"},
	}
	cases := []struct{ unit, want string }{
		{"v01-U1", "内勤新警"},
		{"v01-U5", "内勤新警"},
		{"v01-U8", "重案队试用"},
		{"v02-U1", "重案队试用"},
		{"v02-U3", "特聘专家"},
		{"v03-U1", "特聘专家"}, // open-ended
	}
	for _, c := range cases {
		got := IdentityAt(segments, "wen-nianci", c.unit)
		if got == nil || got.RoleTitle != c.want {
			t.Fatalf("IdentityAt(%s) = %v, want %s", c.unit, got, c.want)
		}
	}
	if got := IdentityAt(segments, "unknown", "v01-U1"); got != nil {
		t.Fatalf("unknown stem should be nil, got %v", got)
	}
}

func TestLoadIdentityTimeline(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "canon"), 0o755)
	yaml := `segments:
  - stem: wen-nianci
    role_title: 内勤新警
    org: 双桥派出所
    from_unit: v01-U1
    to_unit: v01-U7
  - stem: wen-nianci
    role_title: 重案队试用
    org: 市局刑侦支队
    from_unit: v01-U8
`
	_ = os.WriteFile(filepath.Join(root, "canon", "identity-timeline.yaml"), []byte(yaml), 0o644)
	segs, err := LoadIdentityTimeline(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 {
		t.Fatalf("segments=%d", len(segs))
	}
	got := IdentityAt(segs, "wen-nianci", "v01-U8")
	if got == nil || got.RoleTitle != "重案队试用" || got.Org != "市局刑侦支队" {
		t.Fatalf("IdentityAt = %+v", got)
	}
}

func TestValidateNoIntraUnitChange(t *testing.T) {
	bad := "state_deltas:\n  wen-nianci: 晋升为特聘专家\n"
	if blocking := ValidateNoIntraUnitChange(bad, []string{"wen-nianci"}); len(blocking) == 0 {
		t.Fatal("intra-unit identity change should be blocked")
	}
	good := "state_deltas:\n  wen-nianci position: 市局\n"
	if blocking := ValidateNoIntraUnitChange(good, []string{"wen-nianci"}); len(blocking) != 0 {
		t.Fatalf("non-identity delta should pass: %v", blocking)
	}
}

func TestValidateUnitIdentityOverlap(t *testing.T) {
	segments := []domain.IdentitySegment{
		{Stem: "wen-nianci", RoleTitle: "A", FromUnit: "v01-U1", ToUnit: "v01-U5"},
		{Stem: "wen-nianci", RoleTitle: "B", FromUnit: "v01-U4"}, // 与 A 重叠
	}
	blocking, _ := ValidateUnitIdentity(segments, []string{"wen-nianci"}, "v01-U4")
	if len(blocking) == 0 {
		t.Fatal("overlapping segments should be blocking")
	}
}
