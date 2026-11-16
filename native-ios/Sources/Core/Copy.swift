import Foundation

/// Every user-visible string the iOS app shows.
///
/// The two phones duplicate their UI (only the Go core is shared), so wording
/// drifted: Android said 模型服务 / 运行记录 where iOS and the Vue desktop said
/// 提供商 / 模型日志. `docs/mobile-copy.md` is the glossary; this namespace is
/// the iOS half of it and `native-android/…/values/strings.xml` is the other, so
/// a term is changed in one place and diffing the two phones is a single-file
/// comparison per platform.
///
/// Values are plain `String`s rather than `LocalizedStringKey`s on purpose: the
/// shipping language is zh-Hans only, so a String Catalog keyed by Chinese
/// literals would be a no-op. When a second language is added these become the
/// catalog's keys.
enum Copy {
    // MARK: - Shell

    static let tabShelf = "书架"
    static let tabLogs = "日志"
    static let tabKnowledge = "知识"
    static let tabSettings = "设置"
    static let corePreparing = "正在准备本地工作区…"
    static let errorOperationFailed = "操作未完成"
    static let actionOK = "好"
    static let actionCancel = "取消"
    static let actionSave = "保存"
    static let actionClose = "关闭"
    static let actionEdit = "编辑"
    static let actionDelete = "删除"
    static let actionRetry = "重试"
    static let actionDone = "完成"
    static let actionMore = "更多"
    static let saved = "已保存"
    static let saving = "保存中…"
    static let loading = "加载中…"
    static let emptyValue = "—"

    // MARK: - Shelf

    static let shelfTitle = "我的书架"
    static let shelfLoading = "正在打开书架…"
    static let shelfNewBook = "新建书籍"
    static let shelfEmptyTitle = "开始你的第一本书"
    static let shelfEmptyBody = "书稿保存在本机。你可以先读一本完整示例，或从自己的灵感开始。"
    static let shelfOpenDemo = "看看示例书"
    static let shelfPickBookTitle = "选择一本书"
    static let shelfPickBookBody = "在左侧选择书稿，查看单元并继续阅读。"
    static let shelfOpenBookHint = "打开单元和正文"
    static let genreUnknown = "未分类"
    static let readOnly = "只读"
    static let reviewProgressLabel = "定稿进度"

    static func unitCount(_ count: Int) -> String { "\(count) 个单元" }
    static func reviewedCount(_ count: Int) -> String { "\(count) 篇定稿" }
    static func candidates(_ count: Int) -> String { "\(count) 个候选待处理" }
    static func reviewProgress(reviewed: Int, units: Int) -> String { "\(reviewed)/\(units)" }

    // MARK: - New book (basic info + creative seed; same fields as desktop)

    static let newBookSection = "基本信息"
    static let newBookSeedSection = "创意种子"
    static let newBookNameLabel = "书名"
    static let newBookIDLabel = "书 ID"
    static let newBookIDHint = "英文 / 数字 / 连字符"
    static let newBookGenreLabel = "题材"
    static let newBookSubgenreLabel = "子题材"
    static let newBookSubgenreHint = "如：刑侦探案 / 年代刑侦"
    static let newBookPlatformLabel = "平台 / 频道"
    static let newBookPlatformHint = "如：番茄 · 男频"
    static let newBookAudienceLabel = "目标读者"
    static let newBookAudienceHint = "如：18–30 男性读者"
    static let newBookLengthLabel = "预计篇幅"
    static let newBookLengthHint = "如：80 万字 / 3 卷"
    static let newBookChapterCharsLabel = "单章目标字数"
    static let newBookChapterCharsHint = "如：3000"
    static let newBookIdeaLabel = "一句话 idea"
    static let newBookIdeaHint = "主角、困境、目标和核心冲突"
    static let newBookToneLabel = "基调"
    static let newBookToneHint = "如：冷硬、克制、黑色幽默"
    static let newBookPleasureLabel = "核心爽点 / 读者承诺"
    static let newBookPleasureHint = "读者每次翻页得到什么"
    static let newBookProtagonistLabel = "主角种子"
    static let newBookProtagonistHint = "身份、欲望、伤口、不会越过的底线"
    static let newBookConflictLabel = "主要对抗"
    static let newBookConflictHint = "持续制造压力的力量"
    static let newBookEndingLabel = "结局倾向"
    static let newBookEndingHint = "方向与代价，不写真相"
    static let newBookPOVLabel = "POV"
    static let newBookPOVHint = "如：第三人称限知"
    static let newBookTaboosLabel = "禁忌 / 红线"
    static let newBookTaboosHint = "内容、时代、平台边界"
    static let newBookReferencesLabel = "参考作品"
    static let newBookReferencesHint = "只作风格与结构参考"
    static let newBookFootnote = "基础信息与创意种子会写进 outline/idea.md，供后续创作使用。生成内容需要你自己的模型 API Key，阅读和整理不需要。"
    static let newBookFailed = "无法创建书籍"
    static let actionCreate = "创建"

    /// Same list on both phones; Android had dropped 仙侠.
    static let genres = ["玄幻", "仙侠", "都市", "悬疑", "言情"]

    // MARK: - Book workspace

    static let bookFallbackTitle = "书籍"
    static let bookLoading = "正在读取书稿…"
    static let bookUnavailable = "暂时打不开这本书"
    static let bookRefresh = "刷新"
    static let contentPickerLabel = "内容"
    static let contentUnits = "单元"
    static let contentWrite = "写作"
    static let contentOutline = "大纲"
    static let contentBible = "设定"
    static let contentReview = "审校"
    static let contentCandidates = "候选"
    static let contentTasks = "任务"
    static let bookNoUnits = "还没有单元"
    static let bookNoUnitsBody = "先生成卷纲播种单元，或在大纲页完善结构。"
    static let unitUnnamed = "未命名单元"
    static let enableWrite = "启用写入"
    static let enableWriteDone = "已启用写入"

    static func volumeTitle(_ volume: Int) -> String { "第 \(volume) 卷" }
    static func unitChapters(from: Int, to: Int) -> String { "第 \(from)–\(to) 章" }
    static func volumeProgress(_ reviewed: Int, _ total: Int) -> String { "\(reviewed)/\(total)" }

    // MARK: - Primary task

    static let primaryKicker = "下一步"
    static let primaryStarting = "正在启动…"
    static let primaryContinue = "继续创作"
    static let primarySelectNext = "选择下一步"
    static let primaryWholeBook = "全书"
    static func primaryOutline(_ target: String) -> String { "生成 \(target) 细纲" }
    static let primaryOutlineDesc = "第 1 步 · 补全单元写作合同；也可在单元栏随时重写细纲"
    static func primaryWrite(_ target: String) -> String { "写 \(target) 正文" }
    static let primaryWriteDesc = "第 2 步 · 按合同生成候选稿；有正文后可在单元栏重写"
    static func primaryReview(_ target: String) -> String { "审校 \(target)" }
    static let primaryReviewDesc = "第 3 步 · AI 审校；通过后在单元栏点「定稿」回写账本"
    static func primaryFinalize(_ target: String) -> String { "定稿 \(target)" }
    static let primaryFinalizeDesc = "第 4 步 · 本地门禁与逐章账本核对；接受后统一提交"
    static func primaryVolumePlan(_ volume: Int) -> String { "生成卷纲 · 第 \(volume) 卷" }
    static let primaryVolumePlanDesc = "根据总纲生成当前卷纲，并只播种缺失单元"
    static let primaryPlanDeferred = "立项流程请在桌面端完成"
    static let primaryPlanDeferredDesc = "当前版本暂未提供概念碰撞与立项；已有总纲的书可在此继续生产。"

    // MARK: - Unit work

