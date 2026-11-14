package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/runtimepaths"
)

// 分章审校是新安装的默认：单章 prompt 小、注意力集中，不会出现"整单元结论
// 把输出预算挤爆"的截断。所有审校入口都读这一个开关，所以默认值一旦被改回
// false，各处会同时退化成整单元调用。
func TestReviewDefaultsToSplitByChapter(t *testing.T) {
	t.Setenv("NOVEL_HOME", t.TempDir())
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if m.Get().Review.Rounds != 3 {
		t.Fatal("fresh settings should retain three review rounds")
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
	if m.Get().Review.Rounds != 3 {
		t.Fatal("missing review section must keep the default rounds")
	}
}

// 作者显式关掉时必须被尊重：默认值只在"没写"的时候生效。
func TestReviewLegacySplitSettingIsIgnored(t *testing.T) {
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
	if m.Get().Review.Rounds != 3 {
		t.Fatal("legacy splitByChapter must not change the fixed per-chapter mode")
	}
}

// 书籍目录：用户选了就持久化；没选就跟随平台默认。存盘的 libraryRoot 只是
// 上一次的解析结果，不能被信任——否则换了宿主或 iCloud 目录出现后仍然读旧路径。
func TestLibraryRootFollowsCustomChoice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NOVEL_HOME", home)
	t.Setenv("NOVEL_LIBRARY", "")
	t.Setenv("NOVEL_SYNC_DIR", "")
	custom := filepath.Join(t.TempDir(), "my-books")

	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if m.Get().LibraryRoot != runtimepaths.DefaultLibraryRoot() {
		t.Fatalf("fresh settings should follow the platform default")
	}
	if err := m.SetLibraryRoot(custom); err != nil {
		t.Fatal(err)
	}
	if got := m.Get().LibraryRoot; got != custom {
		t.Fatalf("libraryRoot=%q want %q", got, custom)
	}

	// Reload: the explicit choice survives.
	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get().LibraryRoot; got != custom {
		t.Fatalf("reloaded libraryRoot=%q want the persisted choice %q", got, custom)
	}

	// Clearing the choice returns to the platform default.
	if err := reloaded.SetLibraryRoot(""); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get().LibraryRoot; got != runtimepaths.DefaultLibraryRoot() {
		t.Fatalf("libraryRoot=%q want the default after reset", got)
	}
}

// 存盘的 libraryRoot 是上一次的解析结果；若拿它当真，默认目录变化后（macOS
// 出现 iCloud 容器）就永远停在旧目录。只有 customLibraryRoot 是可以信任的选择。
func TestStoredLibraryRootIsRecomputedOnLoad(t *testing.T) {
	home := t.TempDir()
	t.Setenv("NOVEL_HOME", home)
	t.Setenv("NOVEL_LIBRARY", "")
	t.Setenv("NOVEL_SYNC_DIR", "")
	path := runtimepaths.SettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(t.TempDir(), "old-location")
	if err := os.WriteFile(path, []byte(`{"libraryRoot":"`+stale+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Get().LibraryRoot; got == stale {
		t.Fatalf("stale libraryRoot %q was trusted", stale)
	}
	if got, want := m.Get().LibraryRoot, runtimepaths.DefaultLibraryRoot(); got != want {
		t.Fatalf("libraryRoot=%q want the default %q", got, want)
	}
}

// 目录变更不影响设置同步的合并时钟：路径是本机事实，不该被推到别的设备。
func TestSetLibraryRootDoesNotStampSyncClock(t *testing.T) {
	t.Setenv("NOVEL_HOME", t.TempDir())
	t.Setenv("NOVEL_LIBRARY", "")
	t.Setenv("NOVEL_SYNC_DIR", "")
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	before := m.Get().UpdatedAt
	if err := m.SetLibraryRoot(filepath.Join(t.TempDir(), "elsewhere")); err != nil {
		t.Fatal(err)
	}
	if got := m.Get().UpdatedAt; got != before {
		t.Fatalf("updatedAt=%q want unchanged %q", got, before)
	}
}
