package books

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// 细纲第一次给伏笔补章号时，登记表里 plant_unit 还是空的——注释里写明这是
// 「卷纲补、可空；细纲补章」的正常顺序。旧实现先用本次的 PlantUnit 写进 out，
// 再用**写入前**的 registry 快照判章级归属，于是必然报「尚未分配埋入单元」，
// 整份细纲在接受最后一步失败，前面所有 LLM 调用白跑。
func TestRenderForeshadowAssignmentsAssignsUnitAndChapterInOneCall(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "fs-test", "伏笔测试", "玄幻")
	if err != nil {
		t.Fatal(err)
	}
	schedule := foreshadowRegistry(`## 伏笔 · FS-01
- content: "主角枕下那把没有登记的钥匙。"
- type: 物件
- plant_volume: v01
- payoff_volume: v01
- payoff_unit: v01-U1
- status: planned
`)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(ForeshadowSchedulePath)), []byte(schedule), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := RenderForeshadowAssignments(root, []domain.ForeshadowAssignment{
		{ID: "FS-01", PlantUnit: "v01-U1", PlantChapter: 2},
	})
	if err != nil {
		t.Fatalf("单元与章号同时给出时必须能落盘：%v", err)
	}
	got := string(out)
	for _, want := range []string{"plant_unit: v01-U1", "plant_chapter: 2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("schedule missing %q:\n%s", want, got)
		}
	}
}

// 只给章号、登记表里也没有单元时，仍然必须报错：那才是真的没有归属，
// 写下去会让「第几章埋」挂在一个不存在的单元上。
func TestRenderForeshadowAssignmentsRejectsChapterWithoutUnit(t *testing.T) {
	root, err := CreateBookScaffold(t.TempDir(), "fs-test", "伏笔测试", "玄幻")
	if err != nil {
		t.Fatal(err)
	}
	schedule := foreshadowRegistry(`## 伏笔 · FS-01
- content: "主角枕下那把没有登记的钥匙。"
- type: 物件
- plant_volume: v01
- payoff_volume: v01
- payoff_unit: v01-U1
- status: planned
`)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(ForeshadowSchedulePath)), []byte(schedule), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = RenderForeshadowAssignments(root, []domain.ForeshadowAssignment{
		{ID: "FS-01", PlantChapter: 2},
	})
	if err == nil || !strings.Contains(err.Error(), "尚未分配埋入单元") {
		t.Fatalf("want a loud error, got %v", err)
	}
}
