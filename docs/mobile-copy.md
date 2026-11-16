# Mobile copy (iOS / Android)

Both native apps are Chinese-first and share the Go core but not a UI layer, so
copy is duplicated. This file is the single glossary: when the two platforms
name the same thing differently, the term here wins and the other side changes.

Canonical wording follows the **iOS app and the Vue desktop app**, which already
agree (`AGENTS.md` rule 9: iOS is the mobile baseline). Android is the side that
adapts.

## Where the strings live

| Platform | Single source | Rule |
|---|---|---|
| Android | `native-android/app/src/main/res/values/strings.xml` | Every user-visible string in `ui/` comes from `stringResource(R.string.…)`. No Chinese literal in Kotlin UI code. |
| iOS | `native-ios/Sources/Core/Copy.swift` | Every user-visible string in `Sources/Features/` comes from `Copy.…`. No Chinese literal in view code. |

Status and run vocabulary (`已定稿`, `排队`, …) also exists in the data layer,
because it is domain wording rather than screen chrome: `Models.kt`
(`UnitInfo.statusTitle`) and `Models.swift` (`UnitInfo.statusTitle`,
`RunRecord.kindTitle`, `RunRecord.statusTitle`). Those two files must match the
table below; keep them in sync when a status is added.

A String Catalog (`Localizable.xcstrings`) is deliberately **not** used yet: the
shipping language is zh-Hans only, so a catalog keyed by the Chinese literals
would be a no-op. `Copy.swift` gives the same "one file to diff" property and
fails at compile time if a constant is removed. When a second language is added,
`Copy.swift` values become the catalog keys.

## Terms

| Concept | Use | Do not use |
|---|---|---|
| Model provider (object) | **提供商** | 模型服务 |
| Add / edit provider | **添加提供商** / **编辑提供商** | 添加模型服务 |
| Provider name field | **名称** | 服务名称 |
| Run history tab | **日志** | 运行记录 |
| Run history page title | **模型日志** | 运行记录 |
| Run history empty | **暂无模型日志** | 尚无运行记录 |
| Create a book | **新建书籍** | 新建一本书 |
| Book unit | **单元** | 写作单元 |
| Book bible | **设定** | 设定集 |
| Reading surface | **正文** | 内容 |
| Candidate counter on shelf | **N 个候选待处理** | 待审（裸词） |
| Sample book CTA | **看看示例书** | 打开示例书 |
| Empty shelf title | **开始你的第一本书** | — |
| Empty shelf body | **书稿保存在本机。你可以先读一本完整示例，或从自己的灵感开始。** | 先读一本完整示例… |
| Unknown genre | **未分类** | — |
| Unnamed unit | **未命名单元** | — |
| Provider key note (settings footer) | **密钥只保存在本机，不会出现在页面或日志中。模型调用只连接你配置的服务。** | 密钥只保存在本机内核中… |
| Provider key field hint (existing key) | **留空保持不变** | API Key（留空保持不变） |
| Empty logs body | **模型调用、耗时与失败原因会记录在这里。在书籍的写作页发起生成任务后，日志会实时记录每次调用。** | …当前原生版本不会启动模型生成。 |
| Missing model id in a run | **未指定模型** | — |
| Knowledge tab | **知识** | 知识库（tab 标签用短词） |
| Book workspace panels | **写作 / 大纲 / 设定 / 任务** | 审校 tab、候选 tab（已合并为任务） |
| Task page title | **任务与确认** | 待审候选 |
| Task sections | **待确认 / 进行中 / 操作记录** | 待审候选 / 运行记录 |
| Task card context | **单元合同与上下文** | — |
| Task card parent seed | **父级卷纲种子（不可改写的上层合同）** | — |
| Task card gate summary | **门禁与连续性** | — |
| Unit pending pointer | **本单元有 N 项待确认** + **去确认** | — |
| Review submit summary | **将修复 N 条 · 涉及 M 章** | — |
| Unit flow actions | **细纲 / 正文 / 审校 / 定稿**（重做时加「重写」「重新」前缀；位于单元页系统工具栏） | — |
| Chapter actions menu | **章节操作**（菜单项：复制本章 / 编辑本章） | — |
| More actions menu | **更多**（系统 ellipsis 菜单的标签） | — |
| Outline document picker | **选择大纲文档**（全书总纲 / 第 N 卷，系统 Menu） | 手绘 chip 切换 |
| Outline field edit verbs | **编辑描述 / 编辑结构职责 / 编辑主结果** | 裸「编辑」 |
| Run waiting for accept | **候选已生成，去任务页确认** | 去候选页确认（候选 tab 已不存在） |
| Discard unsaved edit | **放弃未保存的修改？**（放弃修改 / 继续编辑） | — |
| Run confirm action | **确认调用**（定稿为 **确认账本核对**） | — |
| Candidate accept | **接受**（修订为 **确认落盘**） | — |
| Candidate expand content | **展开内容** | — |
| Review auto-fix | **自动修复** | 一键修复 |
| Deferred setup notice | **立项流程请在桌面端完成** | — |

## Unit status (`UnitInfo.status`)

| Status | Title | Finished? |
|---|---|---|
| `finalized` | 已定稿 | yes |
| `reviewed` | 已审校 | yes |
| `drafted` | 待审校 | no |
| `accepted` | 待写正文 | no |
| `proposed` | 待写细纲 | no |
| anything else | the raw status | no |

`reviewed` and `finalized` are both finished, but only `finalized` is 已定稿.
Collapsing them made a reviewed unit read as shipped.

## Run kind (`RunRecord.kind`)

Same labels as `frontend/src/shared/utils/runLabels.ts` so the phone and desktop
logs read alike. The iOS list used to print the raw identifier (`write`).

| Kind | Title |
|---|---|
| `plan` | 立项（`mode=volume` 卷纲 / `mode=reinit` 重新立项） |
| `idea` | 创意扩写 |
| `outline` | 细纲 |
| `write` | 正文（`rewriteMode=refine` 正文 · 原文优化） |
| `review` | 审校 |
| `revise` | 修订 |
| `finalize` | 定稿 |
| `rewrite_entity` | 重写设定 |
| `structure_review` | 结构审校 |
| `local_edit` | 局部编辑 |
| `continue` | 续写 |
| `expand` | 扩写 |
| `polish` | 润色 |
| `rewrite_selection` | 选区改写 |
| anything else | the raw kind |

## Run status (`RunRecord.status`)

| Status | Title |
|---|---|
| `pending` | 排队 |
| `running` | 运行中 |
| `waiting_accept` | 待接受 |
| `succeeded` | 成功 |
| `failed` | 失败 |
| `cancelled` | 已取消 |
| `rejected` | 已拒绝 |
| `expired` | 已过期 |
| anything else | the raw status |

Android previously used 已完成 / 执行中 / 等待接受候选 for the same states, which
made the same run read differently on the two phones.

## Deliberate differences (not drift)

Platform chrome stays native and is not forced to match:

- Navigation: `TabView` + `NavigationSplitView` on iOS, `NavigationBar` +
  `NavHost` on Android.
- Empty states: `ContentUnavailableView` on iOS, a composed column on Android.
- Forms: `Form` / `.sheet` on iOS, `OutlinedTextField` /
  `ModalBottomSheet` / `ExposedDropdownMenu` on Android.
- iOS-only surfaces: iCloud sync, reading settings, the 设定 pane, expanded
  reading. They are explained in `docs/native-mobile.md`.

Accessibility labels and hints (content descriptions, `.accessibilityHint`)
follow the same glossary even though they are not visible text.
