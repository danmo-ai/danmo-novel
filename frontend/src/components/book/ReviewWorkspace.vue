<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import { useAppStore } from '@/stores/app'
import { STATUS_LABELS } from '@/utils/runLabels'
import { VERDICT_LABEL, isBlockingFinding, isProseFixable, isUnfinishedFinding, reviewVerdict } from '@/utils/reviewState'
import type { Candidate, ReviewFinding, UnitInfo } from '@/types'
import ChapterDiff from './ChapterDiff.vue'

const props = defineProps<{ selectedUnit: UnitInfo | null; busy?: boolean; readOnly?: boolean }>()
const emit = defineEmits<{
  revise: [unitId: string, issueIds: string[]]
  retrySplit: [unitId?: string]
  focusUnit: [unitId: string]
  rejectUnit: [unitId: string, candidateIds: string[]]
  /** 修订候选落盘：候选写正文、摘掉本次修掉的问题。 */
  accept: [c: Candidate]
  /** 丢弃这次修复：正文不变，清单原样留下。 */
  rejectCandidate: [c: Candidate]
}>()
const store = useAppStore()

type Row = ReviewFinding & { candidateId: string; unitId?: string }

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
function reviseSummary(g: { addressedRows: Row[]; remaining: number }) {
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
  const byUnit = new Map<string, { rows: Row[]; candidateIds: string[] }>()
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

const keyOf = (f: Row) => `${f.candidateId}::${f.id}`
// 勾选按单元隔离：不同单元的问题不能混在一次修订里。
const picked = reactive<Record<string, string[]>>({})
const picksOf = (unitId: string) => picked[unitId] ?? []
const isPicked = (f: Row, unitId: string) => picksOf(unitId).includes(keyOf(f))
function toggle(f: Row, unitId: string) {
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

function issueIdsOf(g: { unitId: string; rows: Row[] }) {
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
function visibleRows(g: { unitId: string; rows: Row[] }) {
  const f = filterOf(g.unitId)
  return f === 'all' ? g.rows : g.rows.filter((r) => r.chapter === f)
}

// 可勾选 = 当前筛选下真正"要修的问题"。两类不参与：
//   1. 截断/分章失败标记——送去修订只会让模型去"修"一条并不存在的问题；
//   2. 没有章号的整单元结论（stale-*、身份规划）——正文补丁无从下手，
//      服务端会整批拒绝，一条就足以让另外几十条白选。
function canPick(f: Row) {
  return isProseFixable(f) && !isUnfinishedFinding(f)
}
function selectableRows(g: { unitId: string; rows: Row[] }) {
  return visibleRows(g).filter(canPick)
}
const allSelected = (g: { unitId: string; rows: Row[] }) => {
  const rows = selectableRows(g)
  return rows.length > 0 && rows.every((f) => isPicked(f, g.unitId))
}
/** 全选/取消全选只作用于当前筛选可见的行：筛到第 2 章时全选不会动别的章。 */
function toggleAll(g: { unitId: string; rows: Row[] }) {
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
function allLabel(g: { unitId: string; rows: Row[] }) {
  const scoped = filterOf(g.unitId) !== 'all'
  if (allSelected(g)) return scoped ? '取消本章全选' : '取消全选'
  return scoped ? '全选本章' : '全选'
}

/** 勾选框说明：为什么这一条不能送进「自动修复」。 */
function pickTitle(f: Row) {
  if (isUnfinishedFinding(f)) return '「这一章没审完」的标记，不是正文问题：先重新审校'
  if (!isProseFixable(f)) return '整单元结论，没有对应章节：改正文修不了，需回规划/状态层处理'
  return '勾选后按这条问题做一次定向修订'
}

function canRevise(g: { unitId: string }) {
  return Boolean(!props.busy && !props.readOnly && issueIdsOf(g as any).length)
}

// 复制 finding 引用：供作者粘贴到追加指令或与他人沟通。
function copyFinding(f: Row) {
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
</script>

<template>
  <section class="review-workspace">
    <div class="view-title">
      <div>
        <p class="eyebrow">Quality</p>
        <h2>审校与定稿</h2>
        <p>审校只出问题清单，出口只有两个：勾选后「自动修复」——diff 就地展开在该单元里，确认落盘才写正文（落盘即完成这一轮修订，清单里没勾的仍留着可继续修）；或者「拒绝」——不同意这些结论，丢弃清单，之后需要时自己重新发起审校。定稿是写作页的独立步骤，不在审校面板上。</p>
      </div>
      <div class="review-totals">
        <span><strong>{{ totalBlocking }}</strong> 阻断</span>
        <span><strong>{{ totalFindings }}</strong> 总问题</span>
      </div>
    </div>

    <div class="rules-bar panel">
      <div>
        <h3>审校规则</h3>
        <ol class="rules-kinds">
          <li><strong>默认注入</strong>：{{ rulesSourceLabel }}——每次审校自动带上，不需勾选</li>
          <li><strong>本地硬门</strong>：程序正则表（不在知识库，永不进提示词），结果直接出现在下面</li>
          <li><strong>题材/技法参考</strong>：{{ selectableCount }} 篇可选，在写作页点审校时的确认窗口勾选</li>
        </ol>
        <p class="continuity-line">
          <template v-if="store.continuity?.units">
            连续性状态：<strong>{{ store.continuity.lastUnit }}</strong> 之后
            <span v-if="store.continuity.afterCh">· 已定稿到第 {{ store.continuity.afterCh }} 章</span>
            · {{ store.continuity.cast }} 人 · {{ store.continuity.openLoops }} 条未回收伏笔
          </template>
          <template v-else>
            连续性状态未建立：定稿第一个单元后自动生成，写作与审校都会读它。
          </template>
        </p>
      </div>
      <div class="rules-actions">
        <RouterLink class="ghost-link" :to="{ path: '/knowledge', query: { id: RULES_DOC_ID } }">
          编辑默认规则
        </RouterLink>
      </div>
    </div>

    <div v-if="latestRun?.status === 'failed'" class="run-error panel">
      <strong>最近一次审校失败</strong>
      <p>{{ latestRun.error }}</p>
      <small v-if="latestRun.error?.includes('截断')">输出被截断。改用分章审校：每章一次调用，不会撞输出预算。</small>
      <small v-else>已按调用预算停止，可修复后重试；正文未改变。</small>
      <button class="primary" @click="emit('retrySplit', latestRun.spec?.unitId)">
        {{ latestRun.error?.includes('截断') ? '重新审校（按章）' : '重新审校' }}
      </button>
    </div>

    <div v-if="!groups.length" class="empty-state panel">
      <h3>全书没有待处理审校问题</h3>
      <p>到写作页对某个单元点「审校」；结果会按单元分组出现在这里。清单处理完（全部修复或拒绝）后，回写作页点「定稿」回写账本。</p>
      <p v-if="selectedUnit?.hasProse" class="muted">没有待处理清单时，可重新审校一遍，或直接去写作页定稿。</p>
      <div v-if="selectedUnit?.hasProse" class="empty-actions">
        <button :disabled="props.busy || props.readOnly" @click="emit('retrySplit', selectedUnit.unitId)">
          重新审校 {{ selectedUnit.unitId }}
        </button>
      </div>
      <p v-if="archivedByCandidate.length" class="muted">已有 {{ archivedByCandidate.length }} 份历史清单，见页面底部。</p>
    </div>

    <section v-for="g in groups" :key="g.unitId" class="unit-group panel">
      <header class="unit-head">
        <div class="unit-title">
          <button class="unit-link" :title="`切换到 ${g.unitId}`" @click="emit('focusUnit', g.unitId)">{{ g.unitId }}</button>
          <span class="unit-badge" :data-severity="g.verdict === 'clean' ? 'advisory' : g.verdict">
            {{ g.verdict === 'blocking' ? `${g.blocking} 阻断` : VERDICT_LABEL[g.verdict] }}
          </span>
          <span class="muted">{{ g.rows.length }} 条 · {{ g.chapters.length }} 章 · {{ g.candidateIds.length }} 份清单</span>
        </div>
        <div class="unit-actions">
          <!-- 修复生成中/待落盘：这一组只处理当前这一步，勾选与「自动修复」先收起。 -->
          <span v-if="g.revise || g.running" class="muted">
            {{ g.running ? '正在生成修复…' : '修复待确认落盘' }}
          </span>
          <template v-else>
            <select v-if="g.chapters.length > 1" :value="filterOf(g.unitId)"
              @change="chapterFilter[g.unitId] = ($event.target as HTMLSelectElement).value === 'all' ? 'all' : Number(($event.target as HTMLSelectElement).value)">
              <option value="all">全部章节</option>
              <option v-for="ch in g.chapters" :key="ch" :value="ch">第 {{ ch }} 章</option>
            </select>
            <span class="muted">已选 {{ issueIdsOf(g).length }}/{{ selectableRows(g).length }}</span>
            <button class="ghost" :disabled="props.busy || props.readOnly"
              title="不同意这些审校结论：丢弃整份清单，正文不变。之后需要时自己重新发起审校"
              @click="emit('rejectUnit', g.unitId, g.candidateIds)">
              拒绝
            </button>
            <button class="primary" :disabled="props.busy || props.readOnly || !issueIdsOf(g).length"
              :title="issueIdsOf(g).length ? '按勾选问题自动修复，产出可 diff 的候选等你确认' : '先勾选要修的问题'"
              @click="emit('revise', g.unitId, issueIdsOf(g))">
              {{ props.busy ? '运行中…' : '自动修复' }}
            </button>
          </template>
        </div>
      </header>

      <!-- 待落盘的修订：清单区改成这一轮的 diff，不再和问题清单并排出现。 -->
      <section v-if="g.revise" class="revise-pending">
        <header class="revise-head">
          <div class="revise-title">
            <strong>本次修复 · 待确认落盘</strong>
            <span class="muted">{{ reviseSummary(g) }}</span>
          </div>
          <div class="revise-actions">
            <button class="ghost" :disabled="props.busy || props.readOnly"
              title="丢弃这次修复：正文不变，清单原样留下"
              @click="emit('rejectCandidate', g.revise)">拒绝这次修复</button>
            <button class="primary" :disabled="props.busy || props.readOnly"
              @click="emit('accept', g.revise)">确认落盘</button>
          </div>
        </header>
        <p class="revise-note">下面是相对现有正文的改动。确认无误再落盘：正文更新后这份清单归档到历史（未勾选的也不再生效），需要时按新正文重新审校。不认可这次改动就拒绝，正文不变。</p>
        <ChapterDiff :base="g.revise.baseChapters" :revised="g.revise.chapters || []" />
        <details v-if="g.addressedRows.length" class="revise-issues">
          <summary>本次针对的 {{ g.addressedRows.length }} 条问题</summary>
          <ul>
            <li v-for="f in g.addressedRows" :key="f.id">
              <span class="sev">{{ f.severity === 'blocking' ? '阻断' : '建议' }}</span>
              {{ f.title }}<span v-if="f.chapter" class="muted"> · 第 {{ f.chapter }} 章</span>
            </li>
          </ul>
        </details>
      </section>
      <section v-else-if="g.running" class="revise-pending running">
        <strong>正在按勾选生成修复…</strong>
        <p class="muted">生成期间这一组只显示这一步。完成后这里直接变成 diff，确认落盘才写正文。</p>
      </section>

      <div v-else class="finding-list">
        <!-- 全选按"当前筛选 + 可修"生效：筛到第 2 章只选第 2 章，截断标记不参与。 -->
        <div class="list-tools">
          <button class="ghost" :disabled="props.busy || props.readOnly || !selectableRows(g).length"
            :title="selectableRows(g).length ? '勾选当前筛选下所有可修问题' : '当前筛选下没有可修的问题'"
            @click="toggleAll(g)">
            {{ allLabel(g) }}
          </button>
          <span class="muted">可修 {{ selectableRows(g).length }} 条 · 已选 {{ issueIdsOf(g).length }}</span>
          <span v-if="g.unfinished" class="muted">· {{ g.unfinished }} 条审校不完整，不参与勾选</span>
          <span v-if="g.unitLevel" class="muted">· {{ g.unitLevel }} 条不属于正文，需回规划/状态层处理</span>
        </div>
        <!-- 没有结论 ≠ 没有问题：截断/分章失败的清单必须先补审，不能当成"审完了"。 -->
        <div v-if="g.verdict === 'unfinished'" class="unfinished-note">
          <strong>审校不完整</strong>
          <span class="muted">有章节的输出被预算截断或调用失败，剩下的结论覆盖不全。先重新审校该单元，再决定修还是拒绝。</span>
          <button class="ghost" :disabled="props.busy || props.readOnly" @click="emit('retrySplit', g.unitId)">
            重新审校
          </button>
        </div>
        <div v-if="rejectedRevises.get(g.unitId)" class="rejected-note">
          <strong>上一次自动修复已丢弃</strong>
          <span class="muted">{{ rejectedRevises.get(g.unitId)!.reason }}</span>
          <small>正文未改变。调整下面的勾选（比如只留最关键的一条）后再点「自动修复」。</small>
        </div>
        <article v-for="f in visibleRows(g)" :key="keyOf(f)" class="finding" :class="f.severity === 'blocking' ? 'blocking' : 'advisory'">
          <div class="finding-head">
            <label class="pick" :title="pickTitle(f)">
              <input type="checkbox" :disabled="!canPick(f)"
                :checked="isPicked(f, g.unitId)" @change="toggle(f, g.unitId)" /><span>{{ canPick(f) ? '选中' : '不勾选' }}</span>
            </label>
            <button class="copy-btn" title="复制引用" @click="copyFinding(f)">复制</button>
          </div>
          <div class="finding-meta">
            <span>{{ f.source === 'local_gate' ? '本地硬门' : 'AI 审校' }}</span>
            <span v-if="f.chapter">第 {{ f.chapter }} 章</span>
            <span v-else>整单元</span>
            <span v-if="f.lens">{{ f.lens }}</span>
            <!-- 多轮并集：命中次数越高越可能是可复现的硬伤，单轮命中的更可能是注意力偏好。 -->
            <span v-if="(f.roundsHit || 0) > 1" class="hits" :title="`多轮审校中有 ${f.roundsHit} 轮独立检出同一处，可复现性高`">×{{ f.roundsHit }}</span>
            <span class="sev" :data-severity="isUnfinishedFinding(f) ? 'unfinished' : f.severity">
              {{ isUnfinishedFinding(f) ? '不完整' : (f.severity === 'blocking' ? '阻断' : '建议') }}
            </span>
          </div>
          <h4>{{ f.title }}</h4>
          <p v-if="f.evidence">{{ f.evidence }}</p>
          <p v-if="f.suggestion" class="suggestion">建议：{{ f.suggestion }}</p>
          <!-- 没有章号的整单元结论：说清它不走「自动修复」，否则作者会以为漏了按钮。 -->
          <p v-if="!isProseFixable(f) && !isUnfinishedFinding(f)" class="suggestion">
            这条不属于任何一章正文，逐章修订改不了它：请按上面的建议回规划/状态层处理（人物状态过期 → 在该单元细纲的 state_deltas 里写显式终值）。
          </p>
        </article>
      </div>
    </section>

    <section v-if="reviewRuns.length" class="history panel">
      <h3>审校记录</h3>
      <div v-for="r in reviewRuns.slice(0, 10)" :key="r.id" class="history-row">
        <span>{{ r.kind }} · {{ r.spec?.unitId || '—' }}</span>
        <span>{{ runStatusLabel(r.status) }}</span>
        <span>调用 {{ r.callsUsed }}/{{ r.policy?.maxCalls || '—' }}</span>
      </div>
    </section>

    <details v-if="archivedByCandidate.length" class="archive panel">
      <summary><strong>历史审校清单</strong><span class="muted"> · {{ archivedByCandidate.length }} 份，只读</span></summary>
      <p class="archive-note">清单只有两个出口：勾选后自动修复，或拒绝丢弃。历史里的「已归档」= 这次修订已把它勾选的问题改掉（没勾的仍留在待处理清单里）。</p>
      <article v-for="c in archivedByCandidate" :key="c.id" class="archive-row">
        <div class="archive-head">
          <span class="archive-status" :data-status="c.status">{{ archiveLabel[c.status] || c.status }}</span>
          <span class="muted">{{ c.unitId || '—' }} · {{ archivedAt(c) }}</span>
          <span class="muted">· {{ (c.findings || []).length }} 条（阻断 {{ (c.findings || []).filter((f) => f.severity === 'blocking').length }}）</span>
        </div>
        <ul class="archive-findings">
          <li v-for="f in c.findings || []" :key="f.id" :class="{ blocking: f.severity === 'blocking' }">
            <span class="sev">{{ f.severity === 'blocking' ? '阻断' : '建议' }}</span>
            {{ f.title }}<span v-if="f.chapter" class="muted"> · 第 {{ f.chapter }} 章</span>
          </li>
        </ul>
      </article>
    </details>
  </section>
</template>

<style scoped>
.review-workspace { display: grid; gap: 18px; }
.view-title { display: flex; justify-content: space-between; gap: 20px; align-items: flex-end; }
.view-title h2 { margin: 4px 0 6px; font-family: var(--font-prose); font-size: 28px; }
.view-title p { margin: 0; color: var(--ink-soft); font-size: 13px; }
.eyebrow { color: var(--accent) !important; font-size: 11px !important; letter-spacing: .12em; text-transform: uppercase; }
.review-totals { display: flex; gap: 18px; flex-shrink: 0; font-size: 12px; color: var(--ink-soft); }
.review-totals strong { font-size: 20px; font-family: var(--font-prose); color: var(--ink); }
.unit-group { padding: 0; overflow: hidden; }
.unit-head { display: flex; justify-content: space-between; align-items: center; gap: 16px; padding: 14px 18px; border-bottom: 1px solid var(--line); background: rgba(15,107,92,.03); flex-wrap: wrap; }
.unit-title { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.unit-link { border: none; background: transparent; padding: 0; font: inherit; font-size: 15px; font-weight: 700; color: var(--accent); cursor: pointer; }
.unit-badge { font-size: 11px; padding: 2px 8px; border-radius: 999px; font-weight: 600; }
.unit-badge[data-severity='blocking'] { background: rgba(163,59,50,.12); color: var(--bad); }
.unit-badge[data-severity='advisory'] { background: rgba(15,107,92,.1); color: var(--accent); }
.unit-badge[data-severity='unfinished'] { background: rgba(176,124,28,.14); color: var(--warn, #8a5a00); }

.unit-actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.unit-actions select { padding: 6px 10px; border: 1px solid var(--line); border-radius: 8px; font-size: 12px; }
.unit-actions .muted { font-size: 12px; color: var(--ink-soft); }
.unit-actions .ghost { background: transparent; border: 1px solid var(--line); color: var(--ink-soft); }
.unit-actions .ghost:hover:not(:disabled) { color: var(--bad); border-color: rgba(163,59,50,.35); }
.empty-actions { display: flex; gap: 10px; justify-content: center; flex-wrap: wrap; }
.unfinished-note { display: grid; gap: 4px; margin: 12px 0 4px; padding: 10px 12px; border-radius: 10px; background: rgba(176,124,28,.08); border: 1px solid rgba(176,124,28,.25); font-size: 12px; justify-items: start; }
.unfinished-note strong { color: var(--warn, #8a5a00); font-size: 13px; }
.unfinished-note .muted { color: var(--ink-soft); line-height: 1.6; }
.unfinished-note button { margin-top: 4px; }
.rules-bar { padding: 14px 18px; display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; }
.rules-bar h3 { margin: 0 0 6px; font-size: 14px; }
.rules-kinds { margin: 0; padding-left: 18px; color: var(--ink-soft); font-size: 12px; line-height: 1.75; }
.rules-kinds strong { color: var(--ink); }
.continuity-line { margin: 8px 0 0; color: var(--ink-soft); font-size: 12px; }
.rules-actions { flex-shrink: 0; display: flex; gap: 8px; flex-wrap: wrap; }
.ghost-link { font-size: 12px; color: var(--accent); text-decoration: none; padding: 6px 12px; border: 1px solid var(--line); border-radius: 8px; white-space: nowrap; }
.ghost-link:hover { border-color: var(--accent); }
/* 待落盘的修复：这一组此刻只显示这一步，diff 和落盘按钮同屏，不再和清单并排。 */
.revise-pending { display: grid; gap: 10px; padding: 14px 18px 16px; }
.revise-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; flex-wrap: wrap; }
.revise-title { display: grid; gap: 3px; }
.revise-title strong { font-size: 14px; color: var(--accent); }
.revise-note { margin: 0; color: var(--ink-soft); font-size: 12px; line-height: 1.7; }
.revise-actions { display: flex; gap: 8px; flex-wrap: wrap; flex-shrink: 0; }
.revise-actions .ghost { background: transparent; border: 1px solid var(--line); color: var(--ink-soft); }
.revise-actions .ghost:hover:not(:disabled) { color: var(--bad); border-color: rgba(163,59,50,.35); }
.revise-issues { border: 1px solid var(--line); border-radius: 10px; padding: 8px 12px; background: rgba(255,255,255,.5); }
.revise-issues summary { cursor: pointer; font-size: 12px; color: var(--ink-soft); }
.revise-issues ul { margin: 8px 0 0; padding-left: 18px; font-size: 12px; line-height: 1.8; color: var(--ink-soft); }
.revise-issues .sev { font-weight: 600; margin-right: 4px; }
.revise-pending.running { padding: 18px; }
.revise-pending.running strong { color: var(--accent); font-size: 13px; }
.revise-pending.running p { margin: 4px 0 0; }
.finding-list { padding: 4px 18px 12px; }
.list-tools { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; padding: 10px 0 8px; border-bottom: 1px solid var(--line); }
.list-tools .ghost { background: transparent; border: 1px solid var(--line); color: var(--ink-soft); padding: 4px 10px; font-size: 12px; }
.list-tools .ghost:hover:not(:disabled) { color: var(--accent); border-color: var(--accent); }
.list-tools .muted { font-size: 12px; color: var(--ink-soft); }
.rejected-note { display: grid; gap: 3px; margin: 12px 0 4px; padding: 10px 12px; border-radius: 10px; background: rgba(163,59,50,.06); border: 1px solid rgba(163,59,50,.18); font-size: 12px; }
.rejected-note strong { color: var(--bad); font-size: 13px; }
.rejected-note small { color: var(--ink-soft); line-height: 1.6; }
.finding { border-top: 1px solid var(--line); padding: 12px 0; }
.finding:first-child { border-top: none; }
.finding-head { display: flex; justify-content: space-between; align-items: center; gap: 10px; }
.pick { display: inline-flex; align-items: center; gap: 6px; font-size: 11px; color: var(--ink-soft); cursor: pointer; }
.copy-btn { border: none; background: transparent; color: var(--accent); font-size: 11px; cursor: pointer; padding: 0; }
.finding-meta { display: flex; gap: 8px; color: var(--ink-soft); font-size: 11px; margin-top: 4px; flex-wrap: wrap; }
.finding-meta .sev { font-weight: 600; }
.finding-meta .sev[data-severity='blocking'] { color: var(--bad); }
.finding-meta .sev[data-severity='unfinished'] { color: var(--warn, #8a5a00); }
/* 多轮并集命中次数：越高越可复现，所以给一点视觉权重。 */
.finding-meta .hits { padding: 0 6px; border-radius: 999px; background: rgba(163,59,50,.1); color: var(--bad); font-weight: 600; }
.finding h4 { margin: 6px 0; font-size: 14px; }
.finding.blocking h4 { color: var(--bad); }
.finding p { margin: 4px 0; color: var(--ink-soft); font-size: 13px; line-height: 1.7; }
.suggestion { color: var(--accent) !important; }
.empty-state { padding: 32px; text-align: center; }
.run-error { padding: 16px; border-color: rgba(163,59,50,.3); display: grid; gap: 8px; justify-items: start; }
.run-error strong { color: var(--bad); }
.run-error p { color: var(--ink-soft); font-size: 13px; margin: 0; }
.run-error small { color: var(--ink-soft); }
.history { padding: 18px; }
.history h3 { margin: 0 0 10px; font-size: 15px; }
.history-row { display: flex; justify-content: space-between; gap: 10px; border-top: 1px solid var(--line); padding: 10px 0; font-size: 12px; color: var(--ink-soft); }
.archive { padding: 18px; }
.archive summary { cursor: pointer; font-size: 15px; }
.archive-note { margin: 10px 0 0; color: var(--ink-soft); font-size: 12px; }
.archive-row { border-top: 1px solid var(--line); padding: 12px 0 4px; }
.archive-head { display: flex; align-items: center; gap: 8px; font-size: 12px; flex-wrap: wrap; }
.archive-status { font-size: 11px; padding: 2px 8px; border-radius: 999px; background: rgba(15,107,92,.1); color: var(--accent); font-weight: 600; }
.archive-status[data-status='rejected'] { background: rgba(163,59,50,.1); color: var(--bad); }
.archive-status[data-status='expired'] { background: rgba(0,0,0,.06); color: var(--ink-soft); }
.archive-findings { margin: 8px 0 0; padding-left: 18px; color: var(--ink-soft); font-size: 12px; line-height: 1.8; }
.archive-findings li.blocking .sev { color: var(--bad); }
.archive-findings .sev { font-weight: 600; margin-right: 4px; }
@media (max-width: 720px) {
  .view-title { flex-direction: column; align-items: flex-start; }
  .unit-head { flex-direction: column; align-items: flex-start; }
  .history-row { flex-direction: column; }
}
</style>
