package books

import (
	"fmt"
	"os"
	"path/filepath"
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
		"canon/writing-rules.md": `# Writing rules

- 单元 3–8 章（硬上限 10）
- 单章默认 2000–3500 字，以单元合同为准
- 正文只写 prose，章间单独一行 ---
- 不用 AI 套话，不提前打光终局底牌
`,
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
		"canon/reveal-schedule.md": `# 揭示计划
- schema_version: 1

# 只登记公开信息释放节奏，不写作者真相细节；真相在 canon/author-lore.md。
# 示例：## 揭示 · R-SECRET
# - public_summary: 可公开的信息概述
# - earliest_volume: v01
# - hint_unit_ids: v01-U1
# - confirm_unit_id: v01-U2
# - status: locked
# - locked_terms: 暂不可公开的关键词
`,
		"canon/identity-timeline.yaml": `# 职务身份时间线：单元边界对齐，单元内部身份恒定。
# 规划 Accept 时创建/续接；定稿只复核，不自动改本文件。
version: 1
segments: []
`,
		"continuity/state.yaml": `# 连续性状态账本（机器可读，按单元段）
# 每个已定稿单元追加一段；读取时取目标单元之前最近的一段。
# 只存随剧情变化的事实：位置、伤势、资源、目标、知情范围、伏笔。
# 静态人设见 canon/cast/，连载位置见 novel-state.yaml 顶层。
version: 1
units: []
`,
		"continuity/facts.md": fmt.Sprintf(`# Continuity facts — %s

读者已知事实的**人类可读视图**。机器读写走 continuity/state.yaml：
应用不再解析本文件，它只供作者翻阅。

## Open loops
| FS-id | 内容 | 状态 | 埋点章 | 计划回收 | 实际回收 |
|-------|------|------|--------|----------|----------|

Status: planted | advanced | paid | dropped.

## Chapter summaries
章摘要写 continuity/summaries/vNN.md；这里只留卷索引。
`, title),
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
