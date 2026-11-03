package books

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"gopkg.in/yaml.v3"
)

var (
	unitOutlineRe    = regexp.MustCompile(`(?i)^v(\d+)-U(\d+)\.ya?ml$`)
	unitProseRe      = regexp.MustCompile(`(?i)^v(\d+)-U(\d+)\.md$`)
	volumeRe         = regexp.MustCompile(`(?i)^(?:volume|vol|v)0*(\d+)`)
	volumeTagPattern = regexp.MustCompile(`^v\d{2,}$`)
	// 人物卡是 Markdown：status/role 必须是行首的纯字段。
	castStatusRe = regexp.MustCompile("(?m)^\\s*`?status`?\\s*[：:]\\s*([A-Za-z_-]+)")
	castRoleRe   = regexp.MustCompile("(?m)^\\s*`?role`?\\s*[：:]\\s*([A-Za-z_-]+)")
)

// auxiliaryPrefixes 是不属于书本身的目录：本应用自己写的改动备份、以及归档件。
// 不过滤的话 backups/<date>/v01-U3.yaml 会跟正式单元文件抢同一个文件名。
var auxiliaryPrefixes = []string{"backups/", "_archive/"}

func isAuxiliaryPath(rel string) bool {
	for _, p := range auxiliaryPrefixes {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

// LoadBook 读标准结构的书：novel-state.yaml + outline/ + units/ + canon/ + continuity/。
func LoadBook(root string, readOnly bool) (domain.BookSnapshot, error) {
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return domain.BookSnapshot{}, err
	}
	if !info.IsDir() {
		return domain.BookSnapshot{}, fmt.Errorf("book root is not a directory: %s", root)
	}
	bookID := filepath.Base(root)
	stateRaw, _ := readFile(filepath.Join(root, "novel-state.yaml"))
	state := parseState(stateRaw)

	files := map[string]string{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if isAuxiliaryPath(rel) {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(rel), ".md") &&
			!strings.HasSuffix(strings.ToLower(rel), ".yaml") &&
			!strings.HasSuffix(strings.ToLower(rel), ".yml") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		files[rel] = string(b)
		return nil
	})

	volumes := loadVolumes(root, files)
	units := loadUnits(root, files)
	cast := loadCast(files)
	identitySegments, _ := LoadIdentityTimeline(root)

	drafted, reviewed := 0, 0
	for _, u := range units {
		if u.Status == domain.UnitDrafted {
			drafted++
		}
		if u.Status == domain.UnitReviewed {
			reviewed++
		}
	}
	title := state.Title
	if title == "" {
		title = bookID
	}
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{
			ID:              bookID,
			Title:           title,
			Genre:           state.Genre,
			Subgenre:        state.Subgenre,
			Stage:           state.Stage,
			LastCommittedCh: state.LastCommittedCh,
			NextAction:      state.NextAction,
			TimeSystem:      state.TimeSystem,
			UnitCount:       len(units),
			DraftedCount:    drafted,
			ReviewedCount:   reviewed,
			RootPath:        root,
			ReadOnly:        readOnly,
		},
		StateRaw:           stateRaw,
		Volumes:            volumes,
		Units:              units,
		Cast:               cast,
		Files:              files,
		Revision:           computeRevision(files),
		ImportedAt:         time.Now().UTC(),
		IdentitySegments:   identitySegments,
		StructuredOutlines: map[string]*domain.StructuredMarkdown{},
	}
	for path, raw := range files {
		protocol := ""
		if path == "outline/book_outline.md" {
			protocol = BookOutlineProtocol
		} else if strings.HasPrefix(path, "outline/volumes/") && strings.HasSuffix(path, ".md") {
			protocol = VolumeOutlineProtocol
		} else if path == "canon/reveal-schedule.md" {
			protocol = "reveal-schedule/v1"
		}
		if protocol != "" {
			doc := ParseStructuredMarkdown(raw, protocol)
			snap.StructuredOutlines[path] = &doc
			if path != "outline/book_outline.md" && path != "canon/reveal-schedule.md" {
				for i := range snap.Volumes {
					if snap.Volumes[i].FileName == filepath.Base(path) {
						snap.Volumes[i].Structured = &doc
					}
				}
			}
		}
	}
	return snap, nil
}

type stateFields struct {
	Title, Genre, Subgenre, Stage, NextAction, TimeSystem string
	ActiveUnit                                            string
	LastCommittedCh                                       int
}

