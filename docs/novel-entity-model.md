# Danmo Novel 实体模型与生产链契约

本文档是小说生产链的唯一实体契约来源。每个字段都有：**唯一来源、可见范围、使用阶段、确认后的写回路径**。

## 三层可见性

| 层 | 内容 | LLM 可见范围 |
|---|---|---|
| 稳定 Canon | 人物卡、世界规则、资源、总纲、卷纲、单元合同 | 按阶段、按当前任务选择性读取（字段级抽取，不是整份塞入） |
| 作者底牌 | 终局真相、未揭规则、角色秘密、未来反转 | 立项/重新立项/优化设定可用；**写正文、续写、审校禁读**（Context Builder 层强制，fail-fast） |
| 公开连载事实 | 读者已知事实、人物当前状态、公开伏笔、已定稿时间 | 写作和审校读取；**只能由定稿接受更新** |

## 实体总表

| 实体 | 文件 / 位置 | 核心字段 | 用于 | 写回时机 |
|---|---|---|---|---|
| 书籍状态 | `novel-state.yaml` | `book_id / genre / stage / active_unit / time_system / last_committed_ch / next_action` | 所有阶段的流程控制 | 任务状态变化、定稿 |
| 作者意图 Bible | `book-bible.md` | 读者承诺、logline、主角目标、故事引擎、终局方向、风格卡 | 立项；写作只取相关约束 | 立项/重新立项接受；优化设定 |
| 全书总纲 | `outline/book_outline.md` | 核心悬念、分卷目标、伏笔计划 | 立项、新卷细纲 | 立项/重新立项接受 |
| 卷纲 | `outline/volumes/vNN.md` | 卷目标、时间线、本卷人物 stem、单元索引表、终局边界 | 细纲、写作、审校 | 生成/重写卷纲接受 |
| 单元合同 | `outline/units/vNN-UN.yaml` | `desire/obstacle/choice/payoff`、`scenes`、`pov`、`story_day`、`gap_from_prev`、`next_hook`、`info_control`、`state_deltas` | 写正文、审校、定稿 | 细纲接受（字段完整保留）；卷纲只播种缺失单元 |
| 人物卡 | `canon/cast/<stem>.md` | `stem / name（纯姓名）/ tagline（一句话身份说明）/ role / 三锚点 / 不知 / 台词 / 关系表` | 细纲、写作、审校 | 立项/重新立项接受；定稿只更新允许变化部分 |
| 人物关系 | 人物卡 `## 关系` 表（对方填 stem） | `from/to/type/current` | 细纲、在场人物写作、设定页关系图 | 立项写入；定稿可推进；**正式表为 canon**；`关系锚点`/`与主角关系` 仅辅助虚线 |
| 身份时间线 | `canon/identity-timeline.yaml` | `stem / role_title / org / from_unit / to_unit`（单元边界对齐） | 写作、审校（本单元身份注入快照）、细纲校验 | 作者编辑；身份转换只在单元边界 |
| 世界规则 | `canon/world.md` | 力量、代价、上限、势力、日常、法律 | 立项、场景写作、审校 | 立项/重新立项；优化设定 |
| 资源/金手指 | `canon/goldfinger.md` | `kind / cost / limits / last_uses` | 写作、审校 | 定稿更新使用记录 |
| 平台定位 | `canon/platform-positioning.md` | 平台、读者、篇幅、更新、红线 | 立项、写作节奏、审校 | 立项与编辑 |
| 风格指纹 | `canon/style-fingerprint.md` | POV、句段、对话、感官、禁语 | 写作、局部编辑、审校 | 作者编辑 |
| 写作规则 | `canon/writing-rules.md` | 单元/章规模、格式、衔接、禁令 | 写作、审校 | 生产规则编辑 |
| 作者底牌 | `canon/author-lore.md` | 真相、秘密、反转、最早解锁卷 | 立项、优化设定 | 立项/重新立项；**正文/审校禁读** |
| 揭示计划 | `canon/reveal-schedule.md` | 信息、可暗示内容、最早确认卷、状态 | 立项、细纲、写作、审校 | 立项/重新立项；揭示推进后 |
| 锁词 | `canon/locked-terms.yaml` | `locked_until / compliance / aliases` | 写作约束、定稿扫描 | 解锁时先移除再写正文 |
| 连续性状态 | `continuity/state.yaml` | 按单元段：位置、伤势、资源、目标、知情范围、伏笔状态 | 写作、审校、细纲 | 定稿接受（从 `state_deltas` 落段） |
| 连载游标 | `novel-state.yaml` 顶层 | `last_committed_ch / active_unit / next_action` | 流程控制 | 定稿接受 |
| 故事时钟 | 单元合同 `story_day / gap_from_prev / flashback / time_label` | 单元剧情时钟 | 细纲、写作（时间规范注入）、审校、定稿校验 | 细纲与定稿 |
| 章节摘要 | `continuity/summaries/vNN.md` | 事件、状态变化、伏笔、钩子、下章指向 | 续写只读上一章摘要 | 定稿接受 |
| 定稿执行日志 | `continuity/commits/<unit>.md` | 门禁、字数、锁词、修订、偏离 | 回溯排查；**不默认给模型** | 定稿接受 |
| 正文 | `units/vNN-UN.md` | 章号、标题、正文、base_revision | 写作、局部编辑、审校 | 接受候选 |
| 审校问题 | SQLite candidates | `severity / source / lens / evidence / suggestion` | 审校、修订 | 作者勾选后修订 |
| 运行/调用记录 | SQLite runs | 书籍、类型、模型、Token、耗时、上下文摘要、错误 | 排查与恢复；**不是 canon** | 每次任务 |
| 参考知识 | `~/.danmo-novel/library/knowledge/*.md`（全局，不属于某书） | 标题、正文（方法/题材/术语/灵感） | 作者在确认窗口**勾选**后作为独立 block 附加（方法/风格参考，不写入正文事实） | 作者编辑；**不是 canon，不进默认上下文** |

