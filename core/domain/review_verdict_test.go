package domain

import "testing"

// 「0 条 blocking」不能单独作为放行条件：截断/分章失败的标记是 advisory，
// 但它意味着这一章没审完，必须判成 unfinished。
func TestReviewVerdict(t *testing.T) {
	cases := []struct {
		name     string
		findings []ReviewFinding
		want     string
	}{
		{"empty", nil, ReviewClean},
		{"advisory only", []ReviewFinding{{ID: "a1", Severity: "advisory"}}, ReviewClean},
		{"blocking", []ReviewFinding{{ID: "b1", Severity: "blocking"}}, ReviewBlocking},
		{"truncated", []ReviewFinding{{ID: FindingReviewTruncated, Severity: "advisory"}}, ReviewUnfinished},
		{"incomplete", []ReviewFinding{{ID: FindingReviewIncomplete, Severity: "advisory"}}, ReviewUnfinished},
		// blocking 优先：已知硬伤是可执行的，缺口只是缺口。
		{"blocking beats truncated", []ReviewFinding{
			{ID: FindingReviewTruncated, Severity: "advisory"},
			{ID: "b1", Severity: "blocking"},
		}, ReviewBlocking},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReviewVerdict(tc.findings); got != tc.want {
				t.Fatalf("ReviewVerdict=%q want %q", got, tc.want)
			}
		})
	}
}
