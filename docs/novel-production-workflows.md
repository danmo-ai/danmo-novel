# Danmo Novel 创作与重写流程

本文描述当前系统的工作流、LLM 输入输出与写入边界。它是生产流程说明，不是旧书迁移指南。迁移所需书籍结构见 [novel-book-templates.md](novel-book-templates.md)。

## 1. 不可违反的边界

1. LLM 不调用工具，不直接写 canon 或正文。
2. 每次生成先产出候选；只有作者 Accept 才写文件、推进状态或更新账本。
3. 任务的数据组装是固定 Go 函数；作者指令只作为补充数据，不改变输出协议、父级合同或可见性。
4. 总纲约束卷纲，卷纲单元种子约束细纲；下层只能细化，不能换故事。
5. `author-lore.md` 只用于允许的作者侧规划，正文写作、正文审校和修订禁止注入。
6. `proposed → accepted → drafted → reviewed` 是单元状态流；候选不等于已接受事实。
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

### 3.1 立项：`kind=plan, mode=init/reinit`

**输入**

- 作者意图 Bible 与已有设定。
- 已存在总纲、人物卡（重新立项时）。
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
      "canon/reveal-schedule.md": "...",
      "canon/author-lore.md": "...",
      "canon/locked-terms.yaml": "..."
    }
  }
}
```

**校验与写入**

- 总纲/设定/人物必填，至少一个 protagonist；人物卡、揭示计划、身份字段和引用合法。
- Accept 一次性写总纲、白名单设定、人物卡及 `canon/identity-timeline.yaml`；不写卷纲、单元或正文。
- `reinit` 可保留精修文件，既有单元、正文、continuity 不受影响。

### 3.2 生成/重写卷纲：`kind=plan, mode=volume`

**输入**

- 总纲及当前 `VolumeArc`，不得只给模型一段自由文本卷名。
- 上一卷承接、最近已接受 continuity、相关人物卡和揭示计划。
- 当前旧卷纲（若为重写）及追加指令。

**输出**

```json
{"volumeOutlineMarkdown": "# 卷纲 · v02\n- schema_version: 1\n..."}
```

卷纲包含卷目标、起终状态、节拍、人物、完整单元种子、内嵌时钟和卷末交接。每单元必须是一个主要戏剧问题、一个主结果和一条主要因果链，时钟是其合同字段而非另一份表。

**校验与写入**

- 校验总纲继承、章范围、单元因果字段、引用、时钟、人物身份及可选身份变更。
- Accept 原子写当前卷纲、缺失单元种子和身份时间线；不覆盖已有细纲/正文/continuity。

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
- 已有细纲（重写时）、本卷人物、身份时间线、continuity 与揭示计划。
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

整单元模式覆盖全部章节；分章模式每次只写一章。校验章号、非空正文、完整覆盖、字数与本地硬门。

**写入**

Accept 写 `units/<unit>.md` 并进入 `drafted`；不更新 continuity。

### 3.6 正文审校：`kind=review`

**输入**

- 正文、单元合同、本章计划、人物/身份/关系、continuity、揭示约束、锁词、风格和本地检查结果。
- 可按整单元或分章调用；分章只带当前章正文与上章结尾。

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
    "suggestion": "恢复本单元身份，或先修改规划"
  }]
}
```

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

**调用预算：每章最多 3 次**

| 轮次 | 契约 | 说明 |
|------|------|------|
| 1 | 补丁 | system prompt 固定 |
| 2 | 补丁 + repair | 锚点失败原因追加在 user payload 尾部，同 system → 命中前缀缓存 |
| 3 | 整章正文兜底 | 唯一一次缓存 miss，也是必然能落盘的最后出口 |

**兜底输出**

```json
{"chapter": 51, "title": "只谈案子", "body": "修订后的完整正文"}
```

候选里同时保留修订前正文（`baseChapters`），前端逐句 diff 后再确认落盘；可自动复审。审校候选的接受只是归档，不是改正文。

### 3.8 定稿：`kind=finalize`

**输入**

- 本地读取正文和单元合同，不调用 LLM。
- 字数、空章、时钟、身份、状态迁移及门禁问题。

**输出与写入**

生成定稿候选；Accept 后：

- 单元状态改 `reviewed`。
- `continuity/state.yaml` 写入实际人物状态、伏笔与实际开闭故事日。
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

- 总纲/卷纲/揭示计划：结构化 Markdown 是唯一事实源，解析对象只在内存/API 使用。
- 人物卡：稳定人格与知识边界；身份时间线：按单元生效的公开职务身份。
- `stateDeltas`：规划的状态迁移；continuity：定稿后的实际状态，不反向覆盖规划。
- `story_day/end_day` 是整数开闭日；章/场面使用 `day_offset`；`time_label/when` 只展示，不作校验依据。
- `ChapterPlan.change` 必须是可验证的状态/信息/关系/行动变化，不以“更紧张/更可疑”冒充推进。
- 新格式缺失的旧文件不在生产链隐式兼容；旧书由 Agent 按模板生成候选、校验并接受后再启用。
