<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import { useAppStore } from '@/stores/app'
import { STATUS_LABELS } from '@/utils/runLabels'
import type { Candidate, ReviewFinding, UnitInfo } from '@/types'

const props = defineProps<{ selectedUnit: UnitInfo | null; busy?: boolean; readOnly?: boolean }>()
const emit = defineEmits<{
  revise: [unitId: string, issueIds: string[]]
  retrySplit: [unitId?: string]
  focusUnit: [unitId: string]
  rejectUnit: [unitId: string, candidateIds: string[]]
}>()
const store = useAppStore()

type Row = ReviewFinding & { candidateId: string; unitId?: string }

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
    .map(([unitId, g]) => ({
      unitId,
      rows: g.rows,
      candidateIds: g.candidateIds,
      blocking: g.rows.filter((f) => f.severity === 'blocking').length,
      chapters: [...new Set(g.rows.map((f) => f.chapter).filter(Boolean))].sort((a, b) => (a as number) - (b as number)) as number[],
    }))
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
      const def = g.rows.filter((f) => f.severity === 'blocking').map(keyOf)
      picked[g.unitId] = [...new Set([...kept, ...def])]
    }
  },
  { immediate: true, deep: false },
)

function issueIdsOf(g: { unitId: string; rows: Row[] }) {
  const ids: string[] = []
  for (const f of g.rows) if (isPicked(f, g.unitId) && !ids.includes(f.id)) ids.push(f.id)
  return ids
}

// 章筛选按单元独立。
const chapterFilter = reactive<Record<string, number | 'all'>>({})
const filterOf = (unitId: string) => chapterFilter[unitId] ?? 'all'
function visibleRows(g: { unitId: string; rows: Row[] }) {
  const f = filterOf(g.unitId)
  return f === 'all' ? g.rows : g.rows.filter((r) => r.chapter === f)
}

function canRevise(g: { unitId: string }) {
  return Boolean(!props.busy && !props.readOnly && issueIdsOf(g as any).length)
}

// 复制 finding 引用：供作者粘贴到追加指令或与他人沟通。
function copyFinding(f: Row) {
  const text = `【第${f.chapter || '?'}章】${f.title}\n${f.evidence || ''}\n${f.suggestion ? '建议：' + f.suggestion : ''}`
  if (navigator.clipboard?.writeText) navigator.clipboard.writeText(text)
}

// 历史清单：拒绝的、被后续修复自动结案的都在这里，只读回看。
// 审校页没有"归档"按钮：清单要么去修订，要么拒绝。
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

