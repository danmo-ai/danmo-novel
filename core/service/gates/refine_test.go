package gates

import (
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

const refineOld = "林尘站在试炼场尽头，雨水顺着刀柄流下来。他没有回头，只看那道缓缓打开的石门。门里传来钟声，像有人在数他的呼吸。他知道这一步迈进去就再没有退路。"

func TestRefineSimilarityFlagsCopy(t *testing.T) {
	original := []domain.ChapterDraft{{Chapter: 1, Body: refineOld}}
	revised := []domain.ChapterDraft{{Chapter: 1, Body: refineOld}}
	f := RefineSimilarityFindings(original, revised)
	if len(f) != 1 {
		t.Fatalf("findings=%d want 1", len(f))
	}
	if f[0].Severity != "advisory" || f[0].Chapter != 1 {
		t.Fatalf("unexpected finding: %+v", f[0])
	}
	if !strings.Contains(f[0].Title, "同义词") {
		t.Fatalf("title should call out the copy: %s", f[0].Title)
	}
}

func TestRefineSimilarityFlagsFullRewrite(t *testing.T) {
	original := []domain.ChapterDraft{{Chapter: 1, Body: refineOld}}
	revised := []domain.ChapterDraft{{Chapter: 1, Body: "沈砚在集市上数最后一枚铜钱，摊主收起伞，天色忽然暗了下来。她把钱推回去，说今日不买了，转身走进人潮。街角的灯一盏盏亮起来，照见她袖口的补丁。"}}
	f := RefineSimilarityFindings(original, revised)
	if len(f) != 1 {
		t.Fatalf("findings=%d want 1", len(f))
	}
	if !strings.Contains(f[0].Title, "更像重写") {
		t.Fatalf("title should flag the rewrite: %s", f[0].Title)
	}
}

func TestRefineSimilarityQuietInTheMiddle(t *testing.T) {
	// 保留开头与结尾，改写中段：这才是"在原文上优化"的典型幅度。
	original := []domain.ChapterDraft{{Chapter: 1, Body: refineOld + "他把刀鞘按回腰间，抬脚跨过门槛。门内没有灯，只有一股很淡的香灰味。远处有人叫他的名字，声音在水汽里散开。"}}
	revised := []domain.ChapterDraft{{Chapter: 1, Body: "林尘站在试炼场尽头，雨水顺着刀柄流下来。他没有回头，只看那道缓缓打开的石门。门里传来钟声，像有人在数他的呼吸。他左手按在刀鞘上，掌心的汗把布带浸湿。门内没有灯，香灰味比雨更冷。远处有人喊他，声音穿过水汽，听不真切。他没有应声，抬脚踏过门槛。他知道这一步迈进去就再没有退路。"}}
	if f := RefineSimilarityFindings(original, revised); len(f) != 0 {
		t.Fatalf("unexpected findings: %+v", f)
	}
}

func TestRefineSimilaritySkipsNewChapters(t *testing.T) {
	original := []domain.ChapterDraft{{Chapter: 1, Body: refineOld}}
	revised := []domain.ChapterDraft{{Chapter: 2, Body: refineOld}}
	if f := RefineSimilarityFindings(original, revised); len(f) != 0 {
		t.Fatalf("chapter without a baseline must be skipped: %+v", f)
	}
}