### 参考知识的定位

行业实践（Novelcrafter Codex / Sudowrite）的一致约束：**知识库默认不进上下文，作者显式勾选才附加**，避免"写作教材污染 prose 文风"。本书采用同样原则：

- 知识库是**作者的私有参考资料**，全局共享，不属于某一本书。
- 入口在**创作确认窗口**的「参考知识」折叠区，不在右上角常驻。
- 勾选的文章经 `TaskSpec.KnowledgeIDs` 传递，`Runner.withKnowledge` 在 LLM 调用前附加到 user prompt，标注「仅作方法/风格参考，不写入正文事实」。
- 与 `ExtraContextIDs`（outline 的 unitIds）语义分离，互不占用。

## 状态流

```
proposed → accepted → drafted → reviewed
```

- `proposed`：规划接受后播种的单元骨架（含空 `scenes` / `next_hook` / 时钟字段）
- `accepted`：细纲候选被接受，合同字段完整写入
- `drafted`：正文候选被接受，`units/<id>.md` 落盘
- `reviewed`：定稿接受，连续性账本回写完成

## 人物来源与临时人物机制

人物卡只有一个 canon 来源：**立项 / 重新立项（plan）生成 + 接受**，或作者手建/晋升。细纲与写作可以引入新人物，但走「候选 → 作者批准 → 晋升 canon」路径，**绝不自动插入 canon**。卷纲模式不写人物卡。

## 设定分层与重写依据

| 层 | 文件 | 重写依据 | 正文是否注入 |
|---|---|---|---|
| **总纲派生** | 总纲、bible、人物卡、揭示计划、world、goldfinger、底牌 | 必须带总纲及相关结构（只读） | 人物压缩卡、揭示按卷截取进正文/审校 |
| **生产约束** | writing-rules、style、platform、locked-terms | 可独立优化（规则/风格） | 写作规则/风格/锁词 |
| **连载账本** | continuity/、novel-state | 不重写；定稿回写 | 字段级抽取 |

