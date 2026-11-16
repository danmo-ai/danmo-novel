<script setup lang="ts">
/**
 * 唯一的确认界面。细纲、正文、审校清单、修订、定稿、卷纲、重写设定都在这里
 * 确认：同一套外壳、同一组动作，只按 kind 换预览渲染器与主按钮文案。
 *
 * 上下文随卡片走（单元合同、父级卷纲种子、门禁摘要），而不是把作者赶回单元页：
 * LLM 调用是分钟级的，结果落地时作者早已不在那个单元上。
 */
import { computed, onMounted, ref, watch } from 'vue'
import { useAppStore } from '@/shared/stores/app'
import type { Candidate, UnitInfo } from '@/shared/types'
import {
  createdAtOf, itemKind, itemKindTitle, itemTarget, needsCandidateDetail, type TaskItem,
} from '@/shared/utils/taskItems'
import { outlineDraftUnit } from '@/shared/utils/outlineDraft'
import { parseStructuredMarkdown } from '@/shared/utils/structuredMarkdown'
import { planModeLabel } from '@/shared/utils/runLabels'
import { hitRateText } from '@/shared/utils/tokenStats'
import ChapterDiff from './ChapterDiff.vue'
import PlanCandidateDiff from './PlanCandidateDiff.vue'
import StructuredOutline from './StructuredOutline.vue'
import UnitContract from './UnitContract.vue'
import ReviewFindingsEditor from './ReviewFindingsEditor.vue'

const props = defineProps<{ item: TaskItem; busy?: boolean }>()
const emit = defineEmits<{
  accept: [c: Candidate]
  reject: [c: Candidate]
  repair: [c: Candidate]
  revise: [unitId: string, issueIds: string[]]
  retrySplit: [unitId?: string]
  rejectUnit: [unitId: string, candidateIds: string[]]
  openUnit: [unitId: string]
}>()

const store = useAppStore()
const loading = ref(false)
const showContext = ref(false)
const unitRaw = ref('')

const kind = computed(() => itemKind(props.item))
const candidate = computed<Candidate | undefined>(() => {
  const row = props.item.candidate
  return row ? store.resolvedCandidate(row) : undefined
})
const unit = computed<UnitInfo | undefined>(() =>
  props.item.unitId ? store.currentBook?.units.find((u) => u.unitId === props.item.unitId) : undefined)
const readOnly = computed(() => Boolean(store.currentBook?.summary.readOnly))
const isPending = computed(() => props.item.section === 'needsAction')
const isReview = computed(() => kind.value === 'review')
const isRevise = computed(() => kind.value === 'revise')
const isRefineWrite = computed(() => kind.value === 'write' && Boolean(
  candidate.value?.baseChapters?.length || candidate.value?.hasBaseChapters
  || props.item.run?.spec?.rewriteMode === 'refine'))

const reviewGroupUnit = computed(() => props.item.unitId)
const hasBlockingStructure = computed(() =>
  kind.value === 'structure_review' && (candidate.value?.findings || []).some((f) => f.severity === 'blocking'))

/** 待确认且需要正文/计划时取详情；审校清单直接用精简响应里的 findings。 */
async function ensureDetail() {
  const row = props.item.candidate
  if (!row || !isPending.value || isReview.value) return
  if (!needsCandidateDetail(props.item)) return
  const cached = store.resolvedCandidate(row)
  if (cached.chapters || cached.baseChapters || cached.planMarkdown || cached.planSettings
    || cached.replacement || cached.unitOutlines || cached.actualChapters) return
  loading.value = true
  await store.candidateDetail(row.id)
  loading.value = false
}

/** 上下文：单元细纲按需单文件取；计划预览需要面板文件。 */
async function ensureContext() {
  if (kind.value === 'plan' || props.item.candidate?.hasPlan) {
    const id = store.currentBook?.summary.id
    if (id) await store.loadBookFiles(id).catch(() => {})
  }
  const u = unit.value
  if (u?.outlinePath && !unitRaw.value) {
    try {
      const res = await fetchFile(u.outlinePath)
      unitRaw.value = res
    } catch { /* 细纲尚未生成时保持空合同 */ }
  }
}

async function fetchFile(path: string): Promise<string> {
  const id = store.currentBook?.summary.id
  const { api } = await import('@/shared/api/client')
  const res = await api.get<{ content: string }>(`/api/books/${id}/file?path=${encodeURIComponent(path)}`)
  return res.content || ''
}

const contextUnit = computed<UnitInfo | undefined>(() => {
  const u = unit.value
  if (!u) return undefined
  return unitRaw.value ? { ...u, outlineRaw: unitRaw.value } : u
})