    static let unitFlowActions = "单元操作"
    static let actionOutline = "细纲"
    static let actionOutlineRewrite = "重写细纲"
    static let actionWrite = "正文"
    static let actionWriteRewrite = "重写正文"
    static let actionReview = "审校"
    static let actionReviewAgain = "重新审校"
    static let actionFinalize = "定稿"
    static let actionFinalizeAgain = "重新定稿"
    static let unitEditOutline = "手工编辑细纲"
    static let unitViewOutlineSource = "查看细纲源文件"
    static let unitContractTitle = "单元合同"
    static let unitContractHint = "写正文时对照"
    static let unitContractEmpty = "这个单元还没有细纲合同。"
    static let unitContractEmptyHint = "细纲会定义欲望、阻碍、关键选择、场面节拍和故事时钟——写正文前必须先补全。"
    static let unitProseTitle = "单元正文"
    static let unitEditChapter = "编辑本章"
    static let unitCopyChapter = "复制本章"
    static let unitCopied = "已复制"
    static let unitChapterActions = "章节操作"
    static let discardTitle = "放弃未保存的修改？"
    static let discardMessage = "本次编辑还没有保存，关闭后修改会丢失。"
    static let discardConfirm = "放弃修改"
    static let discardContinue = "继续编辑"
    static func unitProseSummary(chapters: Int, words: Int) -> String { "共 \(chapters) 章 · \(words.formatted()) 字" }
    static func chapterWords(_ words: Int) -> String { "\(words.formatted()) 字" }
    static let chapterSplitHint = "建议在此切成上下两章"
    static func chapterSplitDetail(above: Int, below: Int) -> String { "上 \(above.formatted()) 字 / 下 \(below.formatted()) 字" }

    // Contract field labels
    static let contractFunction = "单元功能"
    static let contractDesire = "欲望"
    static let contractObstacle = "阻碍"
    static let contractChoice = "关键选择"
    static let contractPayoff = "兑现"
    static let contractPleasure = "读者爽点"
    static let contractForbidden = "禁忌"
    static let contractInfoControl = "信息控制"
    static let contractGapNote = "衔接上一单元"
    static let contractChapterPlans = "逐章推进"
    static let contractGoal = "目标"
    static let contractChange = "变化"
    static let contractCutHook = "断章"
    static let contractScenes = "场面脉络"
    static let contractSceneConflict = "阻力"
    static let contractSceneTurn = "转折"
    static let contractSceneOutcome = "结果"
    static let contractNoScenes = "本章没有场面脉络：写正文时只能临场决定路径。"
    static let contractOrphanScenes = "未归章场面"
    static let contractOrphanHint = "章号越界或章合同缺失"
    static func contractPOV(_ name: String) -> String { "POV \(name)" }
    static func contractWordTarget(perChapter: Int, total: Int) -> String { "每章 \(perChapter.formatted()) 字 · 共约 \(total.formatted()) 字" }
    static func contractStoryDay(_ day: String) -> String { "故事第 \(day) 天" }
    static let contractFollowsPrev = "紧接上一单元"
    static func contractGapDays(_ days: Int) -> String { "距上一单元 \(days) 天" }
    static let contractFlashback = "闪回"
    static func contractNextHook(_ hook: String) -> String { "下一钩：\(hook)" }
    static func dayOffset(_ offset: String) -> String { "第 \(offset) 日偏移" }
    static func scenesCount(_ count: Int) -> String { "\(count) 场" }
    static func chaptersCount(_ count: Int) -> String { "\(count) 章" }

    // MARK: - Reader

    static let readerOpening = "正在打开正文…"
    static let readerFailed = "正文读取失败"
    static let readerUnrecognized = "这份正文还没有可识别的章节。"
    static let readerUnwrittenHint = "正文尚未写作，暂不可阅读"
    static let readerSettings = "阅读设置"
    static let readerSettingsDone = "完成"
    static let readerFontSection = "正文字号"
    static let readerFontSmall = "小"
    static let readerFontLarge = "大"
    static let previousChapter = "上一章"
    static let nextChapter = "下一章"

    static func readerFontSize(_ size: Int) -> String { "字号 \(size)" }
    static func chapterHeading(_ number: Int) -> String { "第 \(number) 章" }
    static func chapterMenuLabel(number: Int, title: String) -> String { "第 \(number) 章 · \(title)" }
    static func chapterCountLabel(number: Int, count: Int) -> String { "第 \(number) 章，共 \(count) 章" }

    // MARK: - Run confirm

    static let runConfirmSuffix = "LLM 调用确认"
    static let runConfirmFinalizeHead = "定稿 · 逐章事实核对"
    static let runConfirmModel = "模型"
    static let runConfirmModelByStage = "按阶段分配"
    static let runConfirmBudget = "调用预算"
    static let runConfirmBudgetLocal = "0 次（本地）"
    static func runConfirmBudgetCalls(_ count: Int) -> String { "\(count) 次" }
    static let runConfirmContext = "上下文"
    static func runConfirmContextSize(blocks: Int, kb: Double) -> String { "\(blocks) 块 · \(String(format: "%.1f", kb)) KB" }
    static let runConfirmContextLabel = "将发送给模型的内容（依据只读；接受才写盘）"
    static let runConfirmPreviewLoading = "正在构建上下文预览…"
    static let runConfirmPreviewFailed = "预览上下文失败"
    static let runConfirmSystemPrompt = "系统提示词"
    static let runConfirmBasisGroup = "重写依据（只读，不写回）"
    static let runConfirmRewriteMode = "重写方式"
    static let runConfirmRewriteRefine = "在原文上优化"
    static let runConfirmRewriteFresh = "重新生成"
    static let runConfirmRewriteNote = "「在原文上优化」会把现有正文整份放进上下文（逐章仍只写本章），适合改表达、压节奏、换镜头；「重新生成」看不到旧稿，适合换写法或推倒重来。改动幅度会在候选里给出提示。"
    static let runConfirmKnowledge = "参考知识（可选）"
    static func runConfirmKnowledgePicked(_ count: Int) -> String { "已选 \(count) 篇" }
    static let runConfirmKnowledgeNote = "勾选的文章会作为「方法/风格参考」附加到本次调用，不写入正文事实。"
    static let runConfirmKnowledgeAuto = "默认注入（无需勾选，已包含在本次上下文里）："
    static let runConfirmKnowledgeShowAll = "显示全部"
    static let runConfirmKnowledgeShowSuitable = "只看适用"
    static let runConfirmKnowledgeEmpty = "该步骤下还没有适用的文章；可在「知识」页给文章补上写作步骤和题材标记。"
    static let knowledgeMismatchStage = "步骤不符"
    static let knowledgeMismatchGenre = "题材不符"
    static func runConfirmKnowledgeScope(stage: String, genre: String, count: Int) -> String { "「\(stage)」· 题材 \(genre) · 可选 \(count) 篇" }
    static let runConfirmInstruction = "追加指令（可选）"
    static let runConfirmInstructionHint = "例如：保持既有事实，重点精简第 2 节；或：本卷新增人物 X，纳入索引…"
    static let runConfirmAction = "确认调用"
    static let runConfirmFinalizeAction = "确认账本核对"
    static let runStarted = "任务已开始"

    static let riskFinalize = "定稿先跑本地门禁，再逐章核对实际摘要、状态与伏笔；作者接受候选后才统一回写账本并标记已定稿。"
    static let riskVolumePlan = "只写当前卷纲，播种缺失单元；不改总纲/设定/人物/正文。"
    static func riskVolumePlanLater(_ prev: Int) -> String { "只写当前卷纲，播种缺失单元。上下文含第 \(prev) 卷承接与相关人物（非全书卡）；不改总纲/设定/正文。" }
    static let riskDerivedRewrite = "总纲派生设定：已附总纲等依据（只读）。接受仅写当前文件。"
    static let riskEntityRewrite = "生产约束可独立优化；接受仅写当前文件。"
    static let riskWriteRefine = "优化模式：把本单元现有正文整份发给模型作底稿（逐章只写本章），保留场次与事件骨架，只优化表达/节奏/镜头/对白；输出仍是整章候选，接受后才覆盖原稿。"
    static let riskWriteFresh = "重新生成：不带旧稿，按合同重写整章候选；原稿只在接受后才会被覆盖。若只想保住已满意的地方，改用「在原文上优化」。"
    static func riskSupersedeReview(_ count: Int) -> String { "重审会先归档该单元 \(count) 份未处理的审校清单（旧结论作废），再按章重新审一遍；正文不受影响。" }
    static let riskStructureReview = "结构编辑只审查父级继承、因果、兑现和状态；只生成问题清单，按问题修复后才产出可接受的文档候选。"

