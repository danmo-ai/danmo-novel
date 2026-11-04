# Danmo Novel 标准书籍模板（迁移参考）

本文件只定义一本书的标准文件结构和字段占位符，供 Agent 迁移旧小说时理解目标格式。它不是创作流程说明；流程见 [novel-production-workflows.md](novel-production-workflows.md)。

## 1. 目录与文件职责

```text
{{book.id}}/
  book-bible.md                         作者意图与读者承诺
  novel-state.yaml                     运行状态与生产游标
  outline/
    idea.md                            立项输入与创意提案（作者意图，非 canon）
    book_outline.md                    全书总纲与分卷合同
    volumes/
      v{{NN}}.md                       卷级节拍、单元种子与内嵌时钟
    units/
      v{{NN}}-U{{N}}.yaml               单元执行合同与逐章计划
  units/
    v{{NN}}-U{{N}}.md                   已接受单元正文
  canon/
    cast/{{character.stem}}.md          稳定人物卡
    identity-timeline.yaml             按单元生效的公开职务身份
    world.md                           世界规则与物质条件
    goldfinger.md                      能力、代价、上限与使用边界
    platform-positioning.md            平台、读者与篇幅定位
    writing-rules.md                   生产硬规则
    style-fingerprint.md               叙事风格
    foreshadow-schedule.md             伏笔登记表（埋/收，含信息类）
    author-lore.md                     作者侧真相与未揭秘密
    locked-terms.yaml                  锁词与永久合规红线
  continuity/
    state.yaml                         定稿后的实际状态账本
    summaries/v{{NN}}.md                定稿章摘要
    commits/v{{NN}}-U{{N}}.md           定稿执行日志
  reviews/                              审校输出（如存在）
```

## 2. 占位符约定

| 占位符 | 含义 |
|---|---|
| `{{book.id}}` | 英文小写连字符书籍 ID |
| `{{book.title}}` | 书名 |
| `{{NN}}` | 两位卷号，如 `01` |
| `{{N}}` | 单元序号，如 `1` |
| `{{unit_id}}` | `v{{NN}}-U{{N}}`，如 `v02-U1` |
| `{{character.stem}}` | 英文小写连字符人物 ID |
| `{{day}}` | 相对故事日的非负/正整数，不是年月日 |
| `{{optional}}` | 可省略的字段，不要以空字符串伪造已确定事实 |

## 3. 全书与作者意图

### `book-bible.md`

```markdown
# Book bible — {{book.title}}

## Reader promise
- Genre / channel: {{题材与频道}}
- Core pleasure: {{持续兑现的核心阅读体验}}
- Tone: {{情绪与叙事基调}}

## One-line logline
> {{主角＋困境＋目标＋核心冲突}}

## Protagonist
- Desire: {{外在目标}}
- Wound: {{内在伤口或误区}}
- Method: {{主要行动方式}}
- Lines they won't cross: {{不可逾越的边界}}

## Antagonistic force
{{对抗力量及其如何持续制造冲突}}

## Story engine
{{冲突如何逐卷/逐单元更新，避免重复}}

## Ending direction (spoiler-safe for author)
{{终局方向及允许的揭示层级}}

## 终局储备（约束，不得提前打光）
| 底牌 | 内容 | 最早解锁卷 |
|------|------|-----------|
| 头号宿敌 | {{可在公开总纲说明的层级}} | v{{NN}} |
| 终极真相 / 金手指上限 | {{可在公开总纲说明的层级}} | v{{NN}} |

## Taboos / constraints
{{内容、时代、人物、平台与题材边界}}

## Style card
- POV: {{叙事视角}}
- Voice notes: {{语域与叙述习惯}}
- Anti-patterns to avoid: {{禁止的表达模式}}
```

### `outline/book_outline.md`

