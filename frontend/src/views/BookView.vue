<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { api } from '@/api/client'
import type { Candidate, CallRecord, UnitInfo } from '@/types'
import OutlineWorkspace from '@/components/book/OutlineWorkspace.vue'
import BibleWorkspace from '@/components/book/BibleWorkspace.vue'
import ProjectSetup from '@/components/book/ProjectSetup.vue'
import ReviewWorkspace from '@/components/book/ReviewWorkspace.vue'
import ChapterDiff from '@/components/book/ChapterDiff.vue'
import PlanCandidateDiff from '@/components/book/PlanCandidateDiff.vue'
import StructuredOutline from '@/components/book/StructuredOutline.vue'
import UnitProse from '@/components/book/UnitProse.vue'
import UnitContract from '@/components/book/UnitContract.vue'
import UnitFlowActions from '@/components/book/UnitFlowActions.vue'
import RunConfirmDialog from '@/components/book/RunConfirmDialog.vue'
import { planModeLabel as sharedPlanModeLabel, runKindLabel } from '@/utils/runLabels'
import { cacheStat, callCacheText, hitRateText } from '@/utils/tokenStats'
import { parseStructuredMarkdown } from '@/utils/structuredMarkdown'

const route = useRoute()
const store = useAppStore()
const bookId = computed(() => route.params.id as string)
const view = ref<'setup' | 'write' | 'outline' | 'bible' | 'review'>('write')
const selectedUnit = ref<UnitInfo | null>(null)
const editorPath = ref('')
const editorContent = ref('')
const saving = ref(false)
const busy = ref(false)
const modelId = ref('')
const runDrawer = ref(false)
/**
 * 待审候选 = 「接受后会写文件」的候选。审校清单不在其中：它不写正文，唯一归属地是
 * 审校页。两处都渲染同一批 findings 只会让作者在两份列表之间来回跳。
 */
const pendingCandidates = computed(() => store.candidates.filter((c) => c.kind !== 'review'))
/**
 * 审校页的修订候选已经在该单元的清单区就地展开（diff + 确认落盘），底部不再重复
 * 一份。立项页的提案/立项候选同理：ProjectSetup 里已经带 diff 与接受按钮，
 * 底部再列一遍只会让人以为有两批待办。
 */
const listedCandidates = computed(() => {
  if (view.value === 'setup') {
    return pendingCandidates.value.filter((c) => c.kind !== 'plan' && c.kind !== 'idea')
  }
  if (view.value !== 'review') return pendingCandidates.value
  const reviewUnits = new Set(store.candidates.filter((c) => c.kind === 'review' && c.unitId).map((c) => c.unitId))
  return pendingCandidates.value.filter((c) => !(c.kind === 'revise' && c.unitId && reviewUnits.has(c.unitId)))
})
/** 定稿候选里"回证未在正文中找到"的 advisory：数量要在卡片顶部点出来。 */
function unverifiedEvidence(c: Candidate) {
  return (c.findings || []).filter((f) => String(f.id || '').startsWith('evidence-'))
}

