package pipeline

import (
	"fmt"
	"strconv"
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
		if len(u.Chapters) == 0 {
			vs = append(vs, Violation{Path: base + ".chapters", Want: "每章一个轻量 ChapterPlan", Got: "空"})
		}
		covered := map[int]bool{}
		for j, ch := range u.Chapters {
			path := fmt.Sprintf("%s.chapters[%d]", base, j)
			if ch.Chapter < u.ChapterFrom || ch.Chapter > u.ChapterTo {
				vs = append(vs, Violation{Path: path + ".chapter", Want: "章号在本单元范围内", Got: fmt.Sprint(ch.Chapter)})
			}
			if covered[ch.Chapter] {
				vs = append(vs, Violation{Path: path + ".chapter", Want: "章号唯一", Got: fmt.Sprint(ch.Chapter)})
			}
			covered[ch.Chapter] = true
			if strings.TrimSpace(ch.Goal) == "" || strings.TrimSpace(ch.Change) == "" || strings.TrimSpace(ch.CutHook) == "" {
				vs = append(vs, Violation{Path: path, Want: "goal/change/cutHook 均为具体非空内容", Got: "缺失"})
			}
			if ch.DayOffset < 0 {
				vs = append(vs, Violation{Path: path + ".dayOffset", Want: "非负整数", Got: fmt.Sprint(ch.DayOffset)})
			}
			for k, scene := range ch.Scenes {
				if scene.Chapter != ch.Chapter {
					vs = append(vs, Violation{Path: fmt.Sprintf("%s.scenes[%d].chapter", path, k), Want: fmt.Sprint(ch.Chapter), Got: fmt.Sprint(scene.Chapter)})
				}
				if strings.TrimSpace(scene.Goal) == "" || strings.TrimSpace(scene.Conflict) == "" || strings.TrimSpace(scene.Turn) == "" || strings.TrimSpace(scene.Outcome) == "" {
					vs = append(vs, Violation{Path: fmt.Sprintf("%s.scenes[%d]", path, k), Want: "最小场面 goal/conflict/turn/outcome", Got: "缺失"})
				}
				if len(scene.MustLand) > 2 {
					vs = append(vs, Violation{Path: fmt.Sprintf("%s.scenes[%d].mustLand", path, k), Want: "最多 2 个可见落地事实", Got: fmt.Sprint(len(scene.MustLand))})
				}
				if scene.DayOffset != nil && (*scene.DayOffset < ch.DayOffset || *scene.DayOffset < 0) {
					vs = append(vs, Violation{Path: fmt.Sprintf("%s.scenes[%d].dayOffset", path, k), Want: "不早于本章时间偏移", Got: fmt.Sprint(*scene.DayOffset)})
				}
			}
		}
		for chapter := u.ChapterFrom; chapter <= u.ChapterTo; chapter++ {
			if !covered[chapter] {
				vs = append(vs, Violation{Path: base + ".chapters", Want: "覆盖单元全部章节", Got: fmt.Sprintf("缺第 %d 章", chapter)})
			}
		}
	}
	for _, id := range requested {
		if !seen[id] {
			vs = append(vs, Violation{Path: "units", Want: "覆盖请求的单元 " + id, Got: "缺失"})
		}
	}
	for _, u := range units {
		day, err := strconv.Atoi(strings.TrimSpace(u.StoryDay))
		if err != nil || day <= 0 || u.EndDay < day || u.GapDays < 0 {
			vs = append(vs, Violation{Path: u.UnitID + ".clock", Want: "整数 story_day/end_day，end_day >= story_day，gapDays >= 0", Got: fmt.Sprintf("%s/%d/%d", u.StoryDay, u.EndDay, u.GapDays)})
		}
		for _, ch := range u.Chapters {
			if day > 0 && u.EndDay > 0 && day+ch.DayOffset > u.EndDay {
				vs = append(vs, Violation{Path: fmt.Sprintf("%s.chapters[%d].dayOffset", u.UnitID, ch.Chapter), Want: "不超出单元结束日", Got: fmt.Sprint(ch.DayOffset)})
			}
		}
	}
	return contractFail("细纲", vs)
}

