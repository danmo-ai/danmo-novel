<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { api } from '@/api/client'
import type { Candidate, CallRecord, UnitInfo } from '@/types'
import OutlineWorkspace from '@/components/book/OutlineWorkspace.vue'
import BibleWorkspace from '@/components/book/BibleWorkspace.vue'
import ReviewWorkspace from '@/components/book/ReviewWorkspace.vue'
import ChapterDiff from '@/components/book/ChapterDiff.vue'
import UnitProse from '@/components/book/UnitProse.vue'
import UnitContract from '@/components/book/UnitContract.vue'
import UnitFlowActions from '@/components/book/UnitFlowActions.vue'
import RunConfirmDialog from '@/components/book/RunConfirmDialog.vue'
import ChainPanel from '@/components/book/ChainPanel.vue'
import { planModeLabel as sharedPlanModeLabel, runKindLabel } from '@/utils/runLabels'
import { cacheStat, callCacheText, hitRateText } from '@/utils/tokenStats'
import type { Chain, ChainSpec } from '@/types'

const route = useRoute()
const store = useAppStore()
const bookId = computed(() => route.params.id as string)
const view = ref<'write' | 'outline' | 'bible' | 'review'>('write')
const selectedUnit = ref<UnitInfo | null>(null)
const editorPath = ref('')
const editorContent = ref('')
const saving = ref(false)
const busy = ref(false)
const modelId = ref('')
const runDrawer = ref(false)
/** 实时日志的缓存汇总：重试（step=repair）命中率应接近 100%。 */
function runCacheOf(run: { calls?: CallRecord[] } | null | undefined) { return cacheStat(run?.calls) }
const pollTimer = ref<number | null>(null)
const customInstruction = ref('')
const editingProse = ref(false)
const editingOutline = ref(false)
const isProseUnit = computed(() => Boolean(selectedUnit.value?.hasProse && selectedUnit.value.prosePath === editorPath.value))
const stageLabels: Record<string, string> = { plan: '立项', outline: '细纲', write: '写作', review: '审校', finalize: '定稿', revise: '修订', local_edit: '局部编辑', rewrite_entity: '重写设定' }
const hasBookOutline = computed(() => Boolean((store.currentBook?.files?.['outline/book_outline.md'] || '').trim()))
const hasVolumes = computed(() => (store.currentBook?.volumes?.length || 0) > 0)
// 单元驱动工作流：proposed→细纲 → accepted→正文 → drafted→审校 → reviewed→定稿。
// 逐单元闭环推进，下一单元细纲才能读到最新连续性账本。
const FLOW_STEPS = [
  { key: 'outline', label: '细纲', desc: '单元合同' },
  { key: 'write', label: '正文', desc: '候选稿' },
  { key: 'review', label: '审校', desc: '本地+AI' },
  { key: 'finalize', label: '定稿', desc: '回写账本' },
]
const flowState = computed(() => {
  const u = selectedUnit.value
  if (!u) return { active: -1, done: -1 }
  const idx = { proposed: 0, accepted: 1, drafted: 2, reviewed: 3 }[u.status] ?? 0
  return { active: idx, done: u.status === 'reviewed' ? 3 : idx - 1 }
})
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
    if (u.status === 'reviewed') g.reviewed++
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
  if (p.kind === 'plan') {
    if (!hasBookOutline.value) {
      return { label: '立项', description: '一次生成总纲、全部设定与人物（不含卷纲）', target, stage: '立项' }
    }
    return { label: `生成卷纲 · 第 ${p.volume || 1} 卷`, description: '根据总纲生成当前卷纲，并只播种缺失单元', target, stage: '卷纲' }
  }
  if (p.kind === 'outline') return { label: `生成 ${target} 细纲`, description: '第 1 步 · 补全单元写作合同；也可在单元栏随时重写细纲', target, stage: '细纲' }
  if (p.kind === 'write') return { label: `写 ${target} 正文`, description: '第 2 步 · 按合同生成候选稿；有正文后可在单元栏重写', target, stage: '写作' }
  if (p.kind === 'review') return { label: `审校 ${target}`, description: '第 3 步 · AI 审校；通过后在单元栏点「定稿」回写账本', target, stage: '审校' }
  if (p.kind === 'finalize') return { label: `定稿 ${target}`, description: '第 4 步 · 本地定稿检查；接受后回写账本', target, stage: '定稿' }
  return { label: '继续创作', description: '执行下一步', target, stage: stageLabels[p.kind] || '写作' }
})

onMounted(async () => {
  await store.loadLLM().catch(() => {})
  await reload()
  if (store.chain && (store.chain.status === 'running' || store.chain.status === 'pending')) startChainPoll()
})
onUnmounted(() => {
  if (pollTimer.value) clearInterval(pollTimer.value)
  stopChainPoll()
})

watch(selectedUnit, () => { editingProse.value = false; editingOutline.value = false; void openSelected() })
watch(view, () => {
  void openSelected()
})

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
    editorContent.value = book.files['book-bible.md'] || ''
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
}

async function saveFile(path = editorPath.value, content = editorContent.value) {
  if (!path) return
  saving.value = true
  try {
    await api.put(`/api/books/${bookId.value}/file`, { path, content })
    store.note('已保存')
    await reload()
  } catch (e) { store.fail(e) } finally { saving.value = false }
}

const confirmSpec = ref<Record<string, any> | null>(null)
const confirmTitle = ref('')

// ---- 连续单元重写（chain）----
const chainPollTimer = ref<number | null>(null)
const chainSignature = ref('')

function signatureOf(ch: Chain | null | undefined) {
  if (!ch) return ''
  return `${ch.status}|${ch.cursor}|${ch.stage}|${ch.units.map((u) => u.status).join(',')}`
}