    // MARK: - Run progress

    static let runProgressTitle = "运行进度"
    static let runProgressNone = "暂无运行"
    static let runInProgressHint = "正在生成，可关闭本页，进度会保留。"
    static let runCancel = "取消运行"
    static let runCancelConfirmTitle = "取消这次运行？"
    static let runCancelled = "已请求取消"
    static let runWarnings = "退路提示"
    static func runCallsProgress(_ used: Int, _ max: Int?) -> String {
        if let max { return "调用 \(used)/\(max)" }
        return "调用 \(used)"
    }
    static func runCacheSummary(read: Int, rate: String) -> String { "缓存命中 \(read.formatted()) token（\(rate)）" }
    static func runCacheCreation(_ count: Int) -> String { "写入 \(count.formatted())" }
    static func callRow(index: Int, step: String, input: Int, output: Int, ms: Int64) -> String {
        "#\(index) \(step) · in \(input.formatted()) / out \(output.formatted()) · \(ms)ms"
    }
    static let runWaitingAccept = "候选已生成，去任务页确认"
    static let runFailedHint = "已按调用预算停止；正文未改变"

    // MARK: - Task page (candidates fused with the operation log)

    static let taskTitle = "任务"
    static let taskEmptyTitle = "还没有任务"
    static let taskEmptyBody = "在写作页对单元发起生成后，任务与待确认结果会出现在这里。"
    static let taskNothingPending = "当前没有待确认的结果。"
    static func taskSectionNeedsAction(_ count: Int) -> String { "待确认 · \(count)" }
    static func taskSectionRunning(_ count: Int) -> String { "进行中 · \(count)" }
    static func taskSectionHistory(_ count: Int) -> String { "操作记录 · \(count)" }
    static let taskOpenUnit = "打开单元"
    static let taskUnitContext = "单元合同与上下文"
    static let taskParentSeed = "父级卷纲种子（不可改写的上层合同）"
    static let taskGateSummary = "门禁与连续性"
    static let taskViewOutlineSource = "查看细纲源（YAML）"
    static let taskFoldedReviewHint = "本单元的审校清单已折叠进这次修复：落盘后随正文版本归档。"
    static func taskReviewExpandFindings(_ count: Int) -> String { "问题清单 · \(count) 条" }
    static func taskReviewSubmitSummary(issues: Int, chapters: Int) -> String {
        issues == 0 ? "先勾选要修的问题" : "将修复 \(issues) 条 · 涉及 \(chapters) 章"
    }
    static func taskUnitPending(_ count: Int) -> String { "本单元有 \(count) 项待确认" }
    static let taskGoConfirm = "去确认"

    // MARK: - Candidates

    static let candidatesTitle = "待审候选"
    static let candidatesEmptyTitle = "没有待审候选"
    static let candidatesEmptyBody = "生成任务完成后，候选会出现在这里；接受后才会写盘。"
    static let candidateAccept = "接受"
    static let candidateAcceptRevise = "确认落盘"
    static let candidateReject = "拒绝"
    static let candidateLoadContent = "展开内容"
    static let candidateAccepted = "已接受候选"
    static let candidateRepair = "按问题修复"
    static let candidateStructureNote = "结构编辑只审查父级继承、因果、兑现和状态。按问题修复会生成新的文档候选，接受后才会写回 Markdown。"
    static let candidateReviseNote = "下面是相对现有正文的改动。确认无误后点「确认落盘」覆盖正文；落盘后旧正文清单按版本归档，需要时按新正文重新审校。"
    static let candidateRefineNote = "下面是相对原稿的改动。接受后才会覆盖正文；若几乎没改、或改得像重写，候选里会有提示。"
    static let candidateFinalizeNote = "逐章核对实际正文；只有作者接受后，以下结果才进入连续性账本。"
    static func candidateUnverified(_ count: Int) -> String {
        "有 \(count) 条状态迁移的回证在本章正文里找不到（下面标出）。接受时会记入定稿日志；请对照正文确认 from → to 是否成立，不确定就先不接受。"
    }
    static let candidatePlantActual = "实际埋下："
    static let candidatePayoffActual = "实际回收："
    static let candidatePlanVolumeNote = "只写当前卷纲，并播种索引中尚不存在的单元；总纲/设定/人物/正文不变。"
    static func candidatePlanFiles(_ count: Int) -> String { "接受后写入 \(count) 个文件" }
    static func candidateSettingsCount(_ count: Int) -> String { "设定 · \(count) 项" }
    static let candidateCastSection = "人物"
    static let candidatePreserved = "保留不覆盖："
    static let candidatePreviewSection = "变更预览（相对当前 canon）"
    static let candidateOverwriteSection = "将覆盖的文件（已排除保留项）"
    static let candidateOutlineSection = "总纲"
    static let candidateVolumeSection = "卷纲"
    static let forceAcceptTitle = "定稿检查仍有阻断"
    static let forceAcceptMessage = "强制接受后账本照样回写，单元标为已定稿。继续吗？"
    static let forceAcceptAction = "强制接受"
    static let rejectReviseTitle = "丢弃这次修复？"
    static let rejectReviseMessage = "正文不会被改动。问题清单仍在审校页，可以调整勾选后重新自动修复。"
    static let rejectReviewTitle = "丢弃这份审校清单？"
    static let rejectReviewMessage = "问题清单会被作废，之后需要自己重新点「审校」才能再拿到结论。"
    static let rejectReasonTitle = "拒绝原因（可选）"
    static let rejectReasonHint = "用于回看为什么要重做"
    static let rejectConfirm = "丢弃"
    static let rejectReviseDone = "已丢弃这次修复，正文未改变；可在审校页调整勾选后重新自动修复"
    static let rejectReviewDone = "已丢弃这份审校清单；重新点「审校」才会生成新结论"
    static func candidateUnits(_ count: Int) -> String { "\(count) 个单元" }
    static let candidateStateDelta = "状态迁移"

    // Diff
    static func diffChangedChapters(_ count: Int) -> String { "\(count) 章有改动" }
    static func diffAdded(_ count: Int) -> String { "+\(count.formatted()) 字" }
    static func diffRemoved(_ count: Int) -> String { "−\(count.formatted()) 字" }
    static func diffShowUnchanged(_ count: Int) -> String { "显示未改动的 \(count) 章" }
    static let diffNoChange = "修订没有产生任何改动。这份候选与现有正文逐字相同，接受不会改变正文。"
    static let diffChangedFlag = "已改"
    static let diffUnchangedFlag = "未改"
    static let diffStatusNew = "新增"
    static let diffStatusChanged = "修改"
    static let diffStatusSame = "未变"
    static let diffStatusKept = "保留（不写）"
    static let diffKeptNote = "该文件在「接受时保留」中，候选内容不会写入。"
    static let diffSameNote = "与当前版本逐字相同。"
    static func diffStatChanged(_ count: Int) -> String { "\(count) 个修改" }
    static func diffStatNew(_ count: Int) -> String { "\(count) 个新增" }
    static func diffStatSame(_ count: Int) -> String { "\(count) 个未变" }
    static func diffStatKept(_ count: Int) -> String { "\(count) 个保留不写" }

    // MARK: - Review

