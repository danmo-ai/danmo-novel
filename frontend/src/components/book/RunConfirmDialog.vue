<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '@/api/client'
import type { ContextPacket } from '@/types'
import { runKindLabel } from '@/utils/runLabels'
import {
  knowledgeAppliesTo,
  knowledgeAppliesToGenre,
  knowledgeGenreSummary,
  knowledgeStageLabel,
  specKnowledgeStage,
} from '@/utils/knowledgeStages'
import { useAppStore } from '@/stores/app'

const store = useAppStore()

const props = defineProps<{
  spec: Record<string, any>
  title: string
  modelLabel: string
}>()
const emit = defineEmits<{ confirm: [payload: { instruction: string; knowledgeIds: string[]; preservePaths: string[] }]; cancel: [] }>()

const loading = ref(true)
const loadError = ref('')
const packet = ref<ContextPacket | null>(null)
const instruction = ref('')
const expandedBlock = ref('')
type KnowledgeDoc = { id: string; title: string; summary: string; bytes: number; stages?: string[]; genres?: string[]; subgenres?: string[] }
const knowledgeDocs = ref<KnowledgeDoc[]>([])
const selectedKnowledge = ref<Set<string>>(new Set())
const knowledgeOpen = ref(false)
const knowledgeShowAll = ref(false)
const preserve = ref<Set<string>>(new Set())
const preserveOpen = ref(true)

const isReinit = computed(() => props.spec.kind === 'plan' && props.spec.mode === 'reinit')
const isLocalFinalize = computed(() => props.spec.kind === 'finalize')
const isDerivedRewrite = computed(() => {
  if (props.spec.kind !== 'rewrite_entity') return false
  const p = String(props.spec.targetPath || '')
  return p.startsWith('canon/cast/') || p === 'canon/reveal-schedule.md' || p === 'canon/author-lore.md'
    || p === 'canon/world.md' || p === 'canon/goldfinger.md' || p === 'book-bible.md'
})
const basisBlocks = computed(() => (packet.value?.blocks || []).filter((b) => b.id.startsWith('basis-')))
const contentBlocks = computed(() => (packet.value?.blocks || []).filter((b) => !b.id.startsWith('basis-')))
const preserveOptions = computed(() => {
  const files = store.currentBook?.files || {}
  const paths: string[] = []
  if (files['outline/book_outline.md']) paths.push('outline/book_outline.md')
  for (const p of ['book-bible.md', 'canon/world.md', 'canon/goldfinger.md', 'canon/reveal-schedule.md', 'canon/author-lore.md',
    'canon/platform-positioning.md', 'canon/writing-rules.md', 'canon/style-fingerprint.md', 'canon/locked-terms.yaml']) {
    if (files[p]) paths.push(p)
  }
  for (const c of store.currentBook?.cast || []) paths.push(`canon/cast/${c.stem}.md`)
  return paths
})

const kindLabel = computed(() => {
  if (props.spec.kind === 'rewrite_entity' && props.spec.entityKind === 'cast') return '重写人物卡'
  return runKindLabel(props.spec.kind, props.spec.mode)
})
const confirmLabel = computed(() => (isLocalFinalize.value ? '确认定稿检查' : '确认调用'))
const headKind = computed(() => (isLocalFinalize.value ? '定稿 · 本地检查确认' : `${kindLabel.value} · LLM 调用确认`))
const modelDisplay = computed(() => (isLocalFinalize.value ? '不调用模型' : (props.modelLabel || '按阶段分配')))
const riskNote = computed(() => {
  if (isLocalFinalize.value) {
    return '定稿只跑本地硬门，不消耗模型额度。生成定稿候选后须点「接受」才会回写账本并标记单元为已定稿。'
  }
  if (props.spec.kind === 'plan' && props.spec.mode === 'reinit') {
    const n = preserve.value.size
    return `重新立项重写总纲派生层；接受只覆盖未保留项${n ? `（已保留 ${n} 项精修文件）` : ''}；不写卷纲、不改单元/正文/账本。`
  }
  if (props.spec.kind === 'plan' && props.spec.mode === 'init') {
    return '立项一次生成总纲派生层（总纲+人物+揭示+底牌+世界）；不含卷纲。'
  }
  if (props.spec.kind === 'plan' && props.spec.mode === 'volume') {
    const vol = Number(String(props.spec.volumeId || 'v01').replace(/^v0*/, '')) || 1
    if (vol >= 2) {
      return `只写当前卷纲，播种缺失单元。上下文含第 ${vol - 1} 卷承接与相关人物（非全书卡）；不改总纲/设定/正文。`
    }
    return '只写当前卷纲，播种缺失单元；不改总纲/设定/人物/正文。'
  }
  if (isDerivedRewrite.value) {
    return '总纲派生设定：已附总纲等依据（只读）。接受仅写当前文件。'
  }
  if (props.spec.kind === 'rewrite_entity') {
    return '生产约束可独立优化；接受仅写当前文件。'
  }
  return ''
})
const totalBytes = computed(() => (packet.value?.blocks || []).reduce((s, b) => s + b.bytes, 0))