```markdown
# 全书总纲 · {{book.title}}
- schema_version: 1
- book_id: {{book.id}}
- title: {{book.title}}
- logline: {{主角＋困境＋目标＋核心冲突}}

## 承诺 · {{promise-id}}
- description: {{持续交付的阅读体验}}

## 故事线 · {{thread-id}}
- kind: {{main|character|relationship|world|mystery|other}}
- description: {{这条线的核心内容}}
- opening_state: {{起点}}
- ending_state: {{终点}}

## 分卷 · v{{NN}}
- arc_id: arc-v{{NN}}
- title: {{本卷标题}}
- chapter_range: {{起始章}}-{{结束章}}
- purpose: {{本卷结构职责}}
- dramatic_question: {{本卷必须回答的问题}}
- central_conflict: {{核心冲突}}
- stakes: {{失败代价}}
- entry_state: {{开卷必须成立的事实}}
- exit_state: {{卷末不可逆状态}}
- thread_moves: {{thread-id=从什么状态->到什么状态}}
- deliverables: {{本卷必须兑现的结果}}
- open_questions: {{允许保留的问题}}
- reveal_ids: {{本卷可揭示 ID，如 r01,r02；没有则该行留空}}
- forbidden_reveal_ids: {{本卷禁提前揭示 ID，如 r11,r15；没有则该行留空}}
- next_volume_entry: {{下一卷行动入口}}

### 转折 · {{event-id}}
- event: {{具体不可逆事件}}
- cause: {{由哪个前序行动导致}}
- effect: {{如何改变后续行动或状态}}
- chapter_window: {{允许落点的章范围}}
```

## 4. 卷纲模板

### `outline/volumes/v{{NN}}.md`

```markdown
# 卷纲 · v{{NN}}
- schema_version: 1
- volume_id: v{{NN}}
- title: {{本卷标题}}
- source_arc_id: arc-v{{NN}}
- chapter_range: {{起始章}}-{{结束章}}
- goal: {{继承总纲的卷级问题}}
- central_conflict: {{核心冲突}}
- stakes: {{失败代价}}
- entry_state: {{开卷事实}}
- exit_state: {{卷末不可逆状态}}
- cast: {{本卷人物 stem 列表}}

## 节拍 · {{event-id}}
- event: {{具体事件}}
- cause: {{前序原因}}
- effect: {{不可逆结果}}
- chapter_window: {{落点章范围}}

## 单元 · {{unit_id}}
- title: {{核心动作或意象，不罗列全部事件}}
- chapter_range: {{起始章}}-{{结束章}}
- objective: {{本单元局部目标}}
- obstacle: {{具体阻碍}}
- attempt: {{主要尝试/方法}}
- setback: {{方法缺口或代价初现}}
- choice: {{主角的关键选择}}
- cost: {{选择承担的代价}}
- outcome: {{一个不可逆主结果}}
- entry_cause: {{前序单元结果或钩子 ID}}
- exit_result: {{本单元离场时已经改变的状态}}
- state_changes: {{目标.字段=原状态->新状态；可多项}}
- clock_start_day: {{本单元开场相对故事日整数}}
- clock_end_day: {{本单元结束相对故事日整数}}
- clock_gap_days: {{距上一单元的最少间隔天数}}
- clock_deadline: {{可选：event-id@day}}
- timeline_id: {{main 或闪回/并行线 ID}}
- allow_flashback: {{非 main 时间线时为 true，否则省略}}
- thread_ids: {{总纲故事线 ID 列表}}
- event_ids: {{本卷节拍/转折 ID 列表}}
- reveal_ids: {{可揭示 ID 列表，如 r01,r02；没有则留空}}
- forbidden_reveal_ids: {{禁止提前揭示 ID 列表，如 r11；没有则留空}}
- next_hook: {{钩子类型 | 具体未决事件}}

### 身份变更 · {{unit_id}}-{{character.stem}}
- stem: {{character.stem}}
- role_title: {{从本单元起生效的新公开职务}}
- org: {{新机构}}
- from_unit: {{unit_id}}
- reason: {{调令/任命/公开身份变化的依据}}

## 卷末交接 · handoff
- deliverables: {{本卷已兑现结果}}
- open_questions: {{未兑现承诺与开放故事线}}
- next_volume_entry: {{下一卷行动入口}}
```

单元必须是一个主要戏剧问题、一个主结果、一种主要冲突形态。章范围由因果密度决定，常见 3–8 章，硬上限 10 章；身份变更只发生在单元边界。`clock_*` 是单元索引的字段，不另建重复时钟表。

