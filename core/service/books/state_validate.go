package books

import (
	"fmt"
	"sort"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// ValidateStateDeltas 校验一个单元声明的 state_deltas。
//
// 它守着三件事，都是过去会静默失真的地方：
//
//  1. 词表封闭。此前提示词写 role_title|location|injury|resources|goal|knowledge，
//     而 StateFromOutline 的 switch 只认其中六个且没有 default 分支——772 个单元
//     399 条 delta 有 281 条被扔掉，涉及 114 种 field 名。现在未知名直接报错，
//     进修复轮改名，而不是落盘后人间蒸发。
//  2. 生命与身份是单向的。life 只能 alive→missing→dead；角色一旦死亡，后续单元的
//     onStage 不得再包含他——账本里从来没有死亡记录，于是死者照样能被要求出场。
//  3. 人物级字段必须指向真实人物卡。role_title / life 只对人物有意义；其余字段
//     允许挂在 career、relationship 这类非人物实体上（细纲一直这么用）。
//
// prior 是进入本单元时的快照（StateAt 的结果），用于判断流转方向与死者在场。
func ValidateStateDeltas(cast []domain.CastCard, unit domain.UnitInfo, raw string, prior domain.UnitState, onStage []string) error {
	deltas := parseStateDeltas(raw)

	if unknown := UnknownStateDeltaFields(raw); len(unknown) > 0 {
		return fmt.Errorf("%s.state_deltas.field：只接受 %s；无法识别 %s",
			unit.UnitID, domain.StateDeltaFieldList(), strings.Join(unknown, "、"))
	}

	people := map[string]bool{}
	for _, c := range cast {
		people[c.Stem] = true
	}

	for _, d := range deltas {
		field := domain.CanonicalStateField(d.Field)
		if field == "role_title" || field == "life" {
			if !people[d.Stem] {
				return fmt.Errorf("%s.state_deltas：%s 使用 %s 字段，但人物卡中没有 stem=%s",
					unit.UnitID, d.Stem, field, d.Stem)
			}
		}
		if field != "life" {
			continue
		}
		to := domain.CanonicalLife(d.To)
		if !containsString(domain.LifeStages, to) {
			return fmt.Errorf("%s.state_deltas.life：需要 %s 之一，实际 %q",
				unit.UnitID, strings.Join(domain.LifeStages, "/"), d.To)
		}
		if from := domain.CanonicalLife(prior.Cast[d.Stem].Life); from != "" {
			if indexOfLife(to) < indexOfLife(from) {
				return fmt.Errorf("%s.state_deltas.life：%s 不能从 %s 回到 %s（生命状态单向）",
					unit.UnitID, d.Stem, from, to)
			}
		}
	}

	// 死者不得再进本单元的在场名单。
	for _, stem := range onStage {
		if life := domain.CanonicalLife(prior.Cast[stem].Life); life == "dead" {
			return fmt.Errorf("%s.on_stage：%s 在上一单元已死亡，不能再次出场；如为回忆或倒叙请拆到独立闪回单元",
				unit.UnitID, stem)
		}
	}
	return nil
}

func indexOfLife(v string) int {
	for i, s := range domain.LifeStages {
		if s == v {
			return i
		}
	}
	return len(domain.LifeStages)
}

// DeadCastIn 返回 prior 快照里已死亡的人物 stem 集合。
// 供细纲接受与写作提示词复用，避免两处各写一遍判定。
func DeadCastIn(prior domain.UnitState) []string {
	var out []string
	for stem, snap := range prior.Cast {
		if domain.CanonicalLife(snap.Life) == "dead" {
			out = append(out, stem)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