- `rewrite_entity` 按目标类型组依据：人物卡→总纲+卷纲+揭示+公开状态；揭示计划→总纲+卷纲+公开账本；工艺类不带总纲。
- 揭示计划正文/审校按当前卷注入「可暗示/可确认 + 禁止写穿」，不含底牌细节。
- 卷纲上下文人物收敛为本卷在场 + 主角 + 上一卷在场，不灌全书卡。

## 操作分层（立项 / 卷纲 / 局部优化）

| 操作 | TaskSpec | 生成范围 | 接受写盘 |
|---|---|---|---|
| **立项** | `kind=plan mode=init` | 总纲派生设定成套 + 人物 | 只写总纲/白名单设定/候选人物；不写卷纲、不播种单元 |
| **重新立项** | `kind=plan mode=reinit` | 与立项对等 | 覆盖总纲/设定/人物；`preservePaths` 勾选文件保留不覆盖；不碰卷纲/正文/账本 |
| **生成/重写卷纲** | `kind=plan mode=volume` | 当前卷纲 + 单元索引；跨卷附上一卷截取 | 只写当前卷文件；只播种缺失单元 |
| **重写设定 / 重写人物卡** | `kind=rewrite_entity` | 单个实体 + 依据包 | 仅该文件；依据只读 |

主流程依据：是否完成立项看 `outline/book_outline.md`；有总纲无卷纲时主按钮为「生成卷纲」。

| 类型 | 来源 | 状态 | 能否进 on_stage |
|---|---|---|---|
| Canon 人物 | plan 接受 / 作者手建 / 晋升 | `canon` | 自由使用 |
| 候选人物 | 细纲/写作引入新 stem，接受时播种骨架卡 | `candidate` | 可进（advisory 提示），晋升后才算正式人物 |
| 功能路人 | 一次性角色（报案群众、值班民警、摊主） | 不建卡 | 不进 on_stage，写在 scenes 的 who/beat 描述 |

- **on_stage 门**：新 stem 给 advisory（不 blocking），提示"新人物待晋升"。
- **播种**：`SeedCandidateCast` 只写骨架卡（stem + `status:candidate` + `role:recurring` + 待补提示），已有卡不覆盖。
- **晋升**：`POST /books/:id/cast/:stem/promote`，要求卡内已补三锚点或知识边界（防空壳晋升）；晋升仅把 `status` 改为 `canon`。
- **prompt 约束**：`OutlineSystemPrompt` 要求模型区分"必须建卡的新人物"与"不必建卡的路人"，路人不进 on_stage。

## 人物角色规范

`role` 统一为三个值，兼容旧值：

| 规范值 | 兼容旧值 | UI 显示 |
|---|---|---|
| `protagonist` | `hero` / `main` | 主角 |
| `volume_antagonist` | `antagonist` / `villain` | 卷反派 |
| `recurring` | 其他/空 | 常驻 |

人名与一句话说明分离：`name` 只存纯姓名，`tagline` 存身份说明（如「痕迹检验专家」「白家第一代」）。角色单独存 `role` 字段，不得混入 `(protagonist)` / `（主角）` 标注。

- **旧格式兼容**：loader 的 `splitNameTagline` 把 `# 姓名（说明）` 拆成 `name` + `tagline`；纯角色标注（`（主角）`/`(protagonist)` 等）直接丢弃；显式 `` `tagline`: `` 字段优先。
- **接受规划**：`AcceptPlan` 规范化 heading 为纯姓名，tagline 写入 `` `tagline`: `` 字段。
- **UI 展示**：姓名 + role 徽标（主角绿 / 卷反派红 / 常驻灰）+ tagline 小字，三层分离。

## 剧情时钟的应用

剧情时钟不只是展示字段，它贯穿生产链三层：

1. **写作包注入**（`buildTimeGuidance`）：把 `story_day/gap/flashback` 转成可执行的正文时间规则——
   - 有 story_day：正文只用相对锚点（第 N 天/次日/当晚），**禁写具体年月日或星期**（与卷纲「卡层分段装置，不反向给正文钉日期」一致）。
   - 有 gap_from_prev：开场必须体现时间间隔，状态/位置不得无解释跳变。
   - flashback：闪回段必须带时标，回现在要有切换标记。
   - 昼夜/天气/季节与上一单元连续，同章内时间不倒流。
