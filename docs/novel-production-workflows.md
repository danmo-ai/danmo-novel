# Danmo Novel 创作与重写流程

本文描述当前系统的工作流、LLM 输入输出与写入边界。它是生产流程说明，不是旧书迁移指南。迁移所需书籍结构见 [novel-book-templates.md](novel-book-templates.md)。

## 1. 不可违反的边界

1. LLM 不调用工具，不直接写 canon 或正文。
2. 每次生成先产出候选；只有作者 Accept 才写文件、推进状态或更新账本。
3. 任务的数据组装是固定 Go 函数；作者指令只作为补充数据，不改变输出协议、父级合同或可见性。
4. 总纲约束卷纲，卷纲单元种子约束细纲；下层只能细化，不能换故事。
5. `author-lore.md` 只用于允许的作者侧规划，正文写作、正文审校和修订禁止注入。
6. `proposed → accepted → drafted → reviewed → finalized` 是单元状态流；候选不等于已接受事实。`reviewed` 是审校/修订已复核，`finalized` 是实际账本已提交。
7. 结构校验失败最多一次定向结构修复；没有无界 Agent Loop。

## 2. LLM 调用的统一形式

除系统提示词配置文件外，模型始终收到两部分：

```text
system = 不可变任务模板 + 输出协议说明 + 自描述协议模板
user   = {
  task: 任务标识,
  mode: 可选模式,
  book: 书籍与目标元数据,
  instruction: 默认任务要求 + 作者追加指令,
  documents: 按任务选择的书籍片段,
  references: 作者显式勾选的知识文章（非 canon）,
  extra: 章号、单元 ID、审校问题等,
  repair: 可选，仅在校验失败后的重试追加在末尾
}
```

- `core/service/prompt/templates/*.md` 是可版本化的任务系统提示词；协议由同一 codec 注入并带 grammar hash。
- `UserPayload` 的稳定前缀在重试时不变，`repair` 只追加在末尾，以复用 KV cache。
- Provider adapter 只负责传输，不理解小说业务；解析器与协议一一绑定。
- `POST /api/runs/preview` 与实际调用共用同一数据组装和系统提示词，可用来核对模型真实输入。

## 3. 首次创作

### 3.0 新建与创意扩写：`kind=idea`

新书由立项表单建立：基本信息（书名、书 ID、题材/子题材、平台、读者、篇幅、单章目标字数）与创意种子（一句话 idea、基调、爽点、主角、对抗、结局倾向、禁忌、参考作品、POV）。创建后只生成最小树：

- `novel-state.yaml`（`stage: idea`、`next_action: idea`）
- `book-bible.md`、`canon/platform-positioning.md`（按表单预填，空白项保持空白）
- `outline/idea.md`（「立项输入 / 一句话 idea / 创意提案」三段）

**不预铺 9 个空设定模板**：它们会被结构化立项当成既有 canon，模型于是「优化」空占位符而不是写故事。

`kind=idea` 是自由输出任务：模型返回纯 Markdown 创意提案，不经过 codec、不要求 JSON、不写总纲/设定/人物。候选的 `targetPath` 固定为 `outline/idea.md`，`replacement` 是**合并后的完整文档**——「立项输入」段由作者维护，模型无权覆盖。接受后写文件并把 `stage` 推进为 `idea_expanded`、`next_action` 推进为 `plan`。

- 校验只有一条：输出过短（空回答或一句敷衍）时允许一次定向修复。
- 提案给了多个方向时，结构化立项默认采用标注的「推荐主线」。
- 「重新扩写」用同一任务、同一文件重跑；快捷方向（更黑暗 / 更强悬疑 等）写进 `instruction`，不新增字段。

**下一步判定**：没有总纲、有 `outline/idea.md`、但提案段为空 → `TaskIdea`；否则仍按原规则走 `TaskPlan`。没有 `outline/idea.md` 的旧书行为不变。

**界面边界**：「立项」Tab 只在**还没有全书总纲**时存在。它是 idea loop 到一次结构化立项（`mode=init`）的入口；总纲接受后 Tab 消失、页面落在大纲。此后重新立项只在大纲 Tab（带优化范围、优化目标与追加指令），单文件优化在设定 Tab，已立项书不再有创意扩写入口。理由很直接：让两个 Tab 都能重写总纲，作者就分不清该用哪个，也容易在一次优化里同时触发两条路径。

