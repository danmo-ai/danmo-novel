package mcp

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// repairEntityResult 是 repair_entity 的返回值：修了什么、改到哪个文件。
type repairEntityResult struct {
	OK          bool   `json:"ok"`
	Kind        string `json:"kind"`                 // volume | unit | cast | identity | none
	Path        string `json:"path,omitempty"`       // 被修改的文件路径
	Field       string `json:"field,omitempty"`      // 被修改的字段
	Before      string `json:"before,omitempty"`     // 修改前的值
	After       string `json:"after,omitempty"`      // 修改后的值
	Note        string `json:"note,omitempty"`       // 给 Agent 看的说明
	NeedsHuman  string `json:"needsHuman,omitempty"` // 修不了时给出原因
	OriginalErr string `json:"originalError,omitempty"`
}

// handleRepairEntity 根据服务端返回的错误文本，自动判断「该改哪个实体、改什么字段」。
//
// 这是给 Agent 的「出错后怎么修」的入口。Agent 不需要知道卷纲/细纲/人物卡/身份
// 时间线的文件格式：它把 accept/start_run 返回的错误原文传进来，这里按**确定性
// 规则**判断归口并执行修复。
//
// 只修「确定性能判」的那一类：身份转换写错单元、字段缺失、引用不存在的 ID。
// 涉及「这个人该不该登场」这类创作决定的，返回 needsHuman，让 Agent 去问作者。
func (s *Server) handleRepairEntity(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	bookID, err := req.RequireString("bookId")
	if err != nil {
		return errResult(err)
	}
	errText := strings.TrimSpace(req.GetString("error", ""))
	if errText == "" {
		return errResult(fmt.Errorf("error required（把服务端返回的错误原文传进来）"))
	}
	unitID := strings.TrimSpace(req.GetString("unitId", ""))

	root, readOnly, err := s.deps.Store.GetBookRoot(ctx, bookID)
	if err != nil {
		return errResult(err)
	}
	if readOnly {
		return errResult(fmt.Errorf("read-only book"))
	}
	snap, err := books.LoadBook(root, readOnly)
	if err != nil {
		return errResult(err)
	}

	// 归口判断：按错误文本里的关键词路由。
	switch {
	case strings.Contains(errText, "缺少生效的职务身份段"):
		return s.repairIdentityMissing(ctx, snap, unitID, errText)
	case strings.Contains(errText, "与本单元生效段") || strings.Contains(errText, "不对应本单元生效段"):
		return s.repairIdentityMismatch(ctx, snap, unitID, errText)
	case strings.Contains(errText, "人物卡中没有 stem="):
		return s.repairCastMissing(ctx, snap, unitID, errText)
	case strings.Contains(errText, "state_deltas"):
		return s.repairStateDelta(ctx, snap, unitID, errText)
	case strings.Contains(errText, "state_changes"):
		return s.repairVolumeStateChange(ctx, snap, unitID, errText)
	default:
		return jsonResult(repairEntityResult{
			OK: false, Kind: "none", NeedsHuman: errText,
			Note: "无法自动判断归口，请人工检查该错误",
		})
	}
}

