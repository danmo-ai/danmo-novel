<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useAppStore } from '@/stores/app'
import type { UnitInfo } from '@/types'
import StoryTimeline from '@/components/book/StoryTimeline.vue'

const props = defineProps<{
  saving: boolean
  busy: boolean
}>()
const emit = defineEmits<{
  openUnit: [unit: UnitInfo]
  save: [path: string, content: string]
  generate: [kind: string]
  plan: [mode: 'init' | 'reinit' | 'volume', volume?: number]
}>()
const store = useAppStore()
const activeVolume = ref(0)
const editing = ref(false)
const editTarget = ref<'volume' | 'book'>('volume')
const draft = ref('')
const showBook = ref(false)
const unitViewMode = ref<'index' | 'timeline'>('index')

const book = computed(() => store.currentBook)
const volume = computed(() => book.value?.volumes.find((v) => v.volume === activeVolume.value) || book.value?.volumes[0])
const units = computed(() => (book.value?.units || []).filter((u) => !volume.value || u.volume === volume.value.volume))
const bookOutline = computed(() => book.value?.files['outline/book_outline.md'] || '')

watch(volume, () => { editing.value = false })

function volumeId(n?: number) { return `v${String(n || 1).padStart(2, '0')}` }
function startEdit(target: 'volume' | 'book') {
  editTarget.value = target
  draft.value = target === 'book' ? bookOutline.value : volume.value?.raw || ''
  editing.value = true
}
function save() {
  const path = editTarget.value === 'book'
    ? 'outline/book_outline.md'
    : `outline/volumes/${volumeId(volume.value?.volume)}.md`
  if (path) emit('save', path, draft.value)
  editing.value = false
}
function unitSummary(u: UnitInfo) {
  const raw = u.outlineRaw || ''
  const m = raw.match(/^function:\s*(.*)$/m)
  return (m?.[1] || '').trim().replace(/^['"]|['"]$/g, '') || u.title || '尚未补全单元功能'
}

type MarkdownSection = { title: string; body: string }
function mdTitle(raw: string) {
  const lines = raw.split('\n')
  const h1 = lines.find((l) => /^#\s+/.test(l))?.replace(/^#\s+/, '')
  if (h1) return h1.trim()
  // 总纲常以「## 全书总纲…」起笔，没有单独 # 标题。
  return lines.find((l) => /^##\s+/.test(l))?.replace(/^##\s+/, '').trim() || ''
}
function mdSections(raw: string): MarkdownSection[] {
  const result: MarkdownSection[] = []
  let title = ''
  let body: string[] = []
  for (const line of raw.split('\n')) {
    // 认 ## / ###：总纲常用 ### 一句话 / ### 分卷走向。
    const match = line.match(/^#{2,3}\s+(.+)/)
    if (match) {
      if (title) result.push({ title, body: body.join('\n').trim() })
      title = match[1].trim()
      body = []
    } else if (title) body.push(line)
  }
  if (title) result.push({ title, body: body.join('\n').trim() })
  return result
}
function findSectionMeta(raw: string, names: string[]) {
  const sections = mdSections(raw)
  for (const name of names) {
    const exact = sections.find((s) => s.title === name || s.title.startsWith(name))
    if (exact) return { title: exact.title, body: exact.body }
  }
  for (const name of names) {
    const fuzzy = sections.find((s) => s.title.includes(name))
    if (fuzzy) return { title: fuzzy.title, body: fuzzy.body }
  }
  return { title: '', body: '' }
}
function findSection(raw: string, names: string[]) {
  return findSectionMeta(raw, names).body
}
function cleanInline(text: string) {
  return text.replace(/\*\*/g, '').replace(/`/g, '').trim()
}
function proseBlock(raw: string) {
  return cleanInline(raw.replace(/\n+/g, ' '))
}
function bullets(raw: string) {
  return raw.split('\n').filter((l) => /^\s*[-*]\s+/.test(l)).map((l) => cleanInline(l.replace(/^\s*[-*]\s+/, ''))).filter(Boolean)
}
function tableRows(raw: string) {
  // 只读「单元索引」表；同文件里的「剧情时钟」表也有 unit_id，扫全文会把 story_day 错填进功能列。
  const section = findSection(raw, ['单元索引', '单元一览', 'Unit Index']) || raw
  const lines = section.split('\n').filter((l) => l.trim().startsWith('|'))
  // 跳过表头：对方列名含 unit_id / 单元 / 功能 等。
  return lines.map((line) => {
    const cells = line.trim().replace(/^\||\|$/g, '').split('|').map((c) => c.trim())
    return { id: cells[0], range: cells[1], purpose: cells[2], boundary: cells[3], hook: cells[4], cells }
  }).filter((r) => /^v\d+-U\d+$/i.test(r.id))
}
// 只认合法 stem（英文小写连字符）；人物弧/卷情绪等散文行不是人物。
const STEM_RE = /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/
function castStems(raw: string): string[] {
  return bullets(raw)
    // 支持 "- wen-nianci：说明" / "- stem (role)"；只取 stem 标识。
    .map((s) => s.replace(/`/g, '').split(/[\s：:（(]/)[0].trim())
    .filter((s) => STEM_RE.test(s))
}
const volumeView = computed(() => {
  const raw = volume.value?.raw || ''
  return {
    title: mdTitle(raw) || `第 ${volume.value?.volume || 1} 卷`,
    goals: bullets(findSection(raw, ['本卷目标', '卷目标', '卷命题', '本卷梗概'])),
    cast: castStems(findSection(raw, ['本卷人物', '人物'])),
    ending: bullets(findSection(raw, ['卷末状态', '终局边界', '卷终局边界', '卷末收束'])),
    units: tableRows(raw),
    otherSections: mdSections(raw).filter((s) => !['本卷目标', '卷目标', '本卷人物', '单元索引', '单元剧情时钟', '卷末状态'].some((name) => s.title.includes(name))),
  }
})
const bookView = computed(() => {
  const raw = bookOutline.value
  const premise = proseBlock(findSection(raw, ['一句话', '一句话故事', '核心梗概', 'logline']))
  const appeal = findSectionMeta(raw, ['核心快感', '读者承诺', '作者意图', '核心卖点'])
  const engine = bullets(findSection(raw, ['双线引擎', '故事引擎', '主引擎']))
  const suspense = bullets(findSection(raw, ['核心悬念', '主悬念']))
  const volumeBody = findSection(raw, ['分卷走向', '分卷规划', '卷规划'])
  const volumeBullets = bullets(volumeBody)
  const volumeSections = mdSections(raw)
    .filter((s) => /第\s*\d+\s*卷/.test(s.title))
    .map((s) => `${cleanInline(s.title)}：${proseBlock(s.body)}`.replace(/：$/, ''))
  return {
    title: mdTitle(raw),
    premise,
    promise: proseBlock(appeal.body),
    promiseLabel: /快感|卖点/.test(appeal.title) ? '核心快感' : '读者承诺',
    engine,
    suspense,
    volumes: volumeBullets.length ? volumeBullets : volumeSections,
  }
})
function castName(stem: string) {
  const card = book.value?.cast.find((c) => c.stem === stem)
  return card?.name.replace(/^#+\s*/, '') || stem
}
function openIndexedUnit(id: string) {
  const target = units.value.find((u) => u.unitId === id)
  if (target) emit('openUnit', target)
}
function unitStatus(id: string) {
  return units.value.find((u) => u.unitId === id)?.status || 'proposed'
}
function statusText(s: string) {
  return ({ proposed: '待细纲', accepted: '待写', drafted: '草稿', reviewed: '定稿' } as Record<string, string>)[s] || s
}
</script>

<template>
  <section class="outline-workspace">
    <div class="view-title">
      <div>
        <p class="eyebrow">Structure</p>
        <h2>卷纲与结构</h2>
        <p>卷级分配，单元落笔。单元合同与正文在「写作」页生产。</p>
      </div>
      <div class="view-actions">
        <button :disabled="busy || book?.summary.readOnly" @click="emit('plan', bookOutline ? 'reinit' : 'init')">
          {{ bookOutline ? '重新立项' : '立项' }}
        </button>
      </div>
    </div>

    <div v-if="editing" class="outline-edit panel">
      <div class="edit-head">
        <span>正在编辑：{{ editTarget === 'book' ? 'outline/book_outline.md' : `outline/volumes/${volumeId(volume?.volume)}.md` }}</span>
        <div><button @click="editing = false">取消</button><button class="primary" :disabled="saving" @click="save">保存更改</button></div>
      </div>
      <textarea v-model="draft" class="source-editor" spellcheck="false" />
    </div>

    <template v-else>
      <details class="book-overview panel" :open="showBook" @toggle="showBook = ($event.target as HTMLDetailsElement).open">
        <summary>
          <span><small>全书大纲</small><strong>故事总览与分卷方向</strong></span>
          <span class="summary-action">{{ showBook ? '收起' : '展开' }}</span>
        </summary>
        <div class="book-body">
          <div class="book-body-head"><button class="text-button" @click="startEdit('book')">编辑全书大纲</button></div>
          <div v-if="bookOutline" class="book-summary">
            <div v-if="bookView.title" class="book-title-line"><small>总纲</small><p>{{ bookView.title }}</p></div>
            <div v-if="bookView.premise" class="book-premise"><small>一句话故事</small><p>{{ bookView.premise }}</p></div>
            <div v-if="bookView.promise"><small>{{ bookView.promiseLabel }}</small><p>{{ bookView.promise }}</p></div>
            <div v-if="bookView.engine.length"><small>故事引擎</small><ul><li v-for="item in bookView.engine" :key="item">{{ item }}</li></ul></div>
            <div v-if="bookView.suspense.length"><small>核心悬念</small><ul><li v-for="item in bookView.suspense" :key="item">{{ item }}</li></ul></div>
            <div v-if="bookView.volumes.length" class="book-volumes"><small>分卷走向</small><ul><li v-for="item in bookView.volumes" :key="item">{{ item }}</li></ul></div>
            <p v-if="!bookView.premise && !bookView.promise && !bookView.volumes.length" class="muted">未能解析出结构小节；下方可阅读原文，或按「一句话 / 核心快感 / 分卷走向」补标题。</p>
          </div>
          <details class="narrative-source">
            <summary>阅读完整总纲</summary>
            <pre class="markdown">{{ bookOutline }}</pre>
          </details>
          <p v-if="!bookOutline" class="muted">暂无全书总纲，请通过「立项」或「重新立项」生成。</p>
        </div>
      </details>

      <div v-if="book?.volumes?.length" class="volume-tabs">
        <button v-for="v in book.volumes" :key="v.volume" :class="{ active: volume?.volume === v.volume }" @click="activeVolume = v.volume">
          第 {{ v.volume }} 卷
        </button>
      </div>

      <article v-if="bookOutline" class="volume-document panel">
        <header>
          <div><small>当前卷</small><h3>{{ volume ? volumeView.title : '第 1 卷' }}</h3></div>
          <div class="volume-actions">
            <button :disabled="busy || book?.summary.readOnly" @click="emit('plan', 'volume', volume?.volume || 1)">
              {{ volume ? '重写卷纲' : '生成卷纲' }}
            </button>
            <button v-if="volume" class="primary" :disabled="saving" @click="startEdit('volume')">编辑卷纲</button>
          </div>
        </header>
        <template v-if="volume">
          <div class="volume-structure">
            <section class="volume-goal">
              <small>本卷目标</small>
              <ul v-if="volumeView.goals.length"><li v-for="goal in volumeView.goals" :key="goal">{{ goal }}</li></ul>
              <p v-else class="muted">尚未填写卷目标</p>
            </section>
            <section class="volume-cast">
              <small>本卷人物 · {{ volumeView.cast.length }}</small>
              <div v-if="volumeView.cast.length" class="cast-chips"><span v-for="stem in volumeView.cast" :key="stem" :title="stem">{{ castName(stem) }}</span></div>
              <p v-else class="cast-empty">本卷人物未以 stem 列表声明。请在卷纲「本卷人物」章节用 <code>- wen-nianci</code> 形式逐行列出，人物弧等说明文字放其他章节。</p>
            </section>
            <section v-if="volumeView.ending.length" class="volume-ending">
              <small>卷末状态与边界</small><ul><li v-for="item in volumeView.ending" :key="item">{{ item }}</li></ul>
            </section>
          </div>
          <div class="volume-unit-index">
            <div class="index-heading">
              <div class="unit-view-tabs">
                <button :class="{ active: unitViewMode === 'index' }" @click="unitViewMode = 'index'">单元索引 · {{ units.length }}</button>
                <button :class="{ active: unitViewMode === 'timeline' }" @click="unitViewMode = 'timeline'">剧情时钟</button>
              </div>
              <span>点击跳到写作页生产合同与正文</span>
            </div>
            <StoryTimeline v-if="unitViewMode === 'timeline'" :volume="volume.volume" @open="emit('openUnit', $event)" />
            <div v-else class="index-table-wrap">
              <table>
                <thead><tr><th>单元</th><th>章范围</th><th>一句话功能</th><th>终局边界</th><th>下一钩</th><th>状态</th></tr></thead>
                <tbody><tr v-for="row in volumeView.units" :key="row.id" @click="openIndexedUnit(row.id)">
                  <td data-label="单元">{{ row.id }}</td><td data-label="章范围">{{ row.range }}</td><td data-label="功能">{{ row.purpose }}</td><td data-label="终局边界">{{ row.boundary }}</td><td data-label="下一钩">{{ row.hook }}</td>
                  <td data-label="状态"><span class="status" :data-status="unitStatus(row.id)">{{ statusText(unitStatus(row.id)) }}</span></td>
                </tr></tbody>
              </table>
            </div>
          </div>
          <details class="narrative-source volume-source">
            <summary>阅读完整卷纲与节奏说明</summary>
            <pre class="markdown">{{ volume.raw }}</pre>
          </details>
        </template>
        <p v-else class="muted volume-empty">总纲已就绪。点「生成卷纲」为第 1 卷建立单元索引；已有正文与账本不会被覆盖。</p>
      </article>
    </template>

    <div v-if="!bookOutline && !volume && !units.length" class="empty-state panel">
      <h3>还没有大纲</h3><p>先「立项」生成总纲、设定与人物；批准后再单独「生成卷纲」。</p>
    </div>
  </section>
</template>

<style scoped>
.outline-workspace { display: grid; gap: 18px; }
.view-title { display: flex; justify-content: space-between; gap: 20px; align-items: flex-end; }
.view-title h2 { margin: 4px 0 6px; font-family: var(--font-prose); font-size: 28px; }
.view-title p { margin: 0; color: var(--ink-soft); font-size: 13px; }
.eyebrow { color: var(--accent) !important; font-size: 11px !important; letter-spacing: .12em; text-transform: uppercase; }
.view-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.volume-empty { padding: 8px 0 4px; }
.book-overview { overflow: hidden; }
.book-overview summary { display: flex; justify-content: space-between; align-items: center; padding: 16px 20px; cursor: pointer; list-style: none; }
.book-overview summary small, .volume-document small, .contract-hero small { display: block; color: var(--accent); font-size: 11px; margin-bottom: 4px; }
.book-overview summary strong { font-size: 15px; }
.summary-action { color: var(--ink-soft); font-size: 12px; }
.book-body { border-top: 1px solid var(--line); padding: 16px 20px 0; }
.book-body-head { display: flex; justify-content: flex-end; }
.book-summary { display: grid; gap: 16px; padding: 8px 0 18px; }
.book-summary small, .volume-structure small, .index-heading small { color: var(--accent); font-size: 11px; font-weight: 600; }
.book-summary p, .book-summary li { color: var(--ink-soft); font-size: 14px; line-height: 1.8; }
.book-title-line p { color: var(--ink); font-weight: 600; }
.book-volumes ul { display: grid; gap: 8px; }
.book-summary p { margin: 6px 0 0; }
.book-summary ul, .volume-structure ul { margin: 8px 0 0; padding-left: 20px; }
.book-premise p { font-family: var(--font-prose); font-size: 20px; color: var(--ink); line-height: 1.6; }
.narrative-source { border-top: 1px solid var(--line); }
.narrative-source summary { padding: 14px 0; cursor: pointer; color: var(--ink-soft); font-size: 13px; }
.text-button { border: 0; background: transparent; color: var(--accent); padding: 4px 0; }
.volume-tabs, .unit-context { display: flex; gap: 6px; border-bottom: 1px solid var(--line); padding-bottom: 12px; overflow-x: auto; }
.volume-tabs button, .unit-context button { white-space: nowrap; }
.volume-tabs button.active, .unit-context button.active { color: white; background: var(--accent); border-color: var(--accent); }
.volume-document { overflow: hidden; }
.volume-document header { display: flex; justify-content: space-between; align-items: center; gap: 12px; padding: 16px 20px; border-bottom: 1px solid var(--line); }
.volume-document h3 { margin: 0; font-family: var(--font-prose); }
.volume-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.volume-structure { display: grid; grid-template-columns: minmax(0, 1.6fr) minmax(180px, .8fr); gap: 20px; padding: 20px; }
.volume-structure li { color: var(--ink-soft); font-size: 13px; line-height: 1.8; margin-bottom: 6px; }
.volume-cast { border-left: 1px solid var(--line); padding-left: 20px; }
.cast-chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 10px; }
.cast-chips span { padding: 5px 9px; border-radius: 8px; background: rgba(15,107,92,.07); color: var(--accent); font-size: 12px; }
.cast-empty { margin: 8px 0 0; font-size: 11px; color: var(--warn); line-height: 1.7; }
.cast-empty code { background: rgba(20,35,31,.06); padding: 1px 5px; border-radius: 5px; font-size: 10px; }
.volume-ending { grid-column: 1 / -1; border-top: 1px solid var(--line); padding-top: 16px; }
.volume-unit-index { border-top: 1px solid var(--line); padding: 16px 20px; }
.index-heading { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; gap: 12px; flex-wrap: wrap; }
.index-heading > span { color: var(--ink-soft); font-size: 11px; }
.unit-view-tabs { display: inline-flex; gap: 2px; padding: 3px; border-radius: 10px; background: rgba(20,35,31,.05); }
.unit-view-tabs button { border: none; background: transparent; color: var(--ink-soft); padding: 5px 14px; border-radius: 8px; font-size: 12px; font-weight: 600; }
.unit-view-tabs button.active { background: white; color: var(--accent); box-shadow: 0 1px 4px rgba(20,35,31,.1); }
.index-table-wrap { overflow-x: auto; }
.index-table-wrap table { width: 100%; border-collapse: collapse; font-size: 12px; }
.index-table-wrap th { text-align: left; color: var(--ink-soft); font-weight: 500; padding: 8px; border-bottom: 1px solid var(--line); white-space: nowrap; }
.index-table-wrap td { padding: 10px 8px; border-bottom: 1px solid var(--line); color: var(--ink-soft); line-height: 1.6; min-width: 70px; }
.index-table-wrap td:first-child { color: var(--accent); font-weight: 600; white-space: nowrap; }
.index-table-wrap tbody tr { cursor: pointer; }
.index-table-wrap tbody tr:hover { background: rgba(15,107,92,.045); }
.volume-source { margin: 0 20px; }
.markdown { white-space: pre-wrap; font-family: var(--font-ui); font-size: 14px; line-height: 1.8; padding: 8px 0 20px; margin: 0; max-height: 540px; overflow: auto; }
.status { font-size: 10px; padding: 1px 7px; border-radius: 20px; background: rgba(20,35,31,.06); color: var(--ink-soft); white-space: nowrap; }
.status[data-status="reviewed"] { background: rgba(31,122,77,.12); color: var(--ok); }
.status[data-status="drafted"] { background: rgba(161,92,18,.12); color: var(--warn); }
.status[data-status="accepted"] { background: rgba(15,107,92,.12); color: var(--accent); }
.outline-edit { padding: 16px; }
.edit-head { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 12px; font-size: 12px; color: var(--ink-soft); }
.edit-head div { display: flex; gap: 8px; }
.source-editor { width: 100%; min-height: 560px; padding: 16px; resize: vertical; border: 1px solid var(--line); border-radius: 12px; font: 13px/1.7 ui-monospace, monospace; background: rgba(255,255,255,.6); }
.empty-state { padding: 32px; text-align: center; }
.muted { color: var(--ink-soft); }
@media (max-width: 720px) {
  .view-title { flex-direction: column; align-items: flex-start; }
  .volume-structure { grid-template-columns: 1fr; }
  .volume-cast { border-left: 0; border-top: 1px solid var(--line); padding-left: 0; padding-top: 16px; }
  .index-table-wrap { overflow: visible; }
  .index-table-wrap table, .index-table-wrap tbody { display: block; }
  .index-table-wrap thead { display: none; }
  .index-table-wrap tr { display: grid; gap: 8px; padding: 12px 0; border-bottom: 1px solid var(--line); }
  .index-table-wrap td { display: grid; grid-template-columns: 74px minmax(0, 1fr); gap: 8px; padding: 0; border: 0; min-width: 0; }
  .index-table-wrap td::before { content: attr(data-label); color: var(--ink-soft); font-size: 11px; }
  .edit-head { align-items: flex-start; flex-direction: column; }
}
</style>
