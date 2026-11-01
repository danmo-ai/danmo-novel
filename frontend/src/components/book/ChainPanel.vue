<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '@/api/client'
import { useAppStore } from '@/stores/app'
import { knowledgeAppliesTo, knowledgeAppliesToGenre, knowledgeGenreSummary, knowledgeStageSummary } from '@/utils/knowledgeStages'
import type { Chain, ChainSpec, UnitInfo } from '@/types'

type KnowledgeDoc = {
  id: string; title: string; summary: string; bytes: number
  stages?: string[]; genres?: string[]; subgenres?: string[]
}

const props = defineProps<{
  units: UnitInfo[]
  selectedUnit: UnitInfo | null
  chain: Chain | null
  readOnly?: boolean
  busy?: boolean
}>()
const emit = defineEmits<{
  start: [spec: ChainSpec]
  resume: []
  cancel: []
}>()

const store = useAppStore()

const stageOptions = [
  { key: 'outline', label: '细纲' },
  { key: 'write', label: '正文' },
  { key: 'review', label: '审校' },
  { key: 'finalize', label: '定稿' },
] as const
const stageLabel = (key?: string) => stageOptions.find((s) => s.key === key)?.label || key || ''
const statusLabel: Record<string, string> = {
  pending: '排队', running: '执行中', waiting_manual: '待人工', completed: '已完成', failed: '失败', cancelled: '已取消',
}
const unitStatusLabel: Record<string, string> = { pending: '待办', running: '进行中', done: '完成', blocked: '卡住' }

const volume = computed(() => props.selectedUnit?.volume ?? props.units[0]?.volume ?? 1)
const volumeUnits = computed(() =>
  props.units
    .filter((u) => u.volume === volume.value)
    .slice()
    .sort((a, b) => a.index - b.index),
)
const fromUnitId = ref('')
const scopeUnits = computed(() => {
  const from = volumeUnits.value.find((u) => u.unitId === fromUnitId.value)
  if (!from) return [] as UnitInfo[]
  return volumeUnits.value.filter((u) => u.index >= from.index)
})
const scopeNote = computed(() => {
  if (!scopeUnits.value.length) return '本卷没有可执行单元'
  return `第 ${volume.value} 卷 · 到卷末 ${scopeUnits.value.length} 个单元`
})

const outlineInstruction = ref('')
const writeInstruction = ref('')
const outlineKnowledge = ref<Set<string>>(new Set())
const writeKnowledge = ref<Set<string>>(new Set())
const knowledgeDocs = ref<KnowledgeDoc[]>([])
const knowledgeError = ref('')
const showParams = ref(false)
const showOutlineKnowledge = ref(false)
const showWriteKnowledge = ref(false)

const running = computed(() => props.chain?.status === 'pending' || props.chain?.status === 'running')
const blocked = computed(() => props.chain?.status === 'waiting_manual' || props.chain?.status === 'failed')
const finished = computed(() => props.chain?.status === 'completed' || props.chain?.status === 'cancelled')
const progress = computed(() => {
  const ch = props.chain
  if (!ch) return { done: 0, total: 0 }
  return { done: ch.units.filter((u) => u.status === 'done').length, total: ch.units.length }
})
const currentUnit = computed(() => {
  const ch = props.chain
  if (!ch || ch.cursor >= ch.units.length) return null
  return ch.units[ch.cursor]
})

const storageKey = computed(() => `chain:stage-config:${store.currentBook?.summary.id || ''}`)

// 与 RunConfirmDialog 一致：只列出「步骤 + 题材」都适用的文章，未打标的文章对
// 所有步骤/题材可见。
const bookGenre = computed(() => store.currentBook?.summary.genre || '')
const bookSubgenre = computed(() => store.currentBook?.summary.subgenre || '')
function docsForStage(stage: 'outline' | 'write') {
  return knowledgeDocs.value.filter(
    (d) => knowledgeAppliesTo(d.stages, stage) && knowledgeAppliesToGenre(d, bookGenre.value, bookSubgenre.value),
  )
}
const outlineDocs = computed(() => docsForStage('outline'))
const writeDocs = computed(() => docsForStage('write'))

function defaultFromUnit() {
  if (fromUnitId.value && volumeUnits.value.some((u) => u.unitId === fromUnitId.value)) return
  const prefer = props.selectedUnit && props.selectedUnit.volume === volume.value ? props.selectedUnit.unitId : ''
  fromUnitId.value = prefer || volumeUnits.value[0]?.unitId || ''
}

watch(() => props.selectedUnit?.unitId, defaultFromUnit)

