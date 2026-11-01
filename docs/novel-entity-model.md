# Danmo Novel 实体模型与生产链契约

本文是当前实体职责、唯一来源和写回边界的总览。详细的任务输入输出见 [创作与重写流程](novel-production-workflows.md)；标准文件占位符见 [书籍模板](novel-book-templates.md)。

## 1. 可见性与唯一来源

| 层 | 内容 | LLM 使用范围 |
|---|---|---|
| 稳定 Canon | 总纲、卷纲、人物、世界规则、身份、揭示计划、单元合同 | 按任务选择字段，不整本塞入 |
| 作者底牌 | 真相、秘密、未揭规则、未来反转 | 仅允许的作者侧规划；正文/审校/修订禁读 |
| 公开连载事实 | 已定稿状态、伏笔、实际时间、公开摘要 | 写作/审校读取，只由定稿 Accept 更新 |
| 参考知识 | 作者资料库的方法、题材、风格文章 | 作者显式勾选才作为参考，不是 canon |

Markdown 是总纲、卷纲和设定的事实源；解析对象只在内存/API 中使用，不生成第二份平行文件。

## 2. 实体职责

| 实体 | 位置 | 职责与关键字段 | 写回时机 |
|---|---|---|---|
| 运行状态 | `novel-state.yaml` | 阶段、当前单元、章游标、下一动作 | 定稿/流程推进 |
| 作者意图 | `book-bible.md` | 读者承诺、logline、主角目标、故事引擎、终局方向 | 立项/重新立项 Accept |
| 全书总纲 | `outline/book_outline.md` | 故事线、分卷起终状态、转折、兑现、故事线迁移、揭示边界 | 立项/重新立项 Accept |
| 卷纲 | `outline/volumes/vNN.md` | 卷目标、节拍、人物、完整单元种子、内嵌时钟、卷末交接 | 卷纲 Accept |
| 单元种子 | 卷纲 `## 单元` | `objective/obstacle/attempt/setback/choice/cost/outcome/state_changes/clock_*` | 随卷纲 Accept |
| 单元细纲 | `outline/units/vNN-UN.yaml` | 继承种子、POV、在场人物、章合同、状态迁移、揭示与下一钩 | 细纲 Accept |
| 章合同 | 细纲 `chapters[]` | `chapter/goal/change/cut_hook/day_offset` | 细纲 Accept；正文/审校的基本输入 |
| 可选场面 | 章合同 `scenes[]` | `goal/conflict/turn/outcome`，可选 `must_land/evidence_in/evidence_out` | 细纲 Accept；只组织复杂行动 |
| 正文 | `units/vNN-UN.md` | 章号、标题、纯散文正文 | 正文/修订候选 Accept |
| 人物卡 | `canon/cast/<stem>.md` | 稳定人格、三锚点、知识边界、声音、关系 | 立项/人物晋升/候选 Accept |
| 身份时间线 | `canon/identity-timeline.yaml` | `stem/role_title/org/from_unit/to_unit/note` | 规划或人工 Accept；单元边界生效 |
| 揭示计划 | `canon/reveal-schedule.md` | `reveal_id/public_summary/earliest_volume/hint_unit_ids/confirm_unit_id/status/locked_terms` | 立项/优化 Accept |
| 作者底牌 | `canon/author-lore.md` | 真相、秘密、真实规则、未来反转 | 仅作者侧 Accept |
| 锁词 | `canon/locked-terms.yaml` | `locked_until/compliance/aliases` | 手工/设定维护 |
| 实际状态 | `continuity/state.yaml` | 位置、伤势、资源、目标、知情范围、伏笔、开闭故事日 | 定稿 Accept |
| 章摘要 | `continuity/summaries/vNN.md` | 每章事件、状态变化、伏笔、钩子 | 定稿 Accept，重写 upsert |
| 定稿日志 | `continuity/commits/<unit>.md` | 字数、门禁、修订与状态迁移 | 定稿 Accept；不默认给模型 |
| 结构审校 | SQLite candidates | `entity_path/severity/lens/evidence/suggestion` | 候选；定向修复后重新审校 |
| 模型运行 | SQLite runs | 任务、模型、协议、输入 hash、token、缓存、错误 | 每次调用；不是 canon |

## 3. 层级合同

```text
全书总纲 VolumeArc
  → 卷纲 UnitSeed
    → 单元细纲 ChapterPlan
      → 可选 Scene
        → 正文候选
          → 定稿实际状态
```

