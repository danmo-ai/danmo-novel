package contextsvc

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

// ChapterForeshadowsKey 是按章切片的伏笔块在 payload 尾部的键名。
//
// 与 chapterBeats 同理：它按章变化，所以不能留在 documents——documents 排在
// references 之前，一旦中途分叉，后面的基线与整份参考知识都无法命中前缀缓存。
const ChapterForeshadowsKey = "chapterForeshadows"

// ForeshadowForChapter 渲染「本章要埋/要收」与「尚未回收」两块。
//
// 它取代原来那块「揭示约束」：后者把登记表里所有未解锁条目的 public_summary
// 全文都塞进写作包，写第 38 章时提示词里就有「陆沉来自 2024 年」
// 「2009 年陆沉苏醒」这类全书结局。而 AssertNoAuthorLore 只按文件来源判断，
// 抓不到这种「合法文件里的剧透」。融合后只有两条路径会进入写作包：
//
//   - 本章 plant / payoff 点名的条目：给内容，payoff 还额外给回收摘要与锁词；
//   - 已埋未收的条目：只给 ID、内容与埋点位置，**不给回收摘要**。
//
// 未到回收点的条目因此永远不会泄露它将来要揭示什么。
func ForeshadowForChapter(snap domain.BookSnapshot, unit domain.UnitInfo, chapter int) string {
	registry := books.ForeshadowByID(snap)
	if len(registry) == 0 {
		return ""
	}
	planted, paid := books.ChapterLoops(unit)
	unitPos := positionOf(unit.Volume, unit.Index, chapter)

	var current, pending []string
	for _, id := range planted[chapter] {
		f, ok := registry[id]
		if !ok {
			continue
		}
		line := "- " + id + "（" + f.Type + "）埋下：" + f.Content
		if f.PayoffChapter > 0 {
			line += fmt.Sprintf("；第 %d 章回收", f.PayoffChapter)
		} else if f.PayoffUnit != "" && f.PayoffUnit != unit.UnitID {
			line += "；" + f.PayoffUnit + " 回收"
		}
		if f.LockedTerms != "" {
			line += "；在此之前不得出现：" + f.LockedTerms
		}
		current = append(current, line)
	}
	for _, id := range paid[chapter] {
		f, ok := registry[id]
		if !ok {
			continue
		}
		line := "- " + id + "（" + f.Type + "）回收：" + f.Content
		if f.PayoffSummary != "" {
			line += "；本章必须让读者得到：" + f.PayoffSummary
		}
		if f.Note != "" {
			line += "；回收方式：" + f.Note
		}
		current = append(current, line)
	}
	for _, f := range books.Foreshadows(snap) {
		if f.PayoffPaid() {
			continue
		}
		plantPos := positionOfVolume(f.PlantVolume, f.PlantUnit, f.PlantChapter)
		payoffPos := positionOfVolume(f.PayoffVolume, f.PayoffUnit, f.PayoffChapter)
		// 只列「已经埋下、还没到回收」的：尚未埋的条目属于别的单元的计划。
		if plantPos == 0 || plantPos >= unitPos || payoffPos == 0 || payoffPos <= unitPos {
			continue
		}
		if contains(planted[chapter], f.ID) || contains(paid[chapter], f.ID) {
			continue
		}
		where := ""
		if f.PlantChapter > 0 {
			where = fmt.Sprintf("埋于第 %d 章", f.PlantChapter)
		} else if f.PlantUnit != "" {
			where = "埋于 " + f.PlantUnit
		}
		pending = append(pending, "- "+f.ID+"："+f.Content+"（"+where+"）")
	}

	var out []string
	if len(current) > 0 {
		out = append(out, "### 本章伏笔（只埋/收下列条目，不要自行发明悬念）\n"+strings.Join(current, "\n"))
	}
	if len(pending) > 0 {
		if len(pending) > 6 {
			pending = pending[:6]
		}
		out = append(out, "### 已埋未收（不要提前回收，也不要写穿它的答案）\n"+strings.Join(pending, "\n"))
	}
	return strings.Join(out, "\n\n")
}

// ForeshadowForReview 渲染本单元负责埋/收的伏笔，供审校判定「该埋没埋」「提前收了」。
func ForeshadowForReview(snap domain.BookSnapshot, unit domain.UnitInfo) string {
	registry := books.ForeshadowByID(snap)
	if len(registry) == 0 {
		return ""
	}
	planted, paid := books.ChapterLoops(unit)
	var rows []string
	seen := map[string]bool{}
	add := func(id string, kind string) {
		if seen[id] {
			return
		}
		seen[id] = true
		f, ok := registry[id]
		if !ok {
			rows = append(rows, "- "+id+"："+kind+"，但登记表里没有这条伏笔")
			return
		}
		line := "- " + id + "（" + f.Type + "）" + kind + "：" + f.Content
		if f.PayoffSummary != "" {
			line += "；读者应得到：" + f.PayoffSummary
		}
		if f.LockedTerms != "" {
			line += "；锁词：" + f.LockedTerms
		}
		rows = append(rows, line)
	}
	for ch := unit.ChapterFrom; ch <= unit.ChapterTo; ch++ {
		for _, id := range planted[ch] {
			add(id, fmt.Sprintf("第 %d 章埋下", ch))
		}
		for _, id := range paid[ch] {
			add(id, fmt.Sprintf("第 %d 章回收", ch))
		}
	}
	if len(rows) == 0 {
		return ""
	}
	return "### 本单元伏笔（逐条核对：该埋的埋了没有、该收的收得够不够、有没有提前收）\n" + strings.Join(rows, "\n")
}

// ForeshadowPlanningEntities returns complete records relevant to this planning run.
// It never slices structured facts by character count.
func ForeshadowPlanningEntities(snap domain.BookSnapshot, volume, unitID string) string {
	var out []string
	for _, f := range books.Foreshadows(snap) {
		relevant := f.PlantVolume == volume || f.PayoffVolume == volume ||
			(unitID != "" && (f.PlantUnit == unitID || f.PayoffUnit == unitID))
		if !relevant || f.PayoffPaid() {
			continue
		}
		row := fmt.Sprintf("- %s｜%s｜埋=%s/%s/%d｜收=%s/%s/%d｜%s", f.ID, f.Type, f.PlantVolume, f.PlantUnit, f.PlantChapter, f.PayoffVolume, f.PayoffUnit, f.PayoffChapter, f.Content)
		if f.LockedTerms != "" {
			row += "｜锁词=" + f.LockedTerms
		}
		out = append(out, row)
	}
	return strings.Join(out, "\n")
}

var unitOrderRe = regexp.MustCompile(`(?i)^v(\d+)-U(\d+)$`)

// positionOf 把 (卷, 单元序号, 章) 压成可比较整数。
func positionOf(volume, index, chapter int) int {
	if volume <= 0 {
		return 0
	}
	return volume*1_000_000 + index*10_000 + chapter
}

func positionOfVolume(volumeTag, unitID string, chapter int) int {
	vol := 0
	if n := len(volumeTag); n >= 2 && volumeTag[0] == 'v' {
		for _, r := range volumeTag[1:] {
			if r < '0' || r > '9' {
				return 0
			}
			vol = vol*10 + int(r-'0')
		}
	}
	index := 0
	if m := unitOrderRe.FindStringSubmatch(unitID); m != nil {
		fmt.Sscanf(m[2], "%d", &index)
	}
	if vol <= 0 {
		return 0
	}
	return positionOf(vol, index, chapter)
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
