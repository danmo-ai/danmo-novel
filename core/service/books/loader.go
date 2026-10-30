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
)

var (
	unitOutlineRe = regexp.MustCompile(`(?i)^v(\d+)-U(\d+)\.ya?ml$`)
	unitProseRe   = regexp.MustCompile(`(?i)^v(\d+)-U(\d+)\.md$`)
	volumeRe      = regexp.MustCompile(`(?i)^(?:volume|vol|v)0*(\d+)`)
	statusRe      = regexp.MustCompile(`(?m)^status:\s*(\S+)`)
	titleRe       = regexp.MustCompile(`(?m)^(?:title_working|title):\s*(.+)$`)
	rangeRe       = regexp.MustCompile(`(?m)^chapter_range:\s*\[\s*(\d+)\s*,\s*(\d+)\s*\]`)
	stateScalarRe = regexp.MustCompile(`(?m)^([a-zA-Z0-9_]+):\s*(.*)$`)
	castStatusRe  = regexp.MustCompile("(?m)^\\s*`?status`?\\s*[：:]\\s*([A-Za-z_-]+)")
	castRoleRe    = regexp.MustCompile("(?m)^\\s*`?role`?\\s*[：:]\\s*([A-Za-z_-]+)")
)

// auxiliaryPrefixes are directories whose files never belong to canon/units:
// Danmo Work tool packs, backups, and repair snapshots. Without this filter a
// backup like backups/<date>/v01-U3.yaml would race the real unit file in the
// files map (random walk order → stale content overwriting current).
var auxiliaryPrefixes = []string{".pack/", ".tools/", "tools/", "backups/", "extras/"}

func isAuxiliaryPath(rel string) bool {
	for _, p := range auxiliaryPrefixes {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

// LoadBook parses a danmo-work compatible novel/<book-id>/ tree.
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
	units := loadUnits(root, files, state.LastCommittedCh)
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
		StateRaw:   stateRaw,
		Volumes:    volumes,
		Units:      units,
		Cast:       cast,
		Files:      files,
		Revision:   computeRevision(files),
		ImportedAt: time.Now().UTC(),
		IdentitySegments: identitySegments,
	}
	return snap, nil
}

type stateFields struct {
	Title, Genre, Subgenre, Stage, NextAction, TimeSystem string
	LastCommittedCh                                       int
}

func parseState(raw string) stateFields {
	out := stateFields{}
	for _, m := range stateScalarRe.FindAllStringSubmatch(raw, -1) {
		k, v := m[1], strings.TrimSpace(m[2])
		v = strings.Trim(v, `"'`)
		switch k {
		case "title":
			out.Title = v
		case "genre":
			out.Genre = v
		case "subgenre":
			out.Subgenre = v
		case "stage":
			out.Stage = v
		case "next_action":
			out.NextAction = v
		case "time_system":
			out.TimeSystem = v
		case "last_committed_ch":
			out.LastCommittedCh, _ = strconv.Atoi(v)
		}
	}
	return out
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

var castStemRe = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// parseVolumeCast extracts only machine-readable stem lines from 本卷人物.
// Prose lines (人物弧描述、卷情绪、说明文字) are not stems and are dropped —
// 卷纲是给人读的，stem 列表必须是 "- wen-nianci" 这种纯标识行。
func parseVolumeCast(md string) []string {
	var out []string
	grab := false
	for _, line := range strings.Split(md, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "#") {
			grab = strings.Contains(s, "本卷人物")
			continue
		}
		if !grab {
			continue
		}
		if !strings.HasPrefix(s, "-") {
			continue
		}
		stem := strings.TrimSpace(strings.TrimPrefix(s, "-"))
		stem = strings.Trim(stem, "`")
		// stem 后常接说明：空格 / 中英文冒号 / 括号。只取标识本身。
		if i := strings.IndexAny(stem, " \t：:（("); i > 0 {
			stem = stem[:i]
		}
		// 只接受合法 stem（英文小写连字符）；中文人名/散文一律跳过。
		if castStemRe.MatchString(stem) {
			out = append(out, stem)
		}
	}
	return out
}