### 3.1 立项：`kind=plan, mode=init/reinit`

**输入**

- `outline/idea.md`（已确认的创意提案，第一依据）。
- 作者意图 Bible 与平台定位（`init`）；已接受的总纲、设定、人物卡（`reinit`）。
- 保留路径 `preservePaths`、作者追加指令和可选参考知识。

**输出**

外层 JSON：

```json
{
  "cast": [
    {
      "stem": "wen-nianci",
      "name": "温念慈",
      "role": "protagonist",
      "tagline": "新入职女警",
      "cardMarkdown": "# 温念慈\n...",
      "initialIdentity": {
        "roleTitle": "内勤新警",
        "org": "双桥派出所",
        "fromUnit": "v01-U1",
        "note": "初始身份"
      }
    }
  ],
  "bookOutlineMarkdown": "# 全书总纲 · ...\n- schema_version: 1\n...",
  "settings": {
    "files": {
      "book-bible.md": "...",
      "canon/world.md": "...",
      "canon/goldfinger.md": "...",
      "canon/platform-positioning.md": "...",
      "canon/writing-rules.md": "...",
      "canon/style-fingerprint.md": "...",
      "canon/foreshadow-schedule.md": "...",
      "canon/author-lore.md": "...",
      "canon/locked-terms.yaml": "..."
    }
  }
}
```

**校验与写入**

- 总纲/设定/人物必填，至少一个 protagonist；人物卡、伏笔登记表、身份字段和引用合法。
- 伏笔在立项只写卷级位置（plant_volume / payoff_volume），单元与章留空。
- Accept 一次性写总纲、白名单设定、人物卡及 `canon/identity-timeline.yaml`；不写卷纲、单元或正文。
- `reinit` 可保留精修文件，既有单元、正文、continuity 不受影响。

**优化闭环（复用 `mode=reinit`，无第二套机制）**

重新立项永远只有一条执行路径：`kind=plan, mode=reinit` + `preservePaths`，接受时由 `AcceptPlan` 决定写哪些文件。界面上的「优化范围」只是预填 `preservePaths` 的快捷方式：

| 范围 | 保留（不覆盖） |
|---|---|
| 全部重写 | 无 |
| 只优化总纲 | 全部设定 + 全部人物卡 |
| 只优化设定 | 总纲 + 全部人物卡 |
| 只优化人物 | 总纲 + 全部设定 |
| 自定义 | 逐文件勾选 |

「优化目标」（加强开篇钩子、提高爽点密度等）只拼进 `spec.instruction`，不改变输出协议。候选在审阅时按文件给出「新增 / 修改 / 未变 / 保留不写」与行级 diff；接受仍是原子写入。只调一个文件时不走 `reinit`：`kind=rewrite_entity` 携带对应依据，接受只写该文件。

### 3.2 生成/重写卷纲：`kind=plan, mode=volume`

**输入**

- 总纲及当前 `VolumeArc`，不得只给模型一段自由文本卷名。
- 上一卷承接、最近已接受 continuity、相关人物卡和伏笔登记表。
- 伏笔只细化本卷条目的 plant_unit / payoff_unit，不新建条目。
- 当前旧卷纲（若为重写）及追加指令。

**输出**

```json
{"volumeOutlineMarkdown": "# 卷纲 · v02\n- schema_version: 1\n..."}
```

卷纲包含卷目标、起终状态、节拍、人物、完整单元种子、内嵌时钟和卷末交接。每单元必须是一个主要戏剧问题、一个主结果和一条主要因果链，时钟是其合同字段而非另一份表。

**校验与写入**

- 校验总纲继承、章范围、单元因果字段、引用、时钟、人物身份及可选身份变更。
- Accept 原子写当前卷纲、缺失单元种子、伏笔单元归属和身份时间线；已有单元标为 `proposed` 要求重新确认，已有正文/continuity 保留但不得绕过新的父级合同。

### 3.3 结构编辑审校：`kind=structure_review`

**输入**

- 当前总纲或卷纲。
- 卷纲审校时附父级总纲；不读正文。
- 固定的结构编辑视角与追加指令。

**输出**

结构化 `editor.md`：

```markdown
# 结构审校 · v02
- schema_version: 1
- source_revision: <hash>

## 问题 · v02-U1-causality-1
- entity_path: units[v02-U1].outcome
- severity: blocking
- lens: causality
- evidence: 结果没有由选择产生
- suggestion: 补上申请复查的选择及获得的明确许可
```

