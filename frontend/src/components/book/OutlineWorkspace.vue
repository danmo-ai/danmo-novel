<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useAppStore } from '@/stores/app'
import type { StructuredMarkdown, UnitInfo } from '@/types'
import StoryTimeline from '@/components/book/StoryTimeline.vue'
import StructuredOutline from '@/components/book/StructuredOutline.vue'

const props = defineProps<{ saving: boolean; busy: boolean }>()
const emit = defineEmits<{
  openUnit: [unit: UnitInfo]
  save: [path: string, content: string]
  saveField: [payload: { path: string; kind: string; entityId: string; field: string; value: string }, done: (ok: boolean) => void]
  plan: [mode: 'init' | 'reinit' | 'volume', volume?: number]
  structureReview: [path: string]
}>()
const store = useAppStore()
const activeVolume = ref(0)
const showBook = ref(true)
const rawMode = ref(false)
const rawDraft = ref('')
const unitViewMode = ref<'index' | 'timeline'>('index')
const editing = ref<{ path: string; kind: string; entityId: string; field: string; value: string; label: string } | null>(null)
const book = computed(() => store.currentBook)
const bookRaw = computed(() => book.value?.files['outline/book_outline.md'] || '')
const bookDoc = computed(() => book.value?.structuredOutlines?.['outline/book_outline.md'])
const volume = computed(() => book.value?.volumes.find((v) => v.volume === activeVolume.value) || book.value?.volumes[0])
const units = computed(() => (book.value?.units || []).filter((u) => !volume.value || u.volume === volume.value.volume))
const volumePath = computed(() => `outline/volumes/v${String(volume.value?.volume || activeVolume.value || 1).padStart(2, '0')}.md`)
const volumeDoc = computed(() => volume.value?.structured || book.value?.structuredOutlines?.[volumePath.value])
const readOnly = computed(() => Boolean(book.value?.summary.readOnly))

watch(volume, () => { rawMode.value = false })
function volumeId(n?: number) { return `v${String(n || 1).padStart(2, '0')}` }
function startRaw() {
  rawDraft.value = showBook.value && !volume.value ? bookRaw.value : (volume.value?.raw || bookRaw.value)
  rawMode.value = true
}
function saveRaw() {
  const path = volume.value && !showBook.value ? volumePath.value : 'outline/book_outline.md'
  emit('save', path, rawDraft.value)
  rawMode.value = false
}
function editField(payload: { kind: string; entityId: string; field: string; value: string; label: string }) {
  const path = showBook.value || !volume.value ? 'outline/book_outline.md' : volumePath.value
  editing.value = { path, ...payload }
}
function saveField() {
  if (!editing.value) return
  const { label: _, ...payload } = editing.value
  emit('saveField', payload, (ok) => { if (ok) editing.value = null })
}
function selectVolume(n: number) {
  activeVolume.value = n
  showBook.value = false
}
function review(path: string) { emit('structureReview', path) }
function openIndexedUnit(unit?: UnitInfo) { if (unit) emit('openUnit', unit) }
</script>

