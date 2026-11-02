package books

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"gopkg.in/yaml.v3"
)

type planCast struct {
	Stem            string                  `json:"stem"`
	Name            string                  `json:"name"`
	Role            string                  `json:"role"`
	Tagline         string                  `json:"tagline"`
	CardMarkdown    string                  `json:"cardMarkdown"`
	InitialIdentity *domain.IdentitySegment `json:"initialIdentity"`
}

// UnitSeed is one 单元 entity of a volume outline, reduced to what the unit
// contract needs. 卷纲接受时由它播种 outline/units/*.yaml。
type UnitSeed struct {
	UnitID   string                 `json:"unitId"`
	Title    string                 `json:"title"`
	From     int                    `json:"from"`
	To       int                    `json:"to"`
	Function string                 `json:"function"`
	Entity   *domain.MarkdownEntity `json:"-"`
}

func SeedsFromStructuredVolume(doc domain.StructuredMarkdown) []UnitSeed {
	out := []UnitSeed{}
	for _, e := range doc.EntitiesOf("单元") {
		from, to := ChapterWindow(e.Value("chapter_range"))
		if from <= 0 || to < from {
			continue
		}
		entity := e
		out = append(out, UnitSeed{UnitID: e.ID, Title: e.Value("title"), From: from, To: to, Function: e.Value("objective"), Entity: &entity})
	}
	return out
}

// AcceptPlan writes the approved plan after explicit author acceptance.
// Mode controls write scope:
//   - init/reinit: book outline + whitelist settings + cast in candidate only
//   - volume: current volume outline + missing unit skeletons only
//
// Never deletes or overwrites: other volumes, existing unit contracts, prose,
// continuity/, identity timeline, unlisted cast, or production cursors.
func AcceptPlan(root string, cand domain.Candidate) error {
	mode := strings.TrimSpace(cand.Mode)
	if mode == "" {
		if len(cand.PlanSettings) > 0 || strings.TrimSpace(cand.PlanMarkdown["book_outline"]) != "" {
			mode = domain.PlanModeInit
		} else {
			mode = domain.PlanModeVolume
		}
	}
	full := mode == domain.PlanModeInit || mode == domain.PlanModeReinit
	if full {
		return acceptPlanBook(root, cand)
	}
	return acceptPlanVolume(root, cand)
}

