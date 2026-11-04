package books

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 细纲接受后不得丢掉卷纲种子独占的合同字段。
// 真实故障：全库 772 个单元文件里，proposed 有 163 个带 entry_cause，
// 而 accepted/drafted/reviewed 共 119 个一个都没有——accept 把它们全抹了。
func TestCarrySeedOwnedFieldsFillsMissingContract(t *testing.T) {
	snap, _ := volumeInheritanceFixture("目标")
	raw := `unit_id: v01-U1
status: accepted
title_working: "起步"
desire: "获得行动资格"
chapter_range: [1, 6]
`

	got := CarrySeedOwnedFields(snap, 1, "v01-U1", raw)
	for _, want := range []string{
		`entry_cause: "v01-U1"`,
		`exit_result: "获得许可"`,
		`state_changes: "lin.role=新人->复核人"`,
		`thread_ids: [main]`,
		`event_ids: [event-1]`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	// 原有内容必须一字不动。
	if !strings.HasPrefix(got, strings.TrimRight(raw, "\n")) {
		t.Fatalf("existing bytes changed:\n%s", got)
	}
}

// 作者手改过的口径优先：文件里已有的键不被种子覆盖。
func TestCarrySeedOwnedFieldsKeepsAuthorOverride(t *testing.T) {
	snap, _ := volumeInheritanceFixture("目标")
	raw := "unit_id: v01-U1\nthread_ids: [custom]\n"
	got := CarrySeedOwnedFields(snap, 1, "v01-U1", raw)
	if !strings.Contains(got, "thread_ids: [custom]") {
		t.Fatalf("author override lost: %s", got)
	}
	if n := countTopLevelKey(got, "thread_ids"); n != 1 {
		t.Fatalf("thread_ids written %d times: %s", n, got)
	}
}

func countTopLevelKey(raw, key string) int {
	n := 0
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, key+":") {
			n++
		}
	}
	return n
}

// 找不到卷纲/种子时不做任何改写，避免凭空造字段。
func TestCarrySeedOwnedFieldsNoSeedNoop(t *testing.T) {
	snap := domain.BookSnapshot{Files: map[string]string{}}
	raw := "unit_id: v99-U1\n"
	if got := CarrySeedOwnedFields(snap, 99, "v99-U1", raw); got != raw {
		t.Fatalf("must be a no-op, got:\n%s", got)
	}
}
