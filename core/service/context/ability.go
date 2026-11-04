package contextsvc

import (
	"strings"
)

// ExtractAbilityLimits 把「能力/资源的代价与上限」蒸馏成写作与审校可用的块。
//
// 这是横向设定层最大的一块空洞：canon/world.md 与 canon/goldfinger.md 只在
// 立项（reinit）时注入，写作层只有锁词与信息边界，没有任何能力上限。于是
// 「不能读心、不能控体、不能隔空得到信息、不能直接宣布凶手」这些硬规则
// 在写正文时根本不在场，本地硬门也不查金手指写穿。
//
// 只取判断所需的两类信息：能力是什么、它的代价与上限是什么。势力地图、
// 区划、人物归属属于规划层，放进写作包只会稀释注意力。
func ExtractAbilityLimits(worldRaw, goldfingerRaw string) string {
	var rows []string
	rows = append(rows, goldfingerLimitRows(goldfingerRaw)...)
	rows = append(rows, worldLimitRows(worldRaw)...)
	if len(rows) == 0 {
		return "未提取到能力规则：不得自行发明能力、代价或例外；需要能力解题时先补全设定。"
	}
	return "### 能力与资源的上限（完整条目，越界即违规）\n" + strings.Join(rows, "\n") + "\n能力只能按其代价、上限和例外使用；没有代价的解决方案视为违规。"
}

// goldfingerLimitRows 读 canon/goldfinger.md 的能力表。
//
// 列约定支持两种现有表头；按表头定位，不假设例外或代价固定在某列。
// 「最近使用」是执行记录，写的时候用不上；「谁能使用」属于人物分工，卡里已有。
func goldfingerLimitRows(raw string) []string {
	var out []string
	columns := map[string]int{}
	for _, line := range strings.Split(raw, "\n") {
		s := strings.TrimSpace(line)
		if !strings.HasPrefix(s, "|") || strings.HasPrefix(s, "|---") {
			continue
		}
		cols := splitTableRow(s)
		if len(cols) == 0 {
			continue
		}
		if strings.Contains(cols[0], "资源") || strings.Contains(cols[0], "能力") {
			columns = map[string]int{}
			for i, col := range cols {
				switch {
				case strings.Contains(col, "代价"):
					columns["cost"] = i
				case strings.Contains(col, "上限"):
					columns["limit"] = i
				case strings.Contains(col, "例外") || strings.Contains(col, "限制") || strings.Contains(col, "备注"):
					columns["exception"] = i
				}
			}
			continue
		}
		if len(columns) == 0 {
			// 旧模板没有“谁能使用”，第二列是代价、第三列是上限。
			columns = map[string]int{"cost": 1, "limit": 2, "exception": 4}
			if len(cols) >= 6 {
				columns = map[string]int{"cost": 2, "limit": 3, "exception": 4}
			}
		}
		var parts []string
		for _, item := range []struct{ key, label string }{{"cost", "代价"}, {"limit", "上限"}, {"exception", "例外/限制"}} {
			i, ok := columns[item.key]
			if ok && i < len(cols) && cols[i] != "" && cols[i] != "—" && cols[i] != "-" {
				parts = append(parts, item.label+"="+cols[i])
			}
		}
		if len(parts) > 0 {
			out = append(out, "- "+cols[0]+"："+strings.Join(parts, "，"))
		}
	}
	return out
}

// worldLimitRows 读 canon/world.md 的「力量 / 规则」小节。
func worldLimitRows(raw string) []string {
	var body string
	for _, heading := range []string{"## 力量 / 规则", "## 力量/规则", "## 力量与规则", "## 世界规则", "## 规则与代价"} {
		if body = mdSection(raw, heading); strings.TrimSpace(body) != "" {
			break
		}
	}
	if strings.TrimSpace(body) == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(body, "\n") {
		s := strings.TrimSpace(line)
		if !strings.HasPrefix(s, "-") {
			continue
		}
		s = strings.TrimSpace(strings.TrimPrefix(s, "-"))
		if s == "" {
			continue
		}
		out = append(out, "- "+clipRunes(s, 90))
	}
	return out
}

func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, "`")
		out = append(out, p)
	}
	return out
}
