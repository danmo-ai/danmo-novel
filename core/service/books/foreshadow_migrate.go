package books

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// ForeshadowMigrationReport summarizes a one-off reveal→foreshadow migration.
type ForeshadowMigrationReport struct {
	Created        int      // 生成的伏笔条目数
	Skipped        int      // 无法映射的揭示条目数
	FilesRewritten []string // 删掉 reveal_ids / forbidden_reveal_ids 的 outline 文件
}

// MigrateRevealToForeshadow 把 canon/reveal-schedule.md 平移成
// canon/foreshadow-schedule.md，并清掉总纲/卷纲/单元里的两个旧引用字段。
//
// 为什么是「平移」而不是「并存」：83 条揭示里 81 条的 hint_unit_ids 与
// confirm_unit_id 不同，它本来就在做单元级埋收；留着两份登记只会让同一事实
// 有两处权威，冲突时无从判断谁对。
//
// 幂等：目标文件已存在则不覆盖（避免二次运行把作者后来的细化写回旧值）。
func MigrateRevealToForeshadow(root string) (ForeshadowMigrationReport, error) {
	var rep ForeshadowMigrationReport

	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(ForeshadowSchedulePath))); err == nil {
		// 目标已存在，只做引用清理。
		rewritten, err := stripRevealRefs(root)
		rep.FilesRewritten = rewritten
		return rep, err
	}

	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash("canon/reveal-schedule.md")))
	if err != nil {
		// 没有揭示计划：写一份空登记表，让后续校验有对象可查。
		if err := writeFileIfAbsent(root, ForeshadowSchedulePath, emptyForeshadowSchedule()); err != nil {
			return rep, err
		}
		rewritten, err := stripRevealRefs(root)
		rep.FilesRewritten = rewritten
		return rep, err
	}

	doc := ParseStructuredMarkdown(string(raw), ForeshadowProtocol)
	if doc.Legacy || len(doc.EntitiesOf("揭示")) == 0 {
		// 非结构化揭示计划（自由表格 / 散文）从来不是本应用可解析的形态：
		// 它没有 ## 揭示 实体，只有人类可读的表格。不做格式兼容，直接归档原文、
		// 建一份空登记表，由作者按新 schema 重登记。归档保证原文不丢。
		if err := archiveFile(root, RevealSchedulePathLegacy); err != nil {
			return rep, err
		}
		if err := writeFileIfAbsent(root, ForeshadowSchedulePath, emptyForeshadowSchedule()); err != nil {
			return rep, err
		}
		rewritten, err := stripRevealRefs(root)
		rep.FilesRewritten = rewritten
		rep.Skipped = len(doc.EntitiesOf("揭示"))
		return rep, err
	}

	var b strings.Builder
	b.WriteString("# 伏笔登记表（唯一权威来源）\n- schema_version: 1\n\n")
	b.WriteString("# 由 canon/reveal-schedule.md 平移而来：信息类伏笔即原揭示条目。\n")
	b.WriteString("# 章级 plant_chapter / payoff_chapter 留空，等细纲重跑时补齐。\n")
	segs := doc.EntitiesOf("揭示")
	for i, e := range segs {
		id := fmt.Sprintf("FS-%02d", i+1)
		// 语义对应关系（原揭示计划的三个位置字段）：
		//   hint_unit_ids   = 可以暗示/埋下的单元，**早于** earliest_volume
		//   earliest_volume = 最早可以确认的卷 → payoff_volume
		//   confirm_unit_id = 确认/回收的单元   → payoff_unit
		// 早期版本把 earliest_volume 同时当成埋入卷，于是 hint 在 v05、earliest 在
		// v06 的条目会被判成「单元不在所属卷内」。
		plantUnit := firstListValue(e.Value("hint_unit_ids"))
		payoffUnit := strings.TrimSpace(e.Value("confirm_unit_id"))
		// 回收卷优先从 confirm_unit_id 推导：它是「实际在哪确认」的具体位置，
		// 而 earliest_volume 是规划期写的「最早可确认卷」，两者在存量数据里会
		// 不一致（实测 r14 写着 v03，但 confirm 是 v04-U8）。具体位置更可信，
		// 否则会产出「回收单元不在回收卷内」的自相矛盾条目。
		payoffVol := volumeOfUnit(payoffUnit)
		if payoffVol == "" {
			payoffVol = strings.TrimSpace(e.Value("earliest_volume"))
		}
		plantVol := volumeOfUnit(plantUnit)
		if plantVol == "" {
			plantVol = payoffVol
		}
		if plantVol == "" || payoffVol == "" {
			rep.Skipped++
			continue
		}
		// 旧状态的 locked 就等于「还没埋下」。
		status := CanonicalForeshadowStatus(e.Value("status"))
		if status == "" {
			status = "planned"
		}
		fmt.Fprintf(&b, "## 伏笔 · %s\n", id)
		fmt.Fprintf(&b, "- content: %s\n", quoteYAML(strings.TrimSpace(e.Value("public_summary"))))
		fmt.Fprintf(&b, "- type: 信息\n")
		fmt.Fprintf(&b, "- plant_volume: %s\n", plantVol)
		if plantUnit != "" {
			fmt.Fprintf(&b, "- plant_unit: %s\n", plantUnit)
		}
		fmt.Fprintf(&b, "- payoff_volume: %s\n", payoffVol)
		if payoffUnit != "" {
			fmt.Fprintf(&b, "- payoff_unit: %s\n", payoffUnit)
		}
		fmt.Fprintf(&b, "- payoff_summary: %s\n", quoteYAML(strings.TrimSpace(e.Value("public_summary"))))
		if terms := strings.TrimSpace(e.Value("locked_terms")); terms != "" && terms != "无" {
			fmt.Fprintf(&b, "- locked_terms: %s\n", quoteYAML(terms))
		}
		fmt.Fprintf(&b, "- status: %s\n", status)
		fmt.Fprintf(&b, "- note: 迁移自揭示计划 %s（hint=%s）\n\n", e.ID, strings.TrimSpace(e.Value("hint_unit_ids")))
		rep.Created++
	}

	if err := writeFileIfAbsent(root, ForeshadowSchedulePath, b.String()); err != nil {
		return rep, err
	}
	rewritten, err := stripRevealRefs(root)
	rep.FilesRewritten = rewritten
	return rep, err
}