func acceptPlanBook(root string, cand domain.Candidate) error {
	if strings.TrimSpace(cand.PlanMarkdown["book_outline"]) == "" {
		return fmt.Errorf("立项缺少全书总纲")
	}
	for _, path := range PlanSettingsPaths {
		if strings.TrimSpace(cand.PlanSettings[path]) == "" {
			return fmt.Errorf("立项缺少设定: %s", path)
		}
		if !IsPlanSettingsPath(path) {
			return fmt.Errorf("不允许的设定路径: %s", path)
		}
	}
	for path := range cand.PlanSettings {
		if !IsPlanSettingsPath(path) {
			return fmt.Errorf("不允许的设定路径: %s", path)
		}
	}

	var cast []planCast
	_ = json.Unmarshal([]byte(cand.PlanMarkdown["cast_json"]), &cast)
	for i := range cast {
		cast[i].Role = normalizeRole(cast[i].Role)
	}
	hasProtagonist := false
	for _, c := range cast {
		if c.Role == "protagonist" {
			hasProtagonist = true
			break
		}
	}
	if !hasProtagonist {
		return fmt.Errorf("立项必须包含 protagonist")
	}

	preserved := map[string]bool{}
	for _, p := range cand.PreservePaths {
		p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
		if p != "" {
			preserved[p] = true
		}
	}
	writes := map[string][]byte{}
	if !preserved["outline/book_outline.md"] {
		doc := ParseStructuredMarkdown(cand.PlanMarkdown["book_outline"], BookOutlineProtocol)
		if !doc.Valid() {
			return fmt.Errorf("总纲未通过结构校验：%s", FormatOutlineDiagnostics(doc.Diagnostics))
		}
		snap, err := LoadBook(root, false)
		if err != nil {
			return err
		}
		if snap.Files == nil {
			snap.Files = map[string]string{}
		}
		for path, body := range cand.PlanSettings {
			snap.Files[path] = body
		}
		revealDoc := ParseStructuredMarkdown(cand.PlanSettings["canon/reveal-schedule.md"], "reveal-schedule/v1")
		snap.StructuredOutlines["canon/reveal-schedule.md"] = &revealDoc
		if err := ValidateStructuredOutlineFile(snap, "outline/book_outline.md", cand.PlanMarkdown["book_outline"]); err != nil {
			return err
		}
		writes["outline/book_outline.md"] = []byte(cand.PlanMarkdown["book_outline"])
	}
	if err := ValidateRevealSchedule(cand.PlanSettings["canon/reveal-schedule.md"]); err != nil {
		return err
	}
	for path, body := range cand.PlanSettings {
		if preserved[path] {
			continue
		}
		writes[path] = []byte(body)
	}
	for _, c := range cast {
		if err := ValidateCastCardContract(domain.CastCard{Stem: c.Stem, Name: strings.TrimSpace(c.Name), Role: c.Role, Status: domain.CastCanon}); err != nil {
			return err
		}
		rel := "canon/cast/" + c.Stem + ".md"
		if preserved[rel] {
			continue
		}
		writes[rel] = []byte(renderCastCard(c))
	}
	identity, err := initialIdentitiesFromCast(cast)
	if err != nil {
		return err
	}
	// 重新立项只负责补登缺失的初始身份，绝不重写已有段落。
	// 一次 reinit 把整份时间线换成"每人都从 v01-U1 开始"，等于把温念慈从
	// 专案组刑警退回内勤新警；这类历史只能由作者或单元接受推进。
	if !preserved[IdentityTimelinePath] {
		existing, err := LoadIdentityTimeline(root)
		if err != nil {
			return err
		}
		identityBody, err := MarshalIdentityTimeline(mergeInitialIdentities(existing, identity))
		if err != nil {
			return err
		}
		writes[IdentityTimelinePath] = identityBody
	}
	return commitPlanWrites(root, writes, map[string]string{
		"stage": "planning", "next_action": "plan",
	})
}

func initialIdentitiesFromCast(cast []planCast) ([]domain.IdentitySegment, error) {
	out := []domain.IdentitySegment{}
	for _, c := range cast {
		if c.InitialIdentity == nil {
			return nil, fmt.Errorf("人物 %s 缺少 initialIdentity（职务/机构/起始单元）", c.Stem)
		}
		identity := *c.InitialIdentity
		identity.Stem = c.Stem
		if identity.FromUnit == "" {
			identity.FromUnit = "v01-U1"
		}
		if identity.Note == "" {
			identity.Note = "初始身份"
		}
		out = append(out, identity)
	}
	return out, nil
}

// mergeInitialIdentities keeps every existing segment and adds 立项 initial
// identities only for cast members that have no history yet.
//
// 追加而不覆盖是有意的：已存在的身份段是"这个人已经走到哪"的既成事实，
// 而 initialIdentity 只描述"他从哪起步"。两者冲突时必须以既有段落为准，
// 否则重新立项会让所有晋升被静默作废。
func mergeInitialIdentities(existing, initial []domain.IdentitySegment) []domain.IdentitySegment {
	out := append([]domain.IdentitySegment(nil), existing...)
	covered := map[string]bool{}
	for _, s := range existing {
		covered[s.Stem] = true
	}
	for _, s := range initial {
		if covered[s.Stem] {
			continue
		}
		covered[s.Stem] = true
		out = append(out, s)
	}
	return out
}