## 5. 单元细纲与正文

### `outline/units/v{{NN}}-U{{N}}.yaml`

```yaml
unit_id: {{unit_id}}
status: proposed
source_seed_id: {{unit_id}}
title_working: "{{核心动作或意象}}"
function: "{{本单元在卷中的结构功能}}"
desire: "{{继承单元 objective}}"
obstacle: "{{继承单元 obstacle}}"
attempt: "{{继承单元 attempt}}"
setback: "{{继承单元 setback}}"
choice: "{{继承单元 choice}}"
cost: "{{继承单元 cost}}"
payoff: "{{继承单元 outcome}}"
pleasure: "{{本单元的阅读兑现/情绪形态}}"
forbidden: "{{禁碰事实、揭示、表达和合规边界}}"
chapter_chars: {{单章目标字数，只能用 2000–3500，默认 2500}}
word_target: "{{由 chapter_chars × 章数派生，不要手填}}"
on_stage: [{{在场人物 stem 列表}}]
pov: {{POV 人物 stem，必须属于 on_stage}}
chapter_range: [{{起始章}}, {{结束章}}]
story_day: {{继承 clock_start_day，整数}}
end_day: {{继承 clock_end_day，整数}}
gap_days: {{继承 clock_gap_days，非负整数}}
timeline_id: {{main 或闪回/并行线 ID}}
gap_from_prev: "{{给写作者的上一单元承接说明}}"
info_control: "{{POV 知道/不知道、读者已知/未知、可揭示/禁揭示}}"
chapters:
  - chapter: {{章号}}
    goal: {{本章推进的具体问题}}
    change: {{章末相对章首的可验证状态/信息/关系/行动变化}}
    cut_hook: {{章末具体未决事件，中间章不提前兑现单元主结果}}
    day_offset: {{相对本单元开场的非负整数日偏移}}
    scenes:                              # 可选：只有复杂章需要
      - id: S{{序号}}
        chapter: {{所属章号}}
        goal: {{这场行动要达成什么}}
        conflict: {{行动遇到什么阻力}}
        turn: {{不可替代的转折或方法改变}}
        outcome: {{这场结束时的实际变化}}
        must_land:
          - {{最多 1–2 个不可遗漏的可见事实}}
        evidence_in:
          - {{可选：本场依赖的前置材料}}
        evidence_out:
          - {{可选：本场产生、后续使用的材料}}
        day_offset: {{可选：不早于本章的日偏移}}
state_deltas:
  - stem: {{人物或实体 ID}}
    field: {{role_title|location|injury|resources|goal|knowledge}}
    from: {{单元前状态}}
    to: {{单元后状态}}
next_hook:
  type: {{钩子类型}}
  out: {{具体未决事件}}
```

**章与场面的职责：** 每章必须有 `goal/change/cut_hook`，不强制 scenes。场面只组织复杂行动与关键材料，核心字段仅 `goal/conflict/turn/outcome`；不预写台词、动作表、情绪曲线。`who/where/pov` 只有偏离单元默认时才补充。

### `units/v{{NN}}-U{{N}}.md`

```markdown
## 第{{章号}}章 {{2–6 字具体意象或动作标题}}

{{纯散文正文，无 Markdown 小节标题、无大纲解释}}

---

## 第{{章号}}章 {{下一章标题}}

{{下一章正文}}
```

## 6. 人物与身份

### `canon/cast/{{character.stem}}.md`

```markdown
# {{纯姓名，不含括号角色标注}}

`status`: {{candidate|canon}}
`role`: {{protagonist|volume_antagonist|recurring}}
`tagline`: {{一句话稳定身份说明，不替代职务时间线}}

## 三锚点
- 视觉：{{观察人物时的稳定外在特征}}
- 行动：{{面对压力时的稳定行动倾向}}
- 关系：{{稳定的关系/情绪锚点}}

## 欲望与伤口
- 外在欲望：{{目标}}
- 内在伤口/误区：{{伤口或错误信念}}
- 行动方式：{{主要方法}}
- 不可逾越边界：{{底线}}

## 知识边界
- 知道：{{此人稳定可知的信息}}
- 不知：{{此人不能提前知道的信息}}

## 台词
- {{能体现人物声音的短句}}

## 关系
| 对方 | 类型 | 当前 |
|------|------|------|
| {{对方 stem}} | {{搭档/宿敌/上司等}} | {{当前关系状态}} |
```

