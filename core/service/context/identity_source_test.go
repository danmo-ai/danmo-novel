package contextsvc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

// 身份只由 canon/identity-timeline.yaml 拥有：账本不再存副本，渲染时现算。
//
// 曾经账本也存一份 role_title，实测 7 段里只有 1 段有值，且人工修正时间线后
// 副本不跟着变——同一份写作提示词里同一个人出现两个身份。
func TestIdentityComesFromTimelineNotLedger(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, books.StatePath, `version: 1
units:
    - unit: v01-U1
      volume: 1
      after_ch: 3
      cast:
        lin:
            goal: 争取入组
`)
	writeFile(t, root, books.IdentityTimelinePath, `version: 1
segments:
    - stem: lin
      role_title: 刑侦线新警
      org: 青江分局
      from_unit: v01-U1
      to_unit: v01-U1
    - stem: lin
      role_title: 重案队刑警
      org: 青江分局
      from_unit: v01-U2
`)
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "b", RootPath: root},
		IdentitySegments: []domain.IdentitySegment{
			{Stem: "lin", RoleTitle: "刑侦线新警", Org: "青江分局", FromUnit: "v01-U1", ToUnit: "v01-U1"},
			{Stem: "lin", RoleTitle: "重案队刑警", Org: "青江分局", FromUnit: "v01-U2"},
		},
	}

	// U2 的身份是时间线上从 U2 起的那一段——与人物卡上的「本单元身份」同源。
	got := ExtractContinuity(snap, domain.UnitInfo{UnitID: "v01-U2"}, []string{"lin"}, ContinuityForWrite)
	card := ExtractCastForWriteWithIdentity(domain.CastCard{Stem: "lin", Name: "林", Raw: "# 林\n"}, "lin", identityLabel(snap.IdentitySegments, "lin", "v01-U2"))
	if !strings.Contains(card, "重案队刑警") || strings.Contains(got, "身份=") {
		t.Fatalf("身份只能在人物块出现一次：card=%s ledger=%s", card, got)
	}
	// 状态仍来自账本。
	if !strings.Contains(got, "争取入组") {
		t.Fatalf("账本状态丢失：\n%s", got)
	}
}

// 账本里若残留 role_title（旧数据），迁移必须清掉它，并且清掉因此变空的条目。
func TestLedgerRoleTitleIsDroppedOnNormalize(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, books.StatePath, `version: 1
units:
    - unit: v01-U1
      cast:
        lin:
            role_title: 旧身份
        you:
            role_title: 只有身份
            goal: 还在
`)
	rep, err := books.NormalizeBookData(root)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.StateRepaired {
		t.Fatal("ledger should be repaired")
	}
	raw, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(books.StatePath)))
	got := string(raw)
	if strings.Contains(got, "role_title") {
		t.Fatalf("role_title 必须清掉：\n%s", got)
	}
	if strings.Contains(got, "lin:") {
		t.Fatalf("只有身份的人应被整条删除，而不是留空映射：\n%s", got)
	}
	if !strings.Contains(got, "you:") || !strings.Contains(got, "还在") {
		t.Fatalf("其他人的状态不该被误删：\n%s", got)
	}
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
