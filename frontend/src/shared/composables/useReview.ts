/**
 * 审校页的全部视图模型：按单元分组的清单、勾选、章节筛选、历史归档。
 *
 * 桌面（ReviewWorkspace.vue）与移动端共用。这里只产出「该显示什么、能点什么」，
 * 不产出任何按钮文案以外的布局；emit 的那几个动作通过 opts 回调传出去，
 * 让每个壳自己决定点击后要不要换页。
 */
import { computed, onMounted, reactive, ref, watch, type Ref } from 'vue'
import { api } from '@/shared/api/client'
import { useAppStore } from '@/shared/stores/app'
import { STATUS_LABELS } from '@/shared/utils/runLabels'
import { isBlockingFinding, isProseFixable, isUnfinishedFinding, reviewVerdict } from '@/shared/utils/reviewState'
import type { Candidate, ReviewFinding, UnitInfo } from '@/shared/types'

export type ReviewRow = ReviewFinding & { candidateId: string; unitId?: string }

export function useReview(opts: {
  selectedUnit: Ref<UnitInfo | null>
  /** 只看某个单元（移动端单元详情页）；不传 = 全书。 */
  onlyUnit?: Ref<string | undefined>
  busy?: Ref<boolean | undefined>
  readOnly?: Ref<boolean | undefined>
  onRetrySplit?: (unitId?: string) => void
  onFocusUnit?: (unitId: string) => void
  onRejectUnit?: (unitId: string, candidateIds: string[]) => void
  onRevise?: (unitId: string, issueIds: string[]) => void
  onAccept?: (c: Candidate) => void
  onRejectCandidate?: (c: Candidate) => void
}) {
  const store = useAppStore()
  const selectedUnit = opts.selectedUnit
  const busy = opts.busy ?? ref<boolean | undefined>(false)
  const readOnly = opts.readOnly ?? ref<boolean | undefined>(false)

  /**
   * 审校页一次只显示「当前这一步」：清单 → 修复中 → diff 待落盘。
   *
   * 以前清单和页面底部的「待审候选」diff 同时存在：同一个单元的同一批问题在页面
   * 首尾各出现一次，作者点完「自动修复」得往下滚几屏才找得到产物，还能在 diff
   * 等待期间再点一次「自动修复」撞上服务端冲突。按单元就地切换后，问题清单在
   * 修复待落盘时让位给 diff，落盘或拒绝后再回来。
   */
  function latestRevise(unitId: string): Candidate | null {
    let latest: Candidate | null = null
    for (const c of store.candidates) {
      if (c.kind !== 'revise' || c.unitId !== unitId) continue
      if (!latest || new Date(c.createdAt) > new Date(latest.createdAt)) latest = c
    }
    return latest
  }

  /** 修订候选本身不记「修了哪几条」，唯一可信来源是它对应 run 的 IssueIDs。 */
  function addressedIds(c: Candidate): Set<string> {
    const run = store.runs.find((r) => r.id === c.runId)
    return new Set((run?.spec?.issueIds as string[] | undefined) || [])
  }

  /**
   * 修复卡片副标题：说清这次修了哪几条、还有多少没勾。
   *
   * 落盘会换掉整份正文，服务端据此把该单元所有未处理的审校清单归档（保留在
   * 历史里只读）——旧清单是按旧正文定位的，留着也没法继续修。所以这里不能
   * 承诺"未勾选的仍可继续修"，否则作者点完落盘会找不到那批问题。
   */
  function reviseSummary(g: { addressedRows: ReviewRow[]; remaining: number }) {
    const parts: string[] = []
    if (g.addressedRows.length) parts.push(`针对 ${g.addressedRows.length} 条`)
    if (g.remaining) parts.push(`另有 ${g.remaining} 条未勾选，落盘后随清单归档`)
    return parts.join(' · ') || '落盘后这份清单归档到历史'
  }

  /** 正在生成修复的单元：还没产出候选，但这一处也已经进入「只显示这一步」。 */
  function runningRevise(unitId: string) {
    const r = store.activeRun
    if (!r || r.kind !== 'revise' || r.spec?.unitId !== unitId) return null
    return r.status === 'running' || r.status === 'pending' ? r : null
  }

  // 全书范围：只渲染选中单元会让作者在审完 v02-U1 后看到"没有待处理问题"，
  // 因为他打开书时默认停在 v01-U1。按单元分组后，每个单元都能独立处理。
  const groups = computed(() => {
    const byUnit = new Map<string, { rows: ReviewRow[]; candidateIds: string[] }>()
    for (const c of store.candidates) {
      if (c.kind !== 'review') continue
      const key = c.unitId || '未归属'
      if (!byUnit.has(key)) byUnit.set(key, { rows: [], candidateIds: [] })
      const g = byUnit.get(key)!
      if (!g.candidateIds.includes(c.id)) g.candidateIds.push(c.id)
      for (const f of c.findings || []) {
        g.rows.push({ ...f, candidateId: c.id, unitId: c.unitId })
      }
    }
    return [...byUnit.entries()]
      .map(([unitId, g]) => {
        const revise = latestRevise(unitId)
        const addressed = revise ? addressedIds(revise) : new Set<string>()
        const addressedRows = g.rows.filter((f) => addressed.has(f.id))
        return {
          unitId,
          rows: g.rows,
          candidateIds: g.candidateIds,
          // 只数会真正阻断的条目：标题上的「N 阻断」必须与 verdict 用同一口径，
          // 否则会出现徽章说「7 阻断」、判定却认为可以前进的矛盾显示。
          blocking: g.rows.filter(isBlockingFinding).length,
          // 截断/分章失败的标记：有它就不是"没有问题"，而是"这章没审完"。
          unfinished: g.rows.filter(isUnfinishedFinding).length,
          // 整单元结论（状态账本/身份规划）：改正文修不了，不参与勾选，界面要说清。
          unitLevel: g.rows.filter((f) => !isProseFixable(f) && !isUnfinishedFinding(f)).length,
          verdict: reviewVerdict(g.rows),
          chapters: [...new Set(g.rows.map((f) => f.chapter).filter(Boolean))].sort((a, b) => (a as number) - (b as number)) as number[],
          revise,
          addressedRows,
          // 落盘后还能继续修的：这次没勾选、也不是「没审完」标记的条目。
          remaining: g.rows.filter((f) => !addressed.has(f.id) && !isUnfinishedFinding(f)).length,
          running: runningRevise(unitId),
        }
      })
      .sort((a, b) => a.unitId.localeCompare(b.unitId))
      .filter((g) => !opts.onlyUnit?.value || g.unitId === opts.onlyUnit.value)
  })

  const totalFindings = computed(() => groups.value.reduce((n, g) => n + g.rows.length, 0))
  const totalBlocking = computed(() => groups.value.reduce((n, g) => n + g.blocking, 0))
  const reviewRuns = computed(() => store.runs.filter((r) => ['review', 'finalize', 'revise'].includes(r.kind)))
  const latestRun = computed(() => reviewRuns.value[0])

  // 被拒绝的修复：正文没变，但这次返工要留痕，否则作者只会看到问题清单又回来了，
  // 却不知道上一轮为什么白跑。按单元归组，显示在对应单元标题下。
  const rejectedRevises = computed(() => {
    const byUnit = new Map<string, { reason: string; at: string }>()
    for (const r of store.runs) {
      if (r.kind !== 'revise' || r.status !== 'rejected') continue
      const unitId = r.spec?.unitId
      if (!unitId || byUnit.has(unitId)) continue
      byUnit.set(unitId, { reason: r.error || '未填写原因', at: r.createdAt })
    }
    return byUnit
  })

  const keyOf = (f: ReviewRow) => `${f.candidateId}::${f.id}`
  // 勾选按单元隔离：不同单元的问题不能混在一次修订里。
  const picked = reactive<Record<string, string[]>>({})
  const picksOf = (unitId: string) => picked[unitId] ?? []
  const isPicked = (f: ReviewRow, unitId: string) => picksOf(unitId).includes(keyOf(f))
  function toggle(f: ReviewRow, unitId: string) {
    const k = keyOf(f)
    const cur = picksOf(unitId)
    picked[unitId] = cur.includes(k) ? cur.filter((x) => x !== k) : [...cur, k]
  }

  // 默认勾选 blocking：审校给出的阻断项就是该修的那几条，作者可逐条取消。
  watch(
    () => store.candidates,
    () => {
      for (const g of groups.value) {
        const valid = new Set(g.rows.map(keyOf))
        const kept = picksOf(g.unitId).filter((k) => valid.has(k))
        // 默认只勾阻断项，且必须真的能靠正文补丁修：整单元结论（身份/状态账本）
        // 一旦预勾上，作者点「自动修复」就只会收到一条整批拒绝的错误。
        const def = g.rows
          .filter((f) => f.severity === 'blocking' && canPick(f))
          .map(keyOf)
        picked[g.unitId] = [...new Set([...kept, ...def])]
      }
    },
    { immediate: true, deep: false },
  )

  function issueIdsOf(g: { unitId: string; rows: ReviewRow[] }) {
    const ids: string[] = []
    for (const f of g.rows) {
      if (!canPick(f)) continue
      if (isPicked(f, g.unitId) && !ids.includes(f.id)) ids.push(f.id)
    }
    return ids
  }

  // 章筛选按单元独立。
  const chapterFilter = reactive<Record<string, number | 'all'>>({})
  const filterOf = (unitId: string) => chapterFilter[unitId] ?? 'all'
  function visibleRows(g: { unitId: string; rows: ReviewRow[] }) {
    const f = filterOf(g.unitId)
    return f === 'all' ? g.rows : g.rows.filter((r) => r.chapter === f)
  }

  // 可勾选 = 当前筛选下真正"要修的问题"。两类不参与：
  //   1. 截断/分章失败标记——送去修订只会让模型去"修"一条并不存在的问题；
  //   2. 没有章号的整单元结论（stale-*、身份规划）——正文补丁无从下手，
  //      服务端会整批拒绝，一条就足以让另外几十条白选。
  function canPick(f: ReviewRow) {
    return isProseFixable(f) && !isUnfinishedFinding(f)
  }
  function selectableRows(g: { unitId: string; rows: ReviewRow[] }) {
    return visibleRows(g).filter(canPick)
  }
  const allSelected = (g: { unitId: string; rows: ReviewRow[] }) => {
    const rows = selectableRows(g)
    return rows.length > 0 && rows.every((f) => isPicked(f, g.unitId))
  }
  /** 全选/取消全选只作用于当前筛选可见的行：筛到第 2 章时全选不会动别的章。 */
  function toggleAll(g: { unitId: string; rows: ReviewRow[] }) {
    const rows = selectableRows(g)
    if (!rows.length) return
    if (allSelected(g)) {
      const visible = new Set(rows.map(keyOf))
      picked[g.unitId] = picksOf(g.unitId).filter((k) => !visible.has(k))
      return
    }
    picked[g.unitId] = [...new Set([...picksOf(g.unitId), ...rows.map(keyOf)])]
  }

  /** 全选按钮文案：筛到某一章时说清作用范围，避免作者以为动了整份清单。 */
  function allLabel(g: { unitId: string; rows: ReviewRow[] }) {
    const scoped = filterOf(g.unitId) !== 'all'
    if (allSelected(g)) return scoped ? '取消本章全选' : '取消全选'
    return scoped ? '全选本章' : '全选'
  }

  /** 勾选框说明：为什么这一条不能送进「自动修复」。 */
  function pickTitle(f: ReviewRow) {
    if (isUnfinishedFinding(f)) return '「这一章没审完」的标记，不是正文问题：先重新审校'
    if (!isProseFixable(f)) return '整单元结论，没有对应章节：改正文修不了，需回规划/状态层处理'
    return '勾选后按这条问题做一次定向修订'
  }

  function canRevise(g: { unitId: string; rows: ReviewRow[] }) {
    return Boolean(!busy.value && !readOnly.value && issueIdsOf(g).length)
  }

  // 复制 finding 引用：供作者粘贴到追加指令或与他人沟通。
  function copyFinding(f: ReviewRow) {
    const text = `【第${f.chapter || '?'}章】${f.title}\n${f.evidence || ''}\n${f.suggestion ? '建议：' + f.suggestion : ''}`
    if (navigator.clipboard?.writeText) navigator.clipboard.writeText(text)
  }

  // 历史清单：拒绝的、被后续修复自动归档的都在这里，只读回看。
  const archivedByCandidate = computed(() =>
    store.candidateHistory.filter((c) => c.kind === 'review').slice(0, 20),
  )
  // 三类规则，界面上一句话讲清，避免作者以为它们可以混用：
  //   1. 程序规则（core/resource/novel/gates/deslop_rules.yaml）—— 引擎执行，永不进提示词
  //   2. 默认注入（27 → 25-review-checklist，front matter `default: true`）—— 每次自动带上
  //   3. 用户选择（知识库里其余带 review 标签的文章）—— 在运行确认窗口勾选
  const RULES_DOC_ID = '25-review-checklist'
  const libraryCustomized = ref(false)
  const selectableCount = ref(0)
  onMounted(async () => {
    try {
      const docs = await api.get<{ id: string; stages?: string[]; customized?: boolean; default?: boolean }[]>('/api/knowledge')
      libraryCustomized.value = Boolean(docs?.find((d) => d.id === RULES_DOC_ID)?.customized)
      // 可选项 = 带 review 标签但不是默认注入的文章。
      selectableCount.value = (docs || []).filter((d) => !d.default && (d.stages || []).includes('review')).length
    } catch { /* 知识库不可读时按内置处理 */ }
  })
  const rulesSource = computed<'book' | 'library' | 'builtin'>(() => {
    if ((store.currentBook?.files?.['canon/review-checklist.md'] || '').trim()) return 'book'
    return libraryCustomized.value ? 'library' : 'builtin'
  })
  const rulesSourceLabel = computed(() => ({
    book: '本书 canon/review-checklist.md',
    library: '知识库 25-review-checklist（作者已编辑）',
    builtin: '内置默认',
  }[rulesSource.value]))

  const archiveLabel: Record<string, string> = { accepted: '已归档', rejected: '已拒绝', expired: '已作废' }
  const runStatusLabel = (s: string) => STATUS_LABELS[s] || s
  function archivedAt(c: Candidate) {
    const ts = c.acceptedAt || c.createdAt
    return ts ? new Date(ts).toLocaleString() : ''
  }

  return {
    store, selectedUnit, busy, readOnly,
    groups, totalFindings, totalBlocking, reviewRuns, latestRun, rejectedRevises,
    picked, picksOf, isPicked, toggle, issueIdsOf,
    chapterFilter, filterOf, visibleRows, canPick, selectableRows, allSelected, toggleAll, allLabel,
    pickTitle, canRevise, copyFinding, archivedByCandidate,
    rulesSource, rulesSourceLabel, selectableCount, RULES_DOC_ID,
    archiveLabel, runStatusLabel, archivedAt,
    reviseSummary, keyOf,
    retrySplit: (unitId?: string) => opts.onRetrySplit?.(unitId),
    focusUnit: (unitId: string) => opts.onFocusUnit?.(unitId),
    rejectUnit: (unitId: string, ids: string[]) => opts.onRejectUnit?.(unitId, ids),
    revise: (unitId: string, ids: string[]) => opts.onRevise?.(unitId, ids),
    accept: (c: Candidate) => opts.onAccept?.(c),
    rejectCandidate: (c: Candidate) => opts.onRejectCandidate?.(c),
  }
}