    static let reviewTitle = "审校与定稿"
    static let reviewIntro = "审校只出问题清单，出口只有两个：勾选后「自动修复」——确认落盘才写正文；或「拒绝」——丢弃清单。定稿是写作页的独立步骤。"
    static func reviewBlockingTotal(_ count: Int) -> String { "\(count) 阻断" }
    static func reviewFindingsTotal(_ count: Int) -> String { "\(count) 总问题" }
    static let reviewRulesTitle = "审校规则"
    static let reviewRuleDefault = "默认注入：每次审校自动带上，不需勾选"
    static let reviewRuleLocal = "本地硬门：程序正则表，结果直接出现在下面"
    static func reviewRuleSelectable(_ count: Int) -> String { "题材/技法参考：\(count) 篇可选，发起审校时勾选" }
    static func continuityEstablished(lastUnit: String, afterCh: Int?, cast: Int, loops: Int) -> String {
        var text = "连续性状态：\(lastUnit) 之后"
        if let afterCh, afterCh > 0 { text += " · 已定稿到第 \(afterCh) 章" }
        text += " · \(cast) 人 · \(loops) 条未回收伏笔"
        return text
    }
    static let continuityMissing = "连续性状态未建立：定稿第一个单元后自动生成，写作与审校都会读它。"
    static let reviewEmptyTitle = "全书没有待处理审校问题"
    static let reviewEmptyBody = "到写作页对某个单元点「审校」；结果会按单元分组出现在这里。清单处理完后，回写作页点「定稿」回写账本。"
    static func reviewRetryUnit(_ unitID: String) -> String { "重新审校 \(unitID)" }
    static let reviewLatestFailed = "最近一次审校失败"
    static let reviewTruncatedHint = "输出被截断。按章重审：每章一次调用，不会撞输出预算。"
    static let reviewFailedHint = "已按调用预算停止，可修复后重试；正文未改变。"
    static let reviewRetryTruncated = "重新审校（按章）"
    static func reviewGroupMeta(rows: Int, chapters: Int, lists: Int) -> String { "\(rows) 条 · \(chapters) 章 · \(lists) 份清单" }
    static let reviewAutoFix = "自动修复"
    static let reviewFixRunning = "正在生成修复…"
    static let reviewFixPending = "修复待确认落盘"
    static let reviewFixPendingTitle = "本次修复 · 待确认落盘"
    static let reviewFixReject = "拒绝这次修复"
    static let reviewFixNote = "下面是相对现有正文的改动。确认无误再落盘：正文更新后这份清单归档到历史，需要时按新正文重新审校。不认可这次改动就拒绝，正文不变。"
    static let reviewFixRunningNote = "生成期间这一组只显示这一步。完成后这里直接变成 diff，确认落盘才写正文。"
    static func reviewFixAddressed(_ count: Int) -> String { "本次针对的 \(count) 条问题" }
    static func reviewFixSummary(addressed: Int, remaining: Int) -> String {
        var parts: [String] = []
        if addressed > 0 { parts.append("针对 \(addressed) 条") }
        if remaining > 0 { parts.append("另有 \(remaining) 条未勾选，落盘后随清单归档") }
        return parts.isEmpty ? "落盘后这份清单归档到历史" : parts.joined(separator: " · ")
    }
    static let reviewSelectAll = "全选"
    static let reviewDeselectAll = "取消全选"
    static let reviewSelectChapter = "全选本章"
    static let reviewDeselectChapter = "取消本章全选"
    static func reviewSelectable(fixable: Int, picked: Int) -> String { "可修 \(fixable) 条 · 已选 \(picked)" }
    static func reviewUnfinishedCount(_ count: Int) -> String { "\(count) 条审校不完整，不参与勾选" }
    static func reviewUnitLevelCount(_ count: Int) -> String { "\(count) 条不属于正文，需回规划/状态层处理" }
    static let reviewUnfinishedTitle = "审校不完整"
    static let reviewUnfinishedBody = "有章节的输出被预算截断或调用失败，剩下的结论覆盖不全。先重新审校该单元，再决定修还是拒绝。"
    static let reviewRejectedTitle = "上一次自动修复已丢弃"
    static let reviewRejectedNoReason = "未填写原因"
    static let reviewRejectedHint = "正文未改变。调整下面的勾选后再点「自动修复」。"
    static let reviewSourceLocal = "本地硬门"
    static let reviewSourceAI = "AI 审校"
    static let reviewWholeUnit = "整单元"
    static let reviewSeverityBlocking = "阻断"
    static let reviewSeverityAdvisory = "建议"
    static let reviewSeverityUnfinished = "不完整"
    static let reviewSuggestionPrefix = "建议："
    static let reviewUnitLevelNote = "这条不属于任何一章正文，逐章修订改不了它：请按建议回规划/状态层处理（人物状态过期 → 在该单元细纲的 state_deltas 里写显式终值）。"
    static let reviewPickHint = "勾选后按这条问题做一次定向修订"
    static let reviewPickUnfinishedHint = "「这一章没审完」的标记，不是正文问题：先重新审校"
    static let reviewPickUnitLevelHint = "整单元结论，没有对应章节：改正文修不了，需回规划/状态层处理"
    static let reviewCopyFinding = "复制"
    static let reviewAllChapters = "全部章节"
    static let reviewChapterFilter = "章节筛选"
    static func reviewHits(_ count: Int) -> String { "×\(count)" }
    static let reviewHitsHint = "多轮审校独立检出同一处，可复现性高"
    static let verdictClean = "无待修问题"
    static let verdictBlocking = "有阻断"
    static let verdictUnfinished = "审校不完整"
    static let reviewHistoryTitle = "审校记录"
    static let reviewArchiveTitle = "历史审校清单"
    static func reviewArchiveCount(_ count: Int) -> String { "\(count) 份，只读" }
    static let reviewArchiveNote = "清单只有两个出口：勾选后自动修复，或拒绝丢弃。历史里的「已归档」= 这次修订已把它勾选的问题改掉。"
    static let archiveAccepted = "已归档"
    static let archiveRejected = "已拒绝"
    static let archiveExpired = "已作废"
    static func reviewArchiveFindings(total: Int, blocking: Int) -> String { "\(total) 条（阻断 \(blocking)）" }
    static func reviewRejectUnitTitle(_ unitID: String) -> String { "丢弃 \(unitID) 的审校清单？" }
    static func reviewRejectUnitMessage(_ count: Int) -> String { "\(count) 份清单会被作废，正文不受影响。之后需要自己重新点「审校」才能再拿到结论。" }
    static func reviewRetryTitle(_ unitID: String) -> String { "重新审校 \(unitID)？" }
    static func reviewRetryMessage(findings: Int, lists: Int) -> String {
        "该单元还有 \(findings) 条未处理的审校结论（\(lists) 份清单），重新审校会先把它们归档作废，再按章重审一遍。如果旧结论还要用，请先勾选「自动修复」，或确认后丢弃。"
    }
    static let reviewReviseStarted = "修订候选已生成：该单元的清单区已换成 diff，确认落盘后写正文"
    static let reviewReviseFailed = "自动修复失败，正文未改变"
    static let reviewRulesDocID = "25-review-checklist"

    // MARK: - Outline