/** 实时日志的缓存汇总：重试（step=repair）命中率应接近 100%。 */
function runCacheOf(run: { calls?: CallRecord[] } | null | undefined) { return cacheStat(run?.calls) }
const pollTimer = ref<number | null>(null)
const customInstruction = ref('')
const editingProse = ref(false)
const editingOutline = ref(false)
const isProseUnit = computed(() => Boolean(selectedUnit.value?.hasProse && selectedUnit.value.prosePath === editorPath.value))
const stageLabels: Record<string, string> = { idea: '创意扩写', plan: '立项', outline: '细纲', write: '写作', review: '审校', structure_review: '结构审校', outline_review: '细纲检查', finalize: '定稿', revise: '修订', local_edit: '局部编辑', rewrite_entity: '重写设定' }
const hasBookOutline = computed(() => Boolean((store.currentBook?.files?.['outline/book_outline.md'] || '').trim()))
// 立项工作台只服务「还没有总纲」的书：它是 idea loop 到一次结构化立项的入口。
// 总纲一旦落盘，优化与重新立项都归大纲 Tab，避免两个页面做同一件事。
const showSetupTab = computed(() => !hasBookOutline.value)
/** 创意提案是否已确认：提案段有内容才算，空模板不算。 */
const hasIdeaProposal = computed(() => {
  const raw = store.currentBook?.files?.['outline/idea.md'] || ''
  const idx = raw.split('\n').findIndex((line) => line.trim() === '## 创意提案')
  if (idx < 0) return false
  return raw.split('\n').slice(idx + 1).join('\n').replace(/<!--[\s\S]*?-->/g, '').trim().length > 0
})
const hasVolumes = computed(() => (store.currentBook?.volumes?.length || 0) > 0)
// 单元驱动工作流：proposed→细纲 → accepted→正文 → drafted→审校 → reviewed→定稿核对 → finalized。
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
  const idx = { proposed: 0, accepted: 1, drafted: 2, reviewed: 3, finalized: 4 }[u.status] ?? 0
  return { active: idx, done: u.status === 'finalized' ? 4 : idx - 1 }
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
    if (!hasBookOutline.value) {
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
  if (!hasBookOutline.value) view.value = 'setup'
})
onUnmounted(() => {
  if (pollTimer.value) clearInterval(pollTimer.value)
})