func identityChangesFromVolume(snap domain.BookSnapshot, doc domain.StructuredMarkdown, seeds []UnitSeed) ([]domain.IdentitySegment, error) {
	segments := append([]domain.IdentitySegment(nil), snap.IdentitySegments...)
	units := append([]domain.UnitInfo(nil), snap.Units...)
	for _, seed := range seeds {
		found := false
		for _, u := range units {
			if u.UnitID == seed.UnitID {
				found = true
				break
			}
		}
		if !found {
			units = append(units, domain.UnitInfo{UnitID: seed.UnitID})
		}
	}
	changes := doc.EntitiesOf("身份变更")
	for _, e := range changes {
		var err error
		segments, err = ApplyIdentityChange(segments, domain.IdentitySegment{Stem: e.Value("stem"), RoleTitle: e.Value("role_title"), Org: e.Value("org"), FromUnit: e.Value("from_unit"), Note: e.Value("reason")}, units)
		if err != nil {
			return nil, err
		}
	}
	return segments, nil
}

func acceptPlanVolume(root string, cand domain.Candidate) error {
	if cand.VolumeID == "" {
		return fmt.Errorf("missing volume id")
	}
	if len(cand.PlanSettings) > 0 {
		return fmt.Errorf("重写卷纲不得写入设定")
	}
	outline := cand.PlanMarkdown["volume_outline"]
	if strings.TrimSpace(outline) == "" {
		return fmt.Errorf("volume outline is empty")
	}
	doc := ParseStructuredMarkdown(outline, VolumeOutlineProtocol)
	if doc.Legacy {
		return fmt.Errorf("旧格式卷纲不进入生产链路，请先通过 Agent 批量迁移")
	}
	snap, err := LoadBook(root, false)
	if err != nil {
		return err
	}
	if err := ValidateStructuredOutlineFile(snap, "outline/volumes/"+cand.VolumeID+".md", outline); err != nil {
		return err
	}
	identity, err := identityChangesFromVolume(snap, doc, SeedsFromStructuredVolume(doc))
	if err != nil {
		return err
	}
	identityBody, err := MarshalIdentityTimeline(identity)
	if err != nil {
		return err
	}
	writes := map[string][]byte{
		"outline/volumes/" + cand.VolumeID + ".md": []byte(outline),
		IdentityTimelinePath:                       identityBody,
	}
	seeds := SeedsFromStructuredVolume(doc)
	if len(seeds) == 0 {
		return fmt.Errorf("卷纲没有可识别的单元索引；请使用结构化单元实体或 unit_id 表")
	}

	for _, s := range seeds {
		rel := "outline/units/" + s.UnitID + ".yaml"
		contract := UnitContractForSeed(s, splitList(doc.Root.Value("cast")), identity)
		// 只校验真正排进本单元场上的角色。整卷人物里包含尚未登场的角色
		// （例如身份段从 v01-U3 才开始的人），按整卷人物校验会把每个单元都判失败。
		if onStage, ok := contract["on_stage"].([]string); ok {
			if err := ValidateIdentityCoverage(identity, snap.Cast, domain.UnitInfo{UnitID: s.UnitID}, onStage); err != nil {
				return err
			}
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(abs); err == nil {
			// Never overwrite existing unit contracts (prose progress preserved).
			continue
		}
		body, err := yaml.Marshal(contract)
		if err != nil {
			return err
		}
		writes[rel] = body
	}
	return commitPlanWrites(root, writes, map[string]string{
		"stage": "outlining", "next_action": "outline",
	})
}

// UnitContractForSeed builds the proposed unit contract for one volume seed.
//
// 卷纲已经写了目标/阻碍/尝试/缺口/选择/代价/结果/状态迁移/揭示边界，这里只做
// 字段搬运，不重新创作——细纲阶段作者只补逐章场面。抽成导出函数是为了让
// 「卷纲已在盘上但单元没播种」的修复能复用同一份契约，不再各自拼一遍 YAML。
//
// on_stage 只放「在本单元已有生效公开身份」的卷内人物：卷纲的 cast 是「本卷
// 出场人物」，不等于「每个单元都出场」，直接摊到每个单元会让身份覆盖校验把
// 尚未登场的角色也算进来（身份段从 v01-U3 才开始的人，在 v01-U1 被要求登记）。
func UnitContractForSeed(s UnitSeed, volumeCast []string, segments []domain.IdentitySegment) map[string]any {
	contract := map[string]any{
		"unit_id": s.UnitID, "status": "proposed", "title_working": s.Title,
		"function": s.Function, "chapter_range": []int{s.From, s.To},
		"desire": "", "obstacle": "", "choice": "", "payoff": "",
		"on_stage": []string{}, "pov": "", "scenes": []any{},
		"next_hook": map[string]any{"type": ""},
		"story_day": "", "gap_from_prev": "", "info_control": "",
	}
	onStage := make([]string, 0, len(volumeCast))
	for _, stem := range volumeCast {
		if IdentityAt(segments, stem, s.UnitID) != nil {
			onStage = append(onStage, stem)
		}
	}
	if len(onStage) > 0 {
		contract["on_stage"] = onStage
		contract["pov"] = onStage[0]
	}
	if s.Entity == nil {
		return contract
	}
	contract["source_seed_id"] = s.Entity.ID
	contract["desire"] = s.Entity.Value("objective")
	contract["obstacle"] = s.Entity.Value("obstacle")
	contract["choice"] = s.Entity.Value("choice")
	contract["payoff"] = s.Entity.Value("outcome")
	contract["attempt"] = s.Entity.Value("attempt")
	contract["setback"] = s.Entity.Value("setback")
	contract["cost"] = s.Entity.Value("cost")
	contract["entry_cause"] = s.Entity.Value("entry_cause")
	contract["exit_result"] = s.Entity.Value("exit_result")
	contract["story_day"] = s.Entity.Value("clock_start_day")
	contract["end_day"] = s.Entity.Value("clock_end_day")
	contract["gap_days"] = s.Entity.Value("clock_gap_days")
	contract["clock_deadline"] = s.Entity.Value("clock_deadline")
	contract["timeline_id"] = s.Entity.Value("timeline_id")
	contract["state_changes"] = s.Entity.Value("state_changes")
	contract["state_deltas"] = stateDeltasFromMarkdown(s.Entity.Value("state_changes"))
	contract["thread_ids"] = splitList(s.Entity.Value("thread_ids"))
	if hook := strings.SplitN(s.Entity.Value("next_hook"), "|", 2); len(hook) == 2 {
		contract["next_hook"] = map[string]any{"type": strings.TrimSpace(hook[0]), "out": strings.TrimSpace(hook[1])}
	}
	return contract
}

func commitPlanWrites(root string, writes map[string][]byte, state map[string]string) error {
	stamp := time.Now().UTC().Format("20060102-150405")
	backupRoot := filepath.Join(root, "backups", "pre-accept-"+stamp)
	stageRoot := filepath.Join(backupRoot, "staging")
	if err := os.MkdirAll(stageRoot, 0o755); err != nil {
		return err
	}
	for rel, body := range writes {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if raw, err := os.ReadFile(abs); err == nil {
			bak := filepath.Join(backupRoot, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(bak), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(bak, raw, 0o644); err != nil {
				return err
			}
		}
		staged := filepath.Join(stageRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(staged, body, 0o644); err != nil {
			return err
		}
	}
	for rel, body := range writes {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(abs, body, 0o644); err != nil {
			return err
		}
	}
	_ = os.RemoveAll(stageRoot)
	return updateState(root, state)
}

func renderCastCard(c planCast) string {
	name, note := splitNameTagline(c.Name)
	tagline := strings.TrimSpace(c.Tagline)
	if tagline == "" {
		tagline = note
	}
	md := c.CardMarkdown
	if md == "" {
		md = fmt.Sprintf("# %s\n\n`status`: canon\n`role`: %s\n", name, c.Role)
	} else if strings.HasPrefix(md, "#") {
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
	return md
}

func ParseUnitSeeds(md, volumeID string) []UnitSeed {
	var out []UnitSeed
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
		out = append(out, UnitSeed{UnitID: id, Title: cells[2], Function: fn, From: from, To: to})
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