    static let outlineTitle = "卷纲与结构"
    static let outlineIntro = "上层锁定不可逆决策，下层在合同内细化。简单修正直接编辑，复杂修改走候选重写。"
    static let outlineBookTab = "全书总纲"
    static let outlineBookHead = "总纲与分卷索引"
    static func outlineVolumeHead(_ volume: Int) -> String { "第 \(volume) 卷与单元索引" }
    static let outlineStructured = "结构化 Markdown"
    static let outlineLegacy = "旧格式 · 需 Agent 批量迁移"
    static let outlineLegacyNote = "这本书是旧格式大纲，不进入新的生产与编辑链路。迁移前原文和正文保持不变。"
    static let outlineEmptyBook = "暂无全书总纲。"
    static let outlineEmptyVolume = "总纲已就绪。生成当前卷纲后，将在这里展示卷级节拍与单元因果卡。"
    static func outlineGenerateVolume(_ volume: Int) -> String { "生成第 \(volume) 卷卷纲" }
    static let outlineRewriteVolume = "重写卷纲"
    static let outlineRawEdit = "原始 MD"
    static let outlineRawNote = "高级模式：保存的是同一份 Markdown。保存前会完整解析校验。"
    static let outlineSaveRaw = "保存 MD"
    static let outlineStructureReview = "结构审校"
    static let outlineWholeStructure = "全书结构"
    static let outlineVolumeStructure = "本卷结构"
    static let outlineThreadsSection = "读者承诺与故事线"
    static let outlineArcsSection = "分卷契约"
    static let outlineBeatsSection = "卷级节拍"
    static let outlineSeedsSection = "单元索引"
    static let outlineSeedsHint = "每个单元是一个因果闭环；时间是索引的合同字段。"
    static let outlineHandoff = "卷末交接"
    static let outlineDiagnostics = "结构错误"
    static func outlineDiagnosticLine(_ line: Int, path: String, message: String) -> String { "第 \(line) 行 · \(path)：\(message)" }
    static let outlineEditField = "编辑字段"
    static let outlineEditFieldNote = "保存时会重新解析整份 Markdown 并校验父子合同；不会重排其他文字或注释。"
    static let outlineFieldSaved = "已保存字段，Markdown 其他内容保持不变"
    static let outlineSeedPending = "待播种"
    static let outlineSeedClockPending = "待锚定"
    static func outlineSeedClock(start: String, end: String, gap: String) -> String { "故事第 \(start)–\(end) 日 · 间隔≥\(gap)日" }
    static let outlineSeedObjective = "目标"
    static let outlineSeedChoice = "选择"
    static let outlineSeedOutcome = "主结果"
    static let outlineSeedClock = "剧情时钟"
    static let outlineSeedObstacle = "阻碍"
    static let outlineSeedAttempt = "尝试与缺口"
    static let outlineSeedCost = "代价"
    static let outlineSeedStateChanges = "状态迁移"
    static let outlineSeedNextHook = "下一钩"
    static let outlineSeedDeadline = "关键死线"
    static let outlineSeedEntryCause = "前置原因"
    static let outlineArcEntry = "开卷"
    static let outlineArcExit = "卷末"
    static let outlineArcDeliverables = "结果兑现"
    static let outlineArcOpenQuestions = "保留问题"
    static let outlineOpenUnit = "进入单元生产"
    static let outlineNextOutline = "生成细纲"
    static let outlineNextWrite = "写正文"
    static let outlineNextReview = "审校正文"
    static let outlineNextFinalize = "定稿"
    static let outlineNextView = "查看定稿"
    static let outlineNextProduce = "进入生产"
    static let outlineNextPending = "待生成细纲"
    static let outlineSelectDocument = "选择大纲文档"
    static let outlineEditDescription = "编辑描述"
    static let outlineEditPurpose = "编辑结构职责"
    static let outlineEditOutcome = "编辑主结果"
    static func outlineFieldLabel(_ key: String) -> String {
        switch key {
        case "title": "标题"
        case "logline": "一句话故事"
        case "purpose": "结构职责"
        case "goal", "dramatic_question": "卷级问题"
        case "chapter_range": "章范围"
        case "central_conflict": "核心冲突"
        case "stakes": "失败代价"
        case "entry_state": "开卷状态"
        case "exit_state": "卷末状态"
        case "description": "描述"
        case "kind": "类型"
        case "opening_state": "起点"
        case "ending_state": "终点"
        case "thread_moves": "故事线迁移"
        case "deliverables": "结果兑现"
        case "open_questions": "保留问题"
        case "next_volume_entry": "下一卷入口"
        case "cast": "本卷人物"
        case "objective": "目标"
        case "obstacle": "阻碍"
        case "attempt": "尝试"
        case "setback": "方法缺口"
        case "choice": "关键选择"
        case "cost": "代价"
        case "outcome": "不可逆结果"
        case "entry_cause": "前置原因"
        case "exit_result": "离场结果"
        case "state_changes": "状态迁移"
        case "thread_ids": "故事线"
        case "event_ids": "关联事件"
        case "plant_ids": "埋入伏笔"
        case "payoff_ids": "回收伏笔"
        case "next_hook": "下一钩"
        case "event": "事件"
        case "cause": "原因"
        case "effect": "影响"
        case "chapter_window": "落点范围"
        case "clock_start_day": "开场日"
        case "clock_end_day": "结束日"
        case "clock_gap_days": "最小间隔"
        case "clock_deadline": "关键死线"
        case "timeline_id": "时间线"
        default: key
        }
    }

    // MARK: - Bible

    static let bibleTitle = "设定与正史"
    static let bibleIntro = "故事约束、作者底牌和读者事实分层管理。"
    static let bibleGroupDerived = "总纲派生"
    static let bibleGroupConstraints = "生产约束"
    static let bibleGroupCast = "人物与关系"
    static let bibleGroupSecrets = "作者底牌"
    static let bibleGroupContinuity = "连续性账本"
    static func bibleCoverage(_ built: Int, _ total: Int) -> String { "\(built)/\(total) 设定与状态已建立" }
    static func bibleCastCount(_ count: Int) -> String { "\(count) 人物" }
    static func bibleSummaryCount(_ count: Int) -> String { "\(count) 卷摘要" }
    static func bibleCommitCount(_ count: Int) -> String { "\(count) 定稿账本" }
    static func bibleMissing(_ count: Int) -> String { "缺 \(count) 项" }
    static func bibleScaffold(_ count: Int) -> String { "补齐 \(count) 项模板" }
    static let bibleScaffoldDone = "已补齐缺失设定模板（已有内容未覆盖）"
    static let bibleNotCreated = "未创建"
    static let bibleMissingDocTitle = "这项设定尚未创建"
    static let bibleCreateDoc = "新建并编辑"
    static let bibleEmptyDoc = "文件为空。"
    static let bibleNoContent = "还没有内容"
    static let bibleNoContentBody = "可以新建模板，或在桌面端批准立项候选后写入。"
    static let bibleViewSource = "查看源文件"
    static let bibleEditCurrent = "编辑当前"
    static let bibleSaveChanges = "保存更改"
    static let bibleCancelEdit = "取消编辑"
    static let bibleSecretTitle = "作者底牌"
    static let bibleSecretNote = "这些内容不会进入正文或审校上下文。展开前确认没有读者在场。"
    static let bibleSecretReveal = "显示作者底牌"
    static let bibleOptimize = "重写设定"
    static let bibleOptimizeCast = "重写人物卡"
    static let bibleManualOnly = "当前项请手工编辑；连续性账本与锁词不提供 AI 优化"
    static let bibleCastEmpty = "人物卡会在立项候选接受后进入这里。"
    static let bibleCandidateBadge = "候选"
    static let bibleCandidatePending = "候选 · 待晋升"
    static let biblePromote = "晋升 canon"
    static let biblePromoting = "晋升中…"
    static func biblePromoted(_ name: String) -> String { "\(name) 已晋升 canon" }
    static let biblePromoteNeedIdentity = "请先登记该人物的初始职务身份，再晋升 canon"
    static let bibleCastUsage = "用于：细纲 · 正文 · 审校（压缩快照）· 定稿回写当前状态"
    static let bibleIdentityTitle = "职务身份时间线"
    static let bibleIdentityNote = "单元内部身份恒定，转换只在单元边界。规划接受即生效；定稿只确认实际状态。"
    static let bibleIdentityMissing = "尚未建立职务身份。正式写作前必须登记初始职务与机构。"
    static let bibleIdentityAdd = "登记初始身份"
    static let bibleIdentityAppend = "新增身份段"
    static let bibleIdentityFix = "修正"
    static let bibleIdentityClose = "结束当前段"
    static let bibleIdentityRewrite = "候选重写"
    static let bibleIdentityUntil = "至今"
    static let bibleIdentitySaved = "职务身份已更新；后续写作与审校将使用生效段"
    static let identityActionAppend = "规划身份变更"
    static let identityActionUpdate = "修正身份段"
    static let identityActionClose = "结束当前身份段"
    static let identityRoleLabel = "职务 / 公开身份"
    static let identityRoleHint = "例如：重案队试用"
    static let identityOrgLabel = "所属机构"
    static let identityOrgHint = "例如：青江分局"
    static let identityFromLabel = "起始单元"
    static let identityToLabel = "结束于单元"
    static let identityNoteLabel = "变更原因 / 依据"
    static let identityNoteHint = "例如：调令生效"
    static let identityDialogNote = "身份只从指定单元开始生效；不会改写人物稳定人格或自动替正文认定变化。"
    static func identityImpact(from: String, role: String, org: String) -> String {
        "将从 \(from) 起使用「\(role.isEmpty ? "新职务" : role) · \(org.isEmpty ? "新机构" : org)」，并结束此前开放身份段。"
    }
    static let identityValidateSave = "校验并保存"
    static let identityValidating = "校验并保存中…"
    static let bibleStateEmpty = "尚未写入状态字段。"
    static let bibleLockedSection = "按卷锁词"
    static let bibleLockedHint = "到期卷之前正文禁用"
    static let bibleLockedEmpty = "暂无锁词。"
    static let bibleLockedTerm = "锁词"
    static let bibleLockedUntil = "解锁卷"
    static let bibleComplianceSection = "永久合规红线"
    static let bibleComplianceEmpty = "暂无红线。"
    static let bibleAliasSection = "别名映射"
    static let bibleAliasPublic = "公开称呼"
    static let bibleAliasReal = "真实指向"
    static let gatePass = "✓ 通过"
    static let gateFail = "✗ 未过"
    static let gateUnknown = "○ 未知"
    static func summaryDocLabel(_ name: String) -> String { "卷摘要 · \(name)" }
    static func commitDocLabel(_ name: String) -> String { "定稿账本 · \(name)" }
    static let summaryDocUsage = "正文 · 审校 · 定稿回写"
    static let commitDocUsage = "回溯排查（不进上下文）"
    static let summaryDocDescription = "逐章事件、状态变化、伏笔与钩子；续写只读上一章摘要"
    static let commitDocDescription = "单元定稿执行日志：门禁、字数、锁词、修订；不默认给模型"
    static func usageLine(_ usage: String) -> String { "用于：\(usage)" }