2. **连续性校验**（`ValidateClockContinuity`）：卷内 story_day 必须单调不减（闪回除外）。接受细纲与定稿接受时，故事日倒退（非闪回）报 blocking；字段缺失报 advisory 提示回填。
3. **UI 展示**：大纲页「剧情时钟」视图按 story_day 排列单元，灰色节点标出待回填的时钟字段。

## 职务身份时间线

人物职务/身份（如「内勤新警 → 刑侦线新警 → 重案队试用 → 特聘专家」）是跨单元变化的动态属性。核心规则：**单元内部身份恒定，转换只落在单元边界**；情节需要中途转换时必须拆单元。

- **数据**：`canon/identity-timeline.yaml` 的 `segments[]`，每段 `{stem, role_title, org, from_unit, to_unit}`，按 stem + from_unit 排序；`to_unit` 空 = 至今。这是身份的唯一来源，优先于人物卡散文里的静态身份。
- **对齐**：身份段以 `unit_id` 索引，与卷纲/单元索引/细纲天然对齐（同一坐标系）。`IdentityAt(segments, stem, unitID)` 取该单元有效身份段。
- **上下文**：`ExtractCastForWriteWithIdentity` 在写作/审校快照里注入「本单元身份：职务 · 机构」，模型看到的永远是本单元此人是谁。
- **校验**：接受细纲时 `ValidateNoIntraUnitChange` 拦截合同里的身份转换声明（blocking，要求拆单元）；`ValidateUnitIdentity` 检测同一单元身份段重叠（数据错误）。
- **UI**：设定页人物卡显示身份时间线（当前段高亮）；单元合同头部显示 on_stage 人物本单元身份 chip。

## 标题规范

- **单元标题**（`title_working`）：2–8 字，概括本单元核心动作或意象（如「入门试炼」「两种手艺」），与 `function` 呼应但不照抄；不用「单元」「第X」字样，不剧透终局。
- **章节标题**：2–6 字短意象或动作，取自本章具体物件、地点、动作或一句话（如「雪夜」「脚印会说话」）；不用「危机/高潮/转折/揭秘」空泛词，不剧透未发生的反转，不堆砌形容词。
- 规范由 `OutlineSystemPrompt` / `WriteSystemPrompt` 在生成时约束。

## 写作上下文（字段级抽取）

`BuildWritePacket` 按以下字段组装，**不塞整份账本、整份人物卡或完整细纲**：

1. **单元合同**：`NormalizeUnitContract`（时钟字段显式化）+ `ChapterBeats`（scenes 优先于整份 YAML）
2. **on_stage 人物**：`ExtractCastForWrite` → 名称+规范 role、三锚点、POV「不知」、一句台词、与主角关系（≤ 整卡压缩）
3. **在场关系**：仅 on_stage 之间的正式关系表行
4. **公开连载事实**：`ExtractFactsForWrite` → 连载游标 + on_stage 的 Cast snapshot 行 + 提及 on_stage 的相关伏笔（最多 6 条）
5. **上一单元钩子**：上一单元文末 1200 字 + 最近卷摘要尾部
6. **风格指纹**：压缩至 480 字
7. **写作规则**：≤ 900 字
8. **当前卷锁词**：`ExtractLockedTerms`，只提示到期卷，不展开未来秘密
9. **题材写时约束**：知识库按题材生成

`AssertNoAuthorLore` 在写作/细纲/审校包构建后 fail-fast 校验：任何 `author-lore` / `作者底牌` / `终局真相` / `未来反转` / `未揭规则` 来源都不允许进入。

## 定稿回写（唯一路径）

只有 `Accept` 定稿候选能写 canon 与连续性账本；生成阶段永不触碰。

`ApplyFinalizationLedger(root, unit, chapters, findings)` 一次性更新：

