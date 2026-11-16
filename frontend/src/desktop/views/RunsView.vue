<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '@/shared/api/client'
import { useAppStore } from '@/shared/stores/app'
import type { CallRecord, Run } from '@/shared/types'
import { KIND_LABELS, STATUS_LABELS, runKindLabel } from '@/shared/utils/runLabels'
import { cacheStat, callCacheText, hitRateText } from '@/shared/utils/tokenStats'

const store = useAppStore()
const runs = ref<Run[]>([])
const loading = ref(true)
const bookFilter = ref('')
const kindFilter = ref('')
const modelFilter = ref('')
const statusFilter = ref('')
const query = ref('')
const expanded = ref('')
/** 展开行的完整 run（含逐调用明细）；列表本身只有预聚合汇总。 */
const expandedRun = ref<Run | null>(null)

const kindLabels = KIND_LABELS
const statusLabels: Record<string, string> = { ...STATUS_LABELS }
const models = computed(() => [...new Set(runs.value.map((r) => r.modelId).filter(Boolean))].sort())
const filtered = computed(() => runs.value.filter((r) => {
  if (bookFilter.value && r.bookId !== bookFilter.value) return false
  if (kindFilter.value && r.kind !== kindFilter.value) return false
  if (modelFilter.value && r.modelId !== modelFilter.value) return false
  if (statusFilter.value && r.status !== statusFilter.value) return false
  if (query.value && !`${r.bookId} ${r.modelId} ${r.spec.unitId || ''} ${r.error || ''}`.toLowerCase().includes(query.value.toLowerCase())) return false
  return true
}))
// 列表行是精简版：汇总读预聚合字段；展开的完整 run 仍可从 calls 计算。
function runInput(r: Run) { return r.inputTokens ?? (r.calls || []).reduce((n, c) => n + (c.promptTokens || 0), 0) }
function runOutput(r: Run) { return r.outputTokens ?? (r.calls || []).reduce((n, c) => n + (c.completionTokens || 0), 0) }
function runDurationMs(r: Run) { return r.durationMs ?? (r.calls || []).reduce((n, c) => n + (c.durationMs || 0), 0) }
const totals = computed(() => {
  const acc = filtered.value.reduce((a, r) => {
    a.calls += r.callsUsed || 0
    a.input += runInput(r)
    a.output += runOutput(r)
    a.duration += runDurationMs(r)
    a.cacheRead += r.cacheRead ?? (r.calls || []).reduce((n, c) => n + (c.cacheReadTokens || 0), 0)
    a.cacheCreation += r.cacheCreation ?? (r.calls || []).reduce((n, c) => n + (c.cacheCreationTokens || 0), 0)
    return a
  }, { calls: 0, input: 0, output: 0, duration: 0, cacheRead: 0, cacheCreation: 0 })
  // 缓存命中是"重试成本"的直接指标：命中率接近 100% 说明修复重试几乎只付
  // repair 那几十 token 的钱。按当前筛选范围汇总，口径与输入 Token 一致。
  const cache = {
    read: acc.cacheRead, creation: acc.cacheCreation, prompt: acc.input,
    ratio: acc.input > 0 ? acc.cacheRead / acc.input : 0,
    hasData: acc.cacheRead > 0 || acc.cacheCreation > 0,
  }
  return { ...acc, cache }
})
function bookTitle(id: string) { return store.books.find((b) => b.id === id)?.title || id }
function date(iso: string) { return new Date(iso).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' }) }
function duration(run: Run) { const ms = runDurationMs(run); return ms ? `${(ms / 1000).toFixed(1)}s` : '—' }
function tokens(run: Run, key: 'promptTokens' | 'completionTokens') { return key === 'promptTokens' ? runInput(run) : runOutput(run) }
function runCache(run: Run) {
  if (run.cacheRead !== undefined || run.cacheCreation !== undefined) {
    const read = run.cacheRead ?? 0
    const creation = run.cacheCreation ?? 0
    const prompt = runInput(run)
    return { read, creation, prompt, ratio: prompt > 0 ? read / prompt : 0, hasData: read > 0 || creation > 0 }
  }
  return cacheStat(run.calls)
}
function cacheHit(c: CallRecord) { return callCacheText(c) }
function target(run: Run) {
  const spec = run.spec || {}
  // plan 任务目标是全书或卷，忽略误带的 unitId（历史记录里常被写成 U1）。
  if (run.kind === 'plan') {
    if (spec.mode === 'volume' || spec.volumeId) return spec.volumeId || '当前卷'
    return '全书'
  }
  if (run.kind === 'idea') return 'outline/idea.md'
  if (run.kind === 'rewrite_entity') return spec.targetPath || '设定'
  if (run.kind === 'outline' && spec.volumeId && !spec.unitId) return spec.volumeId
  return spec.unitId || spec.volumeId || (spec.chapter ? `第 ${spec.chapter} 章` : '全书')
}
async function load() {
  loading.value = true
  try {
    await store.loadBooks()
    runs.value = (await api.get<Run[] | null>('/api/runs?limit=200')) ?? []
  } catch (e) { store.fail(e) } finally { loading.value = false }
}
async function toggleExpand(id: string) {
  if (expanded.value === id) { expanded.value = ''; expandedRun.value = null; return }
  expanded.value = id
  expandedRun.value = null
  try {
    expandedRun.value = await api.get<Run>(`/api/runs/${id}`)
  } catch (e) { store.fail(e) }
}
function reset() { bookFilter.value = ''; kindFilter.value = ''; modelFilter.value = ''; statusFilter.value = ''; query.value = '' }
onMounted(load)
</script>

<template>
  <section class="runs-page">
    <div class="page-head">
      <div><p class="eyebrow">Model Logs</p><h1>模型日志</h1><p>排查每本书的模型调用、输入输出、阶段、成本与失败原因。</p></div>
      <button @click="load">刷新</button>
    </div>
    <div class="summary-grid">
      <div class="summary panel"><small>任务</small><strong>{{ filtered.length }}</strong><span>当前筛选</span></div>
      <div class="summary panel"><small>LLM 调用</small><strong>{{ totals.calls }}</strong><span>实际调用次数</span></div>
      <div class="summary panel"><small>输入 / 输出</small><strong>{{ totals.input.toLocaleString() }} <i>/</i> {{ totals.output.toLocaleString() }}</strong><span>Token</span></div>
      <div class="summary panel cache"><small>缓存命中</small><strong>{{ totals.cache.read.toLocaleString() }} <i>/</i> {{ hitRateText(totals.cache) }}</strong><span>{{ totals.cache.hasData ? `占输入 Token ${Math.round(totals.cache.ratio * 100)}%` : '该模型未返回缓存数据' }}</span></div>
      <div class="summary panel"><small>累计耗时</small><strong>{{ (totals.duration / 1000).toFixed(1) }}s</strong><span>模型请求</span></div>
    </div>
    <div class="filters panel">
      <select v-model="bookFilter" aria-label="书籍"><option value="">全部书籍</option><option v-for="b in store.books" :key="b.id" :value="b.id">{{ b.title }}</option></select>
      <select v-model="kindFilter" aria-label="类型"><option value="">全部类型</option><option v-for="(label, id) in kindLabels" :key="id" :value="id">{{ id === 'plan' ? '立项 / 卷纲' : label }}</option></select>
      <select v-model="modelFilter" aria-label="模型"><option value="">全部模型</option><option v-for="m in models" :key="m" :value="m">{{ m }}</option></select>
      <select v-model="statusFilter" aria-label="状态"><option value="">全部状态</option><option v-for="(label, id) in statusLabels" :key="id" :value="id">{{ label }}</option></select>
      <input v-model="query" placeholder="搜索书籍、单元、模型或错误" aria-label="搜索" />
      <button @click="reset">清空筛选</button>
    </div>
    <div v-if="loading" class="empty panel">加载运行记录…</div>
    <div v-else-if="!filtered.length" class="empty panel"><h2>没有匹配的调用记录</h2><p>调整筛选，或先在书籍中运行一次创作任务。</p></div>
    <div v-else class="run-table panel">
      <div class="table-header"><span>时间 / 书籍</span><span>类型 / 目标</span><span>模型</span><span>输入 / 输出</span><span>调用 / 耗时</span><span>状态</span><span></span></div>
      <article v-for="r in filtered" :key="r.id" class="run-record">
        <button class="run-row" @click="toggleExpand(r.id)">
          <span class="time-book"><strong>{{ bookTitle(r.bookId) }}</strong><small>{{ date(r.createdAt) }}</small></span>
          <span class="kind-target"><strong>{{ runKindLabel(r.kind, r.spec?.rewriteMode || r.spec?.mode) }}</strong><small>{{ target(r) }}</small></span>
          <span class="model" :title="r.modelId">{{ r.modelId }}</span>
          <span class="tokens">
            <strong>{{ tokens(r, 'promptTokens').toLocaleString() }}</strong><small>/ {{ tokens(r, 'completionTokens').toLocaleString() }}</small>
            <em v-if="runCache(r).hasData" title="缓存命中占输入 Token 的比例">缓存 {{ hitRateText(runCache(r)) }}</em>
          </span>
          <span class="calls"><strong>{{ r.callsUsed }}/{{ r.policy?.maxCalls || '—' }}</strong><small>{{ duration(r) }}</small></span>
          <span class="status" :data-status="r.status">{{ statusLabels[r.status] || r.status }}</span>
          <span class="expand">{{ expanded === r.id ? '−' : '+' }}</span>
        </button>
        <div v-if="expanded === r.id" class="run-details">
          <template v-if="expandedRun && expandedRun.id === r.id">
            <div v-if="expandedRun.error" class="error-detail"><strong>错误</strong><p>{{ expandedRun.error }}</p></div>
            <div class="detail-meta"><span>任务 ID：{{ expandedRun.id }}</span><span>基础版本：{{ expandedRun.spec.baseRevision || '未指定' }}</span><span v-if="expandedRun.candidateId">候选：{{ expandedRun.candidateId }}</span></div>
            <div v-if="expandedRun.contextSummary?.length" class="context-detail"><strong>上下文</strong><span v-for="item in expandedRun.contextSummary" :key="item">{{ item }}</span></div>
            <div v-if="runCache(expandedRun).hasData" class="cache-detail">
              <strong>缓存</strong>
              <span>命中 {{ runCache(expandedRun).read.toLocaleString() }} token（{{ hitRateText(runCache(expandedRun)) }}）</span>
              <span v-if="runCache(expandedRun).creation">写入 {{ runCache(expandedRun).creation.toLocaleString() }} token</span>
              <span class="hint">重试调用（step=repair）的命中率应接近 100%：repair 只追加在 payload 尾部，前缀不动。</span>
            </div>
            <table>
              <thead><tr><th>#</th><th>调用用途</th><th>模型</th><th>输入 Token</th><th>缓存命中</th><th>缓存写入</th><th>输出 Token</th><th>耗时</th><th>开始时间</th></tr></thead>
              <tbody><tr v-for="c in expandedRun.calls" :key="c.index"><td>{{ c.index }}</td><td>{{ c.step }}</td><td>{{ c.modelId }}</td><td>{{ c.promptTokens }}</td><td>{{ cacheHit(c) }}</td><td>{{ c.cacheCreationTokens ? c.cacheCreationTokens.toLocaleString() : '—' }}</td><td>{{ c.completionTokens }}</td><td>{{ (c.durationMs / 1000).toFixed(1) }}s</td><td>{{ date(c.startedAt) }}</td></tr></tbody>
            </table>
            <p v-if="!expandedRun.calls?.length" class="muted">尚无模型调用记录。</p>
          </template>
          <p v-else class="muted">正在加载调用明细…</p>
        </div>
      </article>
    </div>
  </section>
</template>

<style scoped>
.runs-page { display: grid; gap: 18px; }
.page-head { display: flex; justify-content: space-between; align-items: flex-end; gap: 20px; padding: 32px 4px 24px; }
.eyebrow { margin: 0; color: var(--accent); font-size: 12px; letter-spacing: 0.16em; text-transform: uppercase; }
h1 { margin: 8px 0 8px; font-family: var(--font-prose); font-size: clamp(28px, 3.5vw, 44px); line-height: 1.05; }
.page-head p:not(.eyebrow) { margin: 0; color: var(--ink-soft); font-size: 13px; }
.summary-grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 12px; }
.summary { padding: 20px; display: grid; gap: 6px; }
.summary small, .summary span { color: var(--ink-soft); font-size: 12px; }
.summary strong { font-family: var(--font-prose); font-size: 24px; }
.summary i { color: var(--ink-soft); font-style: normal; font-size: 16px; }
.summary.cache strong { color: var(--accent); }
.filters { display: flex; flex-wrap: wrap; gap: 8px; padding: 12px; }
.filters select, .filters input { padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; background: white; font-size: 12px; }
.filters input { flex: 1; min-width: 180px; }
.run-table { overflow: hidden; }
.table-header, .run-row { display: grid; grid-template-columns: 1.2fr 1fr 1.35fr .85fr .65fr .65fr 24px; gap: 12px; align-items: center; }
.table-header { padding: 12px 16px; background: rgba(15,107,92,.04); color: var(--ink-soft); font-size: 11px; }
.run-row { width: 100%; padding: 14px 16px; border: 0; border-top: 1px solid var(--line); background: transparent; text-align: left; border-radius: 0; }
.run-row:hover { background: rgba(15,107,92,.035); }
.run-row strong { display: block; font-size: 13px; }
.run-row small { display: block; color: var(--ink-soft); font-size: 11px; margin-top: 4px; }
.model { font-size: 11px; word-break: break-all; color: var(--ink-soft); }
.tokens strong, .tokens small { display: inline; }
.tokens small { margin-left: 3px; }
.tokens em { display: block; margin-top: 4px; color: var(--accent); font-size: 10px; font-style: normal; }
.status { font-size: 11px; color: var(--accent); }
.status[data-status="failed"] { color: var(--bad); }
.status[data-status="waiting_accept"] { color: var(--warn); }
.status[data-status="succeeded"] { color: var(--ok); }
.expand { color: var(--ink-soft); text-align: right; }
.run-details { padding: 16px 20px; border-top: 1px solid var(--line); background: rgba(255,255,255,.45); overflow-x: auto; }
.detail-meta { display: flex; flex-wrap: wrap; gap: 12px; color: var(--ink-soft); font-size: 10px; margin-bottom: 14px; word-break: break-all; }
.context-detail { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 14px; font-size: 11px; }
.context-detail strong { margin-right: 4px; }
.context-detail span { padding: 3px 7px; background: rgba(15,107,92,.07); color: var(--accent); border-radius: 6px; }
.cache-detail { display: flex; flex-wrap: wrap; gap: 10px; align-items: baseline; margin-bottom: 14px; font-size: 11px; }
.cache-detail strong { color: var(--ink-soft); }
.cache-detail span { padding: 3px 7px; background: rgba(15,107,92,.07); color: var(--accent); border-radius: 6px; }
.cache-detail span.hint { background: none; color: var(--ink-soft); padding: 0; font-size: 10px; }
.run-details table { width: 100%; border-collapse: collapse; font-size: 12px; }
.run-details th, .run-details td { text-align: left; padding: 8px; border-top: 1px solid var(--line); white-space: nowrap; }
.run-details th { color: var(--ink-soft); font-weight: 500; }
.error-detail { color: var(--bad); margin-bottom: 14px; font-size: 12px; }
.error-detail p { margin: 4px 0; color: var(--ink-soft); }
.empty { padding: 32px; text-align: center; color: var(--ink-soft); }
.muted { color: var(--ink-soft); font-size: 12px; }
@media (max-width: 1000px) {
  .summary-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .table-header { display: none; }
  .run-row { grid-template-columns: 1fr 1fr 1fr; row-gap: 12px; }
  .model { grid-column: 1 / 3; }
  .expand { position: absolute; right: 16px; }
  .run-record { position: relative; }
}

</style>
