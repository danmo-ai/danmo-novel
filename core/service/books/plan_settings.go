package books

// PlanSettingsPaths are the setting files produced by 立项 / 重新立项.
// Continuity ledgers and novel-state are intentionally excluded.
var PlanSettingsPaths = []string{
	"book-bible.md",
	"canon/world.md",
	"canon/goldfinger.md",
	"canon/platform-positioning.md",
	"canon/writing-rules.md",
	"canon/style-fingerprint.md",
	"canon/reveal-schedule.md",
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