// repairIdentityMissing 处理「某人物缺少生效的职务身份段」。
//
// 成因有两种，判据是「这个人在时间线里有没有任何一段」：
//   - 完全没有 → 作者没登记过这个人，必须问作者（needsHuman）
//   - 有段但不在本单元 → 模型把一个还没登场的人放进了 on_stage，应把 on_stage
//     收窄到「本单元有生效段」的人，而不是补一段身份。
func (s *Server) repairIdentityMissing(ctx context.Context, snap domain.BookSnapshot, unitID, errText string) (*mcpgo.CallToolResult, error) {
	stem := extractStemFromError(errText)
	if stem == "" {
		return jsonResult(repairEntityResult{OK: false, Kind: "none", NeedsHuman: errText})
	}
	// 时间线里有没有这个人？
	hasAny := false
	for _, s := range snap.IdentitySegments {
		if s.Stem == stem {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return jsonResult(repairEntityResult{
			OK: false, Kind: "identity", NeedsHuman: errText,
			Note: fmt.Sprintf("人物 %s 在身份时间线里没有任何段；请用 edit_field(kind=identity,action=append) 登记", stem),
		})
	}
	// 有段但不在本单元：把 on_stage 收窄到本单元有生效段的人。
	unit, ok := findUnitIn(snap, unitID)
	if !ok {
		return jsonResult(repairEntityResult{OK: false, Kind: "none", NeedsHuman: "unit not found: " + unitID})
	}
	onStage := books.OnStageFromOutline(unit.OutlineRaw)
	var keep []string
	for _, st := range onStage {
		if st == stem {
			continue
		}
		keep = append(keep, st)
	}
	// 用字段级编辑把 on_stage 改掉。
	rel := fmt.Sprintf("outline/units/%s.yaml", unitID)
	content, err := readBookFile(ctx, s.deps.Store, snap.Summary.ID, rel)
	if err != nil {
		return jsonResult(repairEntityResult{OK: false, Kind: "unit", NeedsHuman: err.Error()})
	}
	updated := replaceYAMLListField(content, "on_stage", keep)
	if err := writeBookFile(ctx, s.deps.Store, snap.Summary.ID, rel, updated); err != nil {
		return jsonResult(repairEntityResult{OK: false, Kind: "unit", NeedsHuman: err.Error()})
	}
	return jsonResult(repairEntityResult{
		OK: true, Kind: "unit", Path: rel, Field: "on_stage",
		After: strings.Join(keep, ","),
		Note:  fmt.Sprintf("已从 %s 的 on_stage 移除 %s（该人物在本单元没有生效身份段）", unitID, stem),
	})
}

// repairIdentityMismatch 处理「身份与本单元生效段不一致」。
//
// 这类冲突的成因是**卷纲种子把身份转换写在了错误的单元**：例如 v02-U2 写着
// 「重案队试用刑警->重案队刑警」，而时间线里「重案队刑警」从 v02-U3 才生效。
// 修法是把这条迁移挪到真正生效的那个单元，而不是改时间线去迁就种子。
func (s *Server) repairIdentityMismatch(ctx context.Context, snap domain.BookSnapshot, unitID, errText string) (*mcpgo.CallToolResult, error) {
	stem := extractStemFromError(errText)
	if stem == "" {
		return jsonResult(repairEntityResult{OK: false, Kind: "none", NeedsHuman: errText})
	}
	// 找「本单元生效段」与「错误文本里写的 to」。
	cur := identityAt(snap.IdentitySegments, stem, unitID)
	if cur == nil {
		return jsonResult(repairEntityResult{OK: false, Kind: "identity", NeedsHuman: errText})
	}
	// 找 to 对应的身份段（按文案唯一反查）。
	to := extractToFromError(errText)
	target := resolveIdentityByTitle(snap.IdentitySegments, stem, to)
	if target == nil {
		return jsonResult(repairEntityResult{
			OK: false, Kind: "identity", NeedsHuman: errText,
			Note: fmt.Sprintf("身份 %q 在时间线里找不到对应段，请人工检查", to),
		})
	}
	// 把这条迁移从本单元挪到真正生效的单元。
	fromUnit := unitID
	toUnit := target.FromUnit
	// 读本单元所在卷的 state_changes，把 role_title 条目替换为引用。
	volumeID := fmt.Sprintf("v%02d", snap.Units[0].Volume)
	if unit, ok := findUnitIn(snap, unitID); ok {
		volumeID = fmt.Sprintf("v%02d", unit.Volume)
	}
	rel := fmt.Sprintf("outline/volumes/%s.md", volumeID)
	content, err := readBookFile(ctx, s.deps.Store, snap.Summary.ID, rel)
	if err != nil {
		return jsonResult(repairEntityResult{OK: false, Kind: "volume", NeedsHuman: err.Error()})
	}
	// 把本单元的 role_title 迁移改成引用（指向真正生效的单元）。
	updated := replaceRoleTitleInStateChanges(content, unitID, stem, toUnit)
	if updated == content {
		return jsonResult(repairEntityResult{
			OK: false, Kind: "volume", NeedsHuman: errText,
			Note: "本单元种子里没有可替换的 role_title 迁移",
		})
	}
	if err := writeBookFile(ctx, s.deps.Store, snap.Summary.ID, rel, updated); err != nil {
		return jsonResult(repairEntityResult{OK: false, Kind: "volume", NeedsHuman: err.Error()})
	}
	return jsonResult(repairEntityResult{
		OK: true, Kind: "volume", Path: rel, Field: "state_changes",
		After: fmt.Sprintf("%s.role_title=@%s", stem, toUnit),
		Note:  fmt.Sprintf("已将 %s 的身份迁移挪到 %s（真正生效的位置）", fromUnit, toUnit),
	})
}

// repairCastMissing 处理「人物卡中没有 stem=...」。
func (s *Server) repairCastMissing(ctx context.Context, snap domain.BookSnapshot, unitID, errText string) (*mcpgo.CallToolResult, error) {
	stem := extractStemFromError(errText)
	if stem == "" {
		return jsonResult(repairEntityResult{OK: false, Kind: "none", NeedsHuman: errText})
	}
	return jsonResult(repairEntityResult{
		OK: false, Kind: "cast", NeedsHuman: errText,
		Note: fmt.Sprintf("人物卡 %s 不存在；请用 create_book 或 write_book_file 建立 canon/cast/%s.md", stem, stem),
	})
}

// repairStateDelta 处理细纲 state_deltas 的结构/引用错误。
func (s *Server) repairStateDelta(ctx context.Context, snap domain.BookSnapshot, unitID, errText string) (*mcpgo.CallToolResult, error) {
	// 细纲的 state_deltas 错误通常是「字段名不在词表」「引用了不存在的人物」，
	// 这类错误需要模型重新生成，而不是本地改文件。
	return jsonResult(repairEntityResult{
		OK: false, Kind: "unit", NeedsHuman: errText,
		Note: "细纲的 state_deltas 需要按卷纲种子与身份时间线重新生成，请用 start_run(kind=outline) 重跑",
	})
}

// repairVolumeStateChange 处理卷纲 state_changes 的引用错误。
func (s *Server) repairVolumeStateChange(ctx context.Context, snap domain.BookSnapshot, unitID, errText string) (*mcpgo.CallToolResult, error) {
	// 卷纲的 state_changes 错误通常是「引用了不存在的 stem/field」，
	// 这类错误需要按身份时间线与人物卡修正。
	return jsonResult(repairEntityResult{
		OK: false, Kind: "volume", NeedsHuman: errText,
		Note: "卷纲的 state_changes 需要按身份时间线与人物卡修正，请用 edit_field(kind=volume) 修改",
	})
}

// ---- 文本提取辅助 ----

var stemRe = regexp.MustCompile(`[：:]\s*([a-z0-9][a-z0-9\-]*)\s*缺少生效的职务身份段`)

func extractStemFromError(errText string) string {
	m := stemRe.FindStringSubmatch(errText)
	if len(m) == 2 {
		return m[1]
	}
	// 「人物卡中没有 stem=xxx」
	if m := regexp.MustCompile(`stem=([a-z0-9][a-z0-9\-]*)`).FindStringSubmatch(errText); len(m) == 2 {
		return m[1]
	}
	return ""
}

func extractToFromError(errText string) string {
	// 「与本单元生效段 XXX 不一致」里取 XXX
	m := regexp.MustCompile(`与本单元生效段\s*([^\s；;]+)`).FindStringSubmatch(errText)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

func identityAt(segments []domain.IdentitySegment, stem, unitID string) *domain.IdentitySegment {
	key := books.UnitOrderKey(unitID)
	var best *domain.IdentitySegment
	for i := range segments {
		s := &segments[i]
		if s.Stem != stem {
			continue
		}
		from := books.UnitOrderKey(s.FromUnit)
		if from > key {
			continue
		}
		if s.ToUnit != "" && books.UnitOrderKey(s.ToUnit) < key {
			continue
		}
		if best == nil || from >= books.UnitOrderKey(best.FromUnit) {
			best = s
		}
	}
	return best
}

func resolveIdentityByTitle(segments []domain.IdentitySegment, stem, title string) *domain.IdentitySegment {
	for i := range segments {
		if segments[i].Stem == stem && segments[i].RoleTitle == title {
			return &segments[i]
		}
	}
	return nil
}

// replaceYAMLListField 替换 YAML 里的列表字段（on_stage: [a,b,c]）。
func replaceYAMLListField(content, field string, values []string) string {
	re := regexp.MustCompile(`(?m)^(\s*` + regexp.QuoteMeta(field) + `:\s*)\[[^\]]*\]`)
	return re.ReplaceAllString(content, "${1}["+strings.Join(values, ",")+"]")
}

// replaceRoleTitleInStateChanges 把卷纲某单元的 role_title 迁移改成引用。
func replaceRoleTitleInStateChanges(content, unitID, stem, toUnit string) string {
	// 找「## 单元 · unitID」到下一个「## 」之间的内容。
	lines := strings.Split(content, "\n")
	var out []string
	inUnit := false
	for _, line := range lines {
		if strings.HasPrefix(line, "## 单元 · "+unitID) {
			inUnit = true
		} else if strings.HasPrefix(line, "## ") {
			inUnit = false
		}
		if inUnit && strings.Contains(line, stem+".role_title=") {
			// 把整条迁移改成引用。
			line = strings.Replace(line, stem+".role_title=", stem+".role_title=@", 1)
			// 去掉 -> 后面的文案，只留引用。
			if idx := strings.Index(line, "->"); idx >= 0 {
				line = line[:idx] + "->" + toUnit
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
