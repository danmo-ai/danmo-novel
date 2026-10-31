---
stages: [plan, outline, review]
---

# 连载轨（作者 / 读者账本）

长篇连载要把「作者手里的底牌」和「读者已经知道的事」分开，否则模型会提前揭底。

## 三轨

| 轨 | 路径 | 谁可读 | 来源 |
|----|------|--------|------|
| **author-lore** | `canon/author-lore.md` | 作者 / 规划 / 改纲 | 立项时写入；含终局细节、隐藏规则、角色私密 |
| **facts（读者账）** | `continuity/facts.md` | 写正文、审稿、续写 | 从**已接受章节**维护：公开事实 + 进度游标 / 人物状态 + 未关闭伏笔 + 每卷 summaries 索引。**不存章摘要** |
| **summaries（章摘要）** | `continuity/summaries/vNN.md` | 接受定稿写入；续写只看必要的上一章块 | 每章一个 `## chNNN` 块；卷收束在末尾追加卷总结。按卷拆存只为缩小文件，不增加注入 |
| **commits（执行日志）** | `continuity/commits/vNN-U#.md` | 作者 / 回溯 | 每单元一个：本地检查结果 / 四计数 / 锁词核对 / 字数实测 / 扩写技术 / 偏离登记。**不进 facts**，避免日志污染事实层 |
| **locked-terms** | `canon/locked-terms.yaml` | 上下文约束 / 定稿核对 | 本卷之前不得出现的真名/真相词；`locked_until` 按卷解锁 + `compliance` 永久红线 |

`candidate` vs `canon` 管人物卡能不能进正文。三轨管 **信息对读者是否已公开** + **锁词是否提前泄漏**。

## 账本体积治理

章摘要在接受定稿时写入 `continuity/summaries/vNN.md`；facts 只留公开事实、cursor、人物状态、未关闭伏笔、每卷一行索引（`vNN → continuity/summaries/vNN.md`）。facts 体积应约随事实、人物和伏笔增长，而不是复制所有章摘要。人物状态与伏笔只保留一份，不按卷重复。迁移旧书时，把 facts 中的 `## chNNN` 块拆到 summaries，并先备份原文件。

**执行日志不进 facts。** gate 结果、四计数、锁词扫描、字数实测、扩写技术、偏离登记 → 全部写 `continuity/commits/vNN-U#.md`。facts 只回答"读者现在知道什么"，commits 回答"我们这一轮做了什么"。

## 写正文加载

正文上下文由应用按当前单元构建，默认包含：单元合同与场面节拍、`on_stage` 人物的精简卡（身份 + 三锚点 + 台词 + POV 不知）、在场关系、公开事实、上一单元文末与最近摘要、风格指纹、写作规则、锁词、揭示约束、精简题材约束与故事时钟。

**角色卡不全量进入正文上下文**：只按细纲 `on_stage` 注入需要的部分。不重复附加多份相似知识全文；卡文或专项救援时，作者可在确认窗口勾选最相关的一篇。

禁止：`author-lore.md`、圣经「终局储备」真相细节、未接受草稿里的未来剧透、`continuity/summaries/` 归档全量通读。知识文章只提供方法，不能覆盖已接受事实或透露答案。

**单元细纲阶段**：上下文提供本卷人物、公开伏笔与揭示节奏，用于对齐 `state_deltas` 起点；不向写作与审校上下文提供 author-lore。

禁止：`author-lore.md`、圣经「终局储备」细节栏、未 Commit 草稿里的剧透、`continuity/summaries/` 归档全量通读。

**单元细纲阶段例外**：写细纲前允许读 facts 的 `### Cast snapshot` 小节（只读涉及角色的行）与点名人物卡的「关系」段，用于对齐 `state_deltas` 起点；仍不读 facts 全文、不读 author-lore。

## Legacy

旧书可能仍有 `continuity/ledger.md` / `public-lore.md` / `tracking.md` / `chapter_summaries.md` / `foreshadow-tracker.md`。迁移前先备份，再人工合并进 `facts.md`（事实）与 `commits/`（执行日志）；不要假设旧路径会自动兼容。

## 接受定稿

每单元定稿接受时：
1. `## chNNN` 摘要块写入 `continuity/summaries/vNN.md`；刷新 `facts.md` 的公开事实、人物状态与伏笔；关系质态变了时更新相关人物卡。
2. 应用记录运行与候选；如需长期保存人工审校、锁词与字数核对，可另写 `continuity/commits/vNN-U#.md`，不要混进公开事实。
3. 接受后抽查时间线、人物状态、身份与章范围；候选未接受前不得提前当作已发生事实。

## 伏笔表

伏笔是前段悄悄留下、回头看合得上的信号。分四类登记，不要只记「以后有反转」：

| 类 | 记什么 |
|----|--------|
| 主线 | 核心阴谋、最终真相、主线任务 |
| 人物 | 身份、秘密、立场会变 |
| 道具 | 信物、令牌、功法、武器 |
| 世界观 | 隐藏规则、力量体系、历史真相 |

每一条写进 facts 的 Open loops：**FS-id / 内容 / 状态（planted/advanced/paid）/ 埋点章 / 计划回收卷 / 实际回收章**。藏得住、收得回、回头看合理。坑比能填的多，就先停手。作者侧真相仍只在 author-lore，不要写进这张读者账。**总纲 `book_outline.md` 只列计划层（FS-id/内容/埋点卷/计划回收卷），运行时状态在 facts 维护**。

## Checklist

- [ ] 正文未使用 author-lore 中未到解锁卷的底牌
- [ ] facts 没有作者侧真相
- [ ] Open loops ≤5，无 dangling
- [ ] 已对照 `canon/locked-terms.yaml` 核对正文，锁词无泄漏
- [ ] facts 无 `## chNNN` 块；章摘要在 `continuity/summaries/vNN.md`，卷总结在该文件末尾
- [ ] 人工执行日志若保留，放在 `commits/vNN-U#.md`，未污染 facts
- [ ] 写作与审校上下文未包含 author-lore

## 来源

伏笔设计｜挖坑一时爽，填坑火葬场？一张伏笔表全搞定。
