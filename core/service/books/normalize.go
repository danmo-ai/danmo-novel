package books

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// BookNormalizeReport 是一次性数据规整的结果。
//
// 开发期不做运行期兼容：旧字段名、旧文件、旧状态一律由这里一次性改写掉，
// 之后代码只认新形态。留下的每一处「兼容分支」都是将来会分叉的第二真源。
type BookNormalizeReport struct {
	UnitsRenamed   []string       // 改写过 state_deltas 字段名的单元
	VolumesRenamed []string       // 改写过 state_changes 字段名的卷纲
	FieldsRenamed  int            // 改名的 delta 条数
	FieldsDropped  map[string]int // 无法映射而删除的字段名 → 条数
	ChaptersAdded  []string       // 由 word_target 折算补上 chapter_chars 的单元
	StateRepaired  bool           // continuity/state.yaml 是否被改写
	FilesRemoved   []string       // 删除的旧文件
	ConflictRepair []string       // 伏笔登记表的冲突修复记录
}

// NormalizeBookData 把一本书的数据一次性改成当前 schema。
//
// 三件事：
//  1. 单元细纲：state_deltas 的旧字段名改成词表内的名字；无法映射的显式删除并
//     计数（它们此前也是被静默丢弃，只是没人知道）；只写 word_target 的单元补上
//     chapter_chars。
//  2. 连续性账本：injury → health。
//  3. 旧文件：canon/reveal-schedule.md 删除（内容已在伏笔登记表里）。
func NormalizeBookData(root string) (BookNormalizeReport, error) {
	rep := BookNormalizeReport{FieldsDropped: map[string]int{}}

	unitDir := filepath.Join(root, "outline", "units")
	entries, err := os.ReadDir(unitDir)
	if err != nil && !os.IsNotExist(err) {
		return rep, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".yaml") {
			continue
		}
		rel := "outline/units/" + e.Name()
		full := filepath.Join(root, filepath.FromSlash(rel))
		raw, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		next, renamed, dropped := normalizeStateDeltaFields(string(raw))
		for k, v := range dropped {
			rep.FieldsDropped[k] += v
		}
		rep.FieldsRenamed += renamed

		withChars, charsChanged := normalizeChapterChars(next)
		if charsChanged {
			rep.ChaptersAdded = append(rep.ChaptersAdded, strings.TrimSuffix(e.Name(), ".yaml"))
		}
		if withChars == string(raw) {
			continue
		}
		if err := backupAndWrite(root, rel, withChars); err != nil {
			return rep, err
		}
		rep.UnitsRenamed = append(rep.UnitsRenamed, strings.TrimSuffix(e.Name(), ".yaml"))
	}

	// 卷纲种子的 state_changes 与单元 state_deltas 是同一份状态迁移的两层表达：
	// 父层不规整，细纲继承校验就永远对不上（knowledge vs knows 之类）。
	volumeDir := filepath.Join(root, "outline", "volumes")
	if volumeEntries, readErr := os.ReadDir(volumeDir); readErr == nil {
		for _, e := range volumeEntries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
				continue
			}
			rel := "outline/volumes/" + e.Name()
			raw, readErr := os.ReadFile(filepath.Join(volumeDir, e.Name()))
			if readErr != nil {
				continue
			}
			next, renamed, dropped := normalizeStateDeltaFieldsForVolume(string(raw))
			for k, v := range dropped {
				rep.FieldsDropped[k] += v
			}
			rep.FieldsRenamed += renamed
			if next != string(raw) {
				if err := backupAndWrite(root, rel, next); err != nil {
					return rep, err
				}
				rep.VolumesRenamed = append(rep.VolumesRenamed, rel)
			}
		}
	}

	repaired, err := normalizeStateLedger(root)
	if err != nil {
		return rep, err
	}
	rep.StateRepaired = repaired

	if err := removeFileWithBackup(root, "canon/reveal-schedule.md"); err != nil {
		return rep, err
	}
	if _, statErr := os.Stat(filepath.Join(root, "canon", "reveal-schedule.md")); os.IsNotExist(statErr) {
		rep.FilesRemoved = append(rep.FilesRemoved, "canon/reveal-schedule.md")
	}

	conflicts, err := repairForeshadowConflicts(root)
	if err != nil {
		return rep, err
	}
	rep.ConflictRepair = conflicts

	sort.Strings(rep.UnitsRenamed)
	sort.Strings(rep.VolumesRenamed)
	sort.Strings(rep.ChaptersAdded)
	return rep, nil
}

