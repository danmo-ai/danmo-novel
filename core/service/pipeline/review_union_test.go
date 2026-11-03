package pipeline

import (
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

const unionBody = "他推开门。\n\n" +
	"车斗空着，两个人正从车上往下搬纸箱。\n\n" +
	"他把手按在门框上。\n\n" +
	"周卫国看了她两秒。\n\n" +
	"门外没有动静。"

func finding(id, sev, anchor, evidence string) domain.ReviewFinding {
	return domain.ReviewFinding{
		ID: id, Severity: sev, Chapter: 6, Source: "ai_review",
		Title: "t-" + id, Evidence: evidence, Anchor: anchor,
	}
}

// 两轮落到同一处区间 → 合并成一条，severity 取最严，命中次数累加。
//
// 这是并集的核心：同一缺陷两轮措辞可能毫无字面重合，只有区间能判定是同一处。
func TestUnionFindingsMergesSameSpan(t *testing.T) {
	r1 := []domain.ReviewFinding{
		finding("a-1", "advisory", "车斗空着，两个人正从车上往下搬纸箱。", "车斗空着却在卸纸箱"),
	}
	r2 := []domain.ReviewFinding{
		finding("b-1", "blocking", "车斗空着，两个人正从车上往下搬纸箱。", "车斗是空的，不该在卸货"),
	}
	got := unionFindings([][]domain.ReviewFinding{r1, r2}, unionBody)
	if len(got) != 1 {
		t.Fatalf("merged=%d want 1: %+v", len(got), got)
	}
	// 三轮里只要一轮判 blocking 就按 blocking：漏掉硬伤的代价更大。
	if got[0].Severity != "blocking" {
		t.Fatalf("severity=%q want blocking (strictest wins)", got[0].Severity)
	}
	if got[0].RoundsHit != 2 {
		t.Fatalf("roundsHit=%d want 2", got[0].RoundsHit)
	}
}

// 不同位置的锚点不能被合并——误合并比漏合并严重得多。
func TestUnionFindingsKeepsDistinctSpans(t *testing.T) {
	r1 := []domain.ReviewFinding{finding("a-1", "advisory", "他推开门。", "开头太直接")}
	r2 := []domain.ReviewFinding{finding("b-1", "advisory", "门外没有动静。", "结尾缺钩子")}
	got := unionFindings([][]domain.ReviewFinding{r1, r2}, unionBody)
	if len(got) != 2 {
		t.Fatalf("merged=%d want 2 (different spans)", len(got))
	}
}

// 没有锚点（合同/时钟类矛盾在正文里没有对应片段）时不得误合并，
// 但同一章、同一 evidence 的两条应视为同一条被两轮各说一遍。
func TestUnionFindingsFallsBackToEvidenceKey(t *testing.T) {
	r1 := []domain.ReviewFinding{finding("a-1", "advisory", "", "合同 story_day=5，正文写第七天")}
	r2 := []domain.ReviewFinding{finding("b-1", "advisory", "", "合同 story_day=5，正文写第七天")}
	got := unionFindings([][]domain.ReviewFinding{r1, r2}, unionBody)
	if len(got) != 1 {
		t.Fatalf("merged=%d want 1 (same evidence key)", len(got))
	}
	if got[0].RoundsHit != 2 {
		t.Fatalf("roundsHit=%d want 2", got[0].RoundsHit)
	}

	// 不同章的相同 evidence 不能合并：那是两章各自的同类问题。
	r3 := []domain.ReviewFinding{finding("c-1", "advisory", "", "合同 story_day=5，正文写第七天")}
	r3[0].Chapter = 7
	got = unionFindings([][]domain.ReviewFinding{r1, r3}, unionBody)
	if len(got) != 2 {
		t.Fatalf("merged=%d want 2 (different chapters)", len(got))
	}
}

// 排序必须把复现次数多的排前面：3 轮并集的条数约为单轮 2–3 倍，
// 不排序作者无法判断哪些值得修。
func TestRankFindingsPutsConsensusFirst(t *testing.T) {
	got := rankFindings([]domain.ReviewFinding{
		{ID: "x", Severity: "blocking", Chapter: 6, RoundsHit: 1},
		{ID: "y", Severity: "advisory", Chapter: 6, RoundsHit: 3},
		{ID: "z", Severity: "blocking", Chapter: 5, RoundsHit: 2},
	})
	order := []string{got[0].ID, got[1].ID, got[2].ID}
	want := []string{"y", "z", "x"} // 3 次 > 2 次 > 1 次
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order=%v want %v", order, want)
		}
	}
}

// 单轮 blocking 不阻断：实测同一章两次调用给出的 blocking 完全不同，
// 单轮即可阻断会让清单永远关不掉。两轮以上才阻断。
func TestReviewVerdictRequiresConsensus(t *testing.T) {
	single := []domain.ReviewFinding{{ID: "x", Severity: "blocking", RoundsHit: 1}}
	if v := domain.ReviewVerdict(single); v == domain.ReviewBlocking {
		t.Fatal("a single-round blocking must not block the flow")
	}
	two := []domain.ReviewFinding{{ID: "x", Severity: "blocking", RoundsHit: 2}}
	if v := domain.ReviewVerdict(two); v != domain.ReviewBlocking {
		t.Fatalf("verdict=%s want blocking at roundsHit=2", v)
	}
	// 旧数据/本地硬门没有 RoundsHit（0）：保持原语义，避免历史清单突然放行。
	legacy := []domain.ReviewFinding{{ID: "x", Severity: "blocking"}}
	if v := domain.ReviewVerdict(legacy); v != domain.ReviewBlocking {
		t.Fatalf("verdict=%s want blocking for legacy findings", v)
	}
}