- 总纲锁卷的目标、起终状态、转折、结果和揭示边界。
- 卷纲继承总纲，锁单元的主要问题、因果、主结果、状态迁移和时间边界。
- 细纲通过 `source_seed_id` 继承卷纲，不另写一个故事。
- 章合同是正文生产基本单位，每章有可验证 `change` 和具体 `cut_hook`。
- 场面不是必填大纲层，不预写正文，只处理复杂行动与必要材料。
- 定稿只记录实际结果，不自动改规划、人物身份或时钟。

## 4. 单元与章节的粒度

一个单元是一个可独立验收的因果闭环：承接前序结果→局部目标→主要方法→关键阻碍/缺口→有代价的选择→一个不可逆主结果→下一单元入口。

- 一个主要戏剧问题、一个主结果、一种主要冲突/解题形态。
- 从属线必须改变主线选择或代价，不能用双线名义拼接独立任务。
- 常见 3–8 章，硬上限 10 章；不按每卷固定单元数切分。
- 标题只写核心动作/意象，不罗列全部事件。
- 公开职务身份在单元内部恒定，变更只在边界登记。

每章不必形成完整闭环，但必须有可验证的状态、信息、关系或行动变化。普通连续行动无需拆场面；复杂章可有 1–3 个必要行动段，核心只有 `goal/conflict/turn/outcome`，关键 `must_land` 最多两项。

## 5. 人物、身份与状态的区别

- **人物卡**：稳定人格、欲望、伤口、知识边界、声音和关系。
- **身份时间线**：本单元生效的公开职务和机构，按单元自动匹配，无需每单元重复登记。
- **continuity**：随剧情变化的位置、伤势、资源、目标、知情范围与实际状态。
- **stateDeltas**：单元计划的状态迁移，定稿时转成账本记录；不能用它替代公开身份时间线。
- **作者底牌**：真实秘密，不进入正文、审校或修订上下文。

立项为主要人物建立初始身份；卷纲/细纲 Accept 续接规划变更；定稿只核对实际身份与本单元生效段。已有正文的历史身份不得被候选静默覆盖。

## 6. 剧情时钟

- **总纲**：只保存年代跨度或必要的卷级时代边界。
- **卷纲 `UnitSeed`**：`clock_start_day/clock_end_day/clock_gap_days`，可选 `clock_deadline/timeline_id/allow_flashback`。
- **细纲**：整数 `story_day/end_day/gap_days`，章/场面用 `day_offset`。
- **continuity**：定稿后的实际结束日和必要开闭区间。

`time_label/when` 只展示，不作一致性依据。非 main 时间线必须显式允许闪回/并行；主线不得倒退。自由文本故事日、跨边界偏移和未知死线在生成/Accept 时阻断。没有独立的卷级剧情时钟表。

## 7. 生成、Accept 与重写

- LLM 输出先经同版 parser 和 validator，再成为候选；不直接写文件。
- 结构错误最多一次定向 repair；repair 只追加到 `UserPayload` 末尾，保持 KV cache 前缀稳定。
- 简单修改直接编辑 Markdown 源区间，保存前完整复验和 revision 检查。
- 复杂修改通过追加指令生成候选，Accept 复验后写入。
- 卷纲 Accept 原子写当前卷、缺失单元种子与身份变更，不覆盖已有细纲/正文/账本。
- 正文 Accept 只写正文；审校接受只归档问题；修订 Accept 写修订正文；定稿 Accept 才写 continuity。
- Chain 复用同一手工链路，按细纲→正文→审校→定稿串行执行；阻断进入 `waiting_manual`。

## 8. 关键实现索引

| 职责 | 位置 |
|---|---|
| 结构化 Markdown 解析与源区间 | `core/service/books/outline_markdown.go` |
| 总纲/卷纲校验与引用 | `core/service/books/outline_validate.go`、`core/service/pipeline/plan_validate.go` |
| 细纲章合同与父子校验 | `core/service/pipeline/outline_validate.go` |
| 卷纲/章时钟校验 | `core/service/books/clock_contract.go` |
| 人物身份生命周期 | `core/service/books/identity_timeline.go`、`identity_editor.go` |
| 固定上下文组装 | `core/service/context/builder.go`、`review_baseline.go` |
| 任务/协议 runner | `core/service/pipeline/task_definition.go`、`runner.go`、`codec.go` |
| 系统提示词配置 | `core/service/prompt/store.go`、`core/service/prompt/templates/` |
| Accept 与账本 | `core/service/books/accept_plan.go`、`accept_finalize.go` |
| 结构化大纲 UI | `frontend/src/components/book/StructuredOutline.vue`、`OutlineWorkspace.vue` |
| 章合同 UI | `frontend/src/components/book/UnitContract.vue` |