    // Bible doc definitions: label / description / usage
    static let docBibleLabel = "作者意图与读者承诺"
    static let docBibleDescription = "读者承诺、主角目标、故事引擎；重写携带总纲作依据"
    static let docBibleUsage = "重写带总纲"
    static let docWorldLabel = "世界观与力量规则"
    static let docWorldDescription = "力量/代价、势力地图、禁忌；重写携带总纲"
    static let docGoldfingerLabel = "金手指与资源"
    static let docGoldfingerDescription = "能力、代价、上限；重写携带总纲"
    static let docForeshadowLabel = "伏笔登记表"
    static let docForeshadowDescription = "伏笔的埋与收；总纲定卷、卷纲定单元、细纲定章"
    static let docForeshadowUsage = "卷纲 · 细纲 · 正文 · 审校 · 重写带总纲"
    static let docPlatformLabel = "平台与篇幅定位"
    static let docPlatformDescription = "平台、目标读者、更新与开篇承诺"
    static let docPlatformUsage = "可独立优化"
    static let docRulesLabel = "写作规则"
    static let docRulesDescription = "单元与单章生产约束"
    static let docRulesUsage = "正文 · 审校 · 可独立优化"
    static let docStyleLabel = "风格指纹"
    static let docStyleDescription = "POV、句段、对话、感官与反模式"
    static let docStyleUsage = "正文 · 审校 · 可独立优化"
    static let docLockedLabel = "锁词与合规红线"
    static let docLockedDescription = "按卷解锁的真名/真相词与永久禁词"
    static let docLockedUsage = "正文约束 · 定稿扫描 · 仅手工"
    static let docLoreLabel = "作者底牌与终局真相"
    static let docLoreDescription = "未来反转、真实规则、角色秘密；写正文禁止加载"
    static let docLoreUsage = "重写带总纲（正文/审校禁读）"
    static let docStateLabel = "连续性状态账本"
    static let docStateDescription = "按单元段：位置、伤势、资源、目标、知情范围与伏笔状态；定稿时自动追加"
    static let docStateUsage = "正文 · 审校 · 细纲（应用自动读取）"
    static let docRunStateLabel = "书籍运行状态"
    static let docRunStateDescription = "当前阶段、题材、时间系统与已提交章节游标"
    static let docRunStateUsage = "流程控制 · 定稿回写"

    // novel-state field labels
    static func stateFieldLabel(_ key: String) -> String? {
        switch key {
        case "title": "书名"
        case "genre": "题材"
        case "subgenre": "子题材"
        case "stage": "当前阶段"
        case "last_committed_ch": "已定稿章"
        case "next_action": "下一动作"
        case "active_unit": "活动单元"
        case "time_system": "时间系统"
        case "qc_profile": "质检档案"
        case "continuation_mode": "续写模式"
        default: nil
        }
    }
    static func stageText(_ stage: String) -> String {
        switch stage {
        case "planning": "立项中"
        case "outlining": "细纲中"
        case "units": "单元生产"
        case "writing": "写作中"
        case "done": "完结"
        default: stage
        }
    }

    // Cast roles
    static func castRole(_ role: String) -> String {
        switch role.lowercased() {
        case "protagonist", "hero", "main": "主角"
        case "antagonist", "volume_antagonist", "villain": "卷反派"
        default: "常驻"
        }
    }

    // Relations
    static let relationsTitle = "人物关系图"
    static let relationsNote = "实线为正式「## 关系」表；虚线为关系锚点 / 与主角关系等文字关系。"
    static let relationsNoProtagonist = "未识别 protagonist 主角，暂按人物总览排列。"
    static func relationsCount(_ relations: Int, _ cast: Int) -> String { "\(relations) 条关系 · \(cast) 人物" }
    static let relationsEmpty = "还没有人物卡。"
    static let relationsPickHint = "点击人物节点查看关系。"
    static let relationsNoneHint = "还没有关系。请在人物卡添加「## 关系」表，或写「关系锚点 / 与主角关系」。"
    static let relationsFormal = "正式关系表"
    static let relationsInformal = "文字关系"
    static let relationsOpenCard = "打开人物卡"
    static let relationsLegendFormal = "正式关系表"
    static let relationsLegendInformal = "文字关系"

    // MARK: - Knowledge page

