/**
 * 书工作台的全部数据与动作。
 *
 * BookView.vue 只负责决定「什么时候、以什么布局展示」，不再各自复述一次运行、
 * 候选、账本的处理规则。任何在这里出现的东西都必须与布局无关——需要 onClick 里
 * 改导航的，通过传入的 `view` ref 表达意图，具体怎么呈现由视图自己解释。
 */
import { computed, onMounted, onUnmounted, ref, watch, type Ref } from 'vue'
import { useAppStore } from '@/shared/stores/app'
import { api } from '@/shared/api/client'
import type { Candidate, CallRecord, UnitInfo } from '@/shared/types'
import { cacheStat } from '@/shared/utils/tokenStats'
import { buildTaskItems } from '@/shared/utils/taskItems'

export type WorkspaceView = 'setup' | 'write' | 'outline' | 'bible' | 'tasks'

export function useBookWorkspace(opts: { bookId: Ref<string>; view: Ref<WorkspaceView> }) {
  const { bookId, view } = opts
  const store = useAppStore()

  const selectedUnit = ref<UnitInfo | null>(null)
  const editorPath = ref('')
  const editorContent = ref('')
  const saving = ref(false)
  const busy = ref(false)
  const modelId = ref('')
  const runDrawer = ref(false)
  /**
   * 任务页是唯一确认地：runs 与候选融合成一条时间线（待确认/进行中/操作记录）。
   * 立项工作台自带提案与立项候选的确认，所以那里不再重复列出 plan/idea。
   */
  const taskItems = computed(() => buildTaskItems(store.candidates, store.runs, store.activeRun))
  const pendingTaskCount = computed(() => taskItems.value.filter((i) => i.section === 'needsAction').length)
  function pendingTasksOf(unitId: string) {
    return taskItems.value.filter((i) => i.section === 'needsAction' && i.unitId === unitId)
  }
  /** 实时日志的缓存汇总：重试（step=repair）命中率应接近 100%。 */
  function runCacheOf(run: { calls?: CallRecord[] } | null | undefined) { return cacheStat(run?.calls) }

  const pollTimer = ref<number | null>(null)
  const editingProse = ref(false)
  const editingChapter = ref<number | null>(null)
  const proseBeforeEdit = ref('')
  const editingOutline = ref(false)
  const isProseUnit = computed(() => Boolean(selectedUnit.value?.hasProse && selectedUnit.value.prosePath === editorPath.value))
  const stageLabels: Record<string, string> = { idea: '创意扩写', plan: '立项', outline: '细纲', write: '写作', review: '审校', structure_review: '结构审校', finalize: '定稿', revise: '修订', local_edit: '局部编辑', rewrite_entity: '重写设定' }
  // 骨架响应不带 files；「有没有总纲」以服务端算好的 primary 为准
  // （plan/idea 即尚未立项）。面板文件按需加载后才用于渲染。
  const needsSetup = computed(() => store.primary?.kind === 'plan' || store.primary?.kind === 'idea')
  const hasBookOutline = computed(() => !needsSetup.value)
  // 立项工作台只服务「还没有总纲」的书：它是 idea loop 到一次结构化立项的入口。
  // 总纲一旦落盘，优化与重新立项都归大纲 Tab，避免两个页面做同一件事。
  const showSetupTab = computed(() => needsSetup.value)
  /** 创意提案是否已确认：提案段有内容才算，空模板不算。（面板文件加载后可用） */
  const hasIdeaProposal = computed(() => {
    const raw = store.currentBook?.files?.['outline/idea.md'] || ''
    const idx = raw.split('\n').findIndex((line) => line.trim() === '## 创意提案')
    if (idx < 0) return false
    return raw.split('\n').slice(idx + 1).join('\n').replace(/<!--[\s\S]*?-->/g, '').trim().length > 0
  })
  const hasVolumes = computed(() => (store.currentBook?.volumes?.length || 0) > 0)
  // 单元按卷分组；默认展开选中单元所在卷，其余卷可折叠。
  const collapsedVolumes = ref<Set<number>>(new Set())
  const volumeGroups = computed(() => {
    const units = store.currentBook?.units || []
    const groups: { volume: number; units: UnitInfo[]; reviewed: number; title: string }[] = []
    for (const u of units) {
      let g = groups.find((x) => x.volume === u.volume)
      if (!g) {
        const vol = store.currentBook?.volumes.find((v) => v.volume === u.volume)
        const title = vol?.raw?.split('\n').find((l) => /^#\s+/.test(l))?.replace(/^#\s+/, '') || ''
        g = { volume: u.volume, units: [], reviewed: 0, title }
        groups.push(g)
      }
      g.units.push(u)
      if (u.status === 'reviewed' || u.status === 'finalized') g.reviewed++
    }
    return groups.sort((a, b) => a.volume - b.volume)
  })
  function toggleVolume(v: number) {
    const s = new Set(collapsedVolumes.value)
    if (s.has(v)) s.delete(v)
    else s.add(v)
    collapsedVolumes.value = s
  }
  function isCollapsed(v: number) {
    return collapsedVolumes.value.has(v)
  }
  const primaryInfo = computed(() => {
    const p = store.primary
    if (!p) return { label: '继续创作', description: '选择下一步', target: '', stage: '写作' }
    const target = p.unitId || `第 ${p.volume} 卷`
    if (p.kind === 'idea') {
      return { label: '创意扩写', description: '先把 idea 扩成可立项的提案；提案接受后才进入结构化立项', target: '全书', stage: '创意扩写' }
    }
    if (p.kind === 'plan') {
      // 服务端对「无总纲」的书返回 plan(init)；有总纲时返回的是卷纲或单元步骤。
      if (needsSetup.value) {
        return { label: '立项', description: '以已确认的创意提案为依据，一次生成总纲、全部设定与人物（不含卷纲）', target, stage: '立项' }
      }
      return { label: `生成卷纲 · 第 ${p.volume || 1} 卷`, description: '根据总纲生成当前卷纲，并只播种缺失单元', target, stage: '卷纲' }
    }
    if (p.kind === 'outline') return { label: `生成 ${target} 细纲`, description: '第 1 步 · 补全单元写作合同；也可在单元栏随时重写细纲', target, stage: '细纲' }
    if (p.kind === 'write') return { label: `写 ${target} 正文`, description: '第 2 步 · 按合同生成候选稿；有正文后可在单元栏重写', target, stage: '写作' }
    if (p.kind === 'review') return { label: `审校 ${target}`, description: '第 3 步 · AI 审校；通过后在单元栏点「定稿」回写账本', target, stage: '审校' }
    if (p.kind === 'finalize') return { label: `定稿 ${target}`, description: '第 4 步 · 本地门禁与逐章账本核对；接受后统一提交', target, stage: '定稿' }
    return { label: '继续创作', description: '执行下一步', target, stage: stageLabels[p.kind] || '写作' }
  })

  onMounted(async () => {
    await store.loadLLM().catch(() => {})
    await reload()
    // 还没有总纲的书先落在立项工作台：那里才是「下一步」能解释清楚的地方。
    if (needsSetup.value) view.value = 'setup'
    await ensurePanelFiles()
  })
  onUnmounted(() => {
    if (pollTimer.value) clearInterval(pollTimer.value)
  })

  watch(selectedUnit, () => { editingProse.value = false; editingChapter.value = null; editingOutline.value = false; void openSelected() })
  watch(view, (next) => {
    if (next !== 'write') runDrawer.value = false
    void ensurePanelFiles()
  })
  watch(view, () => {
    void openSelected()
  })
  // 立项候选被接受后总纲才出现：此刻作者还停在一个已经不该存在的 Tab 上，
  // 必须在同一帧迁走，否则页面剩一片空白。
  watch(needsSetup, (needed) => {
    if (!needed && view.value === 'setup') view.value = 'outline'
  })

  /** 大纲/设定/立项面板需要文件；写作/审校/候选只用骨架。按需加载，幂等。 */
  async function ensurePanelFiles() {
    if (view.value === 'outline' || view.value === 'bible' || view.value === 'setup') {
      await store.loadBookFiles(bookId.value).catch(() => {})
      // 无单元书的编辑器落在 book-bible：文件到位后回填。
      const book = store.currentBook
      if (book && !book.units.length && editorPath.value === 'book-bible.md') {
        editorContent.value = book.files?.['book-bible.md'] || ''
      }
    }
  }

  async function reload() {
    await store.loadBook(bookId.value)
    const book = store.currentBook
    if (!book) return
    if (!modelId.value) modelId.value = defaultModelFor('write')
    if ((!selectedUnit.value || !book.units.some((u) => u.unitId === selectedUnit.value?.unitId)) && book.units.length) {
      selectedUnit.value = book.units[0]
    }
    if (!book.units.length) {
      editorPath.value = 'book-bible.md'
      editorContent.value = book.files?.['book-bible.md'] || ''
    } else if (selectedUnit.value) {
      await openSelected()
    }
  }

  function defaultModelFor(role: string) {
    const a = store.assignments.find((x) => x.role === role)
    if (a?.modelId) return a.modelId
    return store.models[0]?.id || ''
  }

  async function openSelected() {
    const u = selectedUnit.value
    if (!u) return
    if (u.hasProse && u.prosePath) {
      editorPath.value = u.prosePath
      editorContent.value = u.proseRaw || (await api.get<any>(`/api/books/${bookId.value}/file?path=${encodeURIComponent(u.prosePath)}`)).content
    } else {
      editorPath.value = u.outlinePath
      editorContent.value = u.outlineRaw || ''
    }
    // 骨架不带 outlineRaw；单元合同与细纲编辑都读它，按需取单个细纲文件回填。
    if (!u.outlineRaw && u.outlinePath) {
      try {
        const raw = (await api.get<any>(`/api/books/${bookId.value}/file?path=${encodeURIComponent(u.outlinePath)}`)).content
        editorContent.value = u.hasProse ? editorContent.value : raw
        const book = store.currentBook
        if (book) {
          const idx = book.units.findIndex((x) => x.unitId === u.unitId)
          if (idx >= 0) book.units[idx] = { ...book.units[idx], outlineRaw: raw }
        }
      } catch { /* 细纲尚未生成时保持空合同 */ }
    }
  }

  async function saveFile(path = editorPath.value, content = editorContent.value) {
    if (!path) return
    saving.value = true
    try {
      await api.put(`/api/books/${bookId.value}/file`, { path, content, baseRevision: store.currentBook?.revision })
      store.note('已保存')
      await reload()
    } catch (e) { store.fail(e) } finally { saving.value = false }
  }

  function beginChapterEdit(chapter: number, _title: string, raw: string) {
    proseBeforeEdit.value = editorContent.value
    editingChapter.value = chapter
    editorContent.value = raw
    editingProse.value = true
  }

  function cancelChapterEdit() {
    if (editingChapter.value !== null) editorContent.value = proseBeforeEdit.value
    editingChapter.value = null
    editingProse.value = false
  }

  async function saveChapterEdit() {
    const chapter = editingChapter.value
    if (chapter === null) return
    const original = proseBeforeEdit.value.replace(/\r\n/g, '\n')
    const heading = new RegExp(`^##\\s*第\\s*${chapter}\\s*章(?:\\s.*)?$`, 'm')
    const match = heading.exec(original)
    if (!match) {
      store.fail(`找不到第 ${chapter} 章，未保存`)
      return
    }
    const next = /^##\s*第\s*\d+\s*章(?:\s.*)?$/m
    next.lastIndex = match.index + match[0].length
    const rest = original.slice(next.lastIndex)
    const following = next.exec(rest)
    const end = following ? next.lastIndex + following.index : original.length
    const updated = original.slice(0, match.index) + editorContent.value.trimEnd() + '\n\n' + original.slice(end).trimStart()
    await saveFile(editorPath.value, updated)
    editingChapter.value = null
    editingProse.value = false
  }

  async function saveOutlineField(payload: { path: string; kind: string; entityId: string; field: string; value: string }, done: (ok: boolean) => void) {
    saving.value = true
    try {
      await api.patch(`/api/books/${bookId.value}/outline-field`, { ...payload, baseRevision: store.currentBook?.revision })
      store.note('已保存字段，Markdown 其他内容保持不变')
      await reload()
      done(true)
    } catch (e) { store.fail(e); done(false) } finally { saving.value = false }
  }

  function reviewStructure(path: string) {
    const book = store.currentBook
    if (!book || busy.value) return
    start('structure_review', { title: `结构审校 · ${path}` })
    if (confirmSpec.value) confirmSpec.value.targetPath = path
  }

  function repairStructure(c: Candidate) {
    if (!store.currentBook || !c.targetPath) return
    start('rewrite_entity', { title: `按结构意见修复 · ${c.targetPath}` })
    if (confirmSpec.value) {
      confirmSpec.value.targetPath = c.targetPath
      confirmSpec.value.entityKind = 'structure_repair'
      confirmSpec.value.volumeId = c.volumeId
      confirmSpec.value.issueIds = c.findings?.filter((f) => f.severity === 'blocking').map((f) => f.id) || []
    }
  }

  const confirmSpec = ref<Record<string, any> | null>(null)
  const confirmTitle = ref('')

  function start(kind: string, opts: { mode?: string; volumeId?: string; title?: string } = {}) {
    const book = store.currentBook
    if (!book || busy.value) return
    const spec: any = { kind, bookId: book.summary.id, baseRevision: book.revision }
    // 立项/卷纲不以当前选中单元为目标，避免运行中心全显示成 U1。
    if (selectedUnit.value && kind !== 'plan' && kind !== 'idea') {
      spec.unitId = selectedUnit.value.unitId
      if (selectedUnit.value.volume) {
        spec.volumeId = `v${String(selectedUnit.value.volume).padStart(2, '0')}`
      }
    } else if (kind === 'outline' && store.primary?.volume) {
      // 无选中单元时才回落到全书下一步所在卷。
      spec.volumeId = `v${String(store.primary.volume).padStart(2, '0')}`
    }
    // 审校是"重算结论"的任务：该单元已有的未处理清单会被归档再重审，
    // 否则那条 waiting_accept 的 run 会一直挡住新审校。确认弹窗里会写明。
    if (kind === 'review') spec.supersedeReview = true
    if (kind === 'plan') {
      const mode = opts.mode || (hasBookOutline.value ? 'volume' : 'init')
      spec.mode = mode
      delete spec.unitId
      if (opts.volumeId) spec.volumeId = opts.volumeId
      else if (mode === 'volume') {
        const vol = store.primary?.volume || 1
        spec.volumeId = `v${String(vol).padStart(2, '0')}`
      }
      const titles: Record<string, string> = {
        init: '立项 · 总纲 + 设定 + 人物',
        reinit: '重新立项 · 覆盖总纲 / 设定 / 人物',
        volume: opts.title || `${hasVolumes.value ? '重写' : '生成'}卷纲 · ${spec.volumeId || '当前卷'}`,
      }
      confirmTitle.value = titles[mode] || titles.init
      confirmSpec.value = spec
      return
    }
    const target = selectedUnit.value ? `${selectedUnit.value.unitId} ${selectedUnit.value.title || ''}` : `第 ${store.primary?.volume || 1} 卷`
    confirmTitle.value = `${stageLabels[kind] || kind} · ${target.trim()}`
    confirmSpec.value = spec
  }

  /** 创意扩写：自由 Markdown 候选，不写 canon；复用同一套确认弹窗与轮询。 */
  function startIdea(instruction = '') {
    const book = store.currentBook
    if (!book || busy.value) return
    confirmTitle.value = hasIdeaProposal.value ? '重新扩写创意提案' : '创意扩写 · 把 idea 扩成可立项的提案'
    confirmSpec.value = {
      kind: 'idea', bookId: book.summary.id, baseRevision: book.revision,
      instruction,
    }
  }

  function startRewrite(payload: { targetPath: string; entityKind: string; title: string }) {
    const book = store.currentBook
    if (!book || busy.value) return
    const label = payload.entityKind === 'cast' ? '重写人物卡' : '重写设定'
    confirmTitle.value = `${label} · ${payload.title}`
    confirmSpec.value = {
      kind: 'rewrite_entity', bookId: book.summary.id, baseRevision: book.revision,
      targetPath: payload.targetPath, entityKind: payload.entityKind,
    }
  }

  function startPlan(mode: 'init' | 'reinit' | 'volume', volume?: number) {
    const volumeId = volume ? `v${String(volume).padStart(2, '0')}` : undefined
    const volLabel = hasVolumes.value || mode !== 'volume' ? '重写卷纲' : '生成卷纲'
    start('plan', {
      mode,
      volumeId,
      title: mode === 'volume' ? `${volLabel} · 第 ${volume || 1} 卷` : undefined,
    })
  }

  async function confirmRun(payload: { instruction: string; knowledgeIds: string[]; preservePaths: string[]; rewriteMode?: string }) {
    const spec = confirmSpec.value
    confirmSpec.value = null
    if (!spec || busy.value) return
    if (payload.instruction) spec.instruction = payload.instruction
    if (payload.knowledgeIds?.length) spec.knowledgeIds = payload.knowledgeIds
    if (payload.preservePaths?.length) spec.preservePaths = payload.preservePaths
    if (payload.rewriteMode) spec.rewriteMode = payload.rewriteMode
    busy.value = true
    try {
      const run = await store.startRun(spec)
      runDrawer.value = true
      if (pollTimer.value) clearInterval(pollTimer.value)
      pollTimer.value = window.setInterval(async () => {
        try {
          const r = await store.pollRun(run.id)
          if (r.status !== 'running' && r.status !== 'pending') {
            if (pollTimer.value) clearInterval(pollTimer.value)
            pollTimer.value = null
            await store.loadCandidates(bookId.value)
            await store.loadRuns(bookId.value)
          }
        } catch (e) { store.fail(e) }
      }, 1200)
    } catch (e) { store.fail(e) } finally { busy.value = false }
  }

  async function primaryAction() {
    const p = store.primary
    if (!p) return
    if (p.unitId && store.currentBook) {
      const u = store.currentBook.units.find((x) => x.unitId === p.unitId)
      if (u) selectedUnit.value = u
    }
    if (p.kind === 'idea') {
      view.value = 'setup'
      startIdea()
      return
    }
    if (p.kind === 'plan') {
      if (!hasBookOutline.value) startPlan('init')
      else startPlan('volume', p.volume || 1)
      return
    }
    await runUnitAction(p.kind as 'outline' | 'write' | 'review' | 'finalize')
  }

  /**
   * 对当前选中单元启动生产/重做。只负责发起：结果在任务页确认，所以不切页——
   * 调用要跑几分钟，把作者送到一个此刻还空着的界面没有意义。
   */
  function runUnitAction(kind: 'outline' | 'write' | 'review' | 'finalize') {
    if (!selectedUnit.value || busy.value) return
    start(kind)
  }

  async function accept(c: Candidate, force = false) {
    try {
      // 接受修订只落盘并按正文版本归档旧清单；不能把勾选当成已修复，也不自动起审校。
      //
      // 实测（同单元连续 6 轮）复审报出的"新问题"有 96%+ 落在这次没改过的原文上——
      // 是前几轮没扫到的旧账，不是修复引入的回归。自动复审会把这个循环无限延长，
      // 作者也分不清"修好了"和"又发现一批旧的"。真要重来就重新审校整个单元。
      await store.acceptCandidate(c.id, c.baseRevision, {
        forceRiskAccept: force,
      })
      if (c.kind === 'revise' && c.unitId) focusUnit(c.unitId)
      await reload()
      // 归档后清单从「待审候选」消失，必须在历史里还能回看（历史已展开才刷）。
      await store.loadRuns(bookId.value)
      if (store.historyLoaded) await store.loadCandidateHistory(bookId.value)
    } catch (e: any) {
      const msg = e?.message || String(e)
      // 定稿候选自带本地门禁阻断时，接受会被服务端拒绝。"通过并定稿"若没有这一手，
      // 作者会在最后一步再撞一次死路——所以给一次显式越权确认，而不是只报错。
      if (!force && c.kind === 'finalize' && /blocking|阻断/.test(msg)) {
        if (window.confirm(`定稿检查仍有阻断：\n\n${msg}\n\n强制接受后账本照样回写，单元标为已定稿。继续吗？`)) {
          await accept(c, true)
        }
        return
      }
      store.fail(e)
    }
  }

  /** 按勾选的审校结论做一次定向修订；产物仍是候选，接受后才覆盖正文。 */
  async function reviseFindings(unitId: string, issueIds: string[]) {
    const book = store.currentBook
    if (!book || busy.value || !issueIds.length || !unitId) return
    // 让页面停在被修订的单元，后续操作不会"跳"到别的单元。
    focusUnit(unitId)
    busy.value = true
    try {
      const run = await store.startRun({
        kind: 'revise', bookId: book.summary.id, baseRevision: book.revision,
        unitId, issueIds,
      })
      runDrawer.value = true
      if (pollTimer.value) clearInterval(pollTimer.value)
      pollTimer.value = window.setInterval(async () => {
        try {
          const r = await store.pollRun(run.id)
          if (r.status !== 'running' && r.status !== 'pending') {
            if (pollTimer.value) clearInterval(pollTimer.value)
            pollTimer.value = null
            await store.loadCandidates(bookId.value)
            await store.loadRuns(bookId.value)
            if (r.status === 'waiting_accept') store.note('修订候选已生成：该单元的清单区已换成 diff，确认落盘后写正文')
            else if (r.status === 'failed') store.fail(r.error || '自动修复失败，正文未改变')
          }
        } catch (e) { store.fail(e) }
      }, 1200)
    } catch (e) {
      store.fail(e)
    } finally {
      busy.value = false
    }
  }

  /** 把选中单元切到指定单元（审校页点单元 ID 时用）。 */
  function focusUnit(unitId: string) {
    const u = store.currentBook?.units.find((x) => x.unitId === unitId)
    if (u) selectedUnit.value = u
  }

  /** 审校页「拒绝清单」：丢弃该单元的全部待处理审校结论，之后由作者自己重审。 */
  async function rejectUnitFindings(unitId: string, candidateIds: string[]) {
    if (busy.value || !candidateIds.length) return
    const ok = window.confirm(
      `丢弃 ${unitId} 的审校清单？\n\n${candidateIds.length} 份清单会被作废，正文不受影响。\n之后需要自己重新点「审校」才能再拿到结论。`,
    )
    if (!ok) return
    const reason = window.prompt('拒绝原因（可选，用于回看为什么要重审）', '') ?? ''
    busy.value = true
    try {
      for (const id of candidateIds) await store.rejectCandidate(id, reason)
      await Promise.all([
        store.loadCandidates(bookId.value),
        store.loadRuns(bookId.value),
        ...(store.historyLoaded ? [store.loadCandidateHistory(bookId.value)] : []),
      ])
      store.note('已丢弃审校清单；去写作页重新点「审校」才会生成新结论')
    } catch (e) {
      store.fail(e)
    } finally {
      busy.value = false
    }
  }

  /** 重新审校整个单元：先用新结论替换旧清单（归档），再按章重审一遍。 */
  async function retrySplitReview(unitId?: string) {
    const book = store.currentBook
    const unit = selectedUnit.value
    const targetId = unitId || unit?.unitId
    if (!book || !targetId || busy.value) return
    // 重新审校会用新结论替换旧结论：旧清单里尚未处理的条目随之归档。先讲清后果，
    // 否则作者会以为只是再算一遍、旧结论还在。
    const open = store.candidates.filter((c) => c.kind === 'review' && c.unitId === targetId)
    if (open.length) {
      const n = open.reduce((sum, c) => sum + (c.findings || []).length, 0)
      const ok = window.confirm(
        `重新审校 ${targetId}？\n\n该单元还有 ${n} 条未处理的审校结论（${open.length} 份清单），重新审校会先把它们归档作废，再按章重审一遍。\n如果旧结论还要用，请先在审校页勾选「自动修复」，或确认后丢弃。`,
      )
      if (!ok) return
    }
    busy.value = true
    try {
      const run = await store.startRun({
        kind: 'review', bookId: book.summary.id, baseRevision: book.revision,
        unitId: targetId, supersedeReview: true,
      })
      runDrawer.value = true
      if (pollTimer.value) clearInterval(pollTimer.value)
      pollTimer.value = window.setInterval(async () => {
        try {
          const r = await store.pollRun(run.id)
          if (r.status !== 'running' && r.status !== 'pending') {
            if (pollTimer.value) clearInterval(pollTimer.value)
            pollTimer.value = null
            await store.loadCandidates(bookId.value)
            await store.loadRuns(bookId.value)
          }
        } catch (e) { store.fail(e) }
      }, 1200)
    } catch (e) {
      store.fail(e)
    } finally {
      busy.value = false
    }
  }

  async function reject(c: Candidate) {
    // 拒绝修订候选 = 丢弃这一次修复，正文不变。它是一次真实返工，必须让作者知道后果。
    if (c.kind === 'revise') {
      const ok = window.confirm(
        '丢弃这次修复？\n\n正文不会被改动。问题清单仍在审校页，可以调整勾选后重新自动修复。',
      )
      if (!ok) return
    }
    // 拒绝审校清单是它唯一的正常出口（没有"归档"了）：提醒作者之后要自己重审。
    if (c.kind === 'review') {
      const ok = window.confirm(
        '丢弃这份审校清单？\n\n问题清单会被作废，之后需要自己重新点「审校」才能再拿到结论。',
      )
      if (!ok) return
    }
    const reason = c.kind === 'review' || c.kind === 'revise'
      ? (window.prompt('拒绝原因（可选，用于回看为什么要重做）', '') ?? '')
      : ''
    try {
      await store.rejectCandidate(c.id, reason)
      await store.loadCandidates(bookId.value)
      if (store.historyLoaded) await store.loadCandidateHistory(bookId.value)
      if (c.kind === 'revise') {
        // 留在任务页：该单元的审校清单还在原地，可调整勾选后重试。
        if (c.unitId) focusUnit(c.unitId)
        await store.loadRuns(bookId.value)
        store.note('已丢弃这次修复，正文未改变；可调整勾选后重新自动修复')
      } else if (c.kind === 'review') {
        await store.loadRuns(bookId.value)
        store.note('已丢弃这份审校清单；去写作页重新点「审校」才会生成新结论')
      }
    } catch (e) { store.fail(e) }
  }

  async function enableWrite() {
    try {
      await api.post(`/api/books/${bookId.value}/enable-write`)
      await reload()
      store.note('已启用写入')
    } catch (e) { store.fail(e) }
  }

  const statusText: Record<string, string> = {
    proposed: '待细纲', accepted: '待写', drafted: '草稿', reviewed: '已审校', finalized: '定稿',
  }

  return {
    // 只暴露壳层真正用到的成员。内部状态（pollTimer、proseBeforeEdit、collapsedVolumes
    // 等）留在闭包里：它们由这里的方法维护，壳层读了也没有能做的事。
    store, bookId, view,
    selectedUnit, editorPath, editorContent, saving, busy, modelId, runDrawer,
    editingProse, editingOutline, confirmSpec, confirmTitle,
    taskItems, pendingTaskCount, pendingTasksOf,
    isProseUnit, hasBookOutline, showSetupTab, volumeGroups, primaryInfo,
    stageLabels, statusText,
    runCacheOf, toggleVolume, isCollapsed, defaultModelFor,
    saveFile, beginChapterEdit, cancelChapterEdit, saveChapterEdit, saveOutlineField,
    reviewStructure, repairStructure, start, startIdea, startRewrite, startPlan,
    confirmRun, primaryAction, runUnitAction, accept, reviseFindings,
    focusUnit, rejectUnitFindings, retrySplitReview, reject, enableWrite,
  }
}
