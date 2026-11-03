package domain

import "testing"

// 合同值 2500 必须接受 1800–3500：下限是发布端退回线，不随目标比例缩水；
// 上限是全书单章硬顶，不再按 1.4× 目标放到 4200。
func TestChapterBandClampsToPublishableRange(t *testing.T) {
	cases := []struct {
		target int
		floor  int
		ceil   int
	}{
		{target: 2500, floor: 1800, ceil: 3500},
		{target: 3000, floor: 1800, ceil: 3500},
		{target: 3500, floor: 2100, ceil: 3500},
		{target: ChapterCharsMin, floor: 1800, ceil: 2800},
		{target: 0, floor: 1800, ceil: 3500}, // 0 落到默认 2500
	}
	for _, c := range cases {
		floor, ceil := ChapterBand(c.target)
		if floor != c.floor || ceil != c.ceil {
			t.Fatalf("ChapterBand(%d) = %d–%d, want %d–%d", c.target, floor, ceil, c.floor, c.ceil)
		}
	}
}
