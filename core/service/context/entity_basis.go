package contextsvc

import (
	"fmt"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

// SettingLayer classifies canon files: 总纲派生（骨架） vs 生产约束（工艺） vs 账本。
type SettingLayer string

const (
	LayerDerived    SettingLayer = "总纲派生"
	LayerCraft      SettingLayer = "生产约束"
	LayerLedger     SettingLayer = "连载账本"
	LayerAuthorOnly SettingLayer = "作者底牌"
)

// ClassifySetting returns the production layer for a canon path.
func ClassifySetting(path string) SettingLayer {
	p := strings.ToLower(strings.TrimSpace(path))
	switch {
	case strings.HasPrefix(p, "continuity/"), p == "novel-state.yaml":
		return LayerLedger
	case p == "canon/author-lore.md", p == "canon/locked-terms.yaml":
		return LayerAuthorOnly
	case p == "outline/book_outline.md", p == "book-bible.md",
		strings.HasPrefix(p, "canon/cast/"), p == books.IdentityTimelinePath,
		p == books.ForeshadowSchedulePath,
		p == "canon/world.md", p == "canon/goldfinger.md":
		return LayerDerived
	default:
		return LayerCraft
	}
}

// IsDerivedSetting reports whether the entity depends on the book outline.
func IsDerivedSetting(path string) bool {
	return ClassifySetting(path) == LayerDerived || ClassifySetting(path) == LayerAuthorOnly
}

// CurrentVolumeForSnapshot returns the active/latest volume number (>=1).
func CurrentVolumeForSnapshot(snap domain.BookSnapshot) int {
	if len(snap.Volumes) > 0 {
		return snap.Volumes[len(snap.Volumes)-1].Volume
	}
	for _, u := range snap.Units {
		if u.Volume > 0 {
			return u.Volume
		}
	}
	return 1
}

// VolumeCastStems selects only the cast relevant to planning a volume:
// current volume 在场 + 主角 + 上一卷在场（用于跨卷承接）。
// Empty volume cast (new volume) → 主角 + 上一卷在场，避免灌入全书人物。
func VolumeCastStems(snap domain.BookSnapshot, volume int) []string {
	seen := map[string]bool{}
	var out []string
	push := func(stem string) {
		if stem != "" && !seen[stem] {
			seen[stem] = true
			out = append(out, stem)
		}
	}
	for _, v := range snap.Volumes {
		if v.Volume == volume || v.Volume == volume-1 {
			for _, s := range v.Cast {
				push(s)
			}
		}
	}
	for _, c := range snap.Cast {
		if NormalizeRole(c.Role) == "protagonist" {
			push(c.Stem)
		}
	}
	// 全新卷且没有上一卷人物时，至少保留全部主角；已有在场则只给相关人物。
	if len(out) == 0 {
		for _, c := range snap.Cast {
			push(c.Stem)
		}
	}
	return out
}

// AddRewriteBasis appends outline/structure/ledger evidence for a single-entity
// rewrite. 总纲派生设定必须带依据；工艺类只带规则与风格（调用方补充）。
// The target file itself is never re-added here.
func AddRewriteBasis(snap domain.BookSnapshot, target string, add func(id, label, source, content string)) {
	vol := CurrentVolumeForSnapshot(snap)
	volTag := fmt.Sprintf("v%02d", vol)
	volumePath := "outline/volumes/" + volTag + ".md"
	layer := ClassifySetting(target)

	addOutline := func() {
		if target != "outline/book_outline.md" {
			if body := strings.TrimSpace(snap.Files["outline/book_outline.md"]); body != "" {
				add("basis-book-outline", "依据·全书总纲", "outline/book_outline.md", clip(body, 4000))
			}
		}
	}
	addVolume := func() {
		if target != volumePath {
			if body := strings.TrimSpace(snap.Files[volumePath]); body != "" {
				add("basis-volume", "依据·当前卷纲", volumePath, clip(body, 2200))
			}
		}
	}
	addBible := func() {
		if target != "book-bible.md" {
			if body := strings.TrimSpace(snap.Files["book-bible.md"]); body != "" {
				add("basis-bible", "依据·作者意图", "book-bible.md", clip(body, 1600))
			}
		}
	}

	if layer == LayerCraft {
		return
	}

	addOutline()

	switch {
	case strings.HasPrefix(target, "canon/cast/"):
		stem := strings.TrimSuffix(strings.TrimPrefix(target, "canon/cast/"), ".md")
		addVolume()
		addBible()
		// 揭示计划只作公开节奏参考，不带底牌细节。
		if body := strings.TrimSpace(snap.Files[books.ForeshadowSchedulePath]); body != "" && target != books.ForeshadowSchedulePath {
			add("basis-foreshadow", "依据·伏笔登记表", books.ForeshadowSchedulePath, clip(body, 1000))
		}
		if cont := ExtractContinuityLatest(snap, []string{stem}, ContinuityForReview); cont != "" {
			add("basis-continuity", "依据·人物当前状态", books.StatePath, clip(cont, 1200))
		}
		for _, v := range snap.Volumes {
			for _, s := range v.Cast {
				if s == stem && v.Volume != vol {
					// 人物若在非当前卷也在场，附该卷人物段即可（已由当前卷纲覆盖时跳过）。
					if body := strings.TrimSpace(v.Raw); body != "" && v.Volume != vol {
						add(fmt.Sprintf("basis-volume-v%02d", v.Volume), fmt.Sprintf("依据·第%d卷纲", v.Volume), "outline/volumes/"+fmt.Sprintf("v%02d", v.Volume)+".md", clip(body, 1200))
					}
					break
				}
			}
		}

	case target == books.ForeshadowSchedulePath:
		// 揭示计划依据：总纲 + 当前卷纲 + 公开账本（已揭/未揭），不读 author-lore 到公开重写里。
		addVolume()
		addBible()
		if cont := ExtractContinuityLatest(snap, nil, ContinuityForReview); cont != "" {
			add("basis-continuity", "依据·连续性状态（已揭/未揭）", books.StatePath, clip(cont, 1600))
		}

	case target == "canon/author-lore.md":
		addVolume()
		addBible()
		// 底牌重写允许读揭示计划（同层），不注入公开正文。
		if body := strings.TrimSpace(snap.Files[books.ForeshadowSchedulePath]); body != "" {
			add("basis-foreshadow", "依据·伏笔登记表", books.ForeshadowSchedulePath, clip(body, 1600))
		}

	case target == books.IdentityTimelinePath:
		addBible()
		for _, u := range snap.Units {
			if u.HasProse {
				add("affected-"+u.UnitID, "受影响单元·"+u.UnitID, u.OutlinePath, clip(u.OutlineRaw, 1000))
			}
		}

	case target == "canon/world.md", target == "canon/goldfinger.md", target == "book-bible.md":
		addVolume()
		addBible()
		// 主角/常驻人物压缩作对齐，最多 4 张。
		n := 0
		for _, c := range snap.Cast {
			if n >= 4 {
				break
			}
			if c.Role != "" && c.Role != "protagonist" && c.Role != "recurring" && n > 0 {
				continue
			}
			src := "canon/cast/" + c.Stem + ".md"
			add("basis-cast-"+c.Stem, "依据·人物·"+CleanName(c.Name), src, clip(c.Raw, 900))
			n++
		}
	}
}
