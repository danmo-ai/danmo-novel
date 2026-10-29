package books

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"gopkg.in/yaml.v3"
)

type planCast struct {
	Stem         string `json:"stem"`
	Name         string `json:"name"`
	Role         string `json:"role"`
	Tagline      string `json:"tagline"`
	CardMarkdown string `json:"cardMarkdown"`
}

type unitSeed struct {
	UnitID   string `json:"unitId"`
	Title    string `json:"title"`
	From     int    `json:"from"`
	To       int    `json:"to"`
	Function string `json:"function"`
}

// AcceptPlan writes the approved plan and seeds proposed unit contracts.
// It is called only after explicit author acceptance.
func AcceptPlan(root string, cand domain.Candidate) error {
	if cand.VolumeID == "" {
		return fmt.Errorf("missing volume id")
	}
	outline := cand.PlanMarkdown["volume_outline"]
	if strings.TrimSpace(outline) == "" {
		return fmt.Errorf("volume outline is empty")
	}
	if err := os.MkdirAll(filepath.Join(root, "outline", "volumes"), 0o755); err != nil {
		return err
	}
	if md := cand.PlanMarkdown["book_outline"]; md != "" {
		if err := os.WriteFile(filepath.Join(root, "outline", "book_outline.md"), []byte(md), 0o644); err != nil {
			return err
		}
	}
	volPath := filepath.Join(root, "outline", "volumes", cand.VolumeID+".md")
	if err := os.WriteFile(volPath, []byte(outline), 0o644); err != nil {
		return err
	}

	var cast []planCast
	_ = json.Unmarshal([]byte(cand.PlanMarkdown["cast_json"]), &cast)
	castDir := filepath.Join(root, "canon", "cast")
	if err := os.MkdirAll(castDir, 0o755); err != nil {
		return err
	}
	for _, c := range cast {
		if c.Stem == "" || c.Name == "" {
			continue
		}
		md := c.CardMarkdown
		// 姓名与一句话说明分离：name 纯姓名，tagline 单独字段。
		name, note := splitNameTagline(c.Name)
		tagline := strings.TrimSpace(c.Tagline)
		if tagline == "" {
			tagline = note
		}
		if md == "" {
			md = fmt.Sprintf("# %s\n\n`status`: canon\n`role`: %s\n", name, c.Role)
		} else if strings.HasPrefix(md, "#") {
			// Normalize model card heading to the canonical single-H1 pure name.
			lines := strings.Split(md, "\n")
			lines[0] = "# " + name
			md = strings.Join(lines, "\n")
		}
		if tagline != "" && !strings.Contains(md, "`tagline`:") {
			md += fmt.Sprintf("`tagline`: %s\n", tagline)
		}
		md = strings.Replace(md, "`status`: candidate", "`status`: canon", 1)
		if !strings.Contains(md, "`status`:") {
			md += "\n`status`: canon\n"
		}
		if c.Role != "" && !strings.Contains(md, "`role`:") {
			md += fmt.Sprintf("`role`: %s\n", c.Role)
		}
		if err := os.WriteFile(filepath.Join(castDir, c.Stem+".md"), []byte(md), 0o644); err != nil {
			return err
		}
	}

	// role 统一规范为 protagonist / volume_antagonist / recurring，兼容旧值。
	for i := range cast {
		cast[i].Role = normalizeRole(cast[i].Role)
	}

	seeds := ParseUnitSeeds(outline, cand.VolumeID)
	if len(seeds) == 0 {
		return fmt.Errorf("卷纲没有可识别的单元索引；请使用 unit_id / 章范围 / 一句话功能 表")
	}
	unitDir := filepath.Join(root, "outline", "units")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return err
	}
	for _, s := range seeds {
		u := map[string]any{
			"unit_id": s.UnitID, "status": "proposed", "title_working": s.Title,
			"function": s.Function, "chapter_range": []int{s.From, s.To},
			"desire": "", "obstacle": "", "choice": "", "payoff": "",
			"on_stage": []string{}, "pov": "", "scenes": []any{},
			"next_hook": map[string]any{"type": ""},
			"story_day": "", "gap_from_prev": "", "info_control": "",
		}
		b, err := yaml.Marshal(u)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(unitDir, s.UnitID+".yaml"), b, 0o644); err != nil {
			return err
		}
	}
	return updateState(root, map[string]string{
		"stage": "outlining", "next_action": "outline",
	})
}

func ParseUnitSeeds(md, volumeID string) []unitSeed {
	var out []unitSeed
	seen := map[string]bool{}
	for _, line := range strings.Split(md, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") || strings.Contains(line, "---") {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) < 3 {
			continue
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if cells[0] == "" || strings.Contains(cells[0], "unit_id") || strings.EqualFold(cells[0], "unit") || cells[0] == "单元" {
			continue
		}
		id := cells[0]
		if regexp.MustCompile(`^U\d+$`).MatchString(id) {
			id = volumeID + "-" + id
		}
		if !regexp.MustCompile(`(?i)^v\d{2,}-U\d+$`).MatchString(id) {
			continue
		}
		from, to := parseChapterRange(cells[1])
		if from == 0 || seen[id] {
			continue
		}
		seen[id] = true
		fn := cells[2]
		if len(cells) > 3 && fn == "" {
			fn = cells[3]
		}
		out = append(out, unitSeed{UnitID: id, Title: cells[2], Function: fn, From: from, To: to})
	}
	return out
}

func parseChapterRange(s string) (int, int) {
	re := regexp.MustCompile(`(\d+)\s*[-–—~至到]\s*(\d+)`)
	if m := re.FindStringSubmatch(s); len(m) == 3 {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		return a, b
	}
	n, _ := strconv.Atoi(regexp.MustCompile(`\d+`).FindString(s))
	return n, n
}

var roleAnnotationRe = regexp.MustCompile(`\s*[（(][^）)]*(?:protagonist|antagonist|recurring|villain|hero|main|主角|反派|配角)[^）)]*[）)]\s*`)

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "protagonist", "hero", "main":
		return "protagonist"
	case "antagonist", "volume_antagonist", "villain":
		return "volume_antagonist"
	default:
		return "recurring"
	}
}

// SeedCandidateCast creates a skeleton candidate card for a stem discovered
// during outline/write. It never promotes to canon — the author completes the
// card and晋升 in the Bible page. Existing cards are never touched.
func SeedCandidateCast(root, stem string, onStageExisting []string) error {
	if stem == "" {
		return nil
	}
	path := filepath.Join(root, "canon", "cast", stem+".md")
	if _, err := os.Stat(path); err == nil {
		return nil // 已有卡（canon 或 candidate）不覆盖
	}
	md := fmt.Sprintf(`# %s

`+"`status`: candidate"+`
`+"`role`: recurring"+`

> 临时人物：由细纲/写作引入。请补全三锚点、知识边界与台词样例后，
> 在设定页晋升 canon，才能进入正式人物关系与后续单元。
`, stem)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(md), 0o644)
}

func updateState(root string, fields map[string]string) error {
	path := filepath.Join(root, "novel-state.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(b)
	for key, value := range fields {
		re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*.*$`)
		if re.MatchString(text) {
			text = re.ReplaceAllString(text, key+": "+value)
		} else {
			text += "\n" + key + ": " + value + "\n"
		}
	}
	return os.WriteFile(path, []byte(text), 0o644)
}