func loadUnits(root string, files map[string]string, lastCommitted int) []domain.UnitInfo {
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
	for rel, raw := range files {
		base := filepath.Base(rel)
		if unitOutlineRe.MatchString(base) {
			e := ensure(base)
			if e == nil {
				continue
			}
			e.OutlineRaw = raw
			e.OutlinePath = rel
			if sm := statusRe.FindStringSubmatch(raw); len(sm) > 1 {
				e.Status = domain.UnitStatus(strings.ToLower(sm[1]))
			}
			if tm := titleRe.FindStringSubmatch(raw); len(tm) > 1 {
				e.Title = strings.Trim(strings.TrimSpace(tm[1]), `"'`)
			}
			if rm := rangeRe.FindStringSubmatch(raw); len(rm) > 2 {
				e.ChapterFrom, _ = strconv.Atoi(rm[1])
				e.ChapterTo, _ = strconv.Atoi(rm[2])
			}
		}
		if unitProseRe.MatchString(base) && strings.HasPrefix(rel, "units/") {
			e := ensure(base)
			if e == nil {
				continue
			}
			e.HasProse = true
			e.ProseRaw = raw
			e.ProsePath = rel
			if e.Status == domain.UnitProposed || e.Status == domain.UnitAccepted {
				e.Status = domain.UnitDrafted
			}
			if e.Status == domain.UnitReviewed && e.ChapterTo > 0 && e.ChapterTo <= lastCommitted {
				e.Status = domain.UnitReviewed
			}
		}
	}
	out := make([]domain.UnitInfo, 0, len(byID))
	for _, e := range byID {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Volume != out[j].Volume {
			return out[i].Volume < out[j].Volume
		}
		return out[i].Index < out[j].Index
	})
	_ = root
	return out
}

var castTaglineRe = regexp.MustCompile("(?m)^\\s*`?tagline`?\\s*[：:]\\s*(.+)$")

// splitNameTagline splits a legacy heading "姓名（说明）" into pure name and
// tagline. Role-only annotations (主角/protagonist 等) are dropped entirely —
// role lives in its own field.
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
	if statusRe.MatchString(text) {
		text = statusRe.ReplaceAllString(text, "status: "+string(status))
	} else {
		text = "status: " + string(status) + "\n" + text
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// CreateBookScaffold creates a minimal book tree for init.
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
	state := fmt.Sprintf("title: %q\ngenre: %q\nsubgenre: \"\"\nstage: planning\nlast_committed_ch: 0\nnext_action: plan\nactive_unit: \"\"\ntime_system: story_day\nqc_profile: general\ncontinuation_mode: false\ngates:\n  knowledge: unknown\n  asset: unknown\n  qc: unknown\nblockers: []\n", title, genre)
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

// SelectPrimaryAction mirrors workbench next-step rules at a coarse level.
// Order: 无总纲→立项；有总纲无卷纲→生成卷纲；之后进入单元闭环。
func SelectPrimaryAction(snap domain.BookSnapshot) (kind domain.TaskKind, unitID string, volume int) {
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
	var proposed, ready, drafted []domain.UnitInfo
	for _, u := range snap.Units {
		if u.Volume != vol {
			continue
		}
		switch u.Status {
		case domain.UnitProposed:
			proposed = append(proposed, u)
		case domain.UnitAccepted:
			ready = append(ready, u)
		case domain.UnitDrafted:
			drafted = append(drafted, u)
		}
	}
	if len(proposed) > 0 {
		return domain.TaskOutline, proposed[0].UnitID, vol
	}
	if len(ready) > 0 {
		return domain.TaskWrite, ready[0].UnitID, vol
	}
	if len(drafted) > 0 {
		// 草稿单元：下一步优先审校；定稿在写作/审校页单元操作栏常驻，不从 primary 省略。
		return domain.TaskReview, drafted[0].UnitID, vol
	}
	return domain.TaskPlan, "", vol + 1
}