<template>
  <section class="outline-workspace">
    <div class="view-title">
      <div><p class="eyebrow">Structure</p><h2>卷纲与结构</h2><p>上层锁定不可逆决策，下层在合同内细化。简单修正直接编辑，复杂修改走候选重写。</p></div>
      <div class="view-actions"><button :disabled="busy || readOnly || bookDoc?.legacy" @click="emit('plan', bookRaw ? 'reinit' : 'init')">{{ bookDoc?.legacy ? '需 Agent 迁移' : (bookRaw ? '重新立项' : '立项') }}</button></div>
    </div>

    <div class="outline-tabs">
      <button :class="{ active: showBook }" @click="showBook = true">全书总纲</button>
      <button v-for="v in book?.volumes" :key="v.volume" :class="{ active: !showBook && volume?.volume === v.volume }" @click="selectVolume(v.volume)">第 {{ v.volume }} 卷</button>
    </div>

    <article class="panel outline-panel">
      <header class="document-actions">
        <div><small>{{ showBook ? '总纲与分卷索引' : `第 ${volume?.volume || 1} 卷与单元索引` }}</small><span v-if="(showBook ? bookDoc?.legacy : volumeDoc?.legacy)" class="legacy">旧格式 · 需 Agent 批量迁移</span><span v-else class="valid">结构化 Markdown</span></div>
        <div>
          <button v-if="!rawMode && !(showBook ? bookDoc?.legacy : volumeDoc?.legacy)" @click="startRaw">原始 MD</button>
          <button v-if="!rawMode && !readOnly && !(showBook ? bookDoc?.legacy : volumeDoc?.legacy)" @click="review(showBook ? 'outline/book_outline.md' : volumePath)">结构审校</button>
          <button v-if="!rawMode && !(showBook ? bookDoc?.legacy : volumeDoc?.legacy)" :disabled="busy || readOnly" @click="emit('plan', showBook ? 'reinit' : 'volume', showBook ? undefined : (volume?.volume || 1))">{{ showBook ? '重写总纲' : (volume ? '重写卷纲' : '生成卷纲') }}</button>
        </div>
      </header>

      <div v-if="rawMode" class="raw-editor">
        <p>高级模式：保存的是同一份 Markdown。新结构保存前会完整解析校验；旧格式仅用于兼容阅读。</p>
        <textarea v-model="rawDraft" spellcheck="false" />
        <div><button @click="rawMode = false">取消</button><button class="primary" :disabled="saving || readOnly" @click="saveRaw">保存 MD</button></div>
      </div>
      <template v-else-if="showBook">
        <StructuredOutline v-if="bookDoc && !bookDoc.legacy" :document="bookDoc" :read-only="readOnly" @edit-field="editField" />
        <div v-else class="legacy-document"><p>这本书是旧格式总纲，不进入新的生产与编辑链路。请通过后续 Agent 批量迁移生成结构化候选并校验接受；迁移前原文和正文保持不变。</p><pre>{{ bookRaw }}</pre></div>
        <p v-if="!bookRaw" class="empty">暂无全书总纲，请先立项。</p>
      </template>
      <template v-else>
        <StructuredOutline v-if="volumeDoc && !volumeDoc.legacy" :document="volumeDoc" :units="units" :read-only="readOnly" @edit-field="editField" @open-unit="openIndexedUnit" />
        <div v-else-if="volume" class="legacy-document"><p>旧格式卷纲不再进入生产。请先通过 Agent 批量迁移总纲与卷纲；迁移完成后，这里显示完整的单元因果卡与继承校验。</p><pre>{{ volume.raw }}</pre></div>
        <div v-else class="empty"><p>总纲已就绪。生成当前卷纲后，将在这里展示卷级节拍与单元因果卡。</p><button :disabled="busy || readOnly" @click="emit('plan', 'volume', activeVolume || 1)">生成第 {{ activeVolume || 1 }} 卷卷纲</button></div>
      </template>
    </article>

    <article v-if="!showBook && volume" class="panel timeline-panel">
      <div class="timeline-heading"><div class="unit-view-tabs"><button :class="{ active: unitViewMode === 'index' }" @click="unitViewMode = 'index'">单元生产索引 · {{ units.length }}</button><button :class="{ active: unitViewMode === 'timeline' }" @click="unitViewMode = 'timeline'">剧情时钟</button></div><span>点击单元进入细纲与正文生产</span></div>
      <StoryTimeline v-if="unitViewMode === 'timeline'" :volume="volume.volume" @open="emit('openUnit', $event)" />
      <div v-else class="unit-production-list"><button v-for="unit in units" :key="unit.unitId" @click="emit('openUnit', unit)"><strong>{{ unit.unitId }}</strong><span>{{ unit.title }}</span><small>{{ unit.chapterFrom }}–{{ unit.chapterTo }} 章 · {{ unit.status }}</small></button></div>
    </article>

    <div v-if="editing" class="edit-backdrop" @click.self="editing = null">
      <form class="field-editor panel" @submit.prevent="saveField"><header><div><small>局部编辑 · 最小 MD 补丁</small><h3>{{ editing.label }}</h3></div><button type="button" @click="editing = null">×</button></header><textarea v-model="editing.value" autofocus spellcheck="false" /><p>保存时会重新解析整份 Markdown 并校验父子合同；不会重排其他文字或注释。</p><footer><button type="button" @click="editing = null">取消</button><button class="primary" :disabled="saving || !editing.value.trim()">保存字段</button></footer></form>
    </div>
  </section>
