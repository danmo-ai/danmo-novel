---
audience: author
---

# 连载轨（作者 / 读者账本）

长篇连载要把「作者手里的底牌」和「读者已经知道的事」分开，否则模型会提前揭底。正文层面的知情边界与揭示规则见「连续性与信息控制」；本文只管这些账落在哪个文件、谁读。

## 三轨

| 轨 | 路径 | 谁可读 | 来源 |
|----|------|--------|------|
| **author-lore** | `canon/author-lore.md` | 作者 / 规划 / 改纲 | 立项时写入；含终局细节、隐藏规则、角色私密 |
| **state（状态账）** | `continuity/state.yaml` | 写正文、审稿、细纲（应用自动读取） | 每个已定稿单元一段：该单元结束时的位置/伤势/资源/目标/知情范围 + 未回收伏笔。读取取「目标单元之前最近的一段」，续写与重写同源 |
| **summaries（章摘要）** | `continuity/summaries/vNN.md` | 接受定稿写入；只有单元第一章带卷摘要尾 | 每章一个 `## chNNN` 块。按卷拆分只为缩小文件 |
| **commits（执行日志）** | `continuity/commits/vNN-U#.md` | 作者 / 回溯 | 每单元一个：本地检查结果 / 锁词核对 / 字数实测 / 扩写技术 / 偏离登记。不进 facts |
| **locked-terms** | `canon/locked-terms.yaml` | 上下文约束 / 定稿核对 | 本卷之前不得出现的真名/真相词；`locked_until` 按卷解锁 + `compliance` 永久红线 |

`candidate` vs `canon` 管人物卡能不能进正文。三轨管信息对读者是否已公开 + 锁词是否提前泄漏。

## 账本体积治理

账本分三层，各管一件事，不重复：

- `continuity/state.yaml` 存按单元段的状态（机器读写）。
- `continuity/summaries/vNN.md` 存章摘要（只有第一章带卷摘要尾）。
- `continuity/commits/` 存执行日志（永不进上下文）。

连载位置只由 `novel-state.yaml` 顶层标量表达，不在别处再记一遍。gate 结果、锁词扫描、字数实测、扩写技术、偏离登记全部写进 commits——state 只回答「进这一单元时世界是什么样」，commits 回答「我们这一轮做了什么」。

## 写正文时加载什么

正文上下文由应用按当前单元构建，默认含：单元合同与场面节拍、`on_stage` 人物精简卡（身份 + 三锚点 + 台词 + POV 不知）、在场关系、公开事实、上一单元文末与最近摘要、风格指纹、写作规则、锁词、揭示约束、精简题材约束与故事时钟。角色卡不全量进入，只按细纲 `on_stage` 注入需要的部分；不重复附加多份相似知识——卡文或专项救援时，作者在确认窗口勾选最相关的一篇。

禁止：`author-lore.md`、圣经「终局储备」真相细节、未接受草稿里的未来剧透、`continuity/summaries/` 归档全量通读。知识文章只提供方法，不覆盖已接受事实或透露答案。

**单元细纲阶段**：上下文提供本卷人物、公开伏笔与揭示节奏，用于对齐 `state_deltas` 起点。写细纲前允许读 facts 的 `### Cast snapshot` 小节（只读涉及角色的行）与点名人物卡的「关系」段；仍不读 facts 全文、不读 author-lore。

## Legacy

旧书可能仍有 `continuity/ledger.md` / `public-lore.md` / `tracking.md` / `chapter_summaries.md` / `foreshadow-tracker.md`，这些不再读取。迁移只保留两类：状态并进 `state.yaml`，执行日志并进 `commits/`。

## 接受定稿

`## chNNN` 摘要块写入 `continuity/summaries/vNN.md`；应用按细纲的 `state_deltas` 自动追加 `state.yaml` 段，关系质态变了时更新相关人物卡。应用记录运行与候选；长期保存人工审校、锁词与字数核对可另写 `continuity/commits/vNN-U#.md`，不要混进公开事实。接受后抽查时间线、人物状态、身份与章范围；候选未接受前不得提前当作已发生事实。

## 伏笔表

伏笔是前段悄悄留下、回头看合得上的信号。分四类登记，不要只记「以后有反转」：

| 类 | 记什么 |
|----|--------|
| 主线 | 核心阴谋、最终真相、主线任务 |
| 人物 | 身份、秘密、立场会变 |
| 道具 | 信物、令牌、功法、武器 |
| 世界观 | 隐藏规则、力量体系、历史真相 |

每条写进 facts 的 Open loops：**FS-id / 内容 / 状态（planted/advanced/paid）/ 埋点章 / 计划回收卷 / 实际回收章**。藏得住、收得回、回头看合理；坑比能填的多就先停手。作者侧真相仍只在 author-lore，不要写进这张读者账。总纲 `book_outline.md` 只列计划层（FS-id/内容/埋点卷/计划回收卷），运行时状态在 facts 维护。

## Checklist

- [ ] 正文未使用 author-lore 中未到解锁卷的底牌
- [ ] facts 没有作者侧真相
- [ ] Open loops ≤5，无 dangling
- [ ] 已对照 `canon/locked-terms.yaml` 核对正文，锁词无泄漏
- [ ] facts 无 `## chNNN` 块；章摘要在 `continuity/summaries/vNN.md`，卷总结在该文件末尾
- [ ] 人工执行日志若保留，放在 `commits/vNN-U#.md`，未污染 facts
- [ ] 写作与审校上下文未包含 author-lore
