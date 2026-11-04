package books

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// ScaffoldFiles mirrors the novel expert's canonical/continuity tree. Existing
// files are never overwritten.
func ScaffoldFiles(root, title, genre string) error {
	files := map[string]string{
		"book-bible.md": fmt.Sprintf(`# Book bible — %s

## Reader promise
- Genre / channel: %s
- Core pleasure:
- Tone:

## One-line logline
>

## Protagonist
- Desire:
- Wound:
- Method:
- Lines they won't cross:

## Antagonistic force
-

## Story engine
How conflict renews each arc:

## Ending direction (spoiler-safe for author)
-

## 终局储备（约束，不得提前打光）
| 底牌 | 内容 | 最早解锁卷 |
|------|------|-----------|
| 头号宿敌 | | v |
| 终极真相 / 金手指上限 | | v |
| 升级台阶 | 共 档 | 每档约 卷 |

## Taboos / constraints
-

## Style card
- POV:
- Voice notes:
- Anti-patterns to avoid:
`, title, genre),
		"canon/world.md": fmt.Sprintf(`# World — %s

设定是约束，不是词条展览。新规则先查旧规则，冲突走 change request。

## 力量 / 规则
- 谁能做什么：
- 代价：
- 上限：
- 例外（必须写代价）：

## 势力地图
| 势力 | 占有资源 | 冲突线 | 与主角关系 |
|------|----------|--------|------------|
| | | | |

## 日常物质
- 钱 / 等价物：
- 交通：
- 通讯：
- 阶级观感（烟火气）：

## 禁忌与法律
- 碰什么会死 / 会被追杀：
- 谁来执行：

## 与圣经的衔接
- 读者承诺依赖的世界规则：
- 终局储备里尚未揭开的规则（本章不得写穿）：
`, title),
		"canon/author-lore.md": fmt.Sprintf(`# Author lore — %s

**Author-only.** 写正文与审校禁止加载。真相、未来反转、终局储备细节只在这里。
随剧情变化的事实写 continuity/state.yaml（定稿时自动追加）；锁词写 canon/locked-terms.yaml。

## 终局储备（细节）
| 底牌 | 真相（作者可见） | 最早解锁卷 | 公开层目前可见 |
|------|------------------|------------|----------------|
| 头号宿敌 | | v | 气味 / 化名 / 未知 |
| 终极真相 / 金手指上限 | | v | |

## 未揭规则
-

## 角色私密
| 角色 | 秘密 | 读者何时可推断 | 正文何时确认 |
|------|------|----------------|--------------|
| | | | |

## 未来反转库存
-
`, title),
		"canon/locked-terms.yaml": `# Locked terms
# locked_until: 卷号 -> 该卷之前正文禁用的真名/真相词
# compliance: 永久合规红线
locked_until: {}
compliance: []
aliases: {}
`,
		"canon/writing-rules.md": fmt.Sprintf(`# Writing rules

- 单元 3–8 章（硬上限 10）
- 单章 %d–%d 字，目标 %d 字：在细纲写 `+"`chapter_chars`"+`
- 正文只写 prose，章间单独一行 ---
- 不用 AI 套话，不提前打光终局底牌
`, domain.ChapterCharsMin, domain.ChapterCharsMax, domain.ChapterCharsDefault),
		"canon/style-fingerprint.md": `# Style fingerprint

## POV / 叙事距离

## 句长与段落

## 对话与潜台词

## 感官与物质细节

## 禁语 / 反模式
`,
		"canon/goldfinger.md": `# Goldfinger / resources

主角能力的完整人物卡仍以 canon/cast/<stem>.md 为准。

| 资源 / 能力 | 代价 | 上限 | 最近使用 | 备注 |
|-------------|------|------|----------|------|
| | | | | |
`,
		"canon/platform-positioning.md": `# Platform positioning

- 平台 / 频道：
- 目标读者：
- 篇幅 / 更新：
- 开篇承诺：
- 内容红线：
`,
		ForeshadowSchedulePath: `# 伏笔登记表（唯一权威来源）
- schema_version: 1

# 埋与收的完整生命周期。三级细化：总纲定卷、卷纲定单元、细纲定章。
# 与人物卡同级：带稳定 ID，被其他层按 ID 引用，写入前必须通过结构校验。
# 信息类伏笔即原「揭示计划」——读者何时可以知道某个秘密，是它的一个子集。
#
# 字段说明：
#   content          埋的是什么。必须是可见、可验的具体物或事；不接受「氛围」「张力」。
#   type             物件 / 信息 / 关系 / 承诺 / 疑点
#   plant_volume     埋入卷，vNN。总纲必填。
#   plant_unit       埋入单元，vNN-UN。卷纲补，可空。
#   plant_chapter    埋入章，整数。细纲补，可空。
#   payoff_volume    回收卷，vNN。总纲必填，且不早于 plant_volume。
#   payoff_unit      回收单元。卷纲补，可空。
#   payoff_chapter   回收章。细纲补，可空，必须严格晚于 plant_chapter。
#   payoff_summary   回收时读者得到什么。type=信息 时必填。
#   locked_terms     回收前不得出现的词，逗号分隔。可空。
#   status           planned / planted / advanced / paid / dropped
#   note             可选，一句话说明回收方式。
#
# 示例（五级全填）：
# ## 伏笔 · FS-01
# - content: ICU 同行登记表上的姓名笔迹与脑内声音的用词习惯对应
# - type: 信息
# - plant_volume: v01
# - plant_unit: v01-U7
# - plant_chapter: 38
# - payoff_volume: v01
# - payoff_unit: v01-U7
# - payoff_chapter: 43
# - payoff_summary: 脑内声音就是病床上的陆沉
# - locked_terms: 二〇二四、死档
# - status: planned
# - note: 由张桂兰说出儿子的姓名与住院时间完成确认
`,
		"canon/identity-timeline.yaml": `# 职务身份时间线：单元边界对齐，单元内部身份恒定。
# 规划 Accept 时创建/续接；定稿只复核，不自动改本文件。
version: 1
segments: []
`,
		"continuity/state.yaml": `# 连续性状态账本（机器可读，按单元段）
# 每个已定稿单元一段，每段都是全量快照：读取时取目标单元之前最近的一段即可。
# 只存随剧情变化的事实：位置、身体状态、资源、目标、关系、生命状态、知情范围、伏笔。
# 静态人设见 canon/cast/，连载位置见 novel-state.yaml 顶层。
version: 1
units: []
`,
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}