func emptyForeshadowSchedule() string {
	return "# 伏笔登记表（唯一权威来源）\n- schema_version: 1\n\n"
}

// volumeOfUnit 从 v05-U3 取出 v05；解不出返回空串。
func volumeOfUnit(unitID string) string {
	m := unitIDRe.FindStringSubmatch(strings.TrimSpace(unitID))
	if m == nil {
		return ""
	}
	v, _ := strconv.Atoi(m[1])
	if v <= 0 {
		return ""
	}
	return fmt.Sprintf("v%02d", v)
}

func firstListValue(raw string) string {
	items := splitList(raw)
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

// stripRevealRefs 删除 outline 文件里的 reveal_ids / forbidden_reveal_ids。
//
// 这两个字段由伏笔登记表的回收位置派生（ForeshadowUnitRefs），不再是输入。
// 留着它们只会让同一事实有两处权威。
func stripRevealRefs(root string) ([]string, error) {
	var rewritten []string
	for _, dir := range []string{"outline", "outline/volumes", "outline/units"} {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") && !strings.HasSuffix(strings.ToLower(e.Name()), ".yaml") {
				continue
			}
			rel := dir + "/" + e.Name()
			full := filepath.Join(root, filepath.FromSlash(rel))
			raw, err := os.ReadFile(full)
			if err != nil {
				continue
			}
			cleaned := dropLinesWithPrefixes(string(raw), []string{"reveal_ids:", "forbidden_reveal_ids:"})
			if cleaned == string(raw) {
				continue
			}
			if err := backupAndWrite(root, rel, cleaned); err != nil {
				return rewritten, err
			}
			rewritten = append(rewritten, rel)
		}
	}
	sort.Strings(rewritten)
	return rewritten, nil
}