### `canon/identity-timeline.yaml`

```yaml
version: 1
segments:
  - stem: {{character.stem}}
    role_title: {{本段公开职务/身份}}
    org: {{所属机构}}
    from_unit: {{起始单元 ID}}
    to_unit: {{最后有效单元 ID；开放至今可省略}}
    note: {{初始身份或变更依据}}
```

人物卡保存稳定人格；身份时间线保存公开职务身份；continuity 保存位置、伤势、资源、目标与知情范围。单元内公开身份恒定，变化只在边界新增段。

## 7. 设定文件

### `canon/world.md`

```markdown
# World — {{book.title}}

## 力量 / 规则
- 谁能做什么：{{主体与能力}}
- 代价：{{使用代价}}
- 上限：{{能力上限}}
- 例外（必须写代价）：{{例外与对应代价}}

## 势力地图
| 势力 | 占有资源 | 冲突线 | 与主角关系 |
|------|----------|--------|------------|
| {{势力}} | {{资源}} | {{冲突}} | {{关系}} |

## 日常物质
- 钱 / 等价物：{{经济水平}}
- 交通：{{交通条件}}
- 通讯：{{通讯条件}}
- 阶级观感（烟火气）：{{日常生活与阶层差异}}

## 禁忌与法律
- 碰什么会死 / 会被追杀：{{禁忌}}
- 谁来执行：{{执行者}}

## 与圣经的衔接
- 读者承诺依赖的世界规则：{{必要规则}}
- 终局储备里尚未揭开的规则（本章不得写穿）：{{限制}}
```

### `canon/goldfinger.md`

```markdown
# Goldfinger / resources

| 资源 / 能力 | 谁能使用 | 代价 | 上限 | 例外与限制 | 最近使用 |
|-------------|----------|------|------|------------|----------|
| {{能力}} | {{主体}} | {{代价}} | {{上限}} | {{限制}} | {{可选记录}} |
```

### `canon/platform-positioning.md`

```markdown
# Platform positioning
- 平台 / 频道：{{平台}}
- 目标读者：{{读者}}
- 篇幅 / 更新：{{规模与更新}}
- 开篇承诺：{{开篇必须兑现的体验}}
- 内容红线：{{平台与题材红线}}
```

### `canon/writing-rules.md`

> 字数写进新书模板时取自 `core/domain/chapter_length.go` 的常量（`ChapterCharsMin/Max/Default`），
> 这里只是它们当前值的快照；改标准改那个文件，别改这份示例。

```markdown
# Writing rules
- 单元 3–8 章（硬上限 10）
- 单章 2000–3500 字，目标 2500 字：在细纲写 `chapter_chars`，别写单元总字数
- 正文只写 prose，章间单独一行 ---
- 不用 AI 套话，不提前打光终局底牌
```

### `canon/style-fingerprint.md`

```markdown
# Style fingerprint
## POV / 叙事距离
{{视角与距离}}
## 句长与段落
{{句式、段长与节奏}}
## 对话与潜台词
{{人物对白与潜台词规则}}
## 感官与物质细节
{{感官、道具与日常细节偏好}}
## 禁语 / 反模式
{{禁语与 AI 套话}}
```

### `canon/foreshadow-schedule.md`

伏笔是带稳定 ID 的一等实体，与人物卡同级：总纲定卷、卷纲定单元、细纲定章。

```markdown
# 伏笔登记表（唯一权威来源）
- schema_version: 1

## 伏笔 · FS-01
- content: {{埋的是什么，必须是可见可验的具体物或事}}
- type: {{物件|信息|关系|承诺|疑点}}
- plant_volume: v{{NN}}
- plant_unit: v{{NN}}-U{{N}}          # 卷纲补，可空
- plant_chapter: {{章号}}             # 细纲补，可空
- payoff_volume: v{{NN}}
- payoff_unit: v{{NN}}-U{{N}}
- payoff_chapter: {{章号}}
- payoff_summary: {{回收时读者得到什么；type=信息 时必填}}
- locked_terms: {{回收前不得出现的词，可空}}
- status: {{planned|planted|advanced|paid|dropped}}
- note: {{一句话说明回收方式}}
```

