package books

import "sort"

// PlanSettingsPaths are the setting files produced by 立项 / 重新立项.
// Continuity ledgers and novel-state are intentionally excluded.
var PlanSettingsPaths = []string{
	"book-bible.md",
	"canon/world.md",
	"canon/goldfinger.md",
	"canon/platform-positioning.md",
	"canon/writing-rules.md",
	"canon/style-fingerprint.md",
	ForeshadowSchedulePath,
	"canon/author-lore.md",
	"canon/locked-terms.yaml",
}

// IsPlanSettingsPath reports whether rel is an allowed plan settings target.
func IsPlanSettingsPath(rel string) bool {
	for _, p := range PlanSettingsPaths {
		if p == rel {
			return true
		}
	}
	return false
}

// sortedStringKeys 把 map 的键排成稳定顺序，供遍历 map 生成文本/文件的路径使用。
//
// Go 的 map 迭代顺序是随机的：遍历它拼提示词、拼校验报错或追加 YAML 字段，
// 同一份输入每次都会得到不同字节序。提示词会因此丢掉前缀缓存，写盘的文件会
// 让 computeRevision 漂移（同内容不同 revision，下游缓存全部失效）。
func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