function toggleKnowledge(kind: 'outline' | 'write', id: string) {
  const target = kind === 'outline' ? outlineKnowledge : writeKnowledge
  const next = new Set(target.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  target.value = next
  persistDraft()
}

function persistDraft() {
  if (!storageKey.value) return
  try {
    localStorage.setItem(storageKey.value, JSON.stringify({
      fromUnitId: fromUnitId.value,
      outlineInstruction: outlineInstruction.value,
      writeInstruction: writeInstruction.value,
      outlineKnowledge: [...outlineKnowledge.value],
      writeKnowledge: [...writeKnowledge.value],
    }))
  } catch { /* 隐私模式下忽略 */ }
}

function restoreDraft() {
  if (!storageKey.value) return
  try {
    const raw = localStorage.getItem(storageKey.value)
    if (!raw) return
    const saved = JSON.parse(raw) as any
    if (typeof saved.fromUnitId === 'string') fromUnitId.value = saved.fromUnitId
    outlineInstruction.value = saved.outlineInstruction || ''
    writeInstruction.value = saved.writeInstruction || ''
    outlineKnowledge.value = new Set<string>(saved.outlineKnowledge || [])
    writeKnowledge.value = new Set<string>(saved.writeKnowledge || [])
  } catch { /* 忽略损坏草稿 */ }
}

onMounted(async () => {
  restoreDraft()
  defaultFromUnit()
  try {
    const docs = await api.get<any[] | null>('/api/knowledge')
    knowledgeDocs.value = docs || []
  } catch (e: any) {
    knowledgeError.value = e?.message || '参考知识读取失败'
  }
})

function start() {
  const book = store.currentBook
  if (!book || props.busy || props.readOnly || !fromUnitId.value) return
  const spec: ChainSpec = {
    bookId: book.summary.id,
    volumeId: `v${String(volume.value).padStart(2, '0')}`,
    fromUnitId: fromUnitId.value,
    outline: {
      instruction: outlineInstruction.value.trim(),
      knowledgeIds: [...outlineKnowledge.value],
    },
    write: {
      instruction: writeInstruction.value.trim(),
      knowledgeIds: [...writeKnowledge.value],
    },
  }
  persistDraft()
  emit('start', spec)
}
</script>

<template>
  <section class="side-card panel chain-panel">
    <h4>连续重写 <span class="kb-hint" title="总纲 / 卷纲 / 设定视为已固定；逐单元自动跑细纲→正文→审校→定稿，审校阻断先自动修订一次">?</span></h4>

    <template v-if="chain && (running || blocked)">
      <div class="chain-head">
        <span class="chain-status" :data-status="chain.status">{{ statusLabel[chain.status] || chain.status }}</span>
        <span class="chain-count">{{ progress.done }}/{{ progress.total }}</span>
      </div>
      <p v-if="currentUnit" class="chain-current">
        {{ currentUnit.unitId }} · {{ stageLabel(currentUnit.stage) }}
        <span v-if="currentUnit.note" class="chain-note">{{ currentUnit.note }}</span>
      </p>
      <div class="stage-bar">
        <span
          v-for="s in stageOptions"
          :key="s.key"
          class="stage-chip"
          :class="{ active: currentUnit?.stage === s.key, passed: currentUnit ? stageOptions.findIndex((x) => x.key === currentUnit!.stage) > stageOptions.findIndex((x) => x.key === s.key) : false }"
        >{{ s.label }}</span>
      </div>
      <div v-if="blocked" class="chain-error">
        <strong>已中断，等待人工处理</strong>
        <p>{{ chain.error }}</p>
        <p class="chain-hint">在单元栏或待审候选里修复这个单元，然后点继续。</p>
      </div>
      <ul class="chain-units">
        <li v-for="u in chain.units" :key="u.unitId" :class="`u-${u.status}`">
          <span class="dot" :data-status="u.status"></span>
          <span class="u-id">{{ u.unitId }}</span>
          <span class="u-status">{{ unitStatusLabel[u.status] || u.status }}</span>
        </li>
      </ul>
      <details v-if="chain.spec.outline.instruction || chain.spec.write.instruction || chain.spec.outline.knowledgeIds?.length || chain.spec.write.knowledgeIds?.length" class="chain-spec">
        <summary>本次生效的阶段参数</summary>
        <p v-if="chain.spec.outline.instruction"><b>细纲指令</b>：{{ chain.spec.outline.instruction }}</p>
        <p v-if="chain.spec.write.instruction"><b>正文指令</b>：{{ chain.spec.write.instruction }}</p>
        <p v-if="chain.spec.outline.knowledgeIds?.length"><b>细纲参考知识</b>：{{ chain.spec.outline.knowledgeIds.length }} 篇</p>
        <p v-if="chain.spec.write.knowledgeIds?.length"><b>正文参考知识</b>：{{ chain.spec.write.knowledgeIds.length }} 篇</p>
      </details>
      <div class="chain-actions">
        <button v-if="blocked" class="primary" :disabled="busy || readOnly" @click="emit('resume')">继续</button>
        <button :disabled="busy" @click="emit('cancel')">取消</button>
      </div>
    </template>

    <template v-else>
      <p v-if="finished" class="chain-brief">
        上次任务{{ statusLabel[chain!.status] }}（{{ progress.done }}/{{ progress.total }}）。
      </p>
      <label class="chain-field">
        <span>起始单元</span>
        <select v-model="fromUnitId" :disabled="busy || readOnly" @change="persistDraft">
          <option v-for="u in volumeUnits" :key="u.unitId" :value="u.unitId">
            {{ u.unitId }} · {{ u.title || '未完成合同' }}
          </option>
        </select>
      </label>
      <p class="chain-scope">{{ scopeNote }}</p>
      <button class="param-toggle" type="button" @click="showParams = !showParams">
        <span>阶段参数（可选）</span><span>{{ showParams ? '收起' : '展开' }}</span>
      </button>
      <div v-if="showParams" class="param-body">
        <div class="param-group">
          <label>细纲阶段 · 追加指令</label>
          <textarea
            v-model="outlineInstruction"
            rows="2"
            placeholder="例如：开场冲突前置；本卷前半压缩日常"
            spellcheck="false"
            :disabled="busy || readOnly"
            @blur="persistDraft"
          />
          <button class="knowledge-toggle" type="button" @click="showOutlineKnowledge = !showOutlineKnowledge">
            <span>参考知识{{ outlineKnowledge.size ? ` · 已选 ${outlineKnowledge.size} 篇` : '（未选）' }}</span>
            <span>{{ showOutlineKnowledge ? '收起' : '展开' }}</span>
          </button>
          <div v-if="showOutlineKnowledge" class="knowledge-list">
            <p v-if="knowledgeError" class="chain-hint">{{ knowledgeError }}</p>
            <p v-else-if="!outlineDocs.length" class="chain-hint">没有适用于细纲的知识文章（可在「知识」页添加）。</p>
            <label v-for="d in outlineDocs" :key="d.id" class="knowledge-item" :class="{ selected: outlineKnowledge.has(d.id) }">
              <input type="checkbox" :checked="outlineKnowledge.has(d.id)" @change="toggleKnowledge('outline', d.id)" />
              <span class="k-title">{{ d.title }}</span>
              <span class="k-tag">{{ knowledgeStageSummary(d.stages) }} · {{ knowledgeGenreSummary(d) }}</span>
            </label>
          </div>
        </div>
        <div class="param-group">
          <label>正文阶段 · 追加指令</label>
          <textarea
            v-model="writeInstruction"
            rows="2"
            placeholder="例如：多用动作与对白，少用比喻；每章结尾留钩子"
            spellcheck="false"
            :disabled="busy || readOnly"
            @blur="persistDraft"
          />
          <button class="knowledge-toggle" type="button" @click="showWriteKnowledge = !showWriteKnowledge">
            <span>参考知识{{ writeKnowledge.size ? ` · 已选 ${writeKnowledge.size} 篇` : '（未选）' }}</span>
            <span>{{ showWriteKnowledge ? '收起' : '展开' }}</span>
          </button>
          <div v-if="showWriteKnowledge" class="knowledge-list">
            <p v-if="!writeDocs.length" class="chain-hint">没有适用于正文的知识文章（可在「知识」页添加）。</p>
            <label v-for="d in writeDocs" :key="d.id" class="knowledge-item" :class="{ selected: writeKnowledge.has(d.id) }">
              <input type="checkbox" :checked="writeKnowledge.has(d.id)" @change="toggleKnowledge('write', d.id)" />
              <span class="k-title">{{ d.title }}</span>
              <span class="k-tag">{{ knowledgeStageSummary(d.stages) }} · {{ knowledgeGenreSummary(d) }}</span>
            </label>
          </div>
        </div>
      </div>
      <button class="primary chain-start" :disabled="busy || readOnly || !scopeUnits.length" @click="start">
        {{ busy ? '正在启动…' : '连续重写' }}
      </button>
      <p class="chain-hint">四个阶段候选自动接受；审校出 blocking 时先自动定向修订一次并复审，仍不通过才中断，修复后可继续。</p>
    </template>
  </section>
</template>

<style scoped>
.chain-panel { display: grid; gap: 8px; }
.chain-panel h4 { margin-bottom: 2px; }
.kb-hint { display: inline-grid; place-items: center; width: 14px; height: 14px; border-radius: 50%; background: rgba(20,35,31,.08); font-size: 9px; cursor: help; }
.chain-head { display: flex; justify-content: space-between; align-items: center; }
.chain-status { font-size: 10px; padding: 2px 8px; border-radius: 20px; background: rgba(15,107,92,.1); color: var(--accent); font-weight: 600; }
.chain-status[data-status="waiting_manual"], .chain-status[data-status="failed"] { background: rgba(163,59,50,.1); color: var(--bad); }
.chain-status[data-status="completed"] { background: rgba(31,122,77,.12); color: var(--ok); }
.chain-count { font-size: 10px; color: var(--ink-soft); }
.chain-current { margin: 0; font-size: 11px; }
.chain-note { color: var(--ink-soft); font-size: 10px; }
.stage-bar { display: flex; gap: 3px; }
.stage-chip { flex: 1 1 0; text-align: center; font-size: 9px; padding: 3px 0; border-radius: 20px; background: rgba(20,35,31,.05); color: var(--ink-soft); }
.stage-chip.active { background: var(--accent); color: white; font-weight: 700; }
.stage-chip.passed { background: rgba(31,122,77,.14); color: var(--ok); }
.chain-error { padding: 8px 10px; border-radius: 8px; background: rgba(163,59,50,.08); }
.chain-error strong { font-size: 11px; color: var(--bad); }
.chain-error p { margin: 4px 0 0; font-size: 11px; color: var(--ink-soft); line-height: 1.6; word-break: break-word; }
.chain-units { list-style: none; margin: 0; padding: 0; display: grid; gap: 2px; max-height: 180px; overflow: auto; }
.chain-units li { display: flex; align-items: center; gap: 6px; font-size: 11px; padding: 2px 0; color: var(--ink-soft); }
.chain-units .u-id { flex: 1; }
.chain-units .u-status { font-size: 10px; }
.chain-units .u-done .u-status { color: var(--ok); }
.chain-units .u-blocked .u-status { color: var(--bad); }
.chain-units .dot { width: 6px; height: 6px; border-radius: 50%; background: #9aa; flex-shrink: 0; }
.chain-units .dot[data-status="running"] { background: var(--accent); }
.chain-units .dot[data-status="done"] { background: var(--ok); }
.chain-units .dot[data-status="blocked"] { background: var(--bad); }
.chain-spec { font-size: 10px; color: var(--ink-soft); }
.chain-spec summary { cursor: pointer; padding: 4px 0; }
.chain-spec p { margin: 4px 0; line-height: 1.6; word-break: break-word; }
.chain-actions { display: flex; gap: 8px; }
.chain-actions button { flex: 1; padding: 8px; font-size: 12px; }
.chain-brief { margin: 0; font-size: 11px; color: var(--ink-soft); }
.chain-field { display: grid; gap: 4px; font-size: 11px; color: var(--ink-soft); }
.chain-field select { width: 100%; padding: 6px 8px; border: 1px solid var(--line); border-radius: 8px; font-size: 11px; background: white; }
.chain-scope { margin: 0; font-size: 10px; color: var(--ink-soft); }
.param-toggle, .knowledge-toggle { display: flex; justify-content: space-between; align-items: center; width: 100%; border: none; background: transparent; padding: 4px 0; font-size: 11px; color: var(--ink-soft); cursor: pointer; }
.param-toggle span:last-child, .knowledge-toggle span:last-child { color: var(--accent); }
.param-body { display: grid; gap: 10px; }
.param-group { display: grid; gap: 4px; }
.param-group > label { font-size: 11px; color: var(--ink-soft); }
.param-group textarea { width: 100%; padding: 8px; border: 1px solid var(--line); border-radius: 8px; font-size: 11px; resize: vertical; background: rgba(255,255,255,.85); }
.knowledge-list { display: grid; gap: 4px; max-height: 140px; overflow: auto; }
.knowledge-item { display: flex; gap: 6px; align-items: center; padding: 5px 7px; border: 1px solid var(--line); border-radius: 8px; font-size: 11px; cursor: pointer; }
.knowledge-item.selected { border-color: var(--accent); background: rgba(15,107,92,.05); }
.k-title { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; }
.k-tag { font-size: 9px; color: var(--ink-soft); flex-shrink: 0; max-width: 46%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.chain-start { padding: 9px; font-size: 12px; }
.chain-hint { margin: 0; font-size: 10px; color: var(--ink-soft); line-height: 1.6; }
</style>