const archiveLabel: Record<string, string> = { accepted: '已结案', rejected: '已拒绝', expired: '已作废' }
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
        <p>审校只出问题清单。两条路：勾选后点「自动修复」看 diff 再确认落盘；或「拒绝清单」丢弃后自己重审。</p>
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
        {{ latestRun.error?.includes('截断') ? '重试并分章审校' : '重试审校' }}
      </button>
    </div>

    <div v-if="!groups.length" class="empty-state panel">
      <h3>全书没有待处理审校问题</h3>
      <p>到写作页对某个单元点「审校」；结果会按单元分组出现在这里。</p>
      <button v-if="selectedUnit?.hasProse" class="primary" :disabled="props.busy || props.readOnly"
        @click="emit('retrySplit', selectedUnit.unitId)">
        重新审校 {{ selectedUnit.unitId }}
      </button>
      <p v-if="archivedByCandidate.length" class="muted">已有 {{ archivedByCandidate.length }} 份历史清单，见页面底部。</p>
    </div>

    <section v-for="g in groups" :key="g.unitId" class="unit-group panel">
      <header class="unit-head">
        <div class="unit-title">
          <button class="unit-link" :title="`切换到 ${g.unitId}`" @click="emit('focusUnit', g.unitId)">{{ g.unitId }}</button>
          <span class="unit-badge" :data-severity="g.blocking ? 'blocking' : 'advisory'">
            {{ g.blocking ? `${g.blocking} 阻断` : '无阻断' }}
          </span>
          <span class="muted">{{ g.rows.length }} 条 · {{ g.chapters.length }} 章 · {{ g.candidateIds.length }} 份清单</span>
        </div>
        <div class="unit-actions">
          <select v-if="g.chapters.length > 1" :value="filterOf(g.unitId)"
            @change="chapterFilter[g.unitId] = ($event.target as HTMLSelectElement).value === 'all' ? 'all' : Number(($event.target as HTMLSelectElement).value)">
            <option value="all">全部章节</option>
            <option v-for="ch in g.chapters" :key="ch" :value="ch">第 {{ ch }} 章</option>
          </select>
          <span class="muted">已选 {{ issueIdsOf(g).length }}/{{ g.rows.length }}</span>
          <button class="ghost" :disabled="props.busy || props.readOnly"
            title="丢弃这份清单；之后要自己重新点审校"
            @click="emit('rejectUnit', g.unitId, g.candidateIds)">
            拒绝清单
          </button>
          <button class="primary" :disabled="props.busy || props.readOnly || !issueIdsOf(g).length"
            :title="issueIdsOf(g).length ? '按勾选问题自动修复，产出可 diff 的候选等你确认' : '先勾选要修的问题'"
            @click="emit('revise', g.unitId, issueIdsOf(g))">
            {{ props.busy ? '运行中…' : '自动修复' }}
          </button>
        </div>
      </header>

      <div class="finding-list">
        <div v-if="rejectedRevises.get(g.unitId)" class="rejected-note">
          <strong>上一次自动修复已丢弃</strong>
          <span class="muted">{{ rejectedRevises.get(g.unitId)!.reason }}</span>
          <small>正文未改变。调整下面的勾选（比如只留最关键的一条）后再点「自动修复」。</small>
        </div>
        <article v-for="f in visibleRows(g)" :key="keyOf(f)" class="finding" :class="f.severity === 'blocking' ? 'blocking' : 'advisory'">
          <div class="finding-head">
            <label class="pick"><input type="checkbox" :checked="isPicked(f, g.unitId)" @change="toggle(f, g.unitId)" /><span>选中</span></label>
            <button class="copy-btn" title="复制引用" @click="copyFinding(f)">复制</button>
          </div>
          <div class="finding-meta">
            <span>{{ f.source === 'local_gate' ? '本地硬门' : 'AI 审校' }}</span>
            <span v-if="f.chapter">第 {{ f.chapter }} 章</span>
            <span v-if="f.lens">{{ f.lens }}</span>
            <span class="sev" :data-severity="f.severity">{{ f.severity === 'blocking' ? '阻断' : '建议' }}</span>
          </div>
          <h4>{{ f.title }}</h4>
          <p v-if="f.evidence">{{ f.evidence }}</p>
          <p v-if="f.suggestion" class="suggestion">建议：{{ f.suggestion }}</p>
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
      <p class="archive-note">清单要么去修订、要么拒绝；这里只回看已处理的结论，单元状态不变（仍待定稿）。</p>
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
.unit-actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.unit-actions select { padding: 6px 10px; border: 1px solid var(--line); border-radius: 8px; font-size: 12px; }
.unit-actions .muted { font-size: 12px; color: var(--ink-soft); }
.unit-actions .ghost { background: transparent; border: 1px solid var(--line); color: var(--ink-soft); }
.unit-actions .ghost:hover:not(:disabled) { color: var(--bad); border-color: rgba(163,59,50,.35); }
.rules-bar { padding: 14px 18px; display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; }
.rules-bar h3 { margin: 0 0 6px; font-size: 14px; }
.rules-kinds { margin: 0; padding-left: 18px; color: var(--ink-soft); font-size: 12px; line-height: 1.75; }
.rules-kinds strong { color: var(--ink); }
.continuity-line { margin: 8px 0 0; color: var(--ink-soft); font-size: 12px; }
.rules-actions { flex-shrink: 0; display: flex; gap: 8px; flex-wrap: wrap; }
.ghost-link { font-size: 12px; color: var(--accent); text-decoration: none; padding: 6px 12px; border: 1px solid var(--line); border-radius: 8px; white-space: nowrap; }
.ghost-link:hover { border-color: var(--accent); }
.finding-list { padding: 4px 18px 12px; }
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