watch(selectedUnit, () => { editingProse.value = false; editingOutline.value = false; void openSelected() })
watch(view, () => {
  void openSelected()
})
// 立项候选被接受后总纲才出现：此刻作者还停在一个已经不该存在的 Tab 上，
// 必须在同一帧迁走，否则页面剩一片空白。
watch(hasBookOutline, (done) => {
  if (done && view.value === 'setup') view.value = 'outline'
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
    await api.put(`/api/books/${bookId.value}/file`, { path, content, baseRevision: store.currentBook?.revision })
    store.note('已保存')
    await reload()
  } catch (e) { store.fail(e) } finally { saving.value = false }
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
  const book = store.currentBook
  if (!book || !c.targetPath) return
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

function candidateStructure(c: Candidate, kind: 'book' | 'volume') {
  const raw = kind === 'book' ? c.planMarkdown?.book_outline : c.planMarkdown?.volume_outline
  if (!raw) return null
  return parseStructuredMarkdown(raw, kind === 'book' ? 'book-outline/v1' : 'volume-outline/v1')
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

/** 对当前选中单元启动生产/重做；审校与定稿会切到审校页便于看结果。 */
function runUnitAction(kind: 'outline' | 'write' | 'review' | 'finalize' | 'outline_review') {
  if (!selectedUnit.value || busy.value) return
  if (kind === 'review' || kind === 'finalize' || kind === 'outline_review') view.value = 'review'
  else if (kind === 'outline' || kind === 'write') view.value = 'write'
  start(kind)
}

function runFlowStep(key: string) {
  if (!selectedUnit.value) return
  if (key === 'outline' || key === 'write' || key === 'review' || key === 'finalize' || key === 'outline_review') {
    runUnitAction(key)
  }
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
    // 归档后清单从「待审候选」消失，必须在历史里还能回看。
    await Promise.all([store.loadRuns(bookId.value), store.loadCandidateHistory(bookId.value)])
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

/** 轮询一个已在服务端跑起来的 run（例如定稿）。 */
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
  proposed: '待细纲', accepted: '待写', drafted: '草稿', reviewed: '已审校', finalized: '定稿',
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
      <button v-if="showSetupTab" role="tab" :aria-selected="view === 'setup'" :class="{ active: view === 'setup' }" @click="view = 'setup'">立项</button>
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

      <ProjectSetup
        v-if="view === 'setup' && showSetupTab"
        :busy="busy"
        :saving="saving"
        @save="saveFile"
        @idea="startIdea"
        @plan="(mode, volume) => startPlan(mode, volume)"
        @accept="accept"
        @reject="reject"
      />
      <OutlineWorkspace
        v-else-if="view === 'outline'"
        :saving="saving"
        :busy="busy"
        @open-unit="selectedUnit = $event; view = 'write'"
        @save="saveFile"
        @generate="start"
        @save-field="saveOutlineField"
        @structure-review="reviewStructure"
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
        @accept="accept"
        @reject-candidate="reject"
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
        <section v-if="hasBookOutline" class="side-card panel side-actions">
          <!-- 重新立项的唯一入口在大纲 Tab；这里只是同一动作的快捷方式。 -->
          <button :disabled="busy || store.currentBook.summary.readOnly" @click="startPlan('reinit')">重新立项</button>
        </section>
      </aside>
    </div>

    <section v-if="listedCandidates.length" class="candidates">
      <h2>待审候选</h2>
      <!--
        只列「接受后会写文件」的候选。审校清单不在这里：它不写正文，唯一归属地是
        审校页。修订候选在审校页按单元就地展开 diff（清单区切换成当前这一步），
        所以这里只在其他页面列它，避免同一份 diff 在页面首尾各出现一次。
        也不放"另有 N 条审校结论"的跳转提示——那既不能处理问题，又让人以为清单
        丢了一份，属于噪音；整块为空时直接不渲染。
      -->
      <article v-for="c in listedCandidates" :key="c.id" class="candidate panel">
        <header>
          <div>
            <strong>{{ c.kind === 'plan' ? planModeLabel(c.mode) : (stageLabels[c.kind] || c.kind) }} · {{ candidateTarget(c) }}</strong>
            <span class="muted"> · {{ new Date(c.createdAt).toLocaleString() }}</span>
          </div>
          <div>
            <button v-if="c.kind === 'structure_review' && c.findings?.some((f) => f.severity === 'blocking')" class="primary" @click="repairStructure(c)">按问题修复</button>
            <button @click="reject(c)">拒绝</button>
            <button class="primary" @click="accept(c)">{{ c.kind === 'revise' ? '确认落盘' : '接受' }}</button>
          </div>
        </header>
        <p v-if="c.kind === 'structure_review'" class="muted">结构编辑只审查父级继承、因果、兑现和状态。按问题修复会生成新的文档候选，接受后才会写回 Markdown。</p>
        <!-- 修订候选：先看 diff 再落盘，接受不能是盲签。 -->
        <template v-else-if="c.kind === 'revise'">
          <p class="muted">下面是相对现有正文的改动。确认无误后点「确认落盘」覆盖正文，这次修订即告完成；落盘后旧正文清单按版本归档，不再把已勾选等同于已修复；需要时请按新正文重新审校。</p>
          <ChapterDiff :base="c.baseChapters" :revised="c.chapters || []" />
        </template>
        <template v-else-if="c.kind === 'finalize' && c.actualChapters?.length">
          <p class="muted">逐章核对实际正文；只有作者接受后，以下结果才进入连续性账本。</p>
          <!-- 回证未命中不是错误：引文抄错字形不等于迁移错了。但必须让作者看见，
               否则他会以为每条迁移都对照过正文。 -->
          <p v-if="unverifiedEvidence(c).length" class="warn-text">
            有 {{ unverifiedEvidence(c).length }} 条状态迁移的回证在本章正文里找不到（下面标出）。接受时会记入定稿日志；
            请对照正文确认 from → to 是否成立，不确定就先不接受。
          </p>
          <details v-for="ch in c.actualChapters" :key="ch.chapter" open>
            <summary>第 {{ ch.chapter }} 章 · {{ ch.summary }}</summary>
            <ul class="plan-files">
              <li v-for="d in ch.stateDeltas || []" :key="`${d.stem}-${d.field}-${d.evidence}`">{{ d.stem }} · {{ d.field }}：{{ d.from }} → {{ d.to }}（{{ d.evidence }}）</li>
              <li v-if="ch.plant?.length">实际埋下：{{ ch.plant.join('、') }}</li>
              <li v-if="ch.payoff?.length">实际回收：{{ ch.payoff.join('、') }}</li>
            </ul>
          </details>
        </template>
        <pre v-else-if="c.chapters" class="candidate-prose">{{ chapterText(c) }}</pre>
        <pre v-else-if="c.unitOutlines" class="candidate-prose">{{ JSON.stringify(c.unitOutlines, null, 2) }}</pre>
        <template v-else-if="c.kind === 'idea'">
          <p class="muted">自由格式的创意提案：接受后只写 outline/idea.md，不写总纲、设定或人物。</p>
          <pre class="candidate-prose">{{ c.replacement }}</pre>
        </template>
        <pre v-else-if="c.replacement" class="candidate-prose">{{ c.replacement }}</pre>
        <div v-else-if="c.planMarkdown || c.planSettings" class="plan-candidate">
          <p class="plan-mode"><strong>{{ planModeLabel(c.mode) }}</strong><span class="muted"> · 接受后写入 {{ planCandidateFiles(c).length }} 个文件</span></p>
          <template v-if="c.mode === 'volume'">
            <details v-if="c.planMarkdown?.volume_outline" open><summary>卷纲 · {{ c.volumeId }}</summary><StructuredOutline v-if="candidateStructure(c, 'volume') && !candidateStructure(c, 'volume')!.legacy" :document="candidateStructure(c, 'volume')!" compact read-only /><pre v-else class="candidate-prose">{{ c.planMarkdown.volume_outline }}</pre></details>
            <p class="muted">只写当前卷纲，并播种索引中尚不存在的单元；总纲/设定/人物/正文不变。</p>
          </template>
          <template v-else>
            <details v-if="c.planMarkdown?.book_outline" open><summary>总纲</summary><StructuredOutline v-if="candidateStructure(c, 'book') && !candidateStructure(c, 'book')!.legacy" :document="candidateStructure(c, 'book')!" compact read-only /><pre v-else class="candidate-prose">{{ c.planMarkdown.book_outline }}</pre></details>
            <details v-if="c.planSettings && Object.keys(c.planSettings).length"><summary>设定 · {{ Object.keys(c.planSettings).length }} 项</summary>
              <ul class="plan-files"><li v-for="p in Object.keys(c.planSettings)" :key="p">{{ p }}</li></ul>
            </details>
            <details v-if="c.planMarkdown?.cast_json"><summary>人物</summary><pre class="candidate-prose">{{ planCastPreview(c) }}</pre></details>
            <p class="muted">立项只写总纲派生设定与人物；不写卷纲、不播种单元。</p>
          </template>
          <p v-if="(c as any).preservePaths?.length" class="muted">保留不覆盖：{{ ((c as any).preservePaths as string[]).join('、') }}</p>
          <details open><summary>变更预览（相对当前 canon）</summary><PlanCandidateDiff :candidate="c" :files="store.currentBook?.files || {}" /></details>
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
        <p v-for="w in store.activeRun.warnings || []" :key="w" class="warn-text">{{ w }}</p>
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
.unit-status[data-status="reviewed"], .unit-status[data-status="finalized"] { background: rgba(31,122,77,.12); color: var(--ok); }
.unit-status[data-status="drafted"] { background: rgba(161,92,18,.12); color: var(--warn); }
.unit-status[data-status="accepted"] { background: rgba(15,107,92,.12); color: var(--accent); }
.dot { width: 7px; height: 7px; border-radius: 50%; background: #9aa; flex-shrink: 0; margin-top: 5px; }
.dot[data-status="reviewed"], .dot[data-status="finalized"] { background: var(--ok); }
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
/* 退路提示：跑成了但多花了钱，措辞要比错误轻、比普通说明显眼。 */
.warn-text { color: var(--warn, #b7791f); font-size: 12px; }

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
