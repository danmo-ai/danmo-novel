package contextsvc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 卷纲夹具：3 个单元 + 3 个卷级节拍，节拍章窗分属不同单元。
const outlineVolumeFixture = `# 卷纲 · v01
- schema_version: 1
- volume_id: v01
- goal: 本卷目标
- cast: lu-chen,wen-nianci

## 节拍 · e-01
- event: 首案重启。
- chapter_window: 1-6

## 节拍 · e-02
- event: 中间转折。
- chapter_window: 7-10

## 节拍 · e-03
- event: 卷末调任。
- chapter_window: 11-14

## 单元 · v01-U1
- title: 报到先破局
- chapter_range: 1-6
- objective: 取得首案位置。
- attempt: 用记忆指方向。
- setback: 细节失真。
- cost: 暴露敏锐。
- next_hook: 信息缺口 | 卷宗少一页。

## 单元 · v01-U2
- title: 七份材料
- chapter_range: 7-10
- objective: 把保护网变成案件。
- obstacle: 人情压来。
- attempt: 重组材料。
- setback: 证人翻供。
- choice: 交给有管辖权的组。
- cost: 让出头功。
- outcome: 刘三喜被收网。
- clock_start_day: 18
- clock_end_day: 93
- clock_gap_days: 6
- next_hook: 信息缺口 | 有人递话。

## 单元 · v01-U3
- title: 冷板凳
- chapter_range: 11-14
- objective: 完成交接。
- attempt: 交产业。
- setback: 失庇护。
- cost: 离人脉。
- next_hook: 信息缺口 | 缺页卷宗。

## 卷末交接 · handoff
- deliverables: 首案、转正。
`

func outlineSnapshot() domain.BookSnapshot {
	return domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "demo", Genre: "刑侦"},
		Files:   map[string]string{"outline/volumes/v01.md": outlineVolumeFixture},
		Volumes: []domain.VolumeInfo{{Volume: 1, FileName: "v01.md", Raw: outlineVolumeFixture}},
		Units: []domain.UnitInfo{{
			UnitID: "v01-U2", Volume: 1, ChapterFrom: 7, ChapterTo: 10,
			OutlinePath: "outline/units/v01-U2.yaml", OutlineRaw: "unit_id: v01-U2\n",
		}},
	}
}

func outlineBlock(t *testing.T, p domain.ContextPacket, id string) domain.ContextBlock {
	t.Helper()
	for _, b := range p.Blocks {
		if b.ID == id {
			return b
		}
	}
	t.Fatalf("missing block %q in %v", id, blockIDs(p))
	return domain.ContextBlock{}
}

func blockIDs(p domain.ContextPacket) []string {
	out := make([]string, 0, len(p.Blocks))
	for _, b := range p.Blocks {
		out = append(out, b.ID)
	}
	return out
}

// 种子块只放本次请求的单元，且不带源码位置。
func TestOutlinePacketSeedsOnlyRequestedUnit(t *testing.T) {
	p := BuildOutlinePacket(outlineSnapshot(), 1, []string{"v01-U2"}, "")
	seed := outlineBlock(t, p, "unit-seeds")

	if strings.Contains(seed.Content, "v01-U1") || strings.Contains(seed.Content, "v01-U3") {
		t.Fatalf("seed block leaks other units: %s", seed.Content)
	}
	if strings.Contains(seed.Content, "byteOffset") || strings.Contains(seed.Content, `"range"`) {
		t.Fatalf("seed block still carries source ranges: %s", seed.Content)
	}

	var seeds []map[string]any
	if err := json.Unmarshal([]byte(seed.Content), &seeds); err != nil {
		t.Fatalf("seed block is not JSON: %v\n%s", err, seed.Content)
	}
	if len(seeds) != 1 || seeds[0]["id"] != "v01-U2" {
		t.Fatalf("seeds=%+v", seeds)
	}
	// 字段名必须保持卷纲里的原样：提示词按这些名字要求逐字继承。
	for _, key := range []string{"chapter_range", "objective", "obstacle", "attempt", "setback", "choice", "cost", "outcome", "clock_start_day"} {
		if _, ok := seeds[0][key]; !ok {
			t.Fatalf("seed missing %q: %+v", key, seeds[0])
		}
	}
}