/** 父级卷纲种子：细纲候选必须继承的上层合同。 */
const parentSeed = computed(() => {
  const u = unit.value
  if (!u || kind.value !== 'outline') return []
  const path = `outline/volumes/v${String(u.volume).padStart(2, '0')}.md`
  const doc = store.currentBook?.structuredOutlines?.[path]
    || store.currentBook?.volumes.find((v) => v.volume === u.volume)?.structured
  const seed = doc?.entities.find((e) => e.kind === '单元' && e.id === u.unitId)
  if (!seed) return []
  const keys = ['objective', 'obstacle', 'attempt', 'setback', 'choice', 'cost', 'outcome', 'chapter_range']
  const labels: Record<string, string> = {
    objective: '目标', obstacle: '阻碍', attempt: '尝试', setback: '方法缺口',
    choice: '关键选择', cost: '代价', outcome: '不可逆结果', chapter_range: '章范围',
  }
  return keys
    .map((k) => ({ label: labels[k], value: seed.fields.find((f) => f.name === k)?.value || '' }))
    .filter((x) => x.value)
})

const outlineDrafts = computed(() => (candidate.value?.unitOutlines || []).map((d) =>
  outlineDraftUnit(d, store.currentBook?.units.find((u) => u.unitId === String(d.unitId)))))

function structureOf(c: Candidate, which: 'book' | 'volume') {
  const raw = which === 'book' ? c.planMarkdown?.book_outline : c.planMarkdown?.volume_outline
  if (!raw) return null
  return parseStructuredMarkdown(raw, which === 'book' ? 'book-outline/v1' : 'volume-outline/v1')
}
function chapterText(c: Candidate) {
  return (c.chapters || []).map((ch) => `## 第${ch.chapter}章 ${ch.title || ''}\n\n${ch.body}`).join('\n\n---\n\n')
}
function unverified(c: Candidate) {
  return (c.findings || []).filter((f) => String(f.id || '').startsWith('evidence-'))
}
function planFiles(c: Candidate) {
  const keep = new Set(c.preservePaths || [])
  const files: string[] = []
  if (c.mode === 'volume') {
    if (c.planMarkdown?.volume_outline) files.push(`outline/volumes/${c.volumeId || 'v01'}.md`)
    return files
  }
  if (c.planMarkdown?.book_outline && !keep.has('outline/book_outline.md')) files.push('outline/book_outline.md')
  if (c.planSettings) files.push(...Object.keys(c.planSettings).filter((p) => !keep.has(p)))
  return files
}
function statusLabel() {
  const c = candidate.value
  if (c && c.status !== 'open') {
    return { accepted: '已归档', rejected: '已拒绝', expired: '已作废' }[c.status] || c.status
  }
  return props.item.run?.status || ''
}

onMounted(async () => { await ensureDetail(); await ensureContext() })
watch(() => props.item.id, async () => { unitRaw.value = ''; await ensureDetail(); await ensureContext() })
</script>