审校项包括父级继承、单元粒度、因果、兑现、状态、重复空转、揭示边界和主角能动性。审校只生成问题清单；定向修复仍须重新校验、生成候选、Accept。

### 3.4 单元细纲：`kind=outline`

**输入**

- 当前卷纲及对应的不可改写 `UnitSeed`。
- 已有细纲（重写时）、本卷人物、身份时间线、continuity 与伏笔登记表。
- 伏笔只落到章：`chapters[].plant` / `chapters[].payoff` 填 FS ID，只能引用本单元负责埋/收的条目。
- 追加指令与可选知识文章。

**输出**

```json
{
  "units": [{
    "unitId": "v02-U1",
    "sourceSeedId": "v02-U1",
    "title": "只谈案子",
    "function": "获得旧案复查入口",
    "desire": "在新舞台获得查案入口",
    "obstacle": "试用规矩与关系户议论",
    "choice": "按程序申请复查",
    "payoff": "获得三天复查许可",
    "attempt": "核对口供与现场材料",
    "setback": "疑点不足以翻案",
    "cost": "承担复查责任",
    "forbidden": "不提前揭示死档",
    "chapterChars": 2500,
    "onStage": ["wen-nianci", "pei-guo-dong"],
    "pov": "wen-nianci",
    "chapterFrom": 51,
    "chapterTo": 56,
    "storyDay": "91",
    "endDay": 97,
    "gapDays": 1,
    "timelineId": "main",
    "chapters": [{
      "chapter": 51,
      "goal": "获得复查旧案的程序入口",
      "change": "裴国栋从只给跑腿到批准三天复查",
      "cutHook": "签名齐整得反常，需要现场重勘",
      "dayOffset": 0,
      "scenes": [{
        "chapter": 51,
        "goal": "用可核验疑点争取复查",
        "conflict": "只有口供疑点，尚不足以翻案",
        "turn": "申请现场重勘",
        "outcome": "获得三天许可",
        "mustLand": ["明确三天期限"]
      }]
    }],
    "stateDeltas": [{
      "stem": "wen-nianci",
      "field": "role_title",
      "from": "内勤新警",
      "to": "重案队试用"
    }],
    "nextHook": {"type": "信息缺口", "out": "签名不合"}
  }]
}
```

**约束**

- 每章必须有 `ChapterPlan`；`scenes` 只是复杂章可选辅助，不是必填的第三级大纲。
- 核心场面只需 `goal/conflict/turn/outcome`；`mustLand` 最多两项。不得预写台词、动作清单或情绪曲线。
- 章范围、目标、尝试、缺口、选择、代价、结果、时钟必须继承卷纲。
- Accept 前再次校验身份覆盖、父子合同、时钟、状态迁移与场面归属，写 `outline/units/<unit>.yaml` 并进入 `accepted`。
- `stateDeltas` 的任务是**维护状态**而不是记录变化：细纲上下文里带着进入本单元时的
  完整快照（含「记于 N 个单元前，可能已变」的标注），模型必须逐人逐字段对照，
  对本单元内结束的字段写显式终值（伤好了写 `to: 已愈`），对新增的字段即便很小也写。
  没写终值的字段会被后续所有单元一直继承——实测一条伤势跨了 6 个单元、63 个故事日。
- `state_deltas` 里的 `role_title` 只是「本单元在边界切了身份」的声明，必须与 `canon/identity-timeline.yaml`
  的本单元生效段一致；它不会被写进连续性账本——身份只由时间线拥有，写作与审校都从那里现算。

### 3.4.1 细纲语义检查：`kind=outline_review`

用与生成一致的父级种子、人物、能力、伏笔和入场状态做一次有界检查，覆盖章间因果、父级兑现、POV、时钟、状态迁移与可执行性。它只输出问题候选，不写细纲；作者根据问题回改细纲或卷纲，再明确 Accept。

### 3.5 正文：`kind=write`

**输入**

- 单元合同和当前 `ChapterPlan`，场面只作辅助。
- 在场人物的压缩卡、按单元匹配的身份、关系、continuity。
- 上一单元结尾/上一章、揭示约束、锁词、风格、题材规则、当前章时钟。
- 追加指令；不注入 author-lore。

**输出**

```json
{"chapters": [{"chapter": 51, "title": "只谈案子", "body": "正文……"}]}
```

