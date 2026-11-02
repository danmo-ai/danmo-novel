package books

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 重新立项不得把已有身份历史抹回起点。
// 回归：以前 acceptPlanBook 无条件覆写 identity-timeline.yaml，一次 reinit
// 就把温念慈从"专案组刑警"退回"内勤新警"，10 段历史全部丢失。
func TestAcceptPlanReinitKeepsExistingIdentityHistory(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "reinit-identity", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	existing := []domain.IdentitySegment{
		{Stem: "lin", RoleTitle: "内勤新警", Org: "青江分局", FromUnit: "v01-U1", ToUnit: "v01-U1"},
		{Stem: "lin", RoleTitle: "刑侦支队刑警", Org: "江州市公安局刑侦支队", FromUnit: "v02-U1"},
	}
	if err := SaveIdentityTimeline(root, existing); err != nil {
		t.Fatal(err)
	}

	cast := `[{"stem":"lin","name":"林晚","role":"protagonist","cardMarkdown":"# 林晚\n","initialIdentity":{"roleTitle":"内勤新警","org":"青江分局","fromUnit":"v01-U1"}},` +
		`{"stem":"shen","name":"沈岸","role":"recurring","cardMarkdown":"# 沈岸\n","initialIdentity":{"roleTitle":"法医","org":"市局法医室","fromUnit":"v01-U1"}}]`
	cand := domain.Candidate{
		Mode:         domain.PlanModeReinit,
		PlanMarkdown: map[string]string{"book_outline": sampleBookOutline(), "cast_json": cast},
		PlanSettings: sampleSettings(),
	}
	if err := AcceptPlan(root, cand); err != nil {
		t.Fatal(err)
	}

	got, err := LoadIdentityTimeline(root)
	if err != nil {
		t.Fatal(err)
	}
	// lin 的两段历史必须原样保留。
	var linSegments []domain.IdentitySegment
	for _, s := range got {
		if s.Stem == "lin" {
			linSegments = append(linSegments, s)
		}
	}
	if len(linSegments) != 2 {
		t.Fatalf("lin 历史被改写: %+v", linSegments)
	}
	if linSegments[1].RoleTitle != "刑侦支队刑警" || linSegments[1].FromUnit != "v02-U1" {
		t.Fatalf("lin 晋升段丢失: %+v", linSegments[1])
	}
	// 新人物必须补登初始身份。
	var shen *domain.IdentitySegment
	for i := range got {
		if got[i].Stem == "shen" {
			shen = &got[i]
		}
	}
	if shen == nil || shen.RoleTitle != "法医" {
		t.Fatalf("新人物缺少初始身份: %+v", got)
	}
}

// 首次立项（没有既有时间线）仍然写全量初始身份。
func TestAcceptPlanInitWritesInitialIdentities(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "init-identity", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	cast := `[{"stem":"lin","name":"林晚","role":"protagonist","cardMarkdown":"# 林晚\n","initialIdentity":{"roleTitle":"见习调查员","org":"青江分局","fromUnit":"v01-U1"}}]`
	cand := domain.Candidate{
		Mode:         domain.PlanModeInit,
		PlanMarkdown: map[string]string{"book_outline": sampleBookOutline(), "cast_json": cast},
		PlanSettings: sampleSettings(),
	}
	if err := AcceptPlan(root, cand); err != nil {
		t.Fatal(err)
	}
	got, err := LoadIdentityTimeline(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Stem != "lin" || got[0].RoleTitle != "见习调查员" {
		t.Fatalf("初始身份未写入: %+v", got)
	}
	if got[0].FromUnit != "v01-U1" {
		t.Fatalf("from_unit 应回落到 v01-U1: %+v", got[0])
	}
}

// 显式保留时完全不碰该文件。
func TestAcceptPlanReinitPreservedIdentityTimelineIsUntouched(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "preserve-identity", "测试", "悬疑")
	if err != nil {
		t.Fatal(err)
	}
	existing := []domain.IdentitySegment{
		{Stem: "lin", RoleTitle: "内勤新警", Org: "青江分局", FromUnit: "v01-U1"},
	}
	if err := SaveIdentityTimeline(root, existing); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(IdentityTimelinePath)))
	if err != nil {
		t.Fatal(err)
	}

	cast := `[{"stem":"shen","name":"沈岸","role":"protagonist","cardMarkdown":"# 沈岸\n","initialIdentity":{"roleTitle":"法医","org":"市局法医室","fromUnit":"v01-U1"}}]`
	cand := domain.Candidate{
		Mode:          domain.PlanModeReinit,
		PlanMarkdown:  map[string]string{"book_outline": sampleBookOutline(), "cast_json": cast},
		PlanSettings:  sampleSettings(),
		PreservePaths: []string{IdentityTimelinePath},
	}
	if err := AcceptPlan(root, cand); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(IdentityTimelinePath)))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("preservePaths 未生效:\nbefore=%s\nafter=%s", before, after)
	}
}