// deltaFieldRe 匹配 YAML 里的一处 field 声明，行内映射与块式都能覆盖。
// RE2 不支持反向引用，因此两侧引号各自可选匹配，改写时统一重新加引号。
var deltaFieldRe = regexp.MustCompile(`(?m)(field:\s*)["']?([A-Za-z_][A-Za-z0-9_]*)["']?`)

// normalizeStateDeltaFieldsForVolume 只做已知别名改名，不删除任何条目。
//
// 卷纲的 state_changes 同时承担两种表达：词表内的状态迁移，以及「证据/关系/
// 立场」这类叙事性状态说明。后者不是账本契约，删掉就是丢作者规划；因此这里
// 只把 knowledge/injury 之类改成词表名，其余原样保留，由继承校验按词表过滤。
func normalizeStateDeltaFieldsForVolume(raw string) (string, int, map[string]int) {
	out, n, d := renameCompactChanges(raw, &map[string]int{}, false)
	return out, n, d
}

// normalizeStateDeltaFields 把旧字段名改成词表内的名字。
//
// 两种写法都要处理，因为它们是同一份数据的两个入口：
//
//   - `state_deltas:` YAML 列表（细纲）
//   - `state_changes: stem.field=前->后; …` 紧凑写法（卷纲种子，细纲未重写时
//     它是唯一的状态来源，实测三本书 164 个单元都在用）
//
// 映射规则按语义，不按词形相似：
//
//	injury / wound             → health
//	position / place           → location
//	knowledge / know           → knows
//	identity / title / role    → role_title
//	status 且新值为死亡语义      → life
//	其它（status 的案件进展、method、evidence…）→ 删除并计数
//
// status 不能一律映射成 life：存量数据里它写的是「落网」「宣判」「保护伞→落马」，
// 那是案件进展，不是人物生命状态。只有「病故」「死亡」这类才真的是 life。
func normalizeStateDeltaFields(raw string) (string, int, map[string]int) {
	renamed := 0
	dropped := map[string]int{}

	out, n, d := renameDeltaBlock(raw, &dropped)
	renamed += n
	for k, v := range d {
		dropped[k] += v
	}
	out, n, d = renameCompactChanges(out, &dropped, true)
	renamed += n
	for k, v := range d {
		dropped[k] += v
	}
	return out, renamed, dropped
}

// renameDeltaBlock 处理 `state_deltas:` 下的 YAML 列表。
func renameDeltaBlock(raw string, dropped *map[string]int) (string, int, map[string]int) {
	renamed := 0
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		m := deltaFieldRe.FindStringSubmatch(line)
		if m == nil {
			out = append(out, line)
			continue
		}
		field := strings.ToLower(m[2])
		target := mapDeltaField(field)
		if target == "" && field == "status" && isDeathWording(line) {
			target = "life"
		}
		if target == "" {
			(*dropped)[field]++
			continue // 整条 delta 删掉：留着也会被识别分支跳过
		}
		if target == field {
			out = append(out, line)
			continue
		}
		out = append(out, strings.Replace(line, m[0], m[1]+target, 1))
		renamed++
	}
	return strings.Join(out, "\n"), renamed, map[string]int{}
}