固定逐章生成与逐章定向修复，单元 Accept 一次提交全部章节。没有整单元写作模式或按规模切换分支。校验章号、非空正文、完整覆盖、字数与本地硬门。

**构建顺序**：`UserPayload` 序列化为 `book → documents → references → task → instruction → extra`。分章写作时 documents 与 references 必须逐字共用——按章变化的场面节拍（`extra.chapterBeats`）、章号、本章字数规则与上一章正文一律落在 extra。把按章内容放回 documents（哪怕只是替换内容、位置不变），它后面的人设/连续性/风格块与整份参考知识就全部按全价重算。

**写入**

Accept 写 `units/<unit>.md` 并进入 `drafted`；不更新 continuity。

### 3.6 正文审校：`kind=review`

**输入**

- 正文、单元合同、本章计划、人物/身份/关系、continuity、揭示约束、锁词、风格和本地检查结果。
- 固定按章审校；只带当前章正文、当前章合同、共享事实基线与上章结尾。没有整单元审校分支。

**输出**

```json
{
  "verdict": "revise",
  "findings": [{
    "id": "continuity-51-1",
    "severity": "blocking",
    "chapter": 51,
    "lens": "continuity",
    "title": "时间或身份不一致",
    "evidence": "本单元仍为重案队试用，正文写成特聘专家",
    "anchor": "她还挂着重案队试用的牌子",
    "suggestion": "恢复本单元身份，或先修改规划"
  }]
}
```

`anchor` 是问题所在处的**逐字原文**（10–40 字），用于跨轮去重。它与 `evidence` 分工不同：`evidence` 是给作者看的说明、允许转述；`anchor` 必须可命中，否则无法把两轮指同一处的意见合并。定位失败只降级该条，不判整份失败。

**多轮取并集（`Review.Rounds`，默认 3）**

单轮审校的覆盖率有硬上限，这不是审校不够深，而是每轮注意力落点不同。实测同一份正文跑 4 轮：

| 观测 | 数值 |
|---|---|
| 单轮平均覆盖（对 4 轮并集 128 处） | 25.4% |
| k=1 / k=2 / k=3 并集 | 25.4% / 50.5% / **75.4%** |
| 被 ≥2 轮同时抓到 | 19%（另一本书仅 6%） |
| 被 4 轮全部抓到 | 0 处 |
| 把 findings 上限从 8 提到 20 | 实际只从 4 条变 6 条，上限不是瓶颈 |
| `temperature=0` 能否固定 | 不能，连 JSON 外壳都在变 |

所以每章串行跑 N 轮（轮内串行以复用前缀缓存，跨章仍按 `Review.Concurrency` 并行），合并规则：

- **去重键是 anchor 在正文中的区间**：两轮定位到同一处即同一问题。按 `evidence` 字面比对最多只能对上 19%，所以不能用它当键。
- 合并时 `severity` 取最严、`title` 取较短者、`evidence`/`suggestion` 拼接去重，并累加 `roundsHit`。
- 排序按 `roundsHit` 降序，其次 severity、章号。3 轮并集条数约为单轮 2–3 倍，不排序作者无法判断该修哪些。

**清单只有两个出口**

审校清单是「待处理的问题」，不是「可以批准的通行证」。所以面板上只有两个动作：

- **自动修复**：勾选问题 → 生成修订候选 → 逐句 diff → 确认落盘。
- **拒绝**：不同意这些结论，丢弃整份清单，正文不变。需要时再重新发起审校。

没有「通过 / 强制通过」——那会把审校清单和定稿混成一个动作。**定稿是写作页的独立步骤**（`kind=finalize`），它才做本地门禁与账本回写。

「阻断」的判据（与前端 `isBlockingFinding` 必须一致）：只有 `severity=blocking` **且** `roundsHit>=2` 才算阻断，用于顶部徽章与排序。实测同一章两次调用（同为 high effort）给出的 blocking 完全不同，单轮判定不稳定；若单轮即可阻断，作者会反复被"看起来是 blocking"的新条目拖住——重新审校又会冒出一批。单轮 blocking 仍留在清单里给作者看，只是标成「建议」那一档。`roundsHit` 缺省（旧数据、本地硬门）按原语义算阻断。

**交付流程止于落盘**

完整的交付闭环是「审校（多轮取并集）→ 勾选 → 一次修订 → 逐句 diff → 确认落盘」，到此结束。