1. 单元状态 → `reviewed`
2. `continuity/state.yaml`
   - `### Cursor`：`last_committed_ch` / 当前卷单元 / next action
   - `## Open loops`：`state_deltas` 中标记 `paid`/`payoff` 的 FS-id → `paid`，回填实际回收章
   - `### Cast snapshot`：on_stage 人物缺失行按 `state_deltas` 的 position/status 补入，已有行保持
3. `continuity/summaries/vNN.md`：每章一行（事件 / 状态变化 / 伏笔 / 钩子）。**upsert**：重写单元时按 `continuity/commits/<unit>.md` 记录的旧章范围先删除旧行，再写新行，同一章永不重复。
4. `continuity/commits/<unit>.md`：定稿时间、章范围、总字数、门禁计数、next_hook、state_deltas

## 连续单元重写（Chain）

在总纲、卷纲、设定已确定（**链不重写它们**）的前提下，按卷内单元顺序自动完成 `细纲 → 正文 → 审校 → 定稿`。

| 项 | 契约 |
|---|---|
| 范围 | 指定卷内，从 `fromUnitId` 到卷末（可选 `toUnitId` 收窄） |
| 自动化 | 四个阶段的候选**自动接受**；任一门禁 blocking、调用/解析失败、Accept 拒绝 → 立即 `waiting_manual` |
| 触发 | `POST /api/chains`；进度 `GET /api/chains/:id`；`.../resume`、`.../cancel` |
| 串行 | 一次一个单元、一个阶段。下一单元写作必须读到上一单元的定稿回写；`Runner.KnowledgeDocs` 也是共享字段 |
| 重写 vs 新写 | 同一状态机；差别只在覆盖前是否备份（`backups/<stamp>-chain/units/<id>.md`，`backups/` 被 loader 忽略） |
| 恢复 | 按单元当前状态推导起点：`proposed→细纲`、`accepted→正文`、`drafted→审校`；已 `reviewed` 视为人工已完成。该单元仍有待审候选时拒绝恢复 |

**与手工点击完全同源**：链不构造 prompt，每阶段都用同一个 `TaskSpec` 走 `runs.Service` → `Pipeline` → `ContextBuilder`，系统提示词、上下文块、调用预算与手工同 kind 时逐字节一致。想核对输入用 `POST /api/runs/preview`。

**分阶段追加指令与参考知识**：`ChainSpec.Outline` / `ChainSpec.Write` 各带一份 `instruction` + `knowledgeIds`，整条链对该阶段所有单元生效。`ChainSpec.Review` 只带 `knowledgeIds` —— 审校没有「追加指令」的语义，判定规则来自审校清单与本地门禁，作者的自由文本传进来会被 `normalizeSpec` 丢掉。`finalize` 不带（本地检查，无 LLM 调用）。

- 指令落到 `UserPayload.instruction`，作为默认格式与门禁约束之外的「补充要求」追加（`withUserInstruction`），默认文本永不替换；`outline` / `write` 的 `TaskSpec.Instruction` 现在也用于手工点击。
- 参考知识沿用既有语义：`TaskSpec.KnowledgeIDs` → `loadKnowledge` → `Runner.KnowledgeDocs` → user payload `references`，仅作方法/风格参考，不写入正文事实；启动前用 `knowledge.List` 校验，缺失直接报错。

**审校分章：人工与链读同一个开关**。`settings.review.splitByChapter` 是唯一来源——人工路径在 `runs.Service` 里读它，链通过 `reviewSplitByChapter()` 读同一个值（包括自动复审那一次）。链不再强行 `true`：以前链固定分章、人工默认整单元，同一个单元在两条路下会得到不同粒度、不同条数的 findings。

- 默认 `true`（按章）：prompt 小、注意力集中，不会撞输出预算被整单元结论挤爆。
- 关掉则整单元一次调用：跨章连续性问题更容易一起发现，但长单元可能截断。
- 设置在「设置 → 创作阶段 → 审校设置」里，`GET/PUT /api/settings/review`。

**审校意见必须能定位到章节**：逐章修订按 `finding.chapter` 分配工作，章号为 0（整单元级意见）或超出单元范围时，`RunRevise` 现在直接报错并列出这些意见。以前是静默无操作——候选与原稿逐字节相同，作者点「确认落盘」等于什么都没发生。
5. `novel-state.yaml`：`last_committed_ch`

