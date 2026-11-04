package books

import (
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"gopkg.in/yaml.v3"
)

// ValidateForeshadowChapterRefs checks the chapter-level plant/payoff lists of
// one or more unit outlines against the registry.
//
// 这是三级细化的最后一段：总纲定卷、卷纲定单元、细纲定章。规则只有四条，
// 但每一条都对应一类真实错法：
//
//  1. 引用了不存在的 FS——多半是模型自己发明了 ID。
//  2. 本单元既不负责埋也不负责收这条，却把它写进章节——等于替别的单元做主。
//  3. 同一条既在本单元埋又在本单元收，且落在同一章——埋与收不能是同一瞬间。
//  4. 回收章早于埋入章——登记表的单调性在章级同样成立。
//
// 登记表里 plant_unit / payoff_unit 为空时表示「尚未细化到单元」，此时任何
// 单元都可以认领；一旦已有值，就必须与当前单元一致。
func ValidateForeshadowChapterRefs(snap domain.BookSnapshot, units []domain.UnitOutlineDraft) error {
	registry := ForeshadowByID(snap)
	if len(registry) == 0 {
		return nil
	}
	for _, u := range units {
		plantCh := map[string]int{}
		payoffCh := map[string]int{}
		var problems []string
		for _, ch := range u.Chapters {
			for _, id := range ch.Plant {
				if f, ok := registry[id]; ok {
					if badUnit := claimConflict(f, u.UnitID, "plant"); badUnit != "" {
						problems = append(problems, fmt.Sprintf("chapters[%d].plant：本单元负责埋的伏笔，实际 %s 归属 %s", ch.Chapter, id, badUnit))
					}
				} else {
					problems = append(problems, fmt.Sprintf("chapters[%d].plant：伏笔登记表里不存在的 ID %s", ch.Chapter, id))
				}
				if prev, dup := plantCh[id]; dup && prev != ch.Chapter {
					problems = append(problems, fmt.Sprintf("chapters[%d].plant：同一条伏笔只在一章埋下，%s 已在第 %d 章埋过", ch.Chapter, id, prev))
				}
				plantCh[id] = ch.Chapter
			}
			for _, id := range ch.Payoff {
				if f, ok := registry[id]; ok {
					if badUnit := claimConflict(f, u.UnitID, "payoff"); badUnit != "" {
						problems = append(problems, fmt.Sprintf("chapters[%d].payoff：本单元负责收的伏笔，实际 %s 归属 %s", ch.Chapter, id, badUnit))
					}
				} else {
					problems = append(problems, fmt.Sprintf("chapters[%d].payoff：伏笔登记表里不存在的 ID %s", ch.Chapter, id))
				}
				if prev, dup := payoffCh[id]; dup && prev != ch.Chapter {
					problems = append(problems, fmt.Sprintf("chapters[%d].payoff：同一条伏笔只在一章回收，%s 已在第 %d 章收过", ch.Chapter, id, prev))
				}
				payoffCh[id] = ch.Chapter
			}
		}
		for id, pc := range payoffCh {
			if qc, ok := plantCh[id]; ok && pc <= qc {
				problems = append(problems, fmt.Sprintf("chapters.payoff：回收章严格晚于埋入章，%s 埋于第 %d 章、收于第 %d 章", id, qc, pc))
			}
			// 跨单元回收：登记表里已经记录了更早的埋点，回收章必须晚于它。
			f := registry[id]
			if f.PlantChapter > 0 && f.PlantUnit == u.UnitID && pc <= f.PlantChapter {
				problems = append(problems, fmt.Sprintf("chapters.payoff：回收章严格晚于登记表的埋入章，%s 埋于第 %d 章、收于第 %d 章", id, f.PlantChapter, pc))
			}
		}
		if len(problems) > 0 {
			return fmt.Errorf("细纲伏笔合同不合规（%d 处）：%s", len(problems), strings.Join(problems, "；"))
		}
	}
	return nil
}

// claimConflict 返回「这条伏笔归别的单元」时的归属说明；本单元可以认领时返回空。
func claimConflict(f Foreshadow, unitID, role string) string {
	field := f.PlantUnit
	if role == "payoff" {
		field = f.PayoffUnit
	}
	field = strings.TrimSpace(field)
	if field == "" || field == unitID {
		return ""
	}
	return field
}

// ChapterLoops collects the plant/payoff lists declared by one unit's chapters,
// keyed by chapter. Used by the finalizer to write the continuity ledger.
func ChapterLoops(unit domain.UnitInfo) (planted map[int][]string, paid map[int][]string) {
	var doc struct {
		Chapters []struct {
			Chapter int      `yaml:"chapter"`
			Plant   []string `yaml:"plant"`
			Payoff  []string `yaml:"payoff"`
		} `yaml:"chapters"`
	}
	planted, paid = map[int][]string{}, map[int][]string{}
	if err := yaml.Unmarshal([]byte(unit.OutlineRaw), &doc); err != nil {
		return planted, paid
	}
	for _, ch := range doc.Chapters {
		if len(ch.Plant) > 0 {
			planted[ch.Chapter] = ch.Plant
		}
		if len(ch.Payoff) > 0 {
			paid[ch.Chapter] = ch.Payoff
		}
	}
	return planted, paid
}