- 落盘后**不再自动起审校**。实测同一单元连续 6 轮审校，后续报出的"新问题"有 **96%+ 落在这次根本没改过的原文上**——是前几轮注意力没扫到的旧账，不是修复引入的回归。自动起新一轮会让作者分不清"修好了"和"又发现一批旧的"，同时把调用成本翻倍。
- 落盘时旧正文的审校清单按版本过期归档，供历史回看；不得仅凭勾选 `IssueIDs` 宣称已修复。需要关闭问题时按新正文复核。
- 没有单独的"重审"概念。真要重来就重新审校整个单元：它会先归档旧清单，再按章重跑多轮——这就是 `SupersedeReview` 的语义（内部走 `archiveUnitReviews`）。

审校候选只包含问题，不直接改正文；作者选择问题后进入修订。

### 3.7 定向修订：`kind=revise`

**输入**

- 选中审校问题、原正文、单元合同、人物/身份/continuity/揭示约束。
- 每次只修一章，保留其他章和其他内容。

**输出（补丁契约，默认）**

```json
{"chapter": 51, "edits": [
  {"find": "他把杯子放下，说：「好。」", "replace": "他把杯子放下，说：「随你。」"}
]}
```

修订只动一两句，回整章正文等于把没变的文字按输出价重抄一遍，所以默认输出补丁，由服务端套用到原文得到完整章节。

- **唯一性只在章内成立**：一次调用只带本章正文，跨章重复的空行、对话起头、分隔线不再构成歧义。`chapter` 是寻址维度，回错章号直接拒绝（缺省按本次章号补齐）。
- **`find` 必须逐字连续**，不得改写、不得用「……」省略中间内容。目标 10–40 字（约一句完整话）；超过 60 字说明在整段搬原文，应改成整段替换。短句（如「好。」）只要章内唯一即放行，长度不是门槛。
- **匹配分层**：逐字 → 忽略行尾空白 → 忽略首尾空白 → 标点归一（全角/半角、弯引号、`「」`、破折号、全角空格）。只做字符层归一，**不做语义模糊**——模糊匹配会静默删掉语义重要的行，而正文删字不可逆。
- **命中必须唯一**：0 处报错并附本章最接近的真实原文（供模型改对）；多处报错并附各行号与所在句子（要求扩展上下文）。两者都不擅自取第一处。
- edits 之间不得重叠（同一段的多处修改应合并成一条）；按位置应用，模型输出乱序不影响结果。
- `replace` 为空字符串表示删除；`find` 与 `replace` 相同的空操作、以及套用后与原章逐字相同的结果，都判契约失败。

**扩写与插入**

补丁契约没有单独的"插入"字段，插入有两种形态：

- **章中间加内容**：仍用 `find`+`replace`，锚点抄改动点旁的原文，`replace` 里把它原样带回、新增接在前面或后面。
  ```json
  {"find": "他把杯子放下。", "replace": "他把杯子放下。\n\n窗外的雨小了。"}
  ```
  空 `find` 一律拒绝（空锚点无法定位），错误文本会指向 `append`。
- **章末追加**：用 `append`，不需要锚点。章末最后一句常常是短句，用它当锚点极易撞车，而续写式扩写正是最高频的扩写形态。
  ```json
  {"append": "\n\n他走到门口，又停住了。"}
  ```
  一条 edit 里 `append` 与 `find`/`replace` 不能同时给；追加一定发生在所有替换之后。

成本：插入 N 字只需输出「N + 锚点(10–40 字)」；`append` 则只有 N。都远低于整章重写。

**调用预算：每章最多 3 次**

| 轮次 | 契约 | 说明 |
|------|------|------|
| 1 | 补丁 | 唯一的冷启动 |
| 2 | 补丁 + repair | 锚点失败原因追加在 user payload 尾部，同 system → 命中前缀缓存 |
| 3 | 整章正文兜底 | 换的只是 instruction 里的契约，system 与 blocks 不变，仍命中缓存 |

三次调用共用同一个 system prompt 与同一批 blocks：system 是两种任务都成立的共享规则（`novel.ChapterStageSystemPrompt`），任务自己的输出契约随 `instruction` 下发。这样「审校 → 修订 → 补丁修复 → 整章兜底」四次调用共享同一个约 8600 token 的前缀，只有 instruction 这一小段是新的。

**兜底输出**