// Knowledge is offered per step and per genre: 立项 articles are not noise at
// 审校, and 玄幻 rules are not offered to a 悬疑 book.
const knowledgeStage = computed(() => specKnowledgeStage(props.spec))
const knowledgeStageText = computed(() => knowledgeStageLabel(knowledgeStage.value))
const bookGenre = computed(() => store.currentBook?.summary?.genre || '')
const bookSubgenre = computed(() => store.currentBook?.summary?.subgenre || '')
const genreStepMatch = (d: KnowledgeDoc) =>
  knowledgeAppliesTo(d.stages, knowledgeStage.value) && knowledgeAppliesToGenre(d, bookGenre.value, bookSubgenre.value)
const suitableKnowledge = computed(() => knowledgeDocs.value.filter(genreStepMatch))
const commonKnowledge = computed(() => suitableKnowledge.value.filter((d) => !d.genres?.length && !d.subgenres?.length))
const genreKnowledge = computed(() => suitableKnowledge.value.filter((d) => d.genres?.length || d.subgenres?.length))
const hiddenKnowledge = computed(() => knowledgeDocs.value.length - suitableKnowledge.value.length)
const shownKnowledge = computed(() => (knowledgeShowAll.value ? knowledgeDocs.value : suitableKnowledge.value))
const genreText = computed(() => (bookGenre.value ? bookGenre.value + (bookSubgenre.value ? ` / ${bookSubgenre.value}` : '') : '未设置题材'))

/** Why a row is greyed out when the author chooses to see everything. */
function knowledgeMismatch(d: KnowledgeDoc) {
  const reasons: string[] = []
  if (!knowledgeAppliesTo(d.stages, knowledgeStage.value)) reasons.push('步骤不符')
  if (!knowledgeAppliesToGenre(d, bookGenre.value, bookSubgenre.value)) reasons.push('题材不符')
  return reasons.join(' / ')
}

onMounted(async () => {
  try {
    packet.value = await api.post<ContextPacket>('/api/runs/preview', props.spec)
  } catch (e: any) {
    loadError.value = e?.message || '预览上下文失败'
  } finally {
    loading.value = false
  }
  try {
    knowledgeDocs.value = (await api.get<any[]>('/api/knowledge')) || []
  } catch { /* 知识库为空或不可读时忽略 */ }
})