</template>

<style scoped>
.outline-workspace { display: grid; gap: 18px; }
.view-title { display: flex; justify-content: space-between; gap: 20px; align-items: flex-end; }
.view-title h2 { margin: 4px 0 6px; font-family: var(--font-prose); font-size: 28px; }.view-title p { margin: 0; color: var(--ink-soft); font-size: 13px; }.eyebrow { color: var(--accent) !important; font-size: 11px !important; letter-spacing: .12em; }
.view-actions, .document-actions > div, .document-actions { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.outline-tabs { display: flex; gap: 5px; overflow-x: auto; border-bottom: 1px solid var(--line); padding-bottom: 10px; }.outline-tabs button { white-space: nowrap; }.outline-tabs button.active { color: white; background: var(--accent); border-color: var(--accent); }
.outline-panel { padding: 20px; }.document-actions { justify-content: space-between; padding-bottom: 16px; margin-bottom: 20px; border-bottom: 1px solid var(--line); }.document-actions small { display: block; color: var(--accent); margin-bottom: 4px; }.document-actions span { font-size: 10px; color: var(--ok); }.document-actions .legacy { color: var(--warn); }
.legacy-document > p { color: var(--warn); font-size: 12px; line-height: 1.7; }.legacy-document pre { white-space: pre-wrap; font: 13px/1.85 var(--font-ui); color: var(--ink-soft); max-height: 680px; overflow: auto; }
.raw-editor > p { color: var(--ink-soft); font-size: 12px; }.raw-editor textarea { width: 100%; min-height: 600px; padding: 16px; border: 1px solid var(--line); border-radius: 12px; font: 13px/1.7 ui-monospace, monospace; resize: vertical; }.raw-editor > div { display: flex; justify-content: flex-end; gap: 8px; margin-top: 10px; }
.timeline-panel { padding: 18px; }.timeline-heading { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 16px; }.timeline-heading > span { color: var(--ink-soft); font-size: 11px; }.unit-view-tabs { display: flex; gap: 6px; }.unit-view-tabs button.active { color: var(--accent); border-color: var(--accent); }
.unit-production-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(190px, 1fr)); gap: 8px; }.unit-production-list button { display: grid; text-align: left; gap: 4px; padding: 12px; }.unit-production-list strong { color: var(--accent); }.unit-production-list span { font-size: 12px; }.unit-production-list small { color: var(--ink-soft); }
.empty { padding: 30px; text-align: center; color: var(--ink-soft); }
.edit-backdrop { position: fixed; inset: 0; z-index: 30; display: grid; place-items: center; padding: 20px; background: rgba(20,35,31,.25); }.field-editor { width: min(680px, 100%); padding: 20px; }.field-editor header { display: flex; justify-content: space-between; align-items: flex-start; }.field-editor h3 { margin: 4px 0 16px; font-size: 18px; }.field-editor small { color: var(--accent); }.field-editor textarea { width: 100%; min-height: 180px; padding: 12px; border: 1px solid var(--line); border-radius: 10px; resize: vertical; font: 14px/1.8 var(--font-ui); }.field-editor p { color: var(--ink-soft); font-size: 11px; line-height: 1.7; }.field-editor footer { display: flex; justify-content: flex-end; gap: 8px; }
@media (max-width: 720px) { .view-title, .document-actions { align-items: flex-start; flex-direction: column; }.outline-panel { padding: 14px; }.timeline-heading { align-items: flex-start; flex-direction: column; } }
</style>
