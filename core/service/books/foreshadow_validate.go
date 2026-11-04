package books

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// ForeshadowTypes 是伏笔的五种形态。
//
// 「信息」即原来的揭示计划：读者何时可以知道某个秘密，是伏笔的一个子集，
// 而不是另一套机制。融合后一个事实只存在一处，也就不可能出现
// 「卷纲说可揭示、揭示表说没到时候」这类两份登记互相打架的情况。
var ForeshadowTypes = []string{"物件", "信息", "关系", "承诺", "疑点"}

// ForeshadowStatuses 是伏笔的生命周期状态。
//
// 旧揭示计划的 `locked` 就是「还没埋下」= planned，迁移时归一。
var ForeshadowStatuses = []string{"planned", "planted", "advanced", "paid", "dropped"}

// CanonicalForeshadowStatus 只做小写归一，不做历史别名兼容。
// 旧揭示计划的 locked 已由迁移改写为 planned，运行期再遇到就是错。
func CanonicalForeshadowStatus(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	if s == "null" {
		return ""
	}
	return s
}

// ForeshadowEntityKind 是登记表里实体标题的种类名。
const ForeshadowEntityKind = "伏笔"

// IsForeshadowType reports whether v is one of the five declared types.
func IsForeshadowType(v string) bool {
	return containsString(ForeshadowTypes, strings.TrimSpace(v))
}

// IsForeshadowStatus reports whether v is a declared lifecycle status.
func IsForeshadowStatus(v string) bool {
	return containsString(ForeshadowStatuses, strings.TrimSpace(v))
}

// ValidateForeshadowSchedule enforces the field-level contract of the merged
// foreshadow registry.
//
// 它只做单条记录内部能判定的规则；跨层的先后关系（卷纲改总纲已定字段、
// 细纲引用不属于本单元的条目）在 ValidateForeshadowInheritance 里判，因为
// 那需要同时看到父层文档与当前单元。
func ValidateForeshadowSchedule(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	doc := ParseStructuredMarkdown(raw, ForeshadowProtocol)
	if doc.Legacy {
		return fmt.Errorf("伏笔登记表需要结构化 Markdown（含 schema_version: 1）")
	}
	if len(doc.Diagnostics) > 0 {
		return fmt.Errorf("伏笔登记表结构错误：%s", FormatOutlineDiagnostics(doc.Diagnostics))
	}
	for _, e := range doc.EntitiesOf(ForeshadowEntityKind) {
		if err := validateForeshadowEntity(e); err != nil {
			return err
		}
	}
	return nil
}