// 卷纲只留头部 + 章窗相交的节拍；单元区段与种子块重复，必须丢弃。
func TestOutlinePacketTrimsVolumeToRequestedWindows(t *testing.T) {
	p := BuildOutlinePacket(outlineSnapshot(), 1, []string{"v01-U2"}, "")
	vol := outlineBlock(t, p, "volume").Content

	if !strings.Contains(vol, "本卷目标") {
		t.Fatal("volume header must survive")
	}
	if !strings.Contains(vol, "中间转折") {
		t.Fatal("beat whose window covers 7-10 must survive")
	}
	for _, dropped := range []string{"首案重启", "卷末调任", "## 单元 · v01-U1", "## 单元 · v01-U2"} {
		if strings.Contains(vol, dropped) {
			t.Fatalf("volume should drop %q:\n%s", dropped, vol)
		}
	}
	if !strings.Contains(vol, "卷末交接") {
		t.Fatal("volume-level handoff must survive")
	}
}

// 没指定单元（整卷细纲）时保留整份卷纲与全部种子。
func TestOutlinePacketWithoutUnitIDsKeepsEverything(t *testing.T) {
	p := BuildOutlinePacket(outlineSnapshot(), 1, nil, "")
	vol := outlineBlock(t, p, "volume").Content
	if !strings.Contains(vol, "首案重启") || !strings.Contains(vol, "## 单元 · v01-U3") {
		t.Fatalf("no unit filter must keep whole volume:\n%s", vol)
	}
	seed := outlineBlock(t, p, "unit-seeds").Content
	for _, id := range []string{"v01-U1", "v01-U2", "v01-U3"} {
		if !strings.Contains(seed, id) {
			t.Fatalf("seed block missing %s for whole-volume run", id)
		}
	}
}

// 身份变更按 from_unit 过滤，单独成块，不混进种子。
func TestOutlinePacketCarriesIdentityChangeOfRequestedUnit(t *testing.T) {
	raw := outlineVolumeFixture + `
### 身份变更 · v01-U2-lu-chen
- stem: lu-chen
- role_title: 痕迹技术员
- from_unit: v01-U2
- reason: 正式岗位

### 身份变更 · v01-U3-other
- stem: other
- role_title: 顾问
- from_unit: v01-U3
- reason: 返聘
`
	snap := outlineSnapshot()
	snap.Files["outline/volumes/v01.md"] = raw
	snap.Volumes[0].Raw = raw

	p := BuildOutlinePacket(snap, 1, []string{"v01-U2"}, "")
	id := outlineBlock(t, p, "unit-identity").Content
	if !strings.Contains(id, "lu-chen") || !strings.Contains(id, "痕迹技术员") {
		t.Fatalf("requested unit identity change missing: %s", id)
	}
	if strings.Contains(id, "v01-U3-other") {
		t.Fatalf("identity block leaks other units: %s", id)
	}
	if strings.Contains(outlineBlock(t, p, "unit-seeds").Content, "role_title") {
		t.Fatal("identity change must not be mixed into unit seeds")
	}
}

// 细纲指令必须点名校验会查的每一个"逐字继承"字段。
//
// 「细纲·可编辑子级字段」块为了不把同一份内容注入两遍，会有意摘掉
// attempt/setback/cost/source_seed_id 这些归种子所有的字段。于是这些字段在
// 上下文里只剩种子块那一份，而模型把它们当成"不用输出"——除非指令明确点名。
// 实测两本书 59 次细纲运行失败 19 次，其中 9 次正是这几个字段留空
// （attempt/setback/cost 7 次、sourceSeedId 2 次），修复轮重发同一句指令也救不回来。
//
// 这条测试把「指令里的清单」和 ValidateUnitSeedInheritance 的实际判据钉在一起：
// 以后再加校验项却忘了写进指令，这里会直接失败，而不是让作者在 accept 时才发现。
func TestOutlineInstructionNamesEveryInheritedField(t *testing.T) {
	p := BuildOutlinePacket(outlineSnapshot(), 1, []string{"v01-U2"}, "")
	instr := p.UserPrompt

	// 这些字段只在种子块里出现（head 块被 dropTopLevelKeys 摘掉了），
	// 指令不点名就会被模型跳过。
	for _, field := range []string{"sourceSeedId", "attempt", "setback", "cost"} {
		if !strings.Contains(instr, field) {
			t.Fatalf("细纲指令没有点名 %q；该字段只存在于种子块，模型会当成不需要输出", field)
		}
	}
	// 逐字继承的映射必须写清，否则模型会用同义改写撞校验。
	for _, mapping := range []string{"desire=objective", "payoff=outcome", "逐字照抄"} {
		if !strings.Contains(instr, mapping) {
			t.Fatalf("细纲指令缺少继承映射 %q", mapping)
		}
	}
}