// parseState 读 novel-state.yaml 的顶层标的量。注释、引号、类型转换都交给 YAML。
func parseState(raw string) stateFields {
	var doc struct {
		Title           string `yaml:"title"`
		Genre           string `yaml:"genre"`
		Subgenre        string `yaml:"subgenre"`
		Stage           string `yaml:"stage"`
		NextAction      string `yaml:"next_action"`
		TimeSystem      string `yaml:"time_system"`
		ActiveUnit      string `yaml:"active_unit"`
		LastCommittedCh int    `yaml:"last_committed_ch"`
	}
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return stateFields{}
	}
	return stateFields{
		Title:           doc.Title,
		Genre:           doc.Genre,
		Subgenre:        doc.Subgenre,
		Stage:           doc.Stage,
		NextAction:      doc.NextAction,
		TimeSystem:      doc.TimeSystem,
		ActiveUnit:      doc.ActiveUnit,
		LastCommittedCh: doc.LastCommittedCh,
	}
}

func loadVolumes(root string, files map[string]string) []domain.VolumeInfo {
	out := make([]domain.VolumeInfo, 0)
	for rel, raw := range files {
		base := filepath.Base(rel)
		if !strings.HasPrefix(rel, "outline/") {
			continue
		}
		if unitOutlineRe.MatchString(base) || strings.HasPrefix(rel, "outline/units/") {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(base), ".md") {
			continue
		}
		m := volumeRe.FindStringSubmatch(base)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		out = append(out, domain.VolumeInfo{
			Volume:   n,
			FileName: base,
			Cast:     parseVolumeCast(raw),
			Raw:      raw,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Volume < out[j].Volume })
	_ = root
	return out
}

// castStemRe 是人物卡 stem 的唯一合法形态：小写字母、数字、连字符。
var castStemRe = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// parseVolumeCast extracts the machine-readable stem list of a volume.
//
// 两种写法都要认：卷根字段的 `- cast: a,b,c`（/ `- cast: [a, b]`），以及
// `## 本卷人物` 下的 `- a：说明` 列表。以前只认后者，于是用前一种写法生成的
// 卷纲解析出空 cast —— 细纲 prompt 里一张人物卡都没有，gates.CheckUnitOutline
// 的 `onStage ⊆ 本卷人物` 也因 volumeCast 为空被静默跳过。播种细纲头走的是
// `doc.Root.Value("cast")`，两条路径口径从此不一致。
//
// Prose lines (人物弧描述、卷情绪、说明文字) are not stems and are dropped.
func parseVolumeCast(md string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(stem string) {
		stem = strings.Trim(strings.TrimSpace(stem), "`\"'")
		// 只接受合法 stem（英文小写连字符）；中文人名/散文一律跳过。
		if castStemRe.MatchString(stem) && !seen[stem] {
			seen[stem] = true
			out = append(out, stem)
		}
	}
	grab := false
	for _, line := range strings.Split(md, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "#") {
			grab = strings.Contains(s, "本卷人物")
			continue
		}
		if !strings.HasPrefix(s, "-") {
			continue
		}
		item := strings.TrimSpace(strings.TrimPrefix(s, "-"))
		if rest, ok := castFieldValue(item); ok {
			for _, stem := range strings.FieldsFunc(rest, func(r rune) bool {
				return r == ',' || r == '，' || r == '、' || r == ' '
			}) {
				add(stem)
			}
			continue
		}
		if !grab {
			continue
		}
		stem := strings.Trim(item, "`")
		// stem 后常接说明：空格 / 中英文冒号 / 括号。只取标识本身。
		if i := strings.IndexAny(stem, " \t：:（("); i > 0 {
			stem = stem[:i]
		}
		add(stem)
	}
	return out
}

// castFieldValue 解析 `cast: a,b,c` / `cast: [a, b]` 这种根字段写法。
func castFieldValue(item string) (string, bool) {
	const key = "cast"
	if !strings.HasPrefix(item, key) {
		return "", false
	}
	rest := strings.TrimSpace(item[len(key):])
	if !strings.HasPrefix(rest, ":") && !strings.HasPrefix(rest, "：") {
		return "", false
	}
	rest = strings.TrimPrefix(rest, ":")
	rest = strings.TrimPrefix(rest, "：")
	rest = strings.TrimSpace(rest)
	rest = strings.Trim(rest, "[]")
	return strings.TrimSpace(rest), true
}

// unitOutline 是 outline/units/vNN-U#.yaml 里本应用关心的字段。
// chapter_range 用 yaml.Node 读：块序列与行内数组在 YAML 里都是 sequence。
type unitOutline struct {
	UnitID       string
	Status       string
	TitleWorking string
	Title        string
	Range        []int
}