## LLM 调用的定向修复（Retry 策略）

原则：**retry 必须定向 + 带证据，不是"再试一次"**。盲目全量重生成会改坏已通过的内容。

| 操作 | 修复方式 | 预算 |
|---|---|---|
| 写正文（整单元，未分章） | 一次定向修复：回注哪一章缺什么 | MaxCalls=2 |
| 写正文（分章） | 每章最多一次结构修复（空 body / 章号错）；写成了但过不了本地硬门，另有一次按章修复 | 每章 ≤3 次 |
| 细纲 | 一次定向修复：回注具体哪个单元哪个字段缺失/越界，要求只补该字段 | generate + 1 repair |
| 立项/重新立项/重写卷纲 | 一次：回注结构错误（缺设定/缺单元索引表/缺 protagonist/人物卡字段不全） | generate + 1 repair |
| 实体重写 | 一次定向修复 | generate + 1 repair |
| 审校（整单元 / 分章） | 一次定向修复：回注 `verdict`/`severity`/`id`/`title`/`evidence` 违规项；截断则翻倍预算重试 | 每章 ≤2 次 |
| 定向修订（按章） | 一次定向修复：回注章号/正文问题 | 每章 ≤2 次 |
| 局部编辑 / 定稿 | **不 retry**：局部编辑失败直接报；定稿不走 LLM | MaxCalls=1 |

链路的自动修复是**两轮封顶**（`maxAutoReviseRounds = 2`）：每轮 = 一次定向修订 + 一次复审，复审清零就继续走接受流程，两轮仍阻断则停机并把「已自动修订 N 轮」写进中断原因。两轮的依据是修订会引入新矛盾——加长正文最容易踩时间线，所以第一轮复审常常带着新问题回来。

`MaxCalls` 对分章路径是真实上限，不是装饰：写作按 `章数 × 3`、审校与修订按 `章数 × 2` 计，因为每章自身还能重试一次。之前按章数计，一旦发生修复，UI 就会显示 `5/4` 这种自相矛盾的预算。

实际生效的是 packet 上的 policy，不是 `DefaultPolicy` 的默认值：`BuildReviseChapterPacket` 与 `BuildRewriteEntityPacket` 都把 `MaxCalls` 提到 2、`AllowOneRepair` 设为 true。

修复证据写入 user JSON 的 `repair` 字段（`MergeRepairIntoUserPayload` + `detailedFindings`）：保留已通过部分原样、只修指出的问题、返回完整合规 JSON。错误信息带 evidence（差多少）+ suggestion（怎么改），不是只有标题。

## 结构化输出校验

契约写在 system prompt 里，但"说过"不等于"做到"。`core/service/pipeline/validate.go` 把每个任务的必填字段变成可执行检查：**违规 → 聚合成一条错误文本 → 写进 `repair` → 重试一次**。缺字段不再被零值静默吸收（`severity=""` 被当 advisory、`chapter=0` 排序错位、漏章让摘要与账本游标错位）。

| 任务 | 必填 / 必须满足 | 违反后果 |
|---|---|---|
| 正文（整单元） | `chapters[].chapter > 0` 唯一、`body` 非空、章号集合完整覆盖单元章范围 | 重试；再失败则中断 |
| 正文（分章） | 恰好 1 章、`body` 非空 | 该章重试一次；再失败则中断整单元 |
| 细纲 | `scenes[].chapter` 落在本单元范围内、每章至少 1 个 scene（prompt 要求 2 个，硬性底线是 1） | 重试 |
| 立项 | `cast[].stem/name/role/cardMarkdown` 齐全、`stem` 为英文小写连字符、至少 1 个 `protagonist` | 重试 |
| 审校 | `verdict ∈ {pass,revise}`；每条 finding 的 `id/title/evidence` 非空、`severity ∈ {blocking,advisory}`、分章审校时 `chapter` 等于被审章 | 重试 |
| 定向修订 | `chapter` 等于请求章、`body` 非空 | 重试；再失败则中断 |
| 实体重写 | `content` 非空 | 重试；再失败则中断 |
| 局部编辑 | `replacement` 非空 | 中断（MaxCalls=1） |