分级写入责任：

| 层 | 只允许写 | 典型内容 |
|---|---|---|
| 总纲（立项） | `content/type/plant_volume/payoff_volume` | 全书有哪些伏笔、各自落在哪两卷 |
| 卷纲 | 本卷条目的 `plant_unit/payoff_unit` | 细化到单元 |
| 单元细纲 | `chapters[].plant` / `chapters[].payoff` | 细化到章，只填 FS ID |

`type: 信息` 的伏笔就是原来的揭示计划（读者何时可以知道某个秘密），因此
分卷与单元不再有 `reveal_ids` / `forbidden_reveal_ids`：能否揭示由回收位置派生。

### `canon/author-lore.md`

```markdown
# Author lore — {{book.title}}

**Author-only.** 正文写作、正文审校和修订禁止加载。

## 终局储备（细节）
| 底牌 | 真相（作者可见） | 最早解锁卷 | 公开层目前可见 |
|------|------------------|------------|----------------|
| {{底牌}} | {{真实答案}} | v{{NN}} | {{公开层可感知的现象}} |

## 未揭规则
{{真实规则及预留边界}}

## 角色私密
| 角色 | 秘密 | 读者何时可推断 | 正文何时确认 |
|------|------|----------------|--------------|
| {{角色}} | {{秘密}} | {{可推断时点}} | {{确认时点}} |

## 未来反转库存
{{预留反转与使用条件}}
```

### `canon/locked-terms.yaml`

```yaml
# locked_until: 卷号 -> 该卷之前正文禁用的真名/真相词
# compliance: 永久合规红线
locked_until: {}
compliance: []
aliases: {}
```

## 8. 运行与定稿文件

### `novel-state.yaml`

```yaml
title: {{book.title}}
genre: {{题材}}
subgenre: {{子题材}}
stage: {{planning|outlining|writing}}
last_committed_ch: {{最后定稿章号}}
next_action: {{plan|outline|write|review|finalize}}
active_unit: {{当前单元 ID}}
time_system: story_day
qc_profile: general
continuation_mode: false
gates:
  knowledge: unknown
  asset: unknown
  qc: unknown
blockers: []
```

### `continuity/state.yaml`

```yaml
version: 1
units:
  - unit: {{unit_id}}
    volume: {{卷号}}
    after_ch: {{最后定稿章号}}
    story_day: {{单元开场日}}
    end_day: {{单元实际结束日}}
    cast:
      {{character.stem}}:
        role_title: {{定稿时本单元公开身份，与身份时间线一致}}
        location: {{实际位置}}
        injury: {{实际伤势}}
        resources: {{实际资源}}
        goal: {{实际目标}}
        knows:
          - {{实际已知信息}}
    loops:
      - id: {{伏笔 ID}}
        status: {{planted|advanced|paid|dropped}}
        detail: {{公开伏笔内容}}
        planted: {{埋点章}}
        paid: {{实际回收章，可选}}
```

### `continuity/summaries/v{{NN}}.md`

```markdown
# 第 {{NN}} 卷章摘要

| 章 | 事件 | 状态变化 | 伏笔 | 钩子 |
|----|------|----------|------|------|
| {{章号}} | {{实际关键事件}} | {{实际状态变化}} | {{伏笔推进}} | {{章末钩子}} |
```

### `continuity/commits/v{{NN}}-U{{N}}.md`

```markdown
# 定稿账本 · {{unit_id}}

- 定稿时间: {{RFC3339 时间}}
- 章范围: {{起始章}}-{{结束章}}
- 总字数: {{正文字数}}
- 门禁: blocking={{数量}} advisory={{数量}}
- 下一钩: {{下一钩类型}}

## state_deltas
{{定稿实际采用的状态迁移}}
```

这些运行/账本文件记录已接受结果，不用来替代作者规划或人物身份。旧书中的自由场景长表、散文时钟、人物职务备注应映射到上述实体，而不是原样叠加成重复设定。