    static let knowledgeTitle = "知识库"
    static func knowledgeCount(_ count: Int) -> String { "\(count) 篇" }
    static let knowledgeIntro = "程序规则、默认注入、用户选择与作者文档的分类知识。"
    static let knowledgeSearchHint = "搜索标题 / 摘要"
    static let knowledgeCategoryAll = "全部"
    static let knowledgeCategoryGeneral = "通用方法"
    static let knowledgeCategoryGenre = "题材专项"
    static let knowledgeCategoryCustom = "作者自建"
    static let knowledgeGenreAll = "全部题材"
    static let knowledgeGenreCommon = "公共（不限题材）"
    static func knowledgeGenreExclusive(_ genre: String) -> String { "专属 · \(genre)" }
    static let knowledgeDefaultBadge = "默认注入"
    static let knowledgeAuthorBadge = "作者文档"
    static let knowledgeBuiltinBadge = "内置"
    static let knowledgeEditedBadge = "本地已编辑"
    static let knowledgeCustomBadge = "作者自建"
    static let knowledgeCategoryMethod = "方法"
    static let knowledgeCategoryGenreShort = "题材"
    static let knowledgeCategoryCustomShort = "自建"
    static let knowledgeEditedShort = "已编辑"
    static let knowledgePersonalShort = "个人"
    static let knowledgeNoMatch = "没有匹配的文章。"
    static let knowledgePickTitle = "选择一篇文章"
    static let knowledgePickBody = "在列表中选择要查看或编辑的知识文章。"
    static let knowledgeStagesLabel = "适用写作步骤"
    static let knowledgeStagesUntagged = "未标记 · 所有步骤可选"
    static let knowledgeStagesUntaggedShort = "未标记"
    static let knowledgeGenresLabel = "适用题材"
    static let knowledgeGenresCommon = "公共 · 所有题材可用"
    static let knowledgeGenresRestricted = "只在上述题材的书里出现"
    static let knowledgeSubgenreLabel = "子题材限定"
    static let knowledgeSubgenreHint = "留空=不限；如 刑侦探案"
    static func knowledgeSubgenreOnly(_ joined: String) -> String { "仅 \(joined) 子题材" }
    static let knowledgeGenreSummaryCommon = "公共"
    static func knowledgeGenreOnly(_ joined: String) -> String { "仅 \(joined)" }
    static let knowledgeEdit = "编辑"
    static let knowledgeSaveCopy = "保存副本"
    static let knowledgeRestore = "恢复内置"
    static let knowledgeRestoring = "恢复中…"
    static let knowledgeRestoreTitle = "恢复为内置原文？"
    static let knowledgeRestoreMessage = "你在本地对这篇文章的修改将被移除。"
    static let knowledgeSavedNote = "已保存到本地资料库；内置原文仍保留"
    static let knowledgeRestoredNote = "已恢复为内置原文"
    static let knowledgeDiscardTitle = "放弃未保存的修改？"
    static let knowledgeDiscardAction = "放弃"
    static func knowledgePath(_ id: String, customized: Bool) -> String {
        (customized ? "本地副本覆盖 · " : "") + "knowledge/\(id).md"
    }

    /// Stage vocabulary shared with the backend knowledge.Stages.
    static let knowledgeStages: [(id: String, label: String)] = [
        ("plan", "立项"), ("volume", "卷纲"), ("outline", "单元细纲"),
        ("write", "单元正文"), ("review", "审校"), ("entity", "设定重写"),
    ]
    static func knowledgeStageLabel(_ id: String) -> String {
        knowledgeStages.first { $0.id == id }?.label ?? id
    }
    /// Genre families shared with backend GenreFamiliesList.
    static let knowledgeGenres = ["玄幻", "仙侠", "都市", "悬疑", "现代言情", "古代言情", "仕途扫黑", "系统穿越"]

    // MARK: - Providers (settings)

    static let settingsTitle = "设置"
    static let providersSection = "模型提供商"
    static let keyNote = "密钥只保存在本机，不会出现在页面或日志中。模型调用只连接你配置的服务。"
    static let addProvider = "添加提供商"
    static let editProvider = "编辑提供商"
    static let settingsLoadFailed = "设置读取失败"
    static let saveFailed = "保存失败"
    static let providerFieldSection = "提供商"
    static let providerProtocolLabel = "协议"
    static let providerName = "名称"
    static let providerBaseUrlLabel = "Base URL"
    static let providerAPIKey = "API Key"
    static let providerAPIKeyKeep = "API Key（留空保持不变）"
    static let providerModelsHint = "模型名称，多个用逗号分隔"
    static let providerDialectLabel = "默认方言"
    static let providerDialectAuto = "自动"
    static let providerModelsSection = "模型列表"
    static let providerFetchModels = "刷新模型列表"
    static let providerFetchModelsDone = "模型列表已刷新"
    static let providerAddModel = "添加模型"
    static let providerAddModelHint = "手工添加模型名"
    static let providerNoModels = "无启用模型"
    static let providerDeleteTitle = "删除提供商？"
    static let providerDeleteMessage = "该提供商的模型将不可用；密钥随之删除。"
    static let providerSaved = "已保存提供商"
    static let providerPresetsSection = "从预设开始"
    static let providerProtocolOpenAI = "OpenAI Chat Completions"
    static let providerProtocolResponses = "OpenAI Responses"
    static let providerProtocolAnthropic = "Anthropic Messages"
    static let providerProtocolMock = "Mock（本地测试）"

    // Models & test
    static let modelsSection = "可用模型"
    static let modelsEmpty = "还没有可用模型。先添加提供商。"
    static let modelTest = "测试"
    static let modelTesting = "测试中…"
    static func modelTestOK(_ ms: Int64) -> String { "ok · \(ms)ms" }
    static let modelTestFailed = "failed"

    // Stage assignments
    static let stagesSection = "创作阶段模型"
    static let stagesSave = "保存阶段模型"
    static let stagesSaved = "阶段模型已保存"
    static let stageUnset = "未设置"
    static let stageEffortLabel = "推理等级"
    static let stageEffortDefault = "默认"
    static func stageEffortOff(_ id: String) -> String { id == "off" ? "off（关闭推理）" : id }
    static let stagePlanLabel = "立项"
    static let stagePlanHint = "重写设定携带依据。建议推理强的模型，effort medium–high。"
    static let stageOutlineLabel = "细纲"
    static let stageOutlineHint = "单元合同的欲望/阻碍/选择与场面节拍，需要结构化输出与因果链。建议 effort medium。"
    static let stageWriteLabel = "写作"
    static let stageWriteHint = "正文 prose，文风与节奏优先；过长推理会稀释文气。建议文风好的模型，effort low–medium。"
    static let stageReviewLabel = "审校"
    static let stageReviewHint = "找 OOC、连续性、写穿与 AI 味，需要严格比对与挑错能力。建议 effort medium–high。"
    static let stageFinalizeLabel = "定稿"
    static let stageFinalizeHint = "只把正文读成结构化账本，不创作也不评判。建议便宜模型 + effort low；留空沿用「审校」。"
    static let stageLocalEditLabel = "局部编辑"
    static let stageLocalEditHint = "选区润色/改写，改动小、要求贴原文。轻量模型即可，effort low。"

    // Review settings
    static let reviewSettingsSection = "审校设置"
    static let reviewSettingsNote = "固定按章审校；每章携带同一事实基线、本章合同与上一章尾部。"
    static let reviewRoundsLabel = "审校轮数"
    static func reviewRoundsValue(_ count: Int) -> String { "\(count) 轮" }
    static let reviewRoundsHint = "每章独立审多少轮，结果取并集。单轮只能看到约 25% 的问题，3 轮约 75%。轮数只增加「输出」成本，输入靠前缀缓存几乎不变。"
    static let reviewConcurrencyLabel = "并发数"
    static let reviewConcurrencyHint = "按章审校时同时发出的调用数；并发越高越快，但更容易撞上模型侧的限流。"
    static let reviewSettingsSaved = "审校设置已保存；所有审校入口同时生效"

    // Model configs
    static let modelConfigSection = "模型参数"
    static let modelConfigNote = "配置每个模型的容量、采样与推理参数。留空表示使用默认值。"
    static let modelConfigReset = "重置内置"
    static let modelConfigResetDone = "已重置为内置目录"
    static let modelConfigSaved = "模型参数已保存"
    static let modelConfigAdd = "添加模型配置"
    static let modelConfigNameLabel = "模型名"
    static let modelConfigNameHint = "如 gpt-5.6"
    static let modelConfigCapacity = "容量"
    static let modelConfigContextWindow = "Context Window"
    static let modelConfigMaxOutput = "Max Output"
    static let modelConfigVision = "Vision（图片输入）"
    static let modelConfigSampling = "采样"
    static let modelConfigTemperature = "Temperature"
    static let modelConfigTopP = "Top P"
    static let modelConfigFrequencyPenalty = "Frequency Penalty"
    static let modelConfigPresencePenalty = "Presence Penalty"
    static let modelConfigStop = "Stop Sequences（逗号分隔）"
    static let modelConfigReasoning = "推理"
    static let modelConfigEfforts = "Available Efforts"
    static let modelConfigEffortsHint = "off, low, medium, high, xhigh"
    static let modelConfigDelete = "删除此模型配置"
    static let modelConfigPlaceholder = "留空使用默认"

