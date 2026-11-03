package books

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// novel-state.yaml 的字段是按 map 传入的。遍历顺序若不稳定，同一份立项结果
// 两次接受会写出不同的字节序，computeRevision 随之改变，下游以 revision
// 为键的缓存（提示词/候选）全部失效。
func TestUpdateStateWritesStableKeyOrder(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "novel-state.yaml")
	if err := os.WriteFile(path, []byte("title: 示例\ngenre: 悬疑\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fields := map[string]string{
		"stage":       "volume_outline",
		"next_action": "写第一卷",
		"active_unit": "v01-U1",
		"subgenre":    "刑侦探案",
	}
	if err := updateState(root, fields); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := os.WriteFile(path, []byte("title: 示例\ngenre: 悬疑\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := updateState(root, fields); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(first) {
			t.Fatalf("novel-state.yaml is not byte stable on run %d:\n%s\n---\n%s", i, first, got)
		}
	}
	// 追加的新键必须按字典序，而不是 map 顺序。
	text := strings.TrimSpace(string(first))
	if strings.Index(text, "active_unit:") > strings.Index(text, "next_action:") ||
		strings.Index(text, "next_action:") > strings.Index(text, "stage:") {
		t.Fatalf("appended keys are not sorted:\n%s", text)
	}
}

// 立项校验的报错文本会进 repair 提示词，第一条不允许的路径必须稳定：
// 遍历 cand.PlanSettings（map）时不能随迭代顺序换一条报错。
func TestDisallowedPlanSettingsPathIsDeterministic(t *testing.T) {
	for i := 0; i < 20; i++ {
		root, err := CreateBookScaffold(t.TempDir(), "path-order", "测试", "悬疑")
		if err != nil {
			t.Fatal(err)
		}
		settings := sampleSettings()
		settings["zzz-unknown.md"] = "x"
		settings["aaa-unknown.md"] = "y"
		cand := domain.Candidate{
			Mode: domain.PlanModeInit,
			PlanMarkdown: map[string]string{
				"book_outline": sampleBookOutline(),
				"cast_json":    `[{"stem":"lin","name":"林晚","role":"protagonist"}]`,
			},
			PlanSettings: settings,
		}
		err = AcceptPlan(root, cand)
		if err == nil {
			t.Fatal("expected the disallowed settings path to be rejected")
		}
		if !strings.Contains(err.Error(), "aaa-unknown.md") {
			t.Fatalf("expected the alphabetically first path, got %q", err.Error())
		}
	}
}