两个刻意的例外：

- **`severity` 不做别名归一**。`fatal`/`high` 这类值若被静默当 advisory，一条真硬伤就被藏起来了；宁可让模型按枚举重写。
- **截断抢救路径只做安全规范化**。输出被预算截断时，抢救出的 finding 补 id、`severity` 一律降为 advisory（截断产物判不了等级），并追加一条 budget advisory。重试由预算翻倍负责，不靠丢弃结论。
- **可推导字段安全补齐**：立项 `status` 空 → `candidate`（prompt 已规定立项一律 candidate）；审校 `chapter` 为 0 → 补本次审校章号；修订 `title` 空 → 沿用原标题。这些是"没说"而非"说错"，补值不会引入虚构数据。

## 重试与 KV cache 复用

修复重试天然适合前缀缓存：system prompt 是静态的，`repair` 只做**追加**。前提是它必须真的追加在**末尾**——所以 `UserPayload.Repair` 在结构体里是最后一个字段（`Extra` 之后）。插在中间会让它后面的一切整体后移，因果链是：字段顺序 → 序列化顺序 → 字节前缀 → 缓存命中。

| Provider | 机制 | 重试时的实际命中 |
|---|---|---|
| OpenAI / DeepSeek / Kimi / GLM / Qwen | 自动前缀缓存（DeepSeek 64 token、OpenAI 128 token 块对齐） | system + 全部 payload 前缀命中，只 miss `repair` 段 |
| Anthropic | 显式 `cache_control` 断点 | system 命中；user 在 `,"repair":` 前切一刀打点（`splitPromptCachePrefix`），正文随之命中 |

Anthropic 的切分只做一次纯字符串切割：两个 block 被无缝拼接，所以 `stable+tail` 与原文逐字节相等，模型看到的 JSON 完全不变。首轮没有 `repair` 就不切分。

命中量记在 `CallRecord.cacheReadTokens`（写入量在 `cacheCreationTokens`），两处展示：

- **模型日志**（`RunsView`）：每条记录下方显示整体命中率，展开后可看每次调用的「缓存命中 / 缓存写入」两列，重试那一行会单独标出。
- **Token 统计**：汇总卡片新增「缓存命中」，按当前筛选范围汇总命中量与占比；口径与输入 Token 一致（各家的 prompt token 都包含缓存部分）。

**缓存命中是缓存是否真的生效的唯一可信证据**：重试那一行的命中率应接近 100%，若接近 0 说明有东西字节级改了前缀（字段顺序、时间戳、排序抖动）。无缓存字段的历史记录显示 `—` 而不是 `0%`，避免把"没数据"误读成"完全未命中"。

`EncodeUserPayload` 的确定性由 `encoding/json` 的 map key 排序保证，并有测试钉住——map 迭代顺序随机，一旦编码抖动，缓存就永久失效。

## 关键代码位置

| 职责 | 位置 |
|---|---|
| 字段级抽取 | `core/service/context/extract.go` |
| 结构化输出校验 + 修复反馈 | `core/service/pipeline/validate.go` |
| 上下文组装 + 禁读校验 | `core/service/context/builder.go` |
| 定稿账本回写 | `core/service/books/accept_finalize.go` |
| 立项/卷纲接受（写盘白名单：立项=总纲+设定+人物；卷纲=当前卷+缺失单元） | `core/service/books/accept_plan.go` |
| 细纲序列化（字段完整保留） | `core/service/runs/service.go` `serializeUnitYAML` |
| 书籍加载 / `time_system` | `core/service/books/loader.go` |
| UI 设定页（用途标签、命名） | `frontend/src/components/book/BibleWorkspace.vue` |
| UI 单元剧情时钟 | `frontend/src/components/book/StoryTimeline.vue` |
| UI 人物关系图（正式表实线 / 文字虚线） | `frontend/src/components/book/RelationGraph.vue` |