<template>
  <article class="task-card panel" :data-section="item.section">
    <header class="tc-head">
      <div>
        <strong>{{ itemKindTitle(item) }} · {{ itemTarget(item) }}</strong>
        <span class="muted"> · {{ new Date(createdAtOf(item)).toLocaleString() }}</span>
        <span v-if="item.run?.modelId" class="muted mono"> · {{ item.run.modelId }}</span>
        <span v-if="unit" class="tc-status" :data-status="unit.status"> · {{ unit.status }}</span>
      </div>
      <button v-if="item.unitId" class="tc-link" @click="emit('openUnit', item.unitId)">打开单元</button>
    </header>
    <p v-if="item.foldedReviewCandidateId" class="muted tc-fold">
      本单元的审校清单已折叠进这次修复：落盘后随正文版本归档。
    </p>

    <!-- 待确认：统一预览 + 三动作 -->
    <template v-if="isPending">
      <p v-if="loading" class="muted">正在加载候选内容…</p>
      <template v-else-if="candidate">
        <template v-if="isRevise || isRefineWrite">
          <p class="muted">{{ isRevise
            ? '下面是相对现有正文的改动。确认无误后点「确认落盘」覆盖正文；落盘后旧清单按版本归档。'
            : '下面是相对原稿的改动。接受后才会覆盖正文；改动幅度异常时会有提示。' }}</p>
          <ChapterDiff :base="candidate.baseChapters" :revised="candidate.chapters || []" />
        </template>
        <template v-else-if="kind === 'outline'">
          <div v-for="(d, i) in outlineDrafts" :key="i" class="tc-contract">
            <UnitContract :unit="d" compact />
          </div>
          <details class="tc-raw"><summary>查看细纲源（YAML）</summary>
            <pre>{{ outlineDrafts.map((d) => d.outlineRaw).join('\n\n') }}</pre>
          </details>
        </template>
        <template v-else-if="kind === 'finalize' && candidate.actualChapters?.length">
          <p class="muted">逐章核对实际正文；只有作者接受后，以下结果才进入连续性账本。</p>
          <p v-if="unverified(candidate).length" class="warn-text">
            有 {{ unverified(candidate).length }} 条状态迁移的回证在本章正文里找不到。接受时会记入定稿日志；不确定就先不接受。
          </p>
          <details v-for="ch in candidate.actualChapters" :key="ch.chapter" open>
            <summary>第 {{ ch.chapter }} 章 · {{ ch.summary }}</summary>
            <ul class="tc-list">
              <li v-for="d in ch.stateDeltas || []" :key="`${d.stem}-${d.field}-${d.evidence}`">
                {{ d.stem }} · {{ d.field }}：{{ d.from }} → {{ d.to }}（{{ d.evidence }}）
              </li>
              <li v-if="ch.plant?.length">实际埋下：{{ ch.plant.join('、') }}</li>
              <li v-if="ch.payoff?.length">实际回收：{{ ch.payoff.join('、') }}</li>
            </ul>
          </details>
        </template>
        <template v-else-if="kind === 'structure_review'">
          <p class="muted">结构编辑只审查父级继承、因果、兑现和状态。按问题修复会生成新的文档候选。</p>
          <div v-for="f in candidate.findings || []" :key="f.id" class="tc-finding" :class="f.severity">
            <strong>{{ f.title }}</strong>
            <p v-if="f.evidence" class="muted">{{ f.evidence }}</p>
            <p v-if="f.suggestion" class="tc-suggest">建议：{{ f.suggestion }}</p>
          </div>
        </template>
        <pre v-else-if="candidate.chapters" class="tc-prose">{{ chapterText(candidate) }}</pre>
        <pre v-else-if="candidate.replacement" class="tc-prose">{{ candidate.replacement }}</pre>
        <div v-else-if="candidate.planMarkdown || candidate.planSettings" class="tc-plan">
          <p><strong>{{ planModeLabel(candidate.mode) }}</strong>
            <span class="muted"> · 接受后写入 {{ planFiles(candidate).length }} 个文件</span></p>
          <details v-if="candidate.planMarkdown?.volume_outline" open>
            <summary>卷纲 · {{ candidate.volumeId }}</summary>
            <StructuredOutline v-if="structureOf(candidate, 'volume') && !structureOf(candidate, 'volume')!.legacy"
              :document="structureOf(candidate, 'volume')!" compact read-only />
            <pre v-else class="tc-prose">{{ candidate.planMarkdown.volume_outline }}</pre>
          </details>
          <details v-if="candidate.planMarkdown?.book_outline" open>
            <summary>总纲</summary>
            <StructuredOutline v-if="structureOf(candidate, 'book') && !structureOf(candidate, 'book')!.legacy"
              :document="structureOf(candidate, 'book')!" compact read-only />
            <pre v-else class="tc-prose">{{ candidate.planMarkdown.book_outline }}</pre>
          </details>
          <details open><summary>变更预览（相对当前 canon）</summary>
            <PlanCandidateDiff :candidate="candidate" :files="store.currentBook?.files || {}" />
          </details>
        </div>
      </template>
      <!-- 审校清单：精简响应已带 findings，无需详情 -->
      <ReviewFindingsEditor v-else-if="isReview && reviewGroupUnit"
        :unit-id="reviewGroupUnit" :busy="busy" :read-only="readOnly"
        @revise="(u, ids) => emit('revise', u, ids)"
        @retry-split="(u) => emit('retrySplit', u)"
        @reject-unit="(u, ids) => emit('rejectUnit', u, ids)" />

      <!-- 富确认上下文：合同 / 父级种子 / 门禁 -->
      <details v-if="contextUnit" class="tc-context" :open="showContext"
        @toggle="showContext = ($event.target as HTMLDetailsElement).open">
        <summary>单元合同与上下文</summary>
        <div v-if="parentSeed.length" class="tc-seed">
          <small>父级卷纲种子（不可改写的上层合同）</small>
          <p v-for="s in parentSeed" :key="s.label"><strong>{{ s.label }}</strong>{{ s.value }}</p>
        </div>
        <div v-if="kind === 'finalize' && store.continuity" class="tc-seed">
          <small>门禁与连续性</small>
          <p v-if="store.continuity.units">
            连续性状态：{{ store.continuity.lastUnit }} 之后 · {{ store.continuity.cast }} 人 · {{ store.continuity.openLoops }} 条未回收伏笔
          </p>
          <p v-else class="muted">连续性状态未建立：定稿第一个单元后自动生成。</p>
        </div>
        <UnitContract :unit="contextUnit" compact />
      </details>

      <footer v-if="candidate" class="tc-actions">
        <button v-if="hasBlockingStructure" class="primary" @click="emit('repair', candidate)">按问题修复</button>
        <span class="tc-spacer" />
        <button :disabled="readOnly" @click="emit('reject', candidate)">拒绝</button>
        <button class="primary" :disabled="busy || readOnly" @click="emit('accept', candidate)">
          {{ isRevise ? '确认落盘' : '接受' }}
        </button>
      </footer>
    </template>

    <!-- 进行中 / 已终结：操作记录 -->
    <template v-else>
      <div class="tc-meta">
        <span class="tc-runstatus" :data-status="item.run?.status">{{ statusLabel() }}</span>
        <template v-if="item.run">
          <span class="muted">{{ item.run.callsUsed }}/{{ item.run.policy?.maxCalls || '—' }} 次</span>
          <span class="muted">{{ (item.run.inputTokens ?? 0).toLocaleString() }} / {{ (item.run.outputTokens ?? 0).toLocaleString() }} token</span>
          <span class="muted">{{ ((item.run.durationMs ?? 0) / 1000).toFixed(1) }}s</span>
          <span v-if="(item.run.cacheRead ?? 0) > 0" class="muted">缓存 {{ hitRateText({ read: item.run.cacheRead ?? 0, creation: item.run.cacheCreation ?? 0, prompt: item.run.inputTokens ?? 0, ratio: (item.run.inputTokens ?? 0) > 0 ? (item.run.cacheRead ?? 0) / (item.run.inputTokens ?? 1) : 0, hasData: true }) }}</span>
        </template>
      </div>
      <p v-if="item.run?.error" class="error-text">{{ item.run.error }}</p>
      <p v-for="w in item.run?.warnings || []" :key="w" class="warn-text">{{ w }}</p>
    </template>
  </article>