```json
{"chapter": 51, "title": "只谈案子", "body": "修订后的完整正文"}
```

候选里同时保留修订前正文（`baseChapters`），前端逐句 diff 后再确认落盘；落盘时勾选过的问题从清单里摘掉，这次修订即告完成，不自动起新一轮审校。审校候选的接受只是归档，不是改正文。

**审校意见可能重复**

多轮审校取并集后，同一处缺陷仍可能以不同措辞出现两次（两轮的 anchor 若有一轮定位失败就会留下两条）。为同一处输出多条 edit 会被判「区间重叠」并整批拒绝，这条修订就白做。所以契约里明确要求：同一处只输出一条 edit，并给出判断依据（`evidence` 引用同一句原文，或 `suggestion` 描述同一个改法）；反过来，同一类问题在正文出现多处时，每条意见只修它引用的那一处，不要顺手把全文同类都改掉。

### 3.8 定稿：`kind=finalize`

定稿是**写作页的独立步骤**，不在审校面板上。审校清单只有「自动修复」和「拒绝」两个出口，处理完（或作者判断可以前进）就回写作页发起定稿。

**输入**

- 本地读取正文和单元合同，先跑字数、空章、时钟、身份与审校清单门禁。
- 再固定逐章调用一次有界账本核对，返回实际摘要、带逐字正文证据的实际状态迁移、实际伏笔结果；计划未发生不能记账。
- 这些仍是候选，作者可见并 Accept 后才写盘。

**输出与写入**

生成定稿候选；Accept 后：

- 单元状态改 `finalized`。
- `continuity/actual/<unit>.yaml` 保存作者接受的逐章正文事实索引；`continuity/state.yaml` 从这些实际结果重算全量快照，不直接消费计划 `state_deltas`。
- 伏笔实际开闭与摘要只来自已核对结果；每章计划伏笔必须明确核对。
- `continuity/summaries/vNN.md` 按章 upsert 摘要。
- `continuity/commits/<unit>.md` 写执行日志。
- 更新 `novel-state.yaml` 的章游标。

定稿只确认实际状态，不自动修改人物身份时间线或作者规划。

## 4. 重写流程

### 4.1 简单修正

- UI 字段编辑产生最小 Markdown 补丁，保留其他原文与注释。
- 服务端重新解析全文并校验；`baseRevision` 不一致则拒绝覆盖。
- 保存失败保留作者草稿，不调用模型。

### 4.2 复杂重写

- 总纲/卷纲：`plan` 携带父级合同与追加指令，输出结构化 Markdown 候选。
- 设定/人物/身份：`rewrite_entity` 只重写目标文件，附相关依据与受影响单元；依据只读。
- 细纲：`outline` 只能细化当前 `UnitSeed`，不能修改父级决定。
- 正文：按单元重写或按审校问题定向修订。

## 5. 校验、可见性与文件职责

- 总纲/卷纲/伏笔登记表：结构化 Markdown 是唯一事实源，解析对象只在内存/API 使用。
- 人物卡：稳定人格与知识边界；身份时间线：按单元生效的公开职务身份。
- `stateDeltas`：规划的状态迁移；continuity：定稿后的实际状态，不反向覆盖规划。
- `stateDeltas.field` 词表封闭：`role_title/location/health/resources/goal/knows/relation/life`。未知名在细纲接受时进修复轮，不再静默丢弃。
- 字段失效必须显式写终值（`from: 刀伤未愈, to: 已愈`）；`life` 单向（alive→missing→dead），死者不得再进 onStage。
- `continuity/state.yaml` 每段都是全量快照，读取只看目标单元之前最近的一段；`facts.md` 已退役。
- `story_day/end_day` 是整数开闭日；章/场面使用 `day_offset`；`time_label/when` 只展示，不作校验依据。
- `ChapterPlan.change` 必须是可验证的状态/信息/关系/行动变化，不以“更紧张/更可疑”冒充推进。
- **不做运行期兼容**：旧字段名、旧文件、旧状态由一次性数据规整改写，代码只认当前形态。
  入口 `POST /api/books/:id/migrate-foreshadow`（`books.NormalizeBookData`），
  原文全部留档到 `backups/pre-migrate-<stamp>/`。
- `state_deltas.field` 词表之外的名字在细纲接受时报错并进修复轮，不再静默丢弃；
  `word_target`、`state_deltas_json`、`canon/reveal-schedule.md` 均已退役。
