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
// SanitizeStateDeltas 剔除「字段用在了非人物 stem 上」的条目，并返回被剔除的说明。//
// 典型情形：模型把故事线 ID 当人物用——`career.role_title=全国决赛竞争者->全国第一`
// （名次不是职务），或 `career.resources=…`。career 是故事线，人物卡里没有它，
// 这条迁移永远无法落到账本上。
//
// 为什么剔除而不是报错：这类错误只损失一条记账，故事本身没写错；而报错会让
// 整份细纲作废、整单元停下来问作者——按一天几十个单元的速度，那等于流水线
// 每几小时就要人介入一次。剔除后在候选的 findings 里留一条 advisory，作者
// 打开候选就能看到被丢掉了什么。
//
// 判据与 ValidateStateDeltas 完全一致（同一份 people 集合），只是把「拒绝」
// 改成「剔除」——两处若用不同判据，会出现「剔除了却还是校验失败」。
func SanitizeStateDeltas(cast []domain.CastCard, raw string) (string, []string) {
	deltas := parseStateDeltas(raw)
	if len(deltas) == 0 {
		return raw, nil
	}
	people := map[string]bool{}
	for _, c := range cast {
		people[c.Stem] = true
	}
	var dropped []string
	var kept []string
	for _, d := range deltas {
		field := domain.CanonicalStateField(d.Field)
		needsPerson := domain.OwnedByIdentityTimeline(field) || field == domain.StateFieldLife
		if needsPerson && !people[d.Stem] {
			dropped = append(dropped, fmt.Sprintf("%s.%s（人物卡中没有 %s）", d.Stem, d.Field, d.Stem))
			continue
		}
		kept = append(kept, d.Stem+"."+d.Field+"="+d.From+"->"+d.To)
	}
	if len(dropped) == 0 {
		return raw, nil
	}
	return strings.Join(kept, "；"), dropped
}

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
		if domain.OwnedByIdentityTimeline(field) || field == domain.StateFieldLife {
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