// ValidateOutlineInheritance rechecks the same contract at Accept time. It
// prevents a persisted candidate from bypassing the generation validator.
func ValidateOutlineInheritance(snap domain.BookSnapshot, volume int, units []domain.UnitOutlineDraft) error {
	// 章级伏笔合同不依赖卷纲文档，先查：即使卷纲还没结构化，也要拦住发明 ID、
	// 替别的单元埋收、回收早于埋入这三类错。
	if err := books.ValidateForeshadowChapterRefs(snap, units); err != nil {
		return err
	}
	doc, ok := books.StructuredOutlineFor(snap, fmt.Sprintf("outline/volumes/v%02d.md", volume))
	if !ok {
		return contractFail("细纲父子合同", []Violation{{Path: "volume", Want: "已接受的结构化卷纲", Got: "不存在"}})
	}
	if !doc.Valid() {
		return contractFail("细纲父子合同", []Violation{{Path: "volume", Want: "有效卷纲", Got: books.FormatOutlineDiagnostics(doc.Diagnostics)}})
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
	// function 与 desire 同源于种子的 objective（见 books.UnitSeed.Function）。
	// 提示词一直写着「不得改卷纲已定 function」，但校验只比了 desire，于是模型
	// 改写 function 而保留 desire 时无人发现——单元在卷里的功能定位就这样漂了。
	// function 是细纲自己的定位表达：卷纲只锁 objective/desire，function 可以更具体，
	// 但不能为空。强制逐字相等会把作者对功能定位的细化判成错误（真实书里大量如此）。
	if strings.TrimSpace(unit.Function) == "" {
		vs = append(vs, Violation{Path: "function", Want: "非空功能定位", Got: "空"})
	}
	// 卷纲种子的 next_hook 是「类型 | 事件」，单位是写作阶段选钩子的依据；
	// 同理一直只靠提示词约束，从未校验。
	if hook := strings.SplitN(seed.Value("next_hook"), "|", 2); len(hook) == 2 {
		if want := strings.TrimSpace(hook[0]); want != "" {
			if got := outlineNextHookType(unit.NextHook); got != want {
				vs = append(vs, Violation{Path: "nextHook.type", Want: "继承卷纲：" + want, Got: got})
			}
		}
	}
	if unit.StateDeltas == nil {
		vs = append(vs, Violation{Path: "stateDeltas", Want: "继承卷纲 state_changes 的结构化数组", Got: "缺失"})
	} else if err := ValidateStateDeltaInheritance(unit.StateDeltas, seed.Value("state_changes")); err != nil {
		vs = append(vs, Violation{Path: "stateDeltas", Want: "逐项继承卷纲 state_changes，可追加子级细化", Got: err.Error()})
	}
	if unit.SourceSeedID != seed.ID {
		vs = append(vs, Violation{Path: "sourceSeedId", Want: seed.ID, Got: unit.SourceSeedID})
	}
	if unit.StoryDay != seed.Value("clock_start_day") || unit.EndDay != mustAtoi(seed.Value("clock_end_day")) || unit.GapDays != mustAtoi(seed.Value("clock_gap_days")) {
		vs = append(vs, Violation{Path: "clock", Want: "继承卷纲 clock_start_day/clock_end_day/clock_gap_days", Got: fmt.Sprintf("%s/%d/%d", unit.StoryDay, unit.EndDay, unit.GapDays)})
	}
	for _, pair := range []struct{ path, want, got string }{{"attempt", seed.Value("attempt"), unit.Attempt}, {"setback", seed.Value("setback"), unit.Setback}, {"cost", seed.Value("cost"), unit.Cost}} {
		if strings.TrimSpace(pair.want) != strings.TrimSpace(pair.got) {
			vs = append(vs, Violation{Path: pair.path, Want: "继承卷纲：" + pair.want, Got: pair.got})
		}
	}
	return contractFail("细纲父子合同", vs)
}

// ValidateStateDeltaInheritance locks every parent transition; only additions are allowed.
func ValidateStateDeltaInheritance(value any, parent string) error {
	rows, ok := value.([]any)
	if !ok {
		return fmt.Errorf("stateDeltas 必须是数组")
	}
	children := map[string]bool{}
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			return fmt.Errorf("每项必须是 {stem,field,from,to}")
		}
		key := fmt.Sprintf("%s.%s=%v->%v", m["stem"], m["field"], m["from"], m["to"])
		children[key] = true
	}
	for _, expected := range books.ContractStateDeltaKeys(parent) {
		if !children[expected] {
			return fmt.Errorf("缺少父级迁移 %s", expected)
		}
	}
	return nil
}

func mustAtoi(value string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	return n
}

// outlineNextHookType 读 nextHook["type"]，把缺失/非标量统一成空串，
// 免得报错信息里出现 "<nil>"。
func outlineNextHookType(hook map[string]any) string {
	if hook == nil {
		return ""
	}
	v, ok := hook["type"]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func outlineOnStage(raw string) []string {
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "on_stage:") {
			continue
		}
		value := strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "on_stage:")), "[]")
		var out []string
		for _, stem := range strings.Split(value, ",") {
			if stem = strings.Trim(strings.TrimSpace(stem), `"'`); stem != "" {
				out = append(out, stem)
			}
		}
		return out
	}
	return nil
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