function toggleKnowledge(id: string) {
  const s = new Set(selectedKnowledge.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  selectedKnowledge.value = s
}
function togglePreserve(path: string) {
  const s = new Set(preserve.value)
  if (s.has(path)) s.delete(path)
  else s.add(path)
  preserve.value = s
}

function confirm() {
  emit('confirm', { instruction: instruction.value.trim(), knowledgeIds: [...selectedKnowledge.value], preservePaths: [...preserve.value] })
}
</script>

<template>
  <div class="modal-mask" @click.self="emit('cancel')">
    <div class="run-dialog panel">
      <header class="dlg-head">
        <div>
          <small>{{ headKind }}</small>
          <h3>{{ title }}</h3>
        </div>
        <button class="dlg-close" @click="emit('cancel')">×</button>
      </header>

      <div class="dlg-meta">
        <span class="meta-item"><small>模型</small><strong>{{ modelDisplay }}</strong></span>
        <span class="meta-item"><small>调用预算</small><strong>{{ isLocalFinalize ? '0 次（本地）' : `${packet?.policy.maxCalls ?? '…'} 次` }}</strong></span>
        <span class="meta-item"><small>上下文</small><strong>{{ packet ? `${packet.blocks.length} 块 · ${(totalBytes / 1024).toFixed(1)} KB` : '…' }}</strong></span>
      </div>

      <div class="dlg-body">
        <div class="dlg-context">
          <small class="ctx-label">将发送给模型的内容（依据只读；接受才写盘）</small>
          <p v-if="loading" class="ctx-loading">正在构建上下文预览…</p>
          <p v-else-if="loadError" class="ctx-error">{{ loadError }}</p>
          <template v-else-if="packet">
            <div class="ctx-system"><small>系统提示词</small><p>{{ packet.systemPrompt }}</p></div>
            <div v-if="basisBlocks.length" class="ctx-group">
              <small class="ctx-group-label">重写依据（只读，不写回）</small>
              <details v-for="b in basisBlocks" :key="b.id" class="ctx-block basis" :open="expandedBlock === b.id" @toggle="expandedBlock = ($event.target as HTMLDetailsElement).open ? b.id : ''">
                <summary><span class="ctx-block-label">{{ b.label }}</span><span class="ctx-block-meta">{{ b.source }} · {{ b.bytes }} B</span></summary>
                <pre>{{ b.content }}</pre>
              </details>
            </div>
            <div class="ctx-blocks">
              <details v-for="b in contentBlocks" :key="b.id" class="ctx-block" :open="expandedBlock === b.id" @toggle="expandedBlock = ($event.target as HTMLDetailsElement).open ? b.id : ''">
                <summary><span class="ctx-block-label">{{ b.label }}</span><span class="ctx-block-meta">{{ b.source }} · {{ b.bytes }} B</span></summary>
                <pre>{{ b.content }}</pre>
              </details>
            </div>
          </template>
        </div>

        <div v-if="isReinit && preserveOptions.length" class="dlg-preserve">
          <button class="preserve-toggle" @click="preserveOpen = !preserveOpen">
            <span>接受时保留精修文件（不覆盖）{{ preserve.size ? ` · 已选 ${preserve.size}` : '' }}</span>
            <span>{{ preserveOpen ? '收起' : '展开' }}</span>
          </button>
          <div v-if="preserveOpen" class="preserve-list">
            <p class="preserve-note">勾选后，重新立项接受时这些文件保持现状；其余总纲派生设定按候选覆盖。</p>
            <label v-for="p in preserveOptions" :key="p" class="preserve-item" :class="{ selected: preserve.has(p) }">
              <input type="checkbox" :checked="preserve.has(p)" @change="togglePreserve(p)" />
              <span>{{ p }}</span>
            </label>
          </div>
        </div>

        <div class="dlg-knowledge" v-if="!isLocalFinalize && knowledgeDocs.length">
          <button class="knowledge-toggle" @click="knowledgeOpen = !knowledgeOpen">
            <span>参考知识（可选，内置 + 作者资料库）{{ selectedKnowledge.size ? ` · 已选 ${selectedKnowledge.size} 篇` : '' }}</span>
            <span>{{ knowledgeOpen ? '收起' : '展开' }}</span>
          </button>
          <div v-if="knowledgeStage" class="knowledge-scope">
            <span v-if="!knowledgeShowAll">
              「{{ knowledgeStageText }}」· 题材 {{ genreText }} · 可用 {{ suitableKnowledge.length }} 篇
              （公共 {{ commonKnowledge.length }}<template v-if="genreKnowledge.length"> + 题材专属 {{ genreKnowledge.length }}</template>）<template v-if="hiddenKnowledge">，已隐藏 {{ hiddenKnowledge }} 篇</template>
            </span>
            <span v-else>「{{ knowledgeStageText }}」可用 {{ suitableKnowledge.length }} 篇 · 已显示全部 {{ knowledgeDocs.length }} 篇，灰底为不适用</span>
            <button v-if="hiddenKnowledge" class="scope-link" @click="knowledgeShowAll = !knowledgeShowAll">{{ knowledgeShowAll ? '只看适用' : '显示全部' }}</button>
          </div>
          <div v-if="knowledgeOpen" class="knowledge-list">
            <p class="knowledge-note">勾选的文章会作为「方法/风格参考」附加到本次调用，不写入正文事实。</p>
            <label v-for="d in shownKnowledge" :key="d.id" class="knowledge-item"
              :class="{ selected: selectedKnowledge.has(d.id), 'off-stage': !!knowledgeMismatch(d) }">
              <input type="checkbox" :checked="selectedKnowledge.has(d.id)" @change="toggleKnowledge(d.id)" />
              <span class="knowledge-info">
                <strong>{{ d.title }}</strong>
                <small>{{ d.summary }}</small>
              </span>
              <span class="knowledge-side">
                <span class="knowledge-bytes">{{ (d.bytes / 1024).toFixed(1) }}KB</span>
                <span v-if="knowledgeMismatch(d)" class="knowledge-flag">{{ knowledgeMismatch(d) }}</span>
                <span v-else-if="d.genres?.length || d.subgenres?.length" class="knowledge-tag">{{ knowledgeGenreSummary(d) }}</span>
              </span>
            </label>
            <p v-if="!shownKnowledge.length" class="knowledge-empty">「{{ knowledgeStageText }}」下还没有适用的文章；可在「知识库」页给文章补上写作步骤和题材标记。</p>
          </div>
        </div>

        <p v-if="riskNote" class="dlg-risk">{{ riskNote }}</p>
      </div>

      <div v-if="!isLocalFinalize" class="dlg-instruction">
        <label>追加指令（可选，会附加到本次调用的用户提示）</label>
        <textarea v-model="instruction" rows="3" placeholder="例如：保持既有事实，重点精简第 2 节；或：本卷新增人物 X，纳入索引…" spellcheck="false" />
      </div>

      <footer class="dlg-actions">
        <button @click="emit('cancel')">取消</button>
        <button class="primary" :disabled="loading || !!loadError" @click="confirm">{{ confirmLabel }}</button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.modal-mask { position: fixed; inset: 0; background: rgba(20,35,31,.35); display: grid; place-items: center; z-index: 60; padding: 24px; }
.run-dialog { width: min(720px, 96vw); max-height: 90vh; display: flex; flex-direction: column; padding: 0; overflow: hidden; }
.dlg-head { display: flex; justify-content: space-between; align-items: flex-start; padding: 22px 24px 14px; }
.dlg-head small { color: var(--accent); font-size: 11px; font-weight: 700; letter-spacing: .05em; }
.dlg-head h3 { margin: 6px 0 0; font-family: var(--font-prose); font-size: 22px; }
.dlg-close { border: none; background: transparent; font-size: 22px; color: var(--ink-soft); padding: 0 4px; }
.dlg-risk { margin: 0 24px 12px; padding: 10px 12px; background: color-mix(in srgb, var(--accent) 10%, transparent); border-radius: 8px; color: var(--ink-soft); font-size: 13px; line-height: 1.5; }
.dlg-meta { display: flex; gap: 24px; padding: 0 24px 16px; border-bottom: 1px solid var(--line); flex-wrap: wrap; }
.meta-item { display: grid; gap: 2px; }
.meta-item small { color: var(--ink-soft); font-size: 10px; }
.meta-item strong { font-size: 13px; }
.dlg-body { flex: 1; min-height: 0; overflow-y: auto; overscroll-behavior: contain; }
.dlg-context { padding: 16px 24px; }
.ctx-label { color: var(--ink-soft); font-size: 11px; font-weight: 600; display: block; margin-bottom: 10px; }
.ctx-loading, .ctx-error { color: var(--ink-soft); font-size: 13px; }
.ctx-error { color: var(--bad); }
.ctx-system { margin-bottom: 12px; padding: 12px 14px; background: rgba(15,107,92,.05); border-radius: 10px; }
.ctx-system small { color: var(--accent); font-size: 10px; font-weight: 700; }
.ctx-system p { margin: 6px 0 0; font-size: 12px; line-height: 1.7; color: var(--ink-soft); white-space: pre-wrap; }
.ctx-blocks { display: grid; gap: 6px; }
.ctx-block { border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
.ctx-block summary { display: flex; justify-content: space-between; align-items: center; gap: 10px; padding: 10px 12px; cursor: pointer; background: rgba(255,255,255,.5); }
.ctx-block-label { font-size: 12px; font-weight: 600; }
.ctx-block-meta { font-size: 10px; color: var(--ink-soft); }
.ctx-block pre { margin: 0; padding: 12px; font: 11px/1.7 ui-monospace, monospace; white-space: pre-wrap; color: var(--ink-soft); max-height: 260px; overflow: auto; border-top: 1px solid var(--line); }
.ctx-group { margin-bottom: 12px; }
.ctx-group-label { color: var(--accent); font-size: 11px; font-weight: 700; display: block; margin-bottom: 6px; }
.ctx-block.basis summary { background: rgba(15,107,92,.06); }
.dlg-preserve { border-top: 1px solid var(--line); }
.preserve-toggle { display: flex; justify-content: space-between; align-items: center; width: 100%; border: none; background: transparent; padding: 14px 24px; text-align: left; font-size: 13px; }
.preserve-toggle span:last-child { color: var(--accent); font-size: 12px; }
.preserve-list { padding: 0 24px 14px; max-height: 220px; overflow: auto; }
.preserve-note { margin: 0 0 10px; font-size: 11px; color: var(--ink-soft); line-height: 1.6; }
.preserve-item { display: flex; gap: 10px; align-items: center; padding: 8px 10px; border: 1px solid var(--line); border-radius: 8px; margin-bottom: 4px; font-size: 12px; cursor: pointer; }
.preserve-item.selected { border-color: var(--accent); background: rgba(15,107,92,.05); }
.dlg-knowledge { border-top: 1px solid var(--line); }
.knowledge-toggle { display: flex; justify-content: space-between; align-items: center; width: 100%; border: none; background: transparent; padding: 14px 24px; text-align: left; font-size: 13px; color: var(--ink); }
.knowledge-toggle span:last-child { color: var(--accent); font-size: 12px; }
.knowledge-list { padding: 0 24px 14px; }
.knowledge-scope { display: flex; justify-content: space-between; align-items: center; gap: 10px; padding: 0 24px 10px; font-size: 11px; color: var(--ink-soft); }
.scope-link { border: none; background: transparent; color: var(--accent); font-size: 11px; padding: 0; }
.knowledge-note { margin: 0 0 10px; font-size: 11px; color: var(--ink-soft); line-height: 1.6; }
.knowledge-empty { margin: 0; padding: 10px 0; font-size: 12px; color: var(--ink-soft); line-height: 1.6; }
.knowledge-item { display: flex; align-items: flex-start; gap: 10px; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; margin-bottom: 6px; cursor: pointer; }
.knowledge-item.off-stage { opacity: .55; }
.knowledge-item.selected { border-color: var(--accent); background: rgba(15,107,92,.05); }
.knowledge-item input { margin-top: 3px; }
.knowledge-info { display: grid; gap: 2px; flex: 1; min-width: 0; }
.knowledge-info strong { font-size: 13px; }
.knowledge-info small { font-size: 11px; color: var(--ink-soft); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.knowledge-side { display: grid; gap: 3px; justify-items: end; flex-shrink: 0; }
.knowledge-bytes { font-size: 10px; color: var(--ink-soft); }
.knowledge-flag { font-size: 10px; color: var(--bad); white-space: nowrap; }
.knowledge-tag { font-size: 10px; color: var(--accent); white-space: nowrap; max-width: 110px; overflow: hidden; text-overflow: ellipsis; }
.dlg-instruction { padding: 14px 24px; border-top: 1px solid var(--line); }
.dlg-instruction label { display: block; font-size: 12px; color: var(--ink-soft); margin-bottom: 8px; }
.dlg-instruction textarea { width: 100%; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; font-size: 13px; resize: vertical; background: rgba(255,255,255,.85); }
.dlg-actions { display: flex; justify-content: flex-end; gap: 10px; padding: 14px 24px 20px; }
</style>
