package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/runtimepaths"
)

// 分章审校是新安装的默认：单章 prompt 小、注意力集中，不会出现"整单元结论
// 把输出预算挤爆"的截断。人工审校与连续重写都读这一个开关，所以默认值一旦
// 被改回 false，两条路会同时退化成整单元调用。
func TestReviewDefaultsToSplitByChapter(t *testing.T) {
	t.Setenv("NOVEL_HOME", t.TempDir())
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !m.Get().Review.SplitByChapter {
		t.Fatal("fresh settings should default review.splitByChapter to true")
	}
	if got := m.Get().Review.Concurrency; got != 3 {
		t.Fatalf("concurrency=%d want 3", got)
	}
}

// 老配置文件里没有 review 段时必须继承新默认，而不是被零值（false）静默覆盖。
func TestReviewDefaultSurvivesUnrelatedSettingsFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NOVEL_HOME", home)
	path := runtimepaths.SettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// 只写无关字段：review 整段缺失。
	if err := os.WriteFile(path, []byte(`{"llmHttpTimeoutSec":120}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !m.Get().Review.SplitByChapter {
		t.Fatal("missing review section must keep the split default")
	}
}

// 作者显式关掉时必须被尊重：默认值只在"没写"的时候生效。
func TestReviewExplicitFalseIsRespected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NOVEL_HOME", home)
	path := runtimepaths.SettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"review":{"splitByChapter":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if m.Get().Review.SplitByChapter {
		t.Fatal("explicit false must not be overridden by the default")
	}
}