func parseUnitOutline(raw string) unitOutline {
	var out unitOutline
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &node); err != nil {
		return out
	}
	root := &node
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, val := root.Content[i].Value, root.Content[i+1]
		switch key {
		case "unit_id":
			out.UnitID = strings.TrimSpace(val.Value)
		case "status":
			out.Status = strings.TrimSpace(val.Value)
		case "title_working":
			out.TitleWorking = strings.TrimSpace(val.Value)
		case "title":
			out.Title = strings.TrimSpace(val.Value)
		case "chapter_range":
			if val.Kind == yaml.SequenceNode && len(val.Content) >= 2 {
				from, err1 := strconv.Atoi(strings.TrimSpace(val.Content[0].Value))
				to, err2 := strconv.Atoi(strings.TrimSpace(val.Content[1].Value))
				if err1 == nil && err2 == nil && to >= from && from > 0 {
					out.Range = []int{from, to}
				}
			}
		}
	}
	return out
}

func loadUnits(root string, files map[string]string) []domain.UnitInfo {
	byID := map[string]*domain.UnitInfo{}
	ensure := func(name string) *domain.UnitInfo {
		m := unitOutlineRe.FindStringSubmatch(name)
		if m == nil {
			m = unitProseRe.FindStringSubmatch(name)
		}
		if m == nil {
			return nil
		}
		id := fmt.Sprintf("v%s-U%s", m[1], m[2])
		if e, ok := byID[id]; ok {
			return e
		}
		vol, _ := strconv.Atoi(m[1])
		idx, _ := strconv.Atoi(m[2])
		e := &domain.UnitInfo{
			UnitID: id, Volume: vol, Index: idx, Status: domain.UnitProposed,
			OutlinePath: filepath.ToSlash(filepath.Join("outline", "units", id+".yaml")),
			ProsePath:   filepath.ToSlash(filepath.Join("units", id+".md")),
		}
		byID[id] = e
		return e
	}
	keys := make([]string, 0, len(files))
	for rel := range files {
		keys = append(keys, rel)
	}
	sort.Strings(keys)

	// 先细纲：决定 status 与章范围。
	for _, rel := range keys {
		base := filepath.Base(rel)
		if !unitOutlineRe.MatchString(base) || !strings.HasPrefix(rel, "outline/units/") {
			continue
		}
		e := ensure(base)
		if e == nil {
			continue
		}
		raw := files[rel]
		e.OutlineRaw = raw
		e.OutlinePath = rel
		parsed := parseUnitOutline(raw)
		if parsed.Status == "" {
			// 细纲解析失败：保留单元但不猜它的状态，等作者重写细纲。
			continue
		}
		e.Status = domain.UnitStatus(strings.ToLower(parsed.Status))
		e.Title = parsed.TitleWorking
		if e.Title == "" {
			e.Title = parsed.Title
		}
		if parsed.Range != nil {
			e.ChapterFrom, e.ChapterTo = parsed.Range[0], parsed.Range[1]
		}
	}
	// 再正文：有稿单元的状态由正文决定，不被细纲覆盖。
	for _, rel := range keys {
		base := filepath.Base(rel)
		if !unitProseRe.MatchString(base) || !strings.HasPrefix(rel, "units/") {
			continue
		}
		e := ensure(base)
		if e == nil {
			continue
		}
		e.HasProse = true
		e.ProseRaw = files[rel]
		e.ProsePath = rel
		// 细纲写着 accepted/proposed 但正文已落盘 → 草稿（还没走审校定稿）。
		// 已是 reviewed 的保持原状：状态以细纲为准，章号游标不替它升级。
		switch e.Status {
		case domain.UnitProposed, domain.UnitAccepted:
			e.Status = domain.UnitDrafted
		}
	}

	out := make([]domain.UnitInfo, 0, len(byID))
	for _, e := range byID {
		// 每章目标在这里算一次，之后前端/提示词/硬门都用同一个数。
		e.ChapterTarget = ChapterTarget(*e)
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Volume != out[j].Volume {
			return out[i].Volume < out[j].Volume
		}
		if out[i].Index != out[j].Index {
			return out[i].Index < out[j].Index
		}
		// 同卷同序号理论上不该出现；用 ID 兜底，保证顺序不随 map 迭代漂移
		// （snap.Units 的顺序会决定提示词里人物/单元的排列）。
		return out[i].UnitID < out[j].UnitID
	})
	_ = root
	return out
}

var castTaglineRe = regexp.MustCompile("(?m)^\\s*`?tagline`?\\s*[：:]\\s*(.+)$")