function stopChainPoll() {
  if (chainPollTimer.value) clearInterval(chainPollTimer.value)
  chainPollTimer.value = null
}

/** 每个阶段接受都会改 revision，签名变化时整书重载一次。 */
function startChainPoll() {
  stopChainPoll()
  chainSignature.value = signatureOf(store.chain)
  chainPollTimer.value = window.setInterval(async () => {
    const id = store.chain?.id
    if (!id) { stopChainPoll(); return }
    try {
      const ch = await store.pollChain(id)
      const sig = signatureOf(ch)
      if (sig !== chainSignature.value) {
        chainSignature.value = sig
        await reload()
      }
      if (ch.status !== 'running' && ch.status !== 'pending') stopChainPoll()
    } catch (e) { store.fail(e); stopChainPoll() }
  }, 1500)
}

async function beginChain(spec: ChainSpec) {
  try {
    await store.startChain(spec)
    chainSignature.value = ''
    startChainPoll()
    await reload()
  } catch (e) { store.fail(e) }
}

async function continueChain() {
  const id = store.chain?.id
  if (!id) return
  try {
    await store.resumeChain(id)
    startChainPoll()
    await reload()
  } catch (e) { store.fail(e) }
}

async function stopChain() {
  const id = store.chain?.id
  if (!id) return
  try {
    await store.cancelChain(id)
    stopChainPoll()
    await reload()
  } catch (e) { store.fail(e) }
}

function start(kind: string, opts: { mode?: string; volumeId?: string; title?: string } = {}) {
  const book = store.currentBook
  if (!book || busy.value) return
  const spec: any = { kind, bookId: book.summary.id, baseRevision: book.revision }
  // 立项/卷纲不以当前选中单元为目标，避免运行中心全显示成 U1。
  if (selectedUnit.value && kind !== 'plan') {
    spec.unitId = selectedUnit.value.unitId
    if (selectedUnit.value.volume) {
      spec.volumeId = `v${String(selectedUnit.value.volume).padStart(2, '0')}`
    }
  } else if (kind === 'outline' && store.primary?.volume) {
    // 无选中单元时才回落到全书下一步所在卷。
    spec.volumeId = `v${String(store.primary.volume).padStart(2, '0')}`
  }
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

function planCandidateFiles(c: Candidate) {
  const files: string[] = []
  const mode = c.mode || ''
  const keep = new Set((c as any).preservePaths || [])
  if (mode === 'volume') {
    if (c.planMarkdown?.volume_outline) files.push(`outline/volumes/${c.volumeId || 'v01'}.md`)
    return files
  }
  if (c.planMarkdown?.book_outline && !keep.has('outline/book_outline.md')) files.push('outline/book_outline.md')
  if (c.planSettings) files.push(...Object.keys(c.planSettings).filter((p) => !keep.has(p)))
  if (c.planMarkdown?.cast_json) {
    try {
      const cast = JSON.parse(c.planMarkdown.cast_json) as { stem?: string }[]
      for (const row of cast) {
        if (row.stem && !keep.has(`canon/cast/${row.stem}.md`)) files.push(`canon/cast/${row.stem}.md`)
      }
    } catch { /* ignore */ }
  }
  return files
}

function planModeLabel(mode?: string) {
  return sharedPlanModeLabel(mode)
}

// 候选对象的目标标识：优先具体单元/卷/目标文件；细纲批次取自单元合同，
// 避免把「目标卷」误显示成单元 id。
function candidateTarget(c: Candidate): string {
  if (c.unitId) return c.unitId
  if (c.targetPath) return c.targetPath
  const units = c.unitOutlines ?? []
  if (units.length === 1 && units[0]?.unitId) return units[0].unitId as string
  if (units.length > 1) return `${c.volumeId || ''} · ${units.length} 个单元`
  return c.volumeId || ''
}

function planCastPreview(c: Candidate) {
  try {
    return JSON.stringify(JSON.parse(c.planMarkdown?.cast_json || '[]'), null, 2)
  } catch {
    return c.planMarkdown?.cast_json || ''
  }
}

async function confirmRun(payload: { instruction: string; knowledgeIds: string[]; preservePaths: string[] }) {
  const spec = confirmSpec.value
  confirmSpec.value = null
  if (!spec || busy.value) return
  if (payload.instruction) spec.instruction = payload.instruction
  if (payload.knowledgeIds?.length) spec.knowledgeIds = payload.knowledgeIds
  if (payload.preservePaths?.length) spec.preservePaths = payload.preservePaths
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
  if (p.kind === 'plan') {
    if (!hasBookOutline.value) startPlan('init')
    else startPlan('volume', p.volume || 1)
    return
  }
  await runUnitAction(p.kind as 'outline' | 'write' | 'review' | 'finalize')
}

/** 对当前选中单元启动生产/重做；审校与定稿会切到审校页便于看结果。 */
function runUnitAction(kind: 'outline' | 'write' | 'review' | 'finalize') {
  if (!selectedUnit.value || busy.value) return
  if (kind === 'review' || kind === 'finalize') view.value = 'review'
  else if (kind === 'outline' || kind === 'write') view.value = 'write'
  start(kind)
}

function runFlowStep(key: string) {
  if (!selectedUnit.value) return
  if (key === 'outline' || key === 'write' || key === 'review' || key === 'finalize') {
    runUnitAction(key)
  }
}

async function accept(c: Candidate) {
  try {
    // 修订候选被接受时请求服务端归档旧清单并自动复审：闭环一的最后一步。
    const accepted = await store.acceptCandidate(c.id, c.baseRevision, {
      followUpReview: c.kind === 'revise',
    })
    if (c.kind === 'review') view.value = 'review'
    if (c.kind === 'revise' && c.unitId) focusUnit(c.unitId)
    await reload()
    // 归档后清单从「待审候选」消失，必须在历史里还能回看。
    await Promise.all([store.loadRuns(bookId.value), store.loadCandidateHistory(bookId.value)])
    if (accepted?.followUpRunId) startRunPoll(accepted.followUpRunId)
  } catch (e) { store.fail(e) }
}

/** 轮询一个已在服务端跑起来的 run（接受后的自动复审）。 */
function startRunPoll(runId: string) {
  if (pollTimer.value) clearInterval(pollTimer.value)
  runDrawer.value = true
  pollTimer.value = window.setInterval(async () => {
    try {
      const r = await store.pollRun(runId)
      if (r.status !== 'running' && r.status !== 'pending') {
        if (pollTimer.value) clearInterval(pollTimer.value)
        pollTimer.value = null
        await store.loadCandidates(bookId.value)
        await store.loadRuns(bookId.value)
      }
    } catch (e) { store.fail(e) }
  }, 1200)
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
          if (r.status === 'waiting_accept') store.note('修订候选已生成：去「待审候选」看 diff 后确认落盘')
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

/** 把选中单元切到指定单元（审校页点单元 ID 或在候选卡片点「去修订」）。 */
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
      store.loadCandidateHistory(bookId.value),
      store.loadRuns(bookId.value),
    ])
    store.note('已丢弃审校清单；去写作页重新点「审校」才会生成新结论')
  } catch (e) {
    store.fail(e)
  } finally {
    busy.value = false
  }
}