    // MARK: - Settings: storage / sync

    static let libraryRow = "书籍目录"
    static let modelSettingsRow = "模型设置"
    static let iCloudDrive = "iCloud 云盘"
    static let thisDevice = "本机"
    static let sharedValue = "已共享"
    static let localOnlyValue = "仅本机"
    static let lastSync = "上次同步"
    static let syncing = "正在同步…"
    static let syncNow = "立即同步"
    static let syncSection = "iCloud 同步"
    static let syncFooter = "书籍以文件形式保存在 iCloud 云盘，iPhone 与 Mac 读写同一份。模型提供商、模型列表与阶段绑定会同步；API Key 只留在本机，每台设备各填一次。"
    static let syncDone = "模型设置已同步"
    static let libraryChange = "更改目录"
    static let libraryReset = "恢复默认"
    static let libraryChangeNote = "确认后会把当前目录下的所有书籍、知识文档移动到该位置。目标目录里已有同名书籍时会拒绝操作。留空表示恢复系统默认目录。"
    static let libraryPathLabel = "目录路径"
    static let libraryMoveConfirm = "确认并移动"
    static let libraryMoving = "移动中…"
    static func libraryMoved(_ count: Int) -> String { "已移动 \(count) 项到新目录" }
    static let libraryUpdated = "书籍目录已更新"
    static let libraryCustomNote = "当前使用自定义目录。"
    static let libraryDefaultNote = "当前使用系统默认目录（iCloud 云盘中的 DanmoNovel，未启用时为本机沙盒）。"
    static let privacySection = "数据与隐私"
    static let storageRow = "书稿存储"
    static let accountRow = "账户"
    static let noAccountValue = "无需注册"
    static let trackingRow = "跟踪"
    static let trackingNoneValue = "无"
    static let versionRow = "版本"
    static let versionDev = "开发版"

    // MARK: - Runs (logs)

    static let logsTitle = "模型日志"
    static let logsEmptyTitle = "暂无模型日志"
    static let logsEmptyBody = "模型调用、耗时与失败原因会记录在这里。在书籍的写作页发起生成任务后，日志会实时记录每次调用。"
    static let logsLoadFailed = "日志读取失败"
    static let modelUnknown = "未指定模型"
    static let logsSearchHint = "搜索书籍、单元、模型或错误"
    static let logsFilterBook = "书籍"
    static let logsFilterKind = "类型"
    static let logsFilterModel = "模型"
    static let logsFilterStatus = "状态"
    static let logsFilterAllBooks = "全部书籍"
    static let logsFilterAllKinds = "全部类型"
    static let logsFilterAllModels = "全部模型"
    static let logsFilterAllStatuses = "全部状态"
    static let logsFilterReset = "清空筛选"
    static let logsSummaryTasks = "任务"
    static let logsSummaryCalls = "LLM 调用"
    static let logsSummaryTokens = "输入 / 输出"
    static let logsSummaryCache = "缓存命中"
    static let logsSummaryDuration = "累计耗时"
    static let logsSummaryScope = "当前筛选"
    static let logsCacheNoData = "该模型未返回缓存数据"
    static func logsCacheShare(_ percent: Int) -> String { "占输入 Token \(percent)%" }
    static let logsNoMatchTitle = "没有匹配的调用记录"
    static let logsNoMatchBody = "调整筛选，或先在书籍中运行一次创作任务。"
    static let logsError = "错误"
    static func logsRunID(_ id: String) -> String { "任务 ID：\(id)" }
    static func logsBaseRevision(_ revision: String) -> String { "基础版本：\(revision.isEmpty ? "未指定" : revision)" }
    static func logsCandidate(_ id: String) -> String { "候选：\(id)" }
    static let logsContext = "上下文"
    static let logsCache = "缓存"
    static let logsCacheHint = "重试调用（step=repair）的命中率应接近 100%：repair 只追加在 payload 尾部，前缀不动。"
    static let logsCalls = "调用明细"
    static let logsNoCalls = "尚无模型调用记录。"
    static func logsTokens(input: Int, output: Int) -> String { "\(input.formatted()) / \(output.formatted())" }
    static func callsUsedOver(_ used: Int, _ max: Int?) -> String {
        if let max { return "\(used)/\(max)" }
        return "\(used)/—"
    }
    static func durationSeconds(_ ms: Int64) -> String { ms > 0 ? String(format: "%.1fs", Double(ms) / 1000) : "—" }
    static func callCacheText(read: Int, prompt: Int) -> String {
        guard read > 0 else { return "—" }
        let pct = Int((Double(read) / Double(max(1, prompt)) * 100).rounded())
        return "\(read.formatted()) (\(pct)%)"
    }
    static let runTargetCurrentVolume = "当前卷"
    static let runTargetWholeBook = "全书"

    static func calls(_ count: Int) -> String { "\(count) 次调用" }

    // MARK: - Core / transport

    static let coreStartFailed = "本地内核启动失败"
    static let iCloudUnavailable = "iCloud 不可用（未登录或该构建未带 iCloud 能力）"
    static let isolatedWorkspace = "独立工作区：不连接 iCloud"
    static let apiInvalidURL = "无效的本地 API 地址"
    static let apiInvalidPath = "无效的 API 路径"
    static let apiCoreNotReady = "本地工作区尚未就绪，请稍后重试。"

    static func apiHTTPError(_ status: Int) -> String { "本地服务返回错误（\(status)）。" }

    // MARK: - Domain vocabulary

    // Same table as Android's Models.kt and docs/mobile-copy.md. `reviewed` is
    // finished but is not 已定稿: collapsing the two made a reviewed unit read
    // as shipped.

    static let statusFinalized = "已定稿"
    static let statusReviewed = "已审校"
    static let statusDrafted = "待审校"
    static let statusAccepted = "待写正文"
    static let statusProposed = "待写细纲"

    /// Labels match `frontend/src/shared/utils/runLabels.ts`, so one run reads
    /// the same on the phone and in the desktop log. The list used to print the
    /// raw identifier (`write`).
    static func runKind(_ kind: String) -> String {
        switch kind {
        case "plan": "立项"
        case "idea": "创意扩写"
        case "outline": "细纲"
        case "write": "正文"
        case "review": "审校"
        case "revise": "修订"
        case "finalize": "定稿"
        case "rewrite_entity": "重写设定"
        case "structure_review": "结构审校"
        case "local_edit": "局部编辑"
        case "continue": "续写"
        case "expand": "扩写"
        case "polish": "润色"
        case "rewrite_selection": "选区改写"
        default: kind
        }
    }

    static let kindWriteRefine = "正文 · 原文优化"

    static func planMode(_ mode: String?) -> String {
        switch mode {
        case "reinit": "重新立项"
        case "volume": "卷纲"
        default: "立项"
        }
    }

    static func runKindLabel(_ kind: String, mode: String?) -> String {
        if kind == "plan" { return planMode(mode) }
        if kind == "write", mode == "refine" { return kindWriteRefine }
        return runKind(kind)
    }

    static func runStatus(_ status: String) -> String {
        switch status {
        case "pending": "排队"
        case "running": "运行中"
        case "waiting_accept": "待接受"
        case "succeeded": "成功"
        case "failed": "失败"
        case "cancelled": "已取消"
        case "rejected": "已拒绝"
        case "expired": "已过期"
        default: status
        }
    }

    /// Candidate archive status labels (history list).
    static func candidateStatus(_ status: String) -> String {
        switch status {
        case "accepted": archiveAccepted
        case "rejected": archiveRejected
        case "expired": archiveExpired
        default: status
        }
    }
}