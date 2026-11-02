package novel

import (
	"strings"
	"testing"
)

// 题材元数据是自由文本，约束表是按规范族名索引的。任何非规范写法都不能
// 静默降级成"通用"——那会让刑侦小说丢掉「信息控制严格、禁止全知泄漏」。
func TestGenreBulletsResolvesFreeTextGenre(t *testing.T) {
	cases := []struct {
		genre    string
		contains string
	}{
		{"悬疑", "禁止全知泄漏"},
		{"悬疑刑侦", "禁止全知泄漏"},                    // 曾被精确查表打落到通用
		{"悬疑 / 刑侦 / 职业文 / 年代 / 群像", "禁止全知泄漏"}, // 组合写法
		{"都市官场权谋 + 悬疑解谜（重生流）", "信息控制严格"},      // 多族，取第一个有约束的
		{"都市异能 / 神话复苏（男频）", "现实压力具体"},
		{"刑侦", "禁止全知泄漏"}, // 别名
	}
	for _, c := range cases {
		got := GenreBullets(c.genre, "")
		if !strings.Contains(got, c.contains) {
			t.Errorf("GenreBullets(%q) 未解析到 %q:\n%s", c.genre, c.contains, got)
		}
	}
}

func TestGenreBulletsUnknownGenreFallsBack(t *testing.T) {
	got := GenreBullets("", "")
	if !strings.Contains(got, "通用") || !strings.Contains(got, "章末有钩") {
		t.Fatalf("空题材应落到通用:\n%s", got)
	}
	got = GenreBullets("男频·无限流·规则怪谈", "")
	if !strings.Contains(got, "章末有钩") {
		t.Fatalf("未知题材应落到通用约束:\n%s", got)
	}
}

// 子类按规范名比较：年代刑侦 / 刑侦 / 探案 都指向刑侦探案。
func TestGenreBulletsMatchesCrimeSubgenreSpellings(t *testing.T) {
	want := "动机有社会性数字"
	for _, sub := range []string{"刑侦探案", "刑侦", "年代刑侦", "探案", "警察", "法医", "社会派"} {
		if got := GenreBullets("悬疑", sub); !strings.Contains(got, want) {
			t.Errorf("subgenre=%q 未匹配刑侦子类:\n%s", sub, got)
		}
	}
	// 灵异、规则怪谈不是刑侦子类，不能加上刑侦约束。
	for _, sub := range []string{"悬疑灵异", "规则怪谈", "悬疑脑洞", ""} {
		if got := GenreBullets("悬疑", sub); strings.Contains(got, want) {
			t.Errorf("subgenre=%q 不应匹配刑侦子类:\n%s", sub, got)
		}
	}
}

func TestCanonicalSubgenre(t *testing.T) {
	if got := CanonicalSubgenre("年代刑侦"); got != "刑侦探案" {
		t.Fatalf("CanonicalSubgenre(年代刑侦)=%q", got)
	}
	if got := CanonicalSubgenre("年代职业文"); got != "" {
		t.Fatalf("无法识别的子类应返回空: %q", got)
	}
	if got := CanonicalSubgenre(""); got != "" {
		t.Fatalf("空子类应返回空: %q", got)
	}
}

func TestSubgenreMatched(t *testing.T) {
	if !SubgenreMatched("刑侦探案", "年代刑侦") {
		t.Fatal("同一子类的不同写法必须匹配")
	}
	if SubgenreMatched("刑侦探案", "悬疑灵异") {
		t.Fatal("不同子类不能匹配")
	}
	// 双方都不认识时退回字面比较，保持既有行为。
	if !SubgenreMatched("古风世情", "古风世情") {
		t.Fatal("未知子类应回退到字面比较")
	}
}
