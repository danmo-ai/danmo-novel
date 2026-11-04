package books

import (
	"fmt"
	"sort"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// StaleFieldFinding 描述一个「很久没被刷新、可能已经过期」的人物状态字段。
//
// 它只是提示，不是阻断：过期与否只有作者和正文知道。但没有任何提示时，
// 这类字段会一路被全量快照带着走——实测只有一条 health 跨了 6 个单元、
// 63 个故事日，而写作包一直把它当作「当前身体状况」。
type StaleFieldFinding struct {
	Stem     string
	Field    string
	Value    string
	LastUnit string // 最后被哪个已定稿单元的 delta 写过
	UnitsAgo int    // 距今几个已定稿单元
}

func (f StaleFieldFinding) String() string {
	return fmt.Sprintf("%s.%s=%q 自 %s 起未更新（已过 %d 个单元）",
		f.Stem, f.Field, f.Value, f.LastUnit, f.UnitsAgo)
}

// staleThreshold 是「几个单元没更新就算可疑」。
//
// 取 3 是因为一个单元通常 3–8 章、十几个故事日；连续三个单元都没碰到某个
// 字段，多半意味着它已经变了却没人记录，而不是真的三个单元都没变化。
const staleThreshold = 3

// 只有这些字段会「失效」：位置、身体、资源、关系、生命都会随时间改变。
// goal 与 knows 是累积或替换型语义，久未更新本身不代表过期（目标可以长期不变，
// 知情范围只会增加），因此不参与陈旧扫描。
var staleProneFields = []string{"health", "location", "resources", "relation", "life"}

// FindStaleCharacterFields 扫描末段快照里久未更新的字段。
//
// 传入的 units 应当只包含**已定稿**的单元：未定稿细纲里的 delta 还没发生，
// 不能算作「刷新过」——第一次实现时没区分，于是把 v08-U4 的规划 delta 当成了
// 已经写入的事实，报出一堆假警报。
func FindStaleCharacterFields(state domain.UnitState, finalized []domain.UnitInfo) []StaleFieldFinding {
	if len(finalized) == 0 || len(state.Cast) == 0 {
		return nil
	}
	order := map[string]int{}
	for i, u := range finalized {
		order[u.UnitID] = i
	}
	lastWritten := map[string]string{}
	for _, u := range finalized {
		for _, d := range parseStateDeltas(u.OutlineRaw) {
			field := domain.CanonicalStateField(d.Field)
			if field == "" {
				continue
			}
			lastWritten[d.Stem+"."+field] = u.UnitID
		}
	}

	var out []StaleFieldFinding
	for stem, snap := range state.Cast {
		for _, field := range staleProneFields {
			value := snapshotField(snap, field)
			if strings.TrimSpace(value) == "" {
				continue
			}
			unitID, ok := lastWritten[stem+"."+field]
			if !ok {
				continue // 来自更早的重建，没有细纲来源：不猜
			}
			ago := order[state.Unit] - order[unitID]
			if ago < staleThreshold {
				continue
			}
			out = append(out, StaleFieldFinding{
				Stem: stem, Field: field, Value: value, LastUnit: unitID, UnitsAgo: ago,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].UnitsAgo != out[j].UnitsAgo {
			return out[i].UnitsAgo > out[j].UnitsAgo
		}
		return out[i].Stem+out[i].Field < out[j].Stem+out[j].Field
	})
	return out
}

// StaleFieldFindings 把陈旧字段变成审校清单里的 advisory。
//
// 放 advisory 而不是 blocking：过期与否需要人判断，硬拦会让作者卡在一个
// 只有他能决定的问题上。但它必须可见——这是目前唯一能让人注意到
// 「某个状态已经三个单元没动过」的机制。
func StaleFieldFindings(unit domain.UnitInfo, stale []StaleFieldFinding) []domain.ReviewFinding {
	var out []domain.ReviewFinding
	for _, f := range stale {
		out = append(out, domain.ReviewFinding{
			ID:       fmt.Sprintf("stale-%s-%s", f.Stem, f.Field),
			Severity: "advisory", Source: "local_gate", Lens: "continuity",
			Title:      "人物状态可能已过期",
			Evidence:   fmt.Sprintf("%s.%s 自 %s 起未更新，仍记着「%s」", f.Stem, f.Field, f.LastUnit, clipRunes(f.Value, 24)),
			Suggestion: "若本单元内该状态已变化或已结束，请在 state_deltas 里写一条显式终值（如 from: 受伤, to: 已愈）",
		})
	}
	return out
}

func snapshotField(s domain.CharacterSnapshot, field string) string {
	switch field {
	case "health":
		return s.Health
	case "location":
		return s.Location
	case "resources":
		return s.Resources
	case "relation":
		return s.Relation
	case "life":
		return s.Life
	}
	return ""
}