// splitNameTagline 把「姓名（说明）」拆成纯姓名与一句话说明。
// 纯角色标注（主角/protagonist 等）直接丢弃——role 有自己的字段。
func splitNameTagline(heading string) (name, tagline string) {
	heading = strings.TrimSpace(strings.TrimPrefix(heading, "#"))
	heading = strings.TrimSpace(heading)
	re := regexp.MustCompile(`^(.*?)\s*[（(]([^）)]*)[）)]\s*$`)
	m := re.FindStringSubmatch(heading)
	if m == nil {
		return heading, ""
	}
	name = strings.TrimSpace(m[1])
	note := strings.TrimSpace(m[2])
	if roleOnlyAnnotation(note) {
		return name, ""
	}
	return name, note
}

func roleOnlyAnnotation(note string) bool {
	n := strings.ToLower(strings.TrimSpace(note))
	for _, w := range []string{"protagonist", "antagonist", "recurring", "villain", "hero", "main", "主角", "反派", "配角", "卷反派"} {
		if n == w {
			return true
		}
	}
	return false
}

func loadCast(files map[string]string) []domain.CastCard {
	out := make([]domain.CastCard, 0)
	for rel, raw := range files {
		if !strings.HasPrefix(rel, "canon/cast/") || !strings.HasSuffix(rel, ".md") {
			continue
		}
		stem := strings.TrimSuffix(filepath.Base(rel), ".md")
		card := domain.CastCard{Stem: stem, Raw: raw}
		lines := strings.Split(raw, "\n")
		if len(lines) > 0 {
			card.Name, card.Tagline = splitNameTagline(lines[0])
		}
		if m := castTaglineRe.FindStringSubmatch(raw); len(m) > 1 {
			card.Tagline = strings.TrimSpace(m[1])
		}
		if m := castStatusRe.FindStringSubmatch(raw); len(m) > 1 {
			card.Status = domain.CastStatus(strings.ToLower(m[1]))
		}
		if m := castRoleRe.FindStringSubmatch(raw); len(m) > 1 {
			card.Role = strings.ToLower(m[1])
		}
		out = append(out, card)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Stem < out[j].Stem })
	return out
}