// renameCompactChanges 处理 `state_changes: stem.field=前->后; …`。
//
// 每一项独立判断：能映射就改名，映射不了就整项删掉。分隔符保留原样，
// 避免把作者写的全角分号改成半角。
func renameCompactChanges(raw string, dropped *map[string]int, dropUnknown bool) (string, int, map[string]int) {
	lines := strings.Split(raw, "\n")
	renamed := 0
	for i, line := range lines {
		s := strings.TrimSpace(line)
		prefix := ""
		if strings.HasPrefix(s, "- state_changes:") {
			prefix = "- "
			s = strings.TrimSpace(strings.TrimPrefix(s, "- "))
		}
		if !strings.HasPrefix(s, "state_changes:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(s, "state_changes:"))
		if value == "" {
			continue
		}
		kept := make([]string, 0, 8)
		for _, item := range splitCompactItems(value) {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			left, _, ok := strings.Cut(item, "=")
			if !ok {
				kept = append(kept, item)
				continue
			}
			stem, field, hasDot := strings.Cut(strings.TrimSpace(left), ".")
			if !hasDot {
				kept = append(kept, item)
				continue
			}
			target := mapDeltaField(strings.ToLower(field))
			if target == "" && strings.EqualFold(field, "status") && isDeathWording(item) {
				target = "life"
			}
			if target == "" {
				if !dropUnknown {
					kept = append(kept, item)
					continue
				}
				(*dropped)[strings.ToLower(field)]++
				continue
			}
			if target == field {
				kept = append(kept, item)
				continue
			}
			kept = append(kept, stem+"."+target+item[len(left):])
			renamed++
		}
		if len(kept) == 0 {
			lines[i] = prefix + "state_changes: \"\""
			continue
		}
		lines[i] = prefix + "state_changes: " + strings.Join(kept, "；")
	}
	return strings.Join(lines, "\n"), renamed, map[string]int{}
}

// splitCompactItems 按半角/全角分号切分紧凑写法。
func splitCompactItems(v string) []string {
	return strings.FieldsFunc(v, func(r rune) bool { return r == ';' || r == '；' })
}

func mapDeltaField(field string) string {
	switch field {
	case "injury", "wound":
		return "health"
	case "position", "place":
		return "location"
	case "knowledge", "know":
		return "knows"
	case "identity", "title", "role":
		return "role_title"
	}
	return domain.CanonicalStateField(field)
}

// isDeathWording 判断一条 status delta 是否其实在说死亡。
func isDeathWording(line string) bool {
	for _, w := range []string{"病故", "死亡", "身亡", "已死", "殉职", "牺牲"} {
		if strings.Contains(line, w) {
			return true
		}
	}
	return false
}

// normalizeChapterChars 统一字数口径为 chapter_chars，并删除 word_target。
//
// word_target 是单元总字数，代码已不再读它（总字数永远是派生值）。留着它
// 会让作者以为改它能影响生产；而 chapter_chars 缺失时又要靠它折算，
// 所以两种情形都在这里一次性了结。
func normalizeChapterChars(raw string) (string, bool) {
	hasChars := strings.Contains(raw, "chapter_chars:")
	wt, hasWT := parseIntField(raw, "word_target")
	if !hasWT {
		return raw, false
	}
	out := raw
	changed := false
	if !hasChars && wt > 0 {
		from, to := ChapterWindow(fieldValue(raw, "chapter_range"))
		chapters := to - from + 1
		if chapters <= 0 {
			chapters = 1
		}
		target := domain.ClampChapterChars(wt / chapters)
		out = strings.TrimRight(dropTopLevelKeys(out, []string{"word_target"}), "\n") +
			"\n" + fmt.Sprintf("chapter_chars: %d\n", target)
		return out, true
	}
	if hasChars {
		out = strings.TrimRight(dropTopLevelKeys(out, []string{"word_target"}), "\n") + "\n"
		changed = out != raw
	}
	return out, changed
}

// dropTopLevelKeys 删除顶层 `key:` 及其缩进块（books 包内的最小实现）。
func dropTopLevelKeys(raw string, keys []string) string {
	drop := map[string]bool{}
	for _, k := range keys {
		drop[k] = true
	}
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		topLevel := line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t")
		if topLevel {
			key := line
			if i := strings.IndexByte(key, ':'); i > 0 {
				key = key[:i]
			}
			skipping = drop[strings.TrimSpace(key)]
			if skipping {
				continue
			}
		}
		if skipping {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// parseIntField 读顶层整数字段。
func parseIntField(raw, key string) (int, bool) {
	v := fieldValue(raw, key)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, false
	}
	return n, true
}

// fieldValue 读顶层 `- key: value` 或 `key: value`。
func fieldValue(raw, key string) string {
	for _, line := range strings.Split(raw, "\n") {
		s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if !strings.HasPrefix(s, key+":") {
			continue
		}
		return strings.Trim(strings.TrimSpace(strings.TrimPrefix(s, key+":")), `"'`)
	}
	return ""
}

// normalizeStateLedger 把 continuity/state.yaml 改成当前 schema。
//
// 两件事：
//   - injury → health（旧键改名）
//   - 删掉 cast 下的 role_title：身份由 identity-timeline 独占，账本不再存副本。
//     实测那份副本 7 段里只有 1 段有值，而且人工修正时间线后不会跟着变，
//     于是同一份写作提示词里同一个人出现两个身份。
func normalizeStateLedger(root string) (bool, error) {
	path := filepath.Join(root, filepath.FromSlash(StatePath))
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, nil
	}
	next := strings.ReplaceAll(string(raw), "injury:", "health:")
	next = dropLedgerRoleTitles(next)
	if next == string(raw) {
		return false, nil
	}
	return true, backupAndWrite(root, StatePath, next)
}

// dropLedgerRoleTitles 删除 cast 段里的 role_title 行，并顺手清掉因此变空的条目。
//
// 只删缩进到位的那一类（`        role_title: …`），顶层 `role_title:` 从不出现在
// 这个文件里，所以按「有缩进 + 键名」判定即可，不需要完整 YAML 往返——
// 往返会重排注释与顺序，破坏 diff 可读性。
//
// 清空条目是必需的：某个人原来只有 role_title 一条状态，删掉后就剩
// `wen-nianci:` 这种空映射，它会在提示词里渲染成一条没有信息的行。
func dropLedgerRoleTitles(raw string) string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "role_title:") && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			continue
		}
		out = append(out, line)
	}
	return dropEmptyMappings(out)
}