// dropLinesWithPrefixes 删除以给定前缀开头的行（含其缩进块）。
func dropLinesWithPrefixes(raw string, prefixes []string) string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	skippingIndent := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		match := false
		for _, p := range prefixes {
			if strings.HasPrefix(trimmed, p) {
				match = true
				break
			}
		}
		if match {
			skippingIndent = len(line) - len(strings.TrimLeft(line, " \t"))
			continue
		}
		if skippingIndent >= 0 {
			indent := len(line) - len(strings.TrimLeft(line, " \t"))
			if trimmed == "" || indent > skippingIndent {
				continue
			}
			skippingIndent = -1
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func writeFileIfAbsent(root, rel, body string) error {
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(full); err == nil {
		return nil
	}
	return os.WriteFile(full, []byte(body), 0o644)
}

// backupAndWrite 把原文件备份到 backups/pre-migrate-<stamp>/ 再写入新内容。
func backupAndWrite(root, rel, body string) error {
	full := filepath.Join(root, filepath.FromSlash(rel))
	if raw, err := os.ReadFile(full); err == nil {
		stamp := time.Now().UTC().Format("20060102-150405")
		bak := filepath.Join(root, "backups", "pre-migrate-"+stamp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(bak), 0o755); err == nil {
			_ = os.WriteFile(bak, raw, 0o644)
		}
	}
	return os.WriteFile(full, []byte(body), 0o644)
}

// ApplyForeshadowAssignments updates only the requested planning level.
// Callers run all semantic validation before entering this write primitive.
func RenderForeshadowAssignments(root string, assignments []domain.ForeshadowAssignment) ([]byte, error) {
	snap, err := LoadBook(root, false)
	if err != nil {
		return nil, err
	}
	raw := snap.Files[ForeshadowSchedulePath]
	if strings.TrimSpace(raw) == "" && len(assignments) > 0 {
		return nil, fmt.Errorf("伏笔登记表不存在，不能细化位置")
	}
	out := raw
	registry := ForeshadowByID(snap)
	for _, a := range assignments {
		f, ok := registry[a.ID]
		if !ok {
			return nil, fmt.Errorf("伏笔 %s 不存在", a.ID)
		}
		// 本次调用可能同时给出单元与章号，而 registry 是写入前的快照。章级校验
		// 必须看"这次写下去之后的归属"，否则第一次给某条伏笔补章号（登记表里
		// plant_unit 还空着，全是注释里写明的常态）会被判成"尚未分配埋入单元"，
		// 整份细纲在最后一步失败，而前一步刚把单元写进 out。
		plantUnit := a.PlantUnit
		if plantUnit == "" {
			plantUnit = f.PlantUnit
		}
		payoffUnit := a.PayoffUnit
		if payoffUnit == "" {
			payoffUnit = f.PayoffUnit
		}
		if a.PlantUnit != "" {
			if !unitInVolume(a.PlantUnit, f.PlantVolume) {
				return nil, fmt.Errorf("伏笔 %s 的埋入单元不在父级卷内", a.ID)
			}
			out, _ = setForeshadowField(out, a.ID, "plant_unit", a.PlantUnit)
		}
		if a.PayoffUnit != "" {
			if !unitInVolume(a.PayoffUnit, f.PayoffVolume) {
				return nil, fmt.Errorf("伏笔 %s 的回收单元不在父级卷内", a.ID)
			}
			out, _ = setForeshadowField(out, a.ID, "payoff_unit", a.PayoffUnit)
		}
		if a.PlantChapter > 0 {
			if plantUnit == "" {
				return nil, fmt.Errorf("伏笔 %s 尚未分配埋入单元，无法只给章号（需要同时给出埋入单元）", a.ID)
			}
			out, _ = setForeshadowField(out, a.ID, "plant_chapter", strconv.Itoa(a.PlantChapter))
		}
		if a.PayoffChapter > 0 {
			if payoffUnit == "" {
				return nil, fmt.Errorf("伏笔 %s 尚未分配回收单元，无法只给章号（需要同时给出回收单元）", a.ID)
			}
			out, _ = setForeshadowField(out, a.ID, "payoff_chapter", strconv.Itoa(a.PayoffChapter))
		}
	}
	if err := ValidateForeshadowSchedule(out); err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// BackfillForeshadowChapters 把细纲章合同里的 plant/payoff 回填进登记表的章级位置。
//
// 细纲是唯一知道「在第几章埋、第几章收」的层；登记表只记计划。回填后
// ForeshadowForChapter 才能按章给出本章该做什么。
func BackfillForeshadowChapters(root string) (int, error) {
	snap, err := LoadBook(root, false)
	if err != nil {
		return 0, err
	}
	raw, ok := snap.Files[ForeshadowSchedulePath]
	if !ok || strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	doc := ParseStructuredMarkdown(raw, ForeshadowProtocol)
	if doc.Legacy || len(doc.Diagnostics) > 0 {
		return 0, fmt.Errorf("%s 结构错误", ForeshadowSchedulePath)
	}

	type pos struct {
		plantCh   int
		plantUnit string
		payCh     int
		payUnit   string
	}
	positions := map[string]pos{}
	for _, u := range snap.Units {
		planted, paid := ChapterLoops(u)
		for ch, ids := range planted {
			for _, id := range ids {
				p := positions[id]
				if p.plantCh == 0 || ch < p.plantCh {
					p.plantCh, p.plantUnit = ch, u.UnitID
				}
				positions[id] = p
			}
		}
		for ch, ids := range paid {
			for _, id := range ids {
				p := positions[id]
				if p.payCh == 0 || ch < p.payCh {
					p.payCh, p.payUnit = ch, u.UnitID
				}
				positions[id] = p
			}
		}
	}
	if len(positions) == 0 {
		return 0, nil
	}

	updated := 0
	out := raw
	for id, p := range positions {
		if p.plantCh > 0 {
			if next, changed := setForeshadowField(out, id, "plant_chapter", strconv.Itoa(p.plantCh)); changed {
				out = next
				updated++
			}
			if p.plantUnit != "" {
				if next, changed := setForeshadowField(out, id, "plant_unit", p.plantUnit); changed {
					out = next
				}
			}
		}
		if p.payCh > 0 {
			if next, changed := setForeshadowField(out, id, "payoff_chapter", strconv.Itoa(p.payCh)); changed {
				out = next
				updated++
			}
			if p.payUnit != "" {
				if next, changed := setForeshadowField(out, id, "payoff_unit", p.payUnit); changed {
					out = next
				}
			}
		}
	}
	if updated == 0 {
		return 0, nil
	}
	return updated, backupAndWrite(root, ForeshadowSchedulePath, out)
}

// setForeshadowField 在指定实体的块内替换或追加一个字段。
func setForeshadowField(raw, id, field, value string) (string, bool) {
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
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}
	prefix := "- " + field + ":"
	for i := start + 1; i < end; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), prefix) {
			line := "- " + field + ": " + value
			if strings.TrimSpace(lines[i]) == line {
				return raw, false
			}
			lines[i] = line
			return strings.Join(lines, "\n"), true
		}
	}
	insert := []string{"- " + field + ": " + value}
	// 追加到块内最后一个字段之后，而不是下一个标题之前：实体块末尾通常有空行，
	// 直接插在 end 会把新字段落到空行下面，读起来像另一段，回写后这份 canon
	// 文件就永久带一处"孤立的字段"。校验能过，但没人愿意读。
	at := end
	for at > start+1 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	lines = append(lines[:at], append(insert, lines[at:]...)...)
	return strings.Join(lines, "\n"), true
}

// 供迁移报告复用。
var _ = domain.BookSnapshot{}

// quoteYAML 生成 YAML 双引号标量，中文原样保留。
func quoteYAML(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\\`, `\\\\`), `"`, `\\"`) + `"`
}

// RevealSchedulePathLegacy 是已退役的揭示计划路径。
//
// 它只出现在一次性迁移里：读取旧文件、按 schema 平移、归档原文。
// 运行期任何读写路径都不认这个名字——不做兼容，避免又变成第二真源。
const RevealSchedulePathLegacy = "canon/reveal-schedule.md"

// archiveFile 把不再使用的文件挪进 backups/legacy-<stamp>/ 并从原位置删除。
//
// 迁移是唯一的破坏性操作，原文必须留档：即使当前判定它「结构不合规」，
// 里面也可能有作者手写的、尚未登记进新 schema 的伏笔。
func archiveFile(root, rel string) error {
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