/** 截断后重试：改用按章审校，每章一次调用，不会撞输出预算。 */
async function retrySplitReview(unitId?: string) {
  const book = store.currentBook
  const unit = selectedUnit.value
  const targetId = unitId || unit?.unitId
  if (!book || !targetId || busy.value) return
  busy.value = true
  try {
    const run = await store.startRun({
      kind: 'review', bookId: book.summary.id, baseRevision: book.revision,
      unitId: targetId, splitByChapter: true,
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
    await Promise.all([store.loadCandidates(bookId.value), store.loadCandidateHistory(bookId.value)])
    if (c.kind === 'revise') {
      // 唯一的下一步入口在审校页：直接带作者过去，别让他自己找。
      if (c.unitId) focusUnit(c.unitId)
      view.value = 'review'
      await store.loadRuns(bookId.value)
      store.note('已丢弃这次修复，正文未改变；可在审校页调整勾选后重新自动修复')
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

async function localEdit(instruction: string) {
  if (!editorContent.value) return
  const spec = {
    kind: 'local_edit', bookId: bookId.value, baseRevision: store.currentBook?.revision, modelId: modelId.value,
    instruction,
    selection: { path: editorPath.value, text: editorContent.value, before: '', after: '' },
  }
  try {
    const run = await store.startRun(spec)
    runDrawer.value = true
    if (pollTimer.value) clearInterval(pollTimer.value)
    pollTimer.value = window.setInterval(async () => {
      const r = await store.pollRun(run.id)
      if (r.status !== 'running') {
        if (pollTimer.value) clearInterval(pollTimer.value)
        await store.loadCandidates(bookId.value)
      }
    }, 1200)
  } catch (e) { store.fail(e) }
}

function chapterText(c: Candidate) {
  return (c.chapters || []).map((ch) => `## 第${ch.chapter}章 ${ch.title || ''}\n\n${ch.body}`).join('\n\n---\n\n')
}
const statusText: Record<string, string> = {
  proposed: '待细纲', accepted: '待写', drafted: '草稿', reviewed: '定稿',
}
const castExpanded = ref(false)
const visibleCast = computed(() => {
  const all = store.currentBook?.cast || []
  return castExpanded.value ? all : all.slice(0, 6)
})
function castName(name: string) {
  // loader 已把姓名与说明分离；这里只做兜底清洗。
  return name.replace(/^#+\s*/, '').trim()
}
function castInitial(name: string) { return castName(name).slice(0, 1) || '?' }
function roleLabel(role?: string) {
  const r = (role || '').toLowerCase()
  if (['protagonist', 'hero', 'main'].includes(r)) return '主角'
  if (['antagonist', 'volume_antagonist', 'villain'].includes(r)) return '卷反派'
  return '常驻'
}
</script>

<template>
  <div v-if="store.currentBook" class="book-page">
    <div class="book-head">
      <div>
        <p class="crumb"><router-link to="/">书架</router-link> / {{ store.currentBook.summary.title }}</p>
        <h1>{{ store.currentBook.summary.title }}</h1>
      </div>
      <div class="book-head-actions">
        <button v-if="store.currentBook.summary.readOnly" @click="enableWrite">启用写入</button>
      </div>
    </div>

    <div class="primary-task panel">
      <div class="primary-task-copy">
        <span class="task-kicker">下一步 · {{ primaryInfo.target }}</span>
        <p>{{ primaryInfo.description }}</p>
      </div>
      <button class="primary" :disabled="busy" @click="primaryAction">{{ busy ? '正在启动…' : primaryInfo.label }}</button>
    </div>

    <div class="tabs" role="tablist" aria-label="创作主导航">
      <button role="tab" :aria-selected="view === 'write'" :class="{ active: view === 'write' }" @click="view = 'write'">写作</button>
      <button role="tab" :aria-selected="view === 'outline'" :class="{ active: view === 'outline' }" @click="view = 'outline'">大纲</button>
      <button role="tab" :aria-selected="view === 'bible'" :class="{ active: view === 'bible' }" @click="view = 'bible'">设定</button>
      <button role="tab" :aria-selected="view === 'review'" :class="{ active: view === 'review' }" @click="view = 'review'">审校</button>
    </div>

    <div class="workspace" :class="`view-${view}`">
      <aside v-if="view === 'write'" class="unit-tree panel">
        <div class="flow-guide">
          <div class="flow-steps">
            <template v-for="(s, i) in FLOW_STEPS" :key="s.key">
              <button
                type="button"
                class="flow-step"
                :class="{ active: flowState.active === i, done: flowState.done >= i }"
                :disabled="busy || !selectedUnit || store.currentBook.summary.readOnly"
                :title="selectedUnit ? `${s.desc} · 对 ${selectedUnit.unitId}` : s.desc"
                @click="runFlowStep(s.key)"
              >{{ s.label }}</button>
              <span v-if="i < FLOW_STEPS.length - 1" class="flow-arrow">→</span>
            </template>
          </div>
          <p class="flow-note">点步骤或下方按钮：对<strong>当前选中单元</strong>生成/重做。定稿接受后回写账本，再开下一单元。</p>
        </div>
        <div class="tree-head"><span>单元</span><span class="tree-count">{{ store.currentBook.units.length }}</span></div>
        <div v-for="g in volumeGroups" :key="g.volume" class="volume-group">
          <button class="volume-head" @click="toggleVolume(g.volume)">
            <span class="volume-caret">{{ isCollapsed(g.volume) ? '▸' : '▾' }}</span>
            <span class="volume-label">第 {{ g.volume }} 卷</span>
            <span class="volume-progress">{{ g.reviewed }}/{{ g.units.length }}</span>
          </button>
          <p v-if="g.title && !isCollapsed(g.volume)" class="volume-title">{{ g.title }}</p>
          <template v-if="!isCollapsed(g.volume)">
            <button v-for="u in g.units" :key="u.unitId"
              class="unit-row" :class="{ active: selectedUnit?.unitId === u.unitId }"
              @click="selectedUnit = u">
              <span class="dot" :data-status="u.status"></span>
              <span class="unit-main">
                <span class="unit-line"><span class="unit-id">{{ u.unitId }}</span><span class="unit-status" :data-status="u.status">{{ statusText[u.status] || u.status }}</span></span>
                <span class="unit-title">{{ u.title || '未完成单元合同' }}</span>
              </span>
            </button>
          </template>
        </div>
        <div v-if="!(store.currentBook.units || []).length" class="tree-empty">还没有单元。先到大纲页完成「立项」与「生成卷纲」。</div>
      </aside>

      <OutlineWorkspace
        v-if="view === 'outline'"
        :saving="saving"
        :busy="busy"
        @open-unit="selectedUnit = $event; view = 'write'"
        @save="saveFile"
        @generate="start"
        @plan="startPlan"
      />
      <BibleWorkspace
        v-else-if="view === 'bible'"
        :saving="saving"
        @save="saveFile"
        @open-unit="selectedUnit = $event; view = 'write'"
        @rewrite="startRewrite"
      />
      <ReviewWorkspace
        v-else-if="view === 'review'"
        :selected-unit="selectedUnit"
        :busy="busy"
        :read-only="store.currentBook.summary.readOnly"
        @revise="reviseFindings"
        @retry-split="retrySplitReview"
        @focus-unit="focusUnit"
        @reject-unit="rejectUnitFindings"
      />
      <section v-else class="editor-panel panel">
        <template v-if="isProseUnit && selectedUnit">
          <UnitProse v-if="!editingProse" :unit="selectedUnit" :raw="editorContent"
            :can-write="!store.currentBook.summary.readOnly" @edit="editingProse = true">
            <template #contract>
              <details class="inline-contract">
                <summary>单元合同 · 写正文时对照</summary>
                <UnitContract :unit="selectedUnit" compact />
              </details>
            </template>
            <template #actions>
              <UnitFlowActions
                :unit="selectedUnit"
                :busy="busy"
                :read-only="store.currentBook.summary.readOnly"
                emphasize
                @run="runUnitAction"
              />
            </template>
          </UnitProse>
          <div v-else>
            <div class="editor-toolbar">
              <span class="path">{{ editorPath }}</span>
              <div class="editor-actions">
                <button @click="saveFile(); editingProse = false" :disabled="saving">{{ saving ? '保存中' : '保存' }}</button>
                <button @click="editingProse = false">取消</button>
              </div>
            </div>
            <textarea v-model="editorContent" class="prose-editor" spellcheck="false"></textarea>
          </div>
        </template>
        <template v-else-if="selectedUnit">
          <!-- 无正文单元：写作页直接生产/查看细纲 -->
          <div class="unit-work">
            <header class="unit-work-head">
              <div>
                <small>{{ selectedUnit.unitId }} · {{ statusText[selectedUnit.status] || selectedUnit.status }}<template v-if="selectedUnit.chapterFrom"> · 第 {{ selectedUnit.chapterFrom }}–{{ selectedUnit.chapterTo }} 章</template></small>
                <h3>{{ selectedUnit.title || '未完成单元合同' }}</h3>
              </div>
              <div class="unit-work-actions">
                <UnitFlowActions
                  v-if="!editingOutline"
                  :unit="selectedUnit"
                  :busy="busy"
                  :read-only="store.currentBook.summary.readOnly"
                  emphasize
                  @run="runUnitAction"
                />
                <button v-if="!editingOutline" @click="editingOutline = true">手工编辑细纲</button>
              </div>
            </header>
            <template v-if="!editingOutline">
              <UnitContract :unit="selectedUnit" />
              <details class="raw-outline">
                <summary>查看细纲源文件</summary>
                <pre>{{ editorContent }}</pre>
              </details>
            </template>
            <div v-else class="outline-edit">
              <div class="editor-toolbar">
                <span class="path">{{ selectedUnit.outlinePath }}</span>
                <div class="editor-actions">
                  <button @click="saveFile(); editingOutline = false" :disabled="saving">{{ saving ? '保存中' : '保存' }}</button>
                  <button @click="editingOutline = false">取消</button>
                </div>
              </div>
              <textarea v-model="editorContent" class="prose-editor" spellcheck="false"></textarea>
            </div>
          </div>
        </template>
        <template v-else>
          <div class="editor-toolbar">
            <span class="path">{{ editorPath }}</span>
            <div class="editor-actions">
              <button @click="saveFile()" :disabled="saving">{{ saving ? '保存中' : '保存' }}</button>
            </div>
          </div>
          <textarea v-model="editorContent" class="prose-editor" spellcheck="false"></textarea>
        </template>
      </section>

      <aside v-if="view === 'write'" class="context-panel">
        <ChainPanel
          :units="store.currentBook.units"
          :selected-unit="selectedUnit"
          :chain="store.chain"
          :read-only="store.currentBook.summary.readOnly"
          :busy="busy"
          @start="beginChain"
          @resume="continueChain"
          @cancel="stopChain"
        />
        <section class="side-card panel">
          <h4>全书下一步</h4>
          <div class="next-step">
            <span class="next-kind">{{ stageLabels[store.primary?.kind || ''] || store.primary?.kind }}</span>
            <strong>{{ store.primary?.unitId || `第 ${store.primary?.volume} 卷` }}</strong>
          </div>
          <p class="side-note">页顶大按钮跟这一步。当前单元的细纲/正文/审校/定稿在中间顶栏操作。</p>
        </section>
        <section class="side-card panel">
          <h4>本卷人物 <span class="side-count">{{ store.currentBook.cast?.length || 0 }}</span></h4>
          <div class="cast-list">
            <button class="cast-item" v-for="c in visibleCast" :key="c.stem" @click="view = 'bible'">
              <span class="cast-avatar">{{ castInitial(c.name) }}</span>
              <span class="cast-info">
                <strong>{{ castName(c.name) }}<i class="role-badge" :data-role="c.role">{{ roleLabel(c.role) }}</i></strong>
                <small v-if="c.tagline">{{ c.tagline }}</small>
              </span>
            </button>
          </div>
          <button v-if="(store.currentBook.cast?.length || 0) > 6" class="cast-more" @click="castExpanded = !castExpanded">
            {{ castExpanded ? '收起' : `查看全部 ${store.currentBook.cast?.length} 人` }}
          </button>
        </section>
        <section class="side-card panel side-actions">
          <button :disabled="busy" @click="startPlan(hasBookOutline ? 'reinit' : 'init')">
            {{ hasBookOutline ? '重新立项' : '立项' }}
          </button>
        </section>
      </aside>
    </div>

    <section class="candidates">
      <h2>待审候选</h2>
      <p v-if="!store.candidates.length" class="muted">生成结果会先到这里，接受后才进入正文。</p>
      <article v-for="c in store.candidates" :key="c.id" class="candidate panel">
        <header>
          <div>
            <strong>{{ c.kind === 'plan' ? planModeLabel(c.mode) : (stageLabels[c.kind] || c.kind) }} · {{ candidateTarget(c) }}</strong>
            <span class="muted"> · {{ new Date(c.createdAt).toLocaleString() }}</span>
          </div>
          <div>
            <button v-if="c.kind === 'review' && c.unitId" class="primary" @click="focusUnit(c.unitId); view = 'review'">去修订</button>
            <button @click="reject(c)">拒绝</button>
            <button v-if="c.kind !== 'review'" class="primary" @click="accept(c)">{{ c.kind === 'revise' ? '确认落盘' : '接受' }}</button>
          </div>
        </header>
        <!-- 审校候选没有"归档"：清单只有两条路——去修订，或拒绝后自己重新审校。 -->
        <p v-if="c.kind === 'review'" class="muted">审校结论只能二选一：点「去修订」勾选问题让系统改写；或「拒绝」丢弃这份清单，之后自己重新审校。</p>
        <!-- 修订候选：先看 diff 再落盘，接受不能是盲签。 -->
        <template v-else-if="c.kind === 'revise'">
          <p class="muted">下面是相对现有正文的改动。确认无误后点「确认落盘」覆盖正文；接受后会自动复审。</p>
          <ChapterDiff :base="c.baseChapters" :revised="c.chapters || []" />
        </template>
        <pre v-else-if="c.chapters" class="candidate-prose">{{ chapterText(c) }}</pre>
        <pre v-else-if="c.unitOutlines" class="candidate-prose">{{ JSON.stringify(c.unitOutlines, null, 2) }}</pre>
        <pre v-else-if="c.replacement" class="candidate-prose">{{ c.replacement }}</pre>
        <div v-else-if="c.planMarkdown || c.planSettings" class="plan-candidate">
          <p class="plan-mode"><strong>{{ planModeLabel(c.mode) }}</strong><span class="muted"> · 接受后写入 {{ planCandidateFiles(c).length }} 个文件</span></p>
          <template v-if="c.mode === 'volume'">
            <details v-if="c.planMarkdown?.volume_outline" open><summary>卷纲 · {{ c.volumeId }}</summary><pre class="candidate-prose">{{ c.planMarkdown.volume_outline }}</pre></details>
            <p class="muted">只写当前卷纲，并播种索引中尚不存在的单元；总纲/设定/人物/正文不变。</p>
          </template>
          <template v-else>
            <details v-if="c.planMarkdown?.book_outline" open><summary>总纲</summary><pre class="candidate-prose">{{ c.planMarkdown.book_outline }}</pre></details>
            <details v-if="c.planSettings && Object.keys(c.planSettings).length"><summary>设定 · {{ Object.keys(c.planSettings).length }} 项</summary>
              <ul class="plan-files"><li v-for="p in Object.keys(c.planSettings)" :key="p">{{ p }}</li></ul>
            </details>
            <details v-if="c.planMarkdown?.cast_json"><summary>人物</summary><pre class="candidate-prose">{{ planCastPreview(c) }}</pre></details>
            <p class="muted">立项只写总纲派生设定与人物；不写卷纲、不播种单元。</p>
          </template>
          <p v-if="(c as any).preservePaths?.length" class="muted">保留不覆盖：{{ ((c as any).preservePaths as string[]).join('、') }}</p>
          <details><summary>将覆盖的文件（已排除保留项）</summary><ul class="plan-files"><li v-for="p in planCandidateFiles(c)" :key="p">{{ p }}</li></ul></details>
        </div>
        <div v-if="c.findings?.length" class="candidate-findings">
          <div v-for="f in c.findings.slice(0, 6)" :key="f.id" class="finding" :class="f.severity">
            {{ f.title }} <span v-if="f.evidence">· {{ f.evidence }}</span>
          </div>
        </div>
      </article>
    </section>

    <RunConfirmDialog v-if="confirmSpec" :spec="confirmSpec" :title="confirmTitle"
      :model-label="defaultModelFor(confirmSpec.kind === 'plan' || confirmSpec.kind === 'rewrite_entity' ? 'plan' : confirmSpec.kind)"
      @confirm="confirmRun" @cancel="confirmSpec = null" />

    <div v-if="runDrawer" class="run-drawer panel">
      <header><strong>模型日志</strong><button @click="runDrawer = false">×</button></header>
      <div v-if="store.activeRun">
        <p>{{ runKindLabel(store.activeRun.kind, store.activeRun.spec?.mode) }} · {{ store.activeRun.status }}</p>
        <p class="muted">模型 {{ store.activeRun.modelId }} · 调用 {{ store.activeRun.callsUsed }}/{{ store.activeRun.policy?.maxCalls || '计算中' }}</p>
        <p v-if="runCacheOf(store.activeRun).hasData" class="muted">
          缓存命中 {{ runCacheOf(store.activeRun).read.toLocaleString() }} token（{{ hitRateText(runCacheOf(store.activeRun)) }}）
          <template v-if="runCacheOf(store.activeRun).creation">· 写入 {{ runCacheOf(store.activeRun).creation.toLocaleString() }}</template>
        </p>
        <div v-for="c in store.activeRun.calls" :key="c.index" class="call-row">
          #{{ c.index }} {{ c.step }} · in {{ c.promptTokens }} / out {{ c.completionTokens }} · {{ c.durationMs }}ms
          <template v-if="c.cacheReadTokens"> · 缓存 {{ callCacheText(c) }}</template>
        </div>
        <p v-if="store.activeRun.error" class="error-text">{{ store.activeRun.error }}</p>
      </div>
      <div v-else class="muted">暂无运行</div>
    </div>
  </div>
</template>

<style scoped>
/* ---- 页头 ---- */
.book-head { display: flex; justify-content: space-between; align-items: flex-end; gap: 16px; padding: 4px 0 18px; }
.crumb { color: var(--ink-soft); font-size: 12px; margin: 0; }
h1 { font-family: var(--font-prose); margin: 6px 0 0; font-size: 30px; line-height: 1.25; }
.book-head-actions { display: flex; gap: 8px; align-items: center; flex-shrink: 0; }

/* ---- 主任务条 ---- */
.primary-task { display: flex; justify-content: space-between; align-items: center; gap: 20px; padding: 14px 16px 14px 20px; margin-bottom: 16px; border-color: rgba(15,107,92,.2); background: linear-gradient(120deg, rgba(15,107,92,.06), rgba(255,255,255,.75)); }
.primary-task-copy { min-width: 0; display: grid; gap: 3px; }
.task-kicker { color: var(--accent); font-size: 12px; font-weight: 700; }
.primary-task-copy p { margin: 0; color: var(--ink-soft); font-size: 12px; }
.primary-task .primary { flex-shrink: 0; padding: 10px 18px; }

/* ---- 主 Tab ---- */
.tabs { display: inline-flex; gap: 2px; margin-bottom: 18px; padding: 4px; border-radius: 12px; background: rgba(20,35,31,.05); }
.tabs button { border: none; background: transparent; color: var(--ink-soft); padding: 8px 22px; border-radius: 9px; font-weight: 600; }
.tabs button:hover:not(:disabled) { background: rgba(255,255,255,.7); color: var(--ink); }
.tabs button.active { background: white; color: var(--accent); box-shadow: 0 2px 8px rgba(20,35,31,.1); }

/* ---- 三栏工作区 ---- */
.workspace { display: grid; grid-template-columns: 236px minmax(0, 1fr) 252px; gap: 18px; align-items: start; }
.workspace:not(.view-write) { grid-template-columns: minmax(0, 1fr); }

/* 左：单元树 */
.unit-tree { padding: 10px; max-height: 74vh; overflow-y: auto; position: sticky; top: 12px; }
.tree-head { display: flex; justify-content: space-between; align-items: center; font-size: 12px; font-weight: 600; color: var(--ink-soft); padding: 6px 10px 10px; }
.tree-count { font-size: 11px; background: rgba(20,35,31,.06); padding: 1px 8px; border-radius: 20px; }
.unit-row { display: flex; align-items: flex-start; gap: 8px; width: 100%; border: none; background: transparent; padding: 9px 10px; border-radius: 10px; text-align: left; margin-bottom: 2px; }
.unit-row:hover:not(:disabled) { background: rgba(15,107,92,.06); color: inherit; }
.unit-row.active { background: rgba(15,107,92,.1); }
.unit-row.active .unit-id { color: var(--accent); }
.unit-main { display: grid; gap: 3px; min-width: 0; flex: 1; }
.unit-line { display: flex; justify-content: space-between; align-items: center; gap: 6px; }
.unit-id { font-size: 12px; font-weight: 600; white-space: nowrap; }
.unit-title { font-size: 11px; color: var(--ink-soft); line-height: 1.4; overflow: hidden; text-overflow: ellipsis; display: -webkit-box; -webkit-line-clamp: 1; -webkit-box-orient: vertical; }
.unit-status { font-size: 10px; padding: 1px 7px; border-radius: 20px; background: rgba(20,35,31,.06); color: var(--ink-soft); white-space: nowrap; flex-shrink: 0; }
.unit-status[data-status="reviewed"] { background: rgba(31,122,77,.12); color: var(--ok); }
.unit-status[data-status="drafted"] { background: rgba(161,92,18,.12); color: var(--warn); }
.unit-status[data-status="accepted"] { background: rgba(15,107,92,.12); color: var(--accent); }
.dot { width: 7px; height: 7px; border-radius: 50%; background: #9aa; flex-shrink: 0; margin-top: 5px; }
.dot[data-status="reviewed"] { background: var(--ok); }
.dot[data-status="drafted"] { background: var(--warn); }
.dot[data-status="accepted"] { background: var(--accent); }
.tree-empty { color: var(--ink-soft); font-size: 12px; padding: 16px 10px; }
/* 卷分组 */
.volume-group { margin-bottom: 6px; }
.volume-head { display: flex; align-items: center; gap: 6px; width: 100%; border: none; background: transparent; padding: 8px 10px 6px; border-radius: 8px; text-align: left; }
.volume-head:hover:not(:disabled) { background: rgba(15,107,92,.06); }
.volume-caret { color: var(--ink-soft); font-size: 11px; width: 12px; }
.volume-label { font-size: 12px; font-weight: 700; color: var(--ink); }
.volume-progress { margin-left: auto; font-size: 10px; color: var(--ok); background: rgba(31,122,77,.1); padding: 1px 8px; border-radius: 20px; }
.volume-title { margin: 0 10px 6px 28px; font-size: 10px; color: var(--ink-soft); line-height: 1.5; overflow: hidden; text-overflow: ellipsis; display: -webkit-box; -webkit-line-clamp: 1; -webkit-box-orient: vertical; }
.volume-group .unit-row { margin-left: 6px; }
/* 单元工作流引导：侧栏窄，必须单行 */
.flow-guide { padding: 8px 4px 12px; border-bottom: 1px solid var(--line); margin-bottom: 10px; }
.flow-steps { display: flex; align-items: center; justify-content: space-between; gap: 0; flex-wrap: nowrap; width: 100%; min-width: 0; }
.flow-step { flex: 1 1 0; min-width: 0; font-size: 10px; padding: 3px 0; border-radius: 20px; border: 1px solid transparent; background: rgba(20,35,31,.05); color: var(--ink-soft); white-space: nowrap; cursor: pointer; text-align: center; line-height: 1.2; }
.flow-step:hover:not(:disabled) { border-color: rgba(15,107,92,.35); color: var(--ink); }
.flow-step:disabled { opacity: .45; cursor: not-allowed; }
.flow-step.active { background: var(--accent); color: white; font-weight: 700; }
.flow-step.done { background: rgba(31,122,77,.15); color: var(--ok); }
.flow-arrow { flex: 0 0 auto; color: var(--ink-soft); font-size: 9px; opacity: .45; padding: 0 1px; line-height: 1; }
.flow-note { margin: 10px 2px 0; font-size: 10px; color: var(--ink-soft); line-height: 1.7; }

/* 中：正文/编辑器 */
.editor-panel { min-height: 60vh; overflow: hidden; }
.editor-toolbar { display: flex; justify-content: space-between; align-items: center; gap: 10px; padding: 12px 16px; border-bottom: 1px solid var(--line); }
.path { font-size: 12px; color: var(--ink-soft); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.editor-actions { display: flex; gap: 8px; flex-wrap: wrap; flex-shrink: 0; }
.prose-editor {
  width: 100%; min-height: 64vh; border: none; resize: vertical; padding: 24px 28px;
  font-family: var(--font-prose); font-size: 17px; line-height: 1.9; color: var(--ink);
  background: transparent; outline: none;
}

/* 右：上下文侧栏 */
.context-panel { display: grid; gap: 14px; position: sticky; top: 12px; }
.side-card { padding: 16px; }
.side-card h4 { margin: 0 0 12px; font-size: 12px; font-weight: 700; color: var(--ink-soft); letter-spacing: .04em; display: flex; align-items: center; gap: 6px; }
.side-count { font-size: 10px; background: rgba(20,35,31,.06); padding: 1px 7px; border-radius: 20px; font-weight: 500; }
.next-step { display: flex; align-items: center; gap: 10px; }
.next-kind { font-size: 11px; padding: 3px 10px; border-radius: 8px; background: rgba(15,107,92,.1); color: var(--accent); font-weight: 600; flex-shrink: 0; }
.next-step strong { font-size: 14px; font-family: var(--font-prose); }
.side-note { margin: 12px 0 0; color: var(--ink-soft); font-size: 11px; line-height: 1.7; }
.cast-list { display: grid; gap: 2px; }
.cast-item { display: flex; align-items: center; gap: 10px; width: 100%; border: none; background: transparent; padding: 7px 6px; border-radius: 10px; text-align: left; }
.cast-item:hover:not(:disabled) { background: rgba(15,107,92,.06); }
.cast-avatar { width: 30px; height: 30px; border-radius: 50%; background: linear-gradient(135deg, rgba(15,107,92,.18), rgba(15,107,92,.06)); color: var(--accent); display: grid; place-items: center; font-size: 13px; font-weight: 700; font-family: var(--font-prose); flex-shrink: 0; }
.cast-info { display: grid; gap: 2px; min-width: 0; }
.cast-info strong { font-size: 13px; display: flex; align-items: center; gap: 6px; }
.role-badge { font-style: normal; font-size: 9px; padding: 1px 6px; border-radius: 6px; background: rgba(20,35,31,.06); color: var(--ink-soft); flex-shrink: 0; font-weight: 500; }
.role-badge[data-role="protagonist"] { background: rgba(15,107,92,.12); color: var(--accent); }
.role-badge[data-role="volume_antagonist"] { background: rgba(163,59,50,.1); color: var(--bad); }
.cast-info small { font-size: 10px; color: var(--ink-soft); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cast-more { margin-top: 10px; width: 100%; border: none; background: transparent; color: var(--accent); font-size: 12px; padding: 6px; }
.cast-more:hover:not(:disabled) { background: rgba(15,107,92,.06); }
.side-actions { display: grid; gap: 8px; }
.side-actions :deep(.unit-flow-actions) { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.side-actions :deep(.unit-flow-actions button) { width: 100%; padding: 10px; }
.side-actions > button { width: 100%; padding: 10px; }

/* 无正文单元：细纲工作区 */
.unit-work { padding: 24px 26px; display: grid; gap: 20px; }
.unit-work-head { display: flex; justify-content: space-between; align-items: flex-end; gap: 16px; }
.unit-work-head small { color: var(--accent); font-size: 11px; font-weight: 600; }
.unit-work-head h3 { margin: 6px 0 0; font-family: var(--font-prose); font-size: 24px; }
.unit-work-actions { display: flex; gap: 8px; flex-shrink: 0; flex-wrap: wrap; }
.raw-outline { border-top: 1px solid var(--line); padding-top: 4px; }
.raw-outline summary { padding: 10px 0; cursor: pointer; color: var(--ink-soft); font-size: 12px; }
.raw-outline pre { white-space: pre-wrap; font: 12px/1.7 ui-monospace, monospace; color: var(--ink-soft); background: rgba(255,255,255,.5); padding: 14px; border-radius: 10px; max-height: 320px; overflow: auto; }
.inline-contract { border-bottom: 1px solid var(--line); padding-bottom: 16px; }
.inline-contract summary { cursor: pointer; color: var(--accent); font-size: 12px; font-weight: 600; padding: 4px 0; }
.inline-contract[open] summary { margin-bottom: 14px; }

/* 待审候选 */
.candidates { margin-top: 28px; }
.candidates h2 { font-size: 17px; font-family: var(--font-prose); margin: 0 0 10px; }
.muted { color: var(--ink-soft); font-size: 12px; }
.candidate { padding: 18px; margin-bottom: 14px; }
.candidate header { display: flex; justify-content: space-between; align-items: center; gap: 10px; margin-bottom: 12px; }
.candidate header > div:last-child { display: flex; gap: 8px; flex-shrink: 0; }
.candidate-prose { white-space: pre-wrap; font-family: var(--font-prose); font-size: 14px; line-height: 1.9; max-height: 300px; overflow: auto; background: rgba(255,255,255,.5); padding: 16px; border-radius: 12px; border: 1px solid var(--line); margin: 0; }
.plan-candidate { display: grid; gap: 8px; }
.plan-mode { margin: 0; font-size: 13px; }
.plan-candidate details { border: 1px solid var(--line); border-radius: 10px; padding: 8px 12px; background: rgba(255,255,255,.4); }
.plan-candidate summary { cursor: pointer; font-size: 13px; font-weight: 600; }
.plan-files { margin: 8px 0 0; padding-left: 18px; font-size: 12px; color: var(--ink-soft); }
.finding { padding: 8px 12px; margin: 8px 0 0; border-radius: 10px; background: rgba(161,92,18,.08); font-size: 12px; }
.finding.blocking { background: rgba(163,59,50,.1); color: var(--bad); }

/* 运行抽屉 */
.run-drawer { position: fixed; right: 24px; bottom: 24px; width: min(400px, 92vw); padding: 18px; z-index: 30; }
.run-drawer header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.run-drawer p { margin: 6px 0; font-size: 13px; }
.call-row { font-size: 12px; padding: 8px 0; border-top: 1px solid var(--line); color: var(--ink-soft); }
.error-text { color: var(--bad); font-size: 12px; }

@media (max-width: 1180px) {
  .workspace.view-write { grid-template-columns: 210px minmax(0, 1fr); }
  .context-panel { grid-column: 1 / -1; grid-template-columns: repeat(3, minmax(0, 1fr)); position: static; }
}
@media (max-width: 720px) {
  .workspace { grid-template-columns: minmax(0, 1fr); }
  .book-head { flex-direction: column; align-items: flex-start; gap: 10px; }
  .unit-tree { max-height: 240px; position: static; }
  .context-panel { grid-template-columns: minmax(0, 1fr); }
  .editor-panel { min-height: 0; }
  .prose-editor { min-height: 48vh; padding: 18px 16px; font-size: 16px; }
  .editor-toolbar { align-items: flex-start; flex-direction: column; }
  .candidate header { flex-direction: column; align-items: flex-start; }
  .run-drawer { right: 16px; bottom: 16px; }
  .primary-task { align-items: stretch; flex-direction: column; gap: 12px; }
  .primary-task .primary { width: 100%; }
}
</style>