// dropEmptyMappings 反复删除「只有键名、没有子项」的行（如 `cast:` / `stem:`）。
func dropEmptyMappings(lines []string) string {
	for {
		removed := false
		next := make([]string, 0, len(lines))
		for i, line := range lines {
			if isEmptyMappingLine(line) && !hasChild(lines, i) {
				removed = true
				continue
			}
			next = append(next, line)
		}
		lines = next
		if !removed {
			return strings.Join(lines, "\n")
		}
	}
}

func isEmptyMappingLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "- ") {
		return false
	}
	return strings.HasSuffix(trimmed, ":")
}

// hasChild 判断第 i 行是否有更深缩进的后继行（跳过空行与注释）。
func hasChild(lines []string, i int) bool {
	indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
	for j := i + 1; j < len(lines); j++ {
		trimmed := strings.TrimSpace(lines[j])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return len(lines[j])-len(strings.TrimLeft(lines[j], " ")) > indent
	}
	return false
}

// repairForeshadowConflicts 修掉登记表里能确定性判定的冲突。
//
// 迁移后仍可能出现的三类冲突，全部就地修正并记录：
//   - status 是旧值（locked 等）→ planned
//   - 回收位置早于埋入位置 → 把回收位置抬到埋入位置同卷
//   - 同单元内回收章不晚于埋入章 → 清掉两个章号，退回单元级，等细纲重填
func repairForeshadowConflicts(root string) ([]string, error) {
	path := filepath.Join(root, filepath.FromSlash(ForeshadowSchedulePath))
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	var notes []string
	out := string(raw)

	// 1. 旧状态。
	out = rewriteStatuses(out, &notes)

	// 2 与 3 需要解析后逐条比对。
	doc := ParseStructuredMarkdown(out, ForeshadowProtocol)
	if doc.Legacy || len(doc.Diagnostics) > 0 {
		return notes, nil
	}
	for _, e := range doc.EntitiesOf(ForeshadowEntityKind) {
		plantVol := volumeNumber(strings.TrimSpace(e.Value("plant_volume")))
		payoffVol := volumeNumber(strings.TrimSpace(e.Value("payoff_volume")))
		if plantVol > 0 && payoffVol > 0 && payoffVol < plantVol {
			out, _ = setForeshadowField(out, e.ID, "payoff_volume", fmt.Sprintf("v%02d", plantVol))
			notes = append(notes, fmt.Sprintf("%s：回收卷早于埋入卷，已抬到 v%02d", e.ID, plantVol))
			payoffVol = plantVol
		}
		if plantVol == 0 || payoffVol != plantVol {
			continue
		}
		pc, _ := strconv.Atoi(strings.TrimSpace(e.Value("plant_chapter")))
		qc, _ := strconv.Atoi(strings.TrimSpace(e.Value("payoff_chapter")))
		if pc > 0 && qc > 0 && qc <= pc {
			out, _ = setForeshadowFieldAllowEmpty(out, e.ID, "plant_chapter", "")
			out, _ = setForeshadowFieldAllowEmpty(out, e.ID, "payoff_chapter", "")
			notes = append(notes, fmt.Sprintf("%s：同单元回收章不晚于埋入章，已退回单元级待细纲重填", e.ID))
		}
	}
	if out == string(raw) {
		return notes, nil
	}
	return notes, backupAndWrite(root, ForeshadowSchedulePath, out)
}