func computeRevision(files map[string]string) string {
	h := sha256.New()
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(files[k]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// WriteUnitProse writes accepted chapter drafts into units/<id>.md.
func WriteUnitProse(root, unitID string, chapters []domain.ChapterDraft) error {
	var b strings.Builder
	for i, ch := range chapters {
		if i > 0 {
			b.WriteString("\n---\n\n")
		}
		title := strings.TrimSpace(ch.Title)
		if title != "" {
			fmt.Fprintf(&b, "## 第%d章 %s\n\n", ch.Chapter, title)
		} else {
			fmt.Fprintf(&b, "## 第%d章\n\n", ch.Chapter)
		}
		b.WriteString(strings.TrimSpace(ch.Body))
		b.WriteString("\n")
	}
	path := filepath.Join(root, "units", unitID+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// SetUnitStatus rewrites the status: line in a unit YAML.
func SetUnitStatus(root, unitID string, status domain.UnitStatus) error {
	path := filepath.Join(root, "outline", "units", unitID+".yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(raw)
	if re := regexp.MustCompile(`(?m)^status:.*$`); re.MatchString(text) {
		text = re.ReplaceAllString(text, "status: "+string(status))
	} else {
		text = "status: " + string(status) + "\n" + text
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// CreateBookScaffold 只用于测试夹具与旧数据构造：它一次铺满全部设定模板。
//
// 生产建书必须走 CreateBookFromDraft：空模板会被结构化立项当成既有 canon。
// 测试需要这些模板存在（例如直接写卷纲/细纲），所以保留该函数。
func CreateBookScaffold(libraryRoot, bookID, title, genre string) (string, error) {
	root := filepath.Join(libraryRoot, "novel", bookID)
	dirs := []string{
		root,
		filepath.Join(root, "canon", "cast"),
		filepath.Join(root, "outline", "volumes"),
		filepath.Join(root, "outline", "units"),
		filepath.Join(root, "units"),
		filepath.Join(root, "continuity", "summaries"),
		filepath.Join(root, "continuity", "commits"),
		filepath.Join(root, "reviews"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}
	state := fmt.Sprintf("title: %q\ngenre: %q\nsubgenre: \"\"\nstage: %s\nlast_committed_ch: 0\nnext_action: plan\nactive_unit: \"\"\ntime_system: story_day\nqc_profile: general\ncontinuation_mode: false\ngates:\n  knowledge: unknown\n  asset: unknown\n  qc: unknown\nblockers: []\n", title, genre, domain.StagePlanning)
	if err := os.WriteFile(filepath.Join(root, "novel-state.yaml"), []byte(state), 0o644); err != nil {
		return "", err
	}
	if err := ScaffoldFiles(root, title, genre); err != nil {
		return "", err
	}
	return root, nil
}

// HasBookOutline reports whether the book has an accepted/authored book outline.
func HasBookOutline(snap domain.BookSnapshot) bool {
	return strings.TrimSpace(snap.Files["outline/book_outline.md"]) != ""
}

// activeUnitVolume resolves novel-state.yaml's active_unit (e.g. "v01-U9") to a
// volume that actually exists in the book.
func activeUnitVolume(activeUnit string, volumes []domain.VolumeInfo) (int, bool) {
	m := unitOutlineRe.FindStringSubmatch(strings.TrimSpace(activeUnit) + ".yaml")
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	for _, v := range volumes {
		if v.Volume == n {
			return n, true
		}
	}
	return 0, false
}

func unfinishedInVolume(units []domain.UnitInfo, vol int) bool {
	for _, u := range units {
		if u.Volume != vol {
			continue
		}
		switch u.Status {
		case domain.UnitProposed, domain.UnitAccepted, domain.UnitDrafted:
			return true
		}
	}
	return false
}

// earliestUnfinishedVolume 返回最早一个仍有未定稿单元的卷。导入的书常有多卷骨架
// （有的到第 50 多卷），没有 active_unit 时按单元顺序取第一个待办卷才不会跳到卷末。
func earliestUnfinishedVolume(snap domain.BookSnapshot) (int, bool) {
	best, found := 0, false
	for _, u := range snap.Units {
		if u.Status == domain.UnitReviewed {
			continue
		}
		if !found || u.Volume < best {
			best, found = u.Volume, true
		}
	}
	return best, found
}

// SelectPrimaryAction mirrors workbench next-step rules at a coarse level.
// Order: 有作者输入但无创意提案→创意扩写；无总纲→立项；有总纲无卷纲→生成卷纲；
// 之后进入单元闭环（按单元顺序）。
func SelectPrimaryAction(snap domain.BookSnapshot) (kind domain.TaskKind, unitID string, volume int) {
	if !HasBookOutline(snap) && strings.TrimSpace(snap.Files[domain.IdeaPath]) != "" && !HasIdeaProposal(snap) {
		return domain.TaskIdea, "", 1
	}
	if !HasBookOutline(snap) {
		return domain.TaskPlan, "", 1
	}
	if len(snap.Volumes) == 0 {
		return domain.TaskPlan, "", 1
	}
	vol := snap.Volumes[len(snap.Volumes)-1].Volume
	if len(snap.Units) == 0 {
		return domain.TaskPlan, "", vol
	}
	// active_unit 只在它所属的卷仍有待办单元时才可信；作者定稿完一卷常忘记改它
	// （active_unit 停在上一卷），这时要按单元顺序顺延到下一卷，而不是直接立项。
	vol = 0
	if activeVol, ok := activeUnitVolume(parseState(snap.StateRaw).ActiveUnit, snap.Volumes); ok {
		if unfinishedInVolume(snap.Units, activeVol) {
			vol = activeVol
		}
	}
	if vol == 0 {
		if firstVol, ok := earliestUnfinishedVolume(snap); ok {
			vol = firstVol
		} else {
			return domain.TaskPlan, "", snap.Volumes[len(snap.Volumes)-1].Volume + 1
		}
	}
	// 单元闭环按单元顺序推进：最早未定稿的那个单元决定下一步。
	// 卷纲接受时会把整卷单元一次性播成 proposed，若让 proposed 优先，就会一直
	// 补后面的细纲而不去写前面已 accepted 的单元。
	var next *domain.UnitInfo
	for i := range snap.Units {
		u := &snap.Units[i]
		if u.Volume != vol {
			continue
		}
		switch u.Status {
		case domain.UnitProposed, domain.UnitAccepted, domain.UnitDrafted:
		default: // reviewed 及未知状态不参与选择
			continue
		}
		if next == nil || u.Index < next.Index {
			next = u
		}
	}
	if next == nil {
		return domain.TaskPlan, "", vol + 1
	}
	switch next.Status {
	case domain.UnitProposed:
		return domain.TaskOutline, next.UnitID, vol
	case domain.UnitAccepted:
		return domain.TaskWrite, next.UnitID, vol
	case domain.UnitDrafted:
		// 草稿单元：下一步优先审校；定稿在写作/审校页单元操作栏常驻，不从 primary 省略。
		return domain.TaskReview, next.UnitID, vol
	}
	return domain.TaskPlan, "", vol + 1
}