func validateForeshadowEntity(e domain.MarkdownEntity) error {
	id := e.ID
	if id == "" {
		return fmt.Errorf("伏笔：实体标题缺少稳定 ID（如 FS-01）")
	}
	if strings.TrimSpace(e.Value("content")) == "" {
		return fmt.Errorf("伏笔[%s].content：必填，且必须是可见可验的具体物或事", id)
	}
	typ := strings.TrimSpace(e.Value("type"))
	if !IsForeshadowType(typ) {
		return fmt.Errorf("伏笔[%s].type：需要 %s 之一，实际 %q", id, strings.Join(ForeshadowTypes, "/"), typ)
	}
	if status := CanonicalForeshadowStatus(e.Value("status")); status != "" && !IsForeshadowStatus(status) {
		return fmt.Errorf("伏笔[%s].status：需要 %s 之一，实际 %q", id, strings.Join(ForeshadowStatuses, "/"), status)
	}

	plantVol := strings.TrimSpace(e.Value("plant_volume"))
	payoffVol := strings.TrimSpace(e.Value("payoff_volume"))
	for name, v := range map[string]string{"plant_volume": plantVol, "payoff_volume": payoffVol} {
		if v == "" {
			return fmt.Errorf("伏笔[%s].%s：必填（总纲阶段就要定下卷级位置）", id, name)
		}
		if !volumeTagPattern.MatchString(v) {
			return fmt.Errorf("伏笔[%s].%s：需要 vNN 形式，实际 %q", id, name, v)
		}
	}
	if volumeNumber(payoffVol) < volumeNumber(plantVol) {
		return fmt.Errorf("伏笔[%s]：回收卷 %s 早于埋入卷 %s", id, payoffVol, plantVol)
	}
	if typ == "信息" && strings.TrimSpace(e.Value("payoff_summary")) == "" {
		return fmt.Errorf("伏笔[%s].payoff_summary：type=信息 时必填（回收时读者得到什么）", id)
	}

	plantUnit := strings.TrimSpace(e.Value("plant_unit"))
	payoffUnit := strings.TrimSpace(e.Value("payoff_unit"))
	if plantUnit != "" && !unitInVolume(plantUnit, plantVol) {
		return fmt.Errorf("伏笔[%s].plant_unit：%s 不在埋入卷 %s 内", id, plantUnit, plantVol)
	}
	if payoffUnit != "" && !unitInVolume(payoffUnit, payoffVol) {
		return fmt.Errorf("伏笔[%s].payoff_unit：%s 不在回收卷 %s 内", id, payoffUnit, payoffVol)
	}

	for _, key := range []string{"plant_chapter", "payoff_chapter"} {
		v := strings.TrimSpace(e.Value(key))
		if v == "" {
			continue
		}
		if n, err := strconv.Atoi(v); err != nil || n <= 0 {
			return fmt.Errorf("伏笔[%s].%s：需要正整数章号，实际 %q", id, key, v)
		}
	}

	// 位置单调：回收不得早于埋入。
	//
	// 分两级判断，因为三级细化本来就是逐层落地的：
	//   - 卷/单元级：回收位置不得早于埋入位置；
	//   - 同一单元内：埋与收落在这里是合法的（总纲只知道「都在 v01-U1」），
	//     此时才要求章级严格递增——但章号要等细纲细化出来才有。
	// 早期版本要求「严格晚于」，于是 hint_unit_ids == confirm_unit_id 的条目
	// （实测三本书 83 条里有 2 条）一律判违规，那是把正常的单元内埋收当成错误。
	if plantUnitPos, ok := foreshadowUnitPosition(e, "plant"); ok {
		if payoffUnitPos, ok := foreshadowUnitPosition(e, "payoff"); ok {
			if payoffUnitPos < plantUnitPos {
				return fmt.Errorf("伏笔[%s]：回收位置（%s）早于埋入位置（%s）", id,
					foreshadowUnitLabel(e, "payoff"), foreshadowUnitLabel(e, "plant"))
			}
			if payoffUnitPos == plantUnitPos {
				// 同单元：章级必须严格递增。
				pc, _ := strconv.Atoi(strings.TrimSpace(e.Value("payoff_chapter")))
				qc, _ := strconv.Atoi(strings.TrimSpace(e.Value("plant_chapter")))
				if pc > 0 && qc > 0 && pc <= qc {
					return fmt.Errorf("伏笔[%s]：同单元内回收章（%d）必须晚于埋入章（%d）", id, pc, qc)
				}
			}
		}
	}
	return nil
}

// foreshadowUnitPosition 是 (卷, 单元序号) 的可比较位置；章号不参与。
func foreshadowUnitPosition(e domain.MarkdownEntity, prefix string) (int, bool) {
	vol := volumeNumber(strings.TrimSpace(e.Value(prefix + "_volume")))
	if vol <= 0 {
		return 0, false
	}
	return vol*1_000_000 + unitIndex(strings.TrimSpace(e.Value(prefix+"_unit")))*10_000, true
}

func foreshadowUnitLabel(e domain.MarkdownEntity, prefix string) string {
	vol := strings.TrimSpace(e.Value(prefix + "_volume"))
	if unit := strings.TrimSpace(e.Value(prefix + "_unit")); unit != "" {
		return unit
	}
	return vol
}

func volumeNumber(tag string) int {
	if !volumeTagPattern.MatchString(tag) {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(tag, "v"))
	return n
}

func unitInVolume(unitID, volumeTag string) bool {
	m := unitIDRe.FindStringSubmatch(unitID)
	if m == nil {
		return false
	}
	return "v"+m[1] == volumeTag
}

func unitIndex(unitID string) int {
	m := unitIDRe.FindStringSubmatch(unitID)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[2])
	return n
}

var unitIDRe = regexp.MustCompile(`(?i)^v(\d+)-U(\d+)$`)
