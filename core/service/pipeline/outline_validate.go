package pipeline

import (
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

// ValidateOutlineContract is used at response time, not just as a gate after a
// candidate was already produced. Otherwise zero values silently become prose
// constraints and the next stage is forced to invent the missing unit design.
func ValidateOutlineContract(units []domain.UnitOutlineDraft, requested []string) error {
	var vs []Violation
	seen := map[string]bool{}
	for i, u := range units {
		base := fmt.Sprintf("units[%d]", i)
		if strings.TrimSpace(u.UnitID) == "" {
			vs = append(vs, Violation{Path: base + ".unitId", Want: "当前卷的单元 ID", Got: "空"})
		}
		if seen[u.UnitID] {
			vs = append(vs, Violation{Path: base + ".unitId", Want: "唯一单元 ID", Got: u.UnitID})
		}
		seen[u.UnitID] = true
		if len(requested) > 0 && !containsString(requested, u.UnitID) {
			vs = append(vs, Violation{Path: base + ".unitId", Want: "只输出请求的单元", Got: u.UnitID})
		}
		for _, f := range []struct{ key, value string }{
			{"function", u.Function}, {"desire", u.Desire}, {"obstacle", u.Obstacle},
			{"choice", u.Choice}, {"payoff", u.Payoff}, {"forbidden", u.Forbidden},
			{"infoControl", u.InfoControl},
		} {
			if strings.TrimSpace(f.value) == "" {
				vs = append(vs, Violation{Path: base + "." + f.key, Want: "非空写作合同", Got: "空"})
			}
		}
		if u.StateDeltas == nil {
			vs = append(vs, Violation{Path: base + ".stateDeltas", Want: "结构化状态迁移 [{stem,field,from,to}]", Got: "缺失"})
		}
		if strings.TrimSpace(fmt.Sprint(u.NextHook["type"])) == "" || strings.TrimSpace(fmt.Sprint(u.NextHook["out"])) == "" {
			vs = append(vs, Violation{Path: base + ".nextHook", Want: "{type,out} 均为具体非空内容", Got: fmt.Sprint(u.NextHook)})
		}
		if len(u.Scenes) == 0 {
			vs = append(vs, Violation{Path: base + ".scenes", Want: "至少每章一个场面", Got: "空"})
		}
		for j, scene := range u.Scenes {
			for _, key := range []string{"goal", "conflict", "turn", "outcome", "must_land"} {
				if _, ok := scene[key]; !ok || strings.TrimSpace(fmt.Sprint(scene[key])) == "" || fmt.Sprint(scene[key]) == "[]" {
					vs = append(vs, Violation{Path: fmt.Sprintf("%s.scenes[%d].%s", base, j, key), Want: "可执行场面字段", Got: "缺失"})
				}
			}
		}
	}
	for _, id := range requested {
		if !seen[id] {
			vs = append(vs, Violation{Path: "units", Want: "覆盖请求的单元 " + id, Got: "缺失"})
		}
	}
	if err := validateOutlineScenes(units); err != nil {
		vs = append(vs, Violation{Path: "scenes", Want: "章号合法且完整覆盖", Got: err.Error()})
	}
	return contractFail("细纲", vs)
}

// ValidateOutlineInheritance rechecks the same contract at Accept time. It
// prevents a persisted candidate from bypassing the generation validator.
func ValidateOutlineInheritance(snap domain.BookSnapshot, volume int, units []domain.UnitOutlineDraft) error {
	doc, ok := books.StructuredOutlineFor(snap, fmt.Sprintf("outline/volumes/v%02d.md", volume))
	if !ok || !doc.Valid() {
		return nil
	}
	for _, unit := range units {
		seed, found := doc.Entity("单元", unit.UnitID)
		if !found {
			return contractFail("细纲父子合同", []Violation{{Path: "unitId", Want: "卷纲已有单元种子", Got: unit.UnitID}})
		}
		if err := ValidateUnitSeedInheritance(unit, seed); err != nil {
			return err
		}
	}
	return nil
}

// ValidateUnitSeedInheritance locks the volume's unit-level decisions while
// leaving scenes, pacing and expression free for the outline task.
func ValidateUnitSeedInheritance(unit domain.UnitOutlineDraft, seed domain.MarkdownEntity) error {
	var vs []Violation
	if unit.UnitID != seed.ID {
		vs = append(vs, Violation{Path: "unitId", Want: seed.ID, Got: unit.UnitID})
	}
	from, to := books.ChapterWindow(seed.Value("chapter_range"))
	if unit.ChapterFrom != from || unit.ChapterTo != to {
		vs = append(vs, Violation{Path: "chapterRange", Want: seed.Value("chapter_range"), Got: fmt.Sprintf("%d-%d", unit.ChapterFrom, unit.ChapterTo)})
	}
	for _, pair := range []struct {
		path, want, got string
	}{{"desire", seed.Value("objective"), unit.Desire}, {"obstacle", seed.Value("obstacle"), unit.Obstacle}, {"choice", seed.Value("choice"), unit.Choice}, {"payoff", seed.Value("outcome"), unit.Payoff}} {
		if strings.TrimSpace(pair.want) != strings.TrimSpace(pair.got) {
			vs = append(vs, Violation{Path: pair.path, Want: "继承卷纲：" + pair.want, Got: pair.got})
		}
	}
	if unit.StateDeltas == nil {
		vs = append(vs, Violation{Path: "stateDeltas", Want: "继承卷纲 state_changes 的结构化数组", Got: "缺失"})
	}
	if unit.SourceSeedID != seed.ID {
		vs = append(vs, Violation{Path: "sourceSeedId", Want: seed.ID, Got: unit.SourceSeedID})
	}
	for _, pair := range []struct{ path, want, got string }{{"attempt", seed.Value("attempt"), unit.Attempt}, {"setback", seed.Value("setback"), unit.Setback}, {"cost", seed.Value("cost"), unit.Cost}} {
		if strings.TrimSpace(pair.want) != strings.TrimSpace(pair.got) {
			vs = append(vs, Violation{Path: pair.path, Want: "继承卷纲：" + pair.want, Got: pair.got})
		}
	}
	return contractFail("细纲父子合同", vs)
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