// rewriteStatuses 把不在词表内的 status 一律改写为 planned。
func rewriteStatuses(raw string, notes *[]string) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		s := strings.TrimSpace(line)
		if !strings.HasPrefix(s, "- status:") {
			continue
		}
		v := strings.Trim(strings.TrimSpace(strings.TrimPrefix(s, "- status:")), `"'`)
		if IsForeshadowStatus(v) {
			continue
		}
		lines[i] = "- status: planned"
		*notes = append(*notes, "status "+v+" → planned")
	}
	return strings.Join(lines, "\n")
}

// setForeshadowField 支持清空（value 为空时删除该字段）。
func setForeshadowFieldAllowEmpty(raw, id, field, value string) (string, bool) {
	if value != "" {
		return setForeshadowField(raw, id, field, value)
	}
	return removeForeshadowField(raw, id, field)
}

func removeForeshadowField(raw, id, field string) (string, bool) {
	lines := strings.Split(raw, "\n")
	heading := "## 伏笔 · " + id
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == heading {
			start = i
			break
		}
	}
	if start < 0 {
		return raw, false
	}
	for i := start + 1; i < len(lines); i++ {
		s := strings.TrimSpace(lines[i])
		if strings.HasPrefix(s, "## ") {
			break
		}
		if strings.HasPrefix(s, "- "+field+":") {
			lines = append(lines[:i], lines[i+1:]...)
			return strings.Join(lines, "\n"), true
		}
	}
	return raw, false
}

// removeFileWithBackup 先备份再删除，避免「退役旧文件」变成数据丢失。
//
// 这一步最初直接 os.Remove：对已经迁移完的三本书无害（内容已在伏笔登记表里），
// 但对还没迁移的书就是静默删掉唯一一份数据。
func removeFileWithBackup(root, rel string) error {
	full := filepath.Join(root, filepath.FromSlash(rel))
	raw, err := os.ReadFile(full)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := backupAndWrite(root, rel, string(raw)); err != nil {
		return err
	}
	return os.Remove(full)
}