</template>

<style scoped>
.task-card { padding: 16px; display: grid; gap: 10px; }
.tc-head { display: flex; align-items: flex-start; gap: 10px; }
.tc-head strong { font-size: 14px; }
.tc-link { margin-left: auto; border: none; background: none; color: var(--accent); font-size: 12px; cursor: pointer; white-space: nowrap; }
.tc-status { font-size: 11px; color: var(--ink-soft); }
.tc-status[data-status='reviewed'], .tc-status[data-status='finalized'] { color: var(--ok); }
.muted { color: var(--ink-soft); font-size: 12px; }
.mono { font-family: ui-monospace, monospace; }
.tc-fold { margin: 0; }
.tc-contract { border: 1px solid var(--line); border-radius: 10px; padding: 12px; }
.tc-raw summary, .tc-context summary { cursor: pointer; font-size: 12px; color: var(--accent); }
.tc-raw pre { white-space: pre-wrap; font: 11px/1.7 ui-monospace, monospace; color: var(--ink-soft); max-height: 260px; overflow: auto; }
.tc-context { border-top: 1px solid var(--line); padding-top: 8px; display: grid; gap: 10px; }
.tc-seed { padding: 10px 12px; background: rgba(15,107,92,.05); border-radius: 10px; }
.tc-seed small { display: block; color: var(--accent); font-size: 11px; margin-bottom: 4px; }
.tc-seed p { margin: 3px 0; font-size: 12px; color: var(--ink-soft); line-height: 1.7; }
.tc-seed strong { color: var(--ink); margin-right: 6px; }
.tc-prose { white-space: pre-wrap; font-family: var(--font-prose); font-size: 13px; line-height: 1.9; max-height: 320px; overflow: auto; background: rgba(255,255,255,.5); padding: 14px; border-radius: 10px; border: 1px solid var(--line); margin: 0; }
.tc-plan { display: grid; gap: 8px; }
.tc-plan details, .task-card > details { border: 1px solid var(--line); border-radius: 10px; padding: 8px 12px; }
.tc-plan summary, .task-card > details summary { cursor: pointer; font-size: 13px; font-weight: 600; }
.tc-list { margin: 8px 0 0; padding-left: 18px; font-size: 12px; color: var(--ink-soft); line-height: 1.8; }
.tc-finding { padding: 8px 12px; border-radius: 10px; background: rgba(161,92,18,.08); font-size: 12px; }
.tc-finding.blocking { background: rgba(163,59,50,.1); }
.tc-finding strong { display: block; margin-bottom: 3px; }
.tc-finding p { margin: 2px 0; }
.tc-suggest { color: var(--accent); }
.tc-actions { display: flex; align-items: center; gap: 8px; }
.tc-spacer { flex: 1; }
.tc-meta { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.tc-runstatus { font-size: 12px; font-weight: 600; color: var(--accent); }
.tc-runstatus[data-status='failed'] { color: var(--bad); }
.tc-runstatus[data-status='succeeded'] { color: var(--ok); }
.error-text { color: var(--bad); font-size: 12px; margin: 0; }
.warn-text { color: var(--warn, #b7791f); font-size: 12px; margin: 0; }
</style>
