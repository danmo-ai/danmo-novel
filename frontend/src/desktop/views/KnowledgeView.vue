<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/shared/stores/app'
import { api } from '@/shared/api/client'
import {
  KNOWLEDGE_GENRES,
  KNOWLEDGE_STAGES,
  knowledgeGenreSummary,
  knowledgeStageSummary,
} from '@/shared/utils/knowledgeStages'

type Doc = {
  id: string; title: string; summary: string; bytes: number; category: string
  builtin: boolean; customized: boolean; stages: string[]; genres: string[]; subgenres: string[]
  /** 默认注入：pipeline 每次自动带上，不需要作者勾选。 */
  default?: boolean
  /** author = 作者操作说明，永不进提示词。 */
  audience?: string
}
const store = useAppStore()
const route = useRoute()
const docs = ref<Doc[]>([])
const selectedId = ref('')
const content = ref('')
const draft = ref('')
const stages = ref<string[]>([])
const draftStages = ref<string[]>([])
const genres = ref<string[]>([])
const draftGenres = ref<string[]>([])
const subgenres = ref<string[]>([])
const draftSubgenreText = ref('')
const editing = ref(false)
const saving = ref(false)
const activeCategory = ref('all')
const search = ref('')

const CATEGORIES = [
  { id: 'all', label: '全部' },
  { id: 'general', label: '通用方法' },
  { id: 'genre', label: '题材专项' },
  { id: 'custom', label: '作者自建' },
]
const genreFilter = ref('all')
const filtered = computed(() => docs.value.filter((d) => {
  if (activeCategory.value !== 'all' && d.category !== activeCategory.value) return false
  if (genreFilter.value === 'common' && (d.genres?.length || d.subgenres?.length)) return false
  if (genreFilter.value !== 'all' && genreFilter.value !== 'common' && !(d.genres || []).includes(genreFilter.value)) return false
  if (search.value && !`${d.title}${d.summary}${d.id}`.toLowerCase().includes(search.value.toLowerCase())) return false
  return true
}))
const current = computed(() => docs.value.find((d) => d.id === selectedId.value))

onMounted(async () => {
  await load()
  // 支持 /knowledge?id=25-review-checklist：从审校页直达规则编辑。
  const wanted = String(route.query.id || '')
  if (wanted && docs.value.some((d) => d.id === wanted)) {
    await select(wanted)
  } else if (docs.value.length) {
    select(docs.value[0].id)
  }
})

async function load() {
  docs.value = (await api.get<Doc[]>('/api/knowledge')) || []
}
async function select(id: string) {
  if (editing.value && !confirm('放弃未保存的修改？')) return
  selectedId.value = id
  editing.value = false
  const res = await api.get<{ content: string; stages: string[]; genres: string[]; subgenres: string[] }>(`/api/knowledge/${id}`)
  content.value = res.content
  draft.value = res.content
  stages.value = res.stages || []
  draftStages.value = [...stages.value]
  genres.value = res.genres || []
  draftGenres.value = [...genres.value]
  subgenres.value = res.subgenres || []
  draftSubgenreText.value = (res.subgenres || []).join('、')
}
function beginEdit() {
  draft.value = content.value
  draftStages.value = [...stages.value]
  draftGenres.value = [...genres.value]
  draftSubgenreText.value = subgenres.value.join('、')
  editing.value = true
}
function cancelEdit() {
  editing.value = false
  draft.value = content.value
  draftStages.value = [...stages.value]
  draftGenres.value = [...genres.value]
  draftSubgenreText.value = subgenres.value.join('、')
}
function toggleStage(id: string) {
  const s = new Set(draftStages.value)
  if (s.has(id)) s.delete(id)
  else s.add(id)
  draftStages.value = KNOWLEDGE_STAGES.filter((st) => s.has(st.id)).map((st) => st.id)
}
function toggleGenre(name: string) {
  const s = new Set(draftGenres.value)
  if (s.has(name)) s.delete(name)
  else s.add(name)
  draftGenres.value = KNOWLEDGE_GENRES.filter((g) => s.has(g))
}
function parseSubgenres(text: string) {
  return text.split(/[、,，\s]+/).map((s) => s.trim()).filter(Boolean)
}
async function save() {
  saving.value = true
  try {
    const sub = parseSubgenres(draftSubgenreText.value)
    await api.put(`/api/knowledge/${selectedId.value}`, {
      content: draft.value, stages: draftStages.value, genres: draftGenres.value, subgenres: sub,
    })
    content.value = draft.value
    stages.value = [...draftStages.value]
    genres.value = [...draftGenres.value]
    subgenres.value = sub
    editing.value = false
    await load()
    store.note('已保存到本地资料库；内置原文仍保留')
  } catch (e) { store.fail(e) } finally { saving.value = false }
}
async function restore() {
  if (!current.value?.builtin || !confirm('恢复为当前版本的内置原文？你在本地对这篇文章的修改将被移除。')) return
  saving.value = true
  try {
    const res = await api.post<{ content: string; stages: string[]; genres: string[]; subgenres: string[] }>(`/api/knowledge/${selectedId.value}/restore`)
    content.value = res.content
    draft.value = res.content
    stages.value = res.stages || []
    draftStages.value = [...stages.value]
    genres.value = res.genres || []
    draftGenres.value = [...genres.value]
    subgenres.value = res.subgenres || []
    draftSubgenreText.value = subgenres.value.join('、')
    editing.value = false
    await load()
    store.note('已恢复为内置原文')
  } catch (e) { store.fail(e) } finally { saving.value = false }
}
const rendered = computed(() => renderMarkdown(content.value))
type Block = { kind: string; text: string; items?: string[]; rows?: string[][] }
function renderMarkdown(raw: string) {
  const blocks: Block[] = []
  let list: string[] = []
  const flush = () => { if (list.length) { blocks.push({ kind: 'ul', text: '', items: list }); list = [] } }
  const lines = raw.split('\n')
  for (let i = 0; i < lines.length; i++) {
    const t = lines[i].trim()
    if (/^\|.+\|$/.test(t) && /^\|[\s:|-]+\|$/.test(lines[i + 1]?.trim() || '')) {
      flush()
      const rows: string[][] = []
      const cells = (line: string) => line.trim().replace(/^\||\|$/g, '').split('|').map((cell) => cell.trim())
      rows.push(cells(t))
      i += 2
      while (i < lines.length && /^\|.+\|$/.test(lines[i].trim())) { rows.push(cells(lines[i])); i++ }
      i--
      blocks.push({ kind: 'table', text: '', rows })
      continue
    }
    if (/^#{1,3}\s/.test(t)) { flush(); blocks.push({ kind: 'h' + t.match(/^#+/)![0].length, text: t.replace(/^#+\s*/, '') }); continue }
    if (/^[-*]\s+(?!\[[ xX]\])/.test(t)) { list.push(t.replace(/^[-*]\s+/, '')); continue }
    flush()
    if (/^```/.test(t)) {
      const code: string[] = []
      while (i + 1 < lines.length && !lines[i + 1].trim().startsWith('```')) code.push(lines[++i])
      i++
      blocks.push({ kind: 'code', text: code.join('\n') })
      continue
    }
    if (/^- \[[ xX]\]\s+/.test(t)) { list.push(t.replace(/^[-*]\s+/, '')); continue }
    if (t) blocks.push({ kind: t.startsWith('>') ? 'quote' : 'p', text: t.replace(/^>\s?/, '') })
  }
  flush()
  return blocks
}
</script>

<template>
  <section class="knowledge-page">
    <header class="page-head">
      <div class="page-title">
        <p class="eyebrow">Knowledge Base</p>
        <div class="title-line"><h1>知识库</h1><span class="doc-count">{{ docs.length }} 篇</span></div>
        <div class="header-description">
          <p class="sub">程序规则、默认注入、用户选择与作者文档的分类知识。</p>
          <details class="usage-details">
            <summary>说明</summary>
            <div class="usage-content panel">
              <p class="sub">
                四类内容：<strong>程序规则</strong>（代码执行，不在知识库）· <strong>默认注入</strong>（每次自动带上）·
                <strong>用户选择</strong>（在创作确认窗口勾选）· <strong>作者文档</strong>（平台运营/应用流程/出处，永不进提示词）。
              </p>
              <p class="sub">写作步骤与题材标签决定“什么时候可选”；带「作者文档」标记的只供阅读，不会出现在选择器里。</p>
            </div>
          </details>
        </div>
      </div>
    </header>

    <div class="kb-layout">
      <aside class="kb-nav panel">
        <input v-model="search" class="kb-search" placeholder="搜索标题 / 摘要" />
        <div class="kb-cats">
          <button v-for="c in CATEGORIES" :key="c.id" :class="{ active: activeCategory === c.id }" @click="activeCategory = c.id">{{ c.label }}</button>
        </div>
        <select v-model="genreFilter" class="kb-genre-filter">
          <option value="all">全部题材</option>
          <option value="common">公共（不限题材）</option>
          <option v-for="g in KNOWLEDGE_GENRES" :key="g" :value="g">专属 · {{ g }}</option>
        </select>
        <button v-for="d in filtered" :key="d.id" class="kb-item" :class="{ active: selectedId === d.id }" @click="select(d.id)">
          <strong>{{ d.title }}</strong>
          <small>{{ d.summary || d.id }}</small>
          <span class="kb-meta">
            <span v-if="d.default" class="kb-auto-badge">默认注入</span>
            <span v-else-if="d.audience === 'author'" class="kb-author-badge">作者文档</span>
            {{ d.category === 'genre' ? '题材' : d.category === 'general' ? '方法' : '自建' }} · {{ d.customized ? '已编辑' : d.builtin ? '内置' : '个人' }} · {{ (d.bytes / 1024).toFixed(1) }}KB
          </span>
          <span class="kb-stages-inline" :class="{ untagged: !d.stages?.length }">{{ knowledgeStageSummary(d.stages) }}</span>
          <span class="kb-stages-inline" :class="{ untagged: !d.genres?.length && !d.subgenres?.length }">题材 · {{ knowledgeGenreSummary(d) }}</span>
        </button>
        <p v-if="!filtered.length" class="kb-empty">没有匹配的文章。</p>
      </aside>

      <article class="kb-content panel">
        <template v-if="current">
          <header class="kb-content-head">
            <div>
              <div class="kb-title-row"><h2>{{ current.title }}</h2><span class="kb-badge" :class="{ edited: current.customized }">{{ current.customized ? '本地已编辑' : current.builtin ? '内置' : '作者自建' }}</span></div>
              <span class="kb-path">{{ current.customized ? '本地副本覆盖 · ' : '' }}knowledge/{{ current.id }}.md</span>
            </div>
            <div class="kb-actions">
              <button v-if="current.builtin && current.customized && !editing" :disabled="saving" @click="restore">{{ saving ? '恢复中' : '恢复内置' }}</button>
              <button v-if="editing" @click="cancelEdit">取消</button>
              <button class="primary" :disabled="saving" @click="editing ? save() : beginEdit()">{{ editing ? (saving ? '保存中' : '保存副本') : '编辑' }}</button>
            </div>
          </header>
          <div class="kb-stage-bar">
            <small>适用写作步骤</small>
            <button v-for="s in KNOWLEDGE_STAGES" :key="s.id" class="stage-chip"
              :class="{ on: (editing ? draftStages : stages).includes(s.id) }"
              :disabled="!editing" :title="editing ? '点击切换适用步骤' : '点右上「编辑」可调整'"
              @click="toggleStage(s.id)">{{ s.label }}</button>
            <span v-if="!(editing ? draftStages : stages).length" class="stage-hint">未标记 · 所有步骤可选</span>
          </div>
          <div class="kb-stage-bar">
            <small>适用题材</small>
            <button v-for="g in KNOWLEDGE_GENRES" :key="g" class="stage-chip"
              :class="{ on: (editing ? draftGenres : genres).includes(g) }"
              :disabled="!editing" :title="editing ? '点击切换题材归属' : '点右上「编辑」可调整'"
              @click="toggleGenre(g)">{{ g }}</button>
            <span v-if="!(editing ? draftGenres : genres).length" class="stage-hint">公共 · 所有题材可用</span>
            <span v-else-if="!editing" class="stage-hint">只在上述题材的书里出现</span>
            <label v-if="editing" class="kb-subgenre">
              子题材限定
              <input v-model="draftSubgenreText" placeholder="留空=不限；如 刑侦探案" />
            </label>
            <span v-else-if="subgenres.length" class="stage-hint">仅 {{ subgenres.join('、') }} 子题材</span>
          </div>
          <textarea v-if="editing" v-model="draft" class="kb-editor" spellcheck="false" />
          <div v-else class="kb-body">
            <template v-for="(b, i) in rendered" :key="i">
              <h3 v-if="b.kind.startsWith('h')" :class="b.kind">{{ b.text }}</h3>
              <p v-else-if="b.kind === 'p'">{{ b.text }}</p>
              <ul v-else-if="b.kind === 'ul'"><li v-for="(it, j) in b.items" :key="j">{{ it }}</li></ul>
              <div v-else-if="b.kind === 'table'" class="kb-table-wrap"><table><thead><tr><th v-for="(cell, j) in b.rows?.[0]" :key="j">{{ cell }}</th></tr></thead><tbody><tr v-for="(row, ri) in b.rows?.slice(1)" :key="ri"><td v-for="(cell, ci) in row" :key="ci">{{ cell }}</td></tr></tbody></table></div>
              <pre v-else-if="b.kind === 'code'" class="kb-code">{{ b.text }}</pre>
              <blockquote v-else-if="b.kind === 'quote'">{{ b.text }}</blockquote>
            </template>
          </div>
        </template>
        <div v-else class="kb-empty-state"><h3>选择一篇文章</h3><p>左侧选择要查看或编辑的知识文章。</p></div>
      </article>
    </div>
  </section>
</template>

<style scoped>
.page-head { display: flex; justify-content: space-between; align-items: flex-end; gap: 20px; padding: 32px 4px 24px; }
.page-title { flex: 1; min-width: 0; }
.title-line { display: flex; align-items: baseline; flex-wrap: wrap; gap: 12px; }
.title-line h1 { flex-shrink: 0; }
.header-description { display: flex; align-items: center; gap: 10px; min-width: 0; }
.header-description > .sub { flex: 0 1 auto; min-width: 0; max-width: none; }
.usage-details { position: relative; flex-shrink: 0; font-size: 12px; color: var(--ink-soft); }
.usage-details summary { cursor: pointer; color: var(--accent); padding: 6px 0; list-style: none; }
.usage-details summary::-webkit-details-marker { display: none; }
.usage-details summary::before { content: 'ⓘ '; }
.usage-content { position: absolute; z-index: 10; top: calc(100% + 6px); left: 0; width: min(480px, calc(100vw - 56px)); display: grid; gap: 10px; padding: 16px 18px; background: var(--paper); box-shadow: 0 12px 32px rgba(20,35,31,.14); }
/* hover 能力判断：触控笔电没有 hover，光标悬停展开会挡住点选。 */
@media (hover: hover) {
  .usage-details:hover .usage-content { display: grid; }
}
.eyebrow { color: var(--accent); letter-spacing: 0.16em; text-transform: uppercase; font-size: 12px; margin: 0; }
.page-head h1 { font-family: var(--font-prose); font-size: clamp(28px, 3.5vw, 44px); margin: 8px 0 8px; line-height: 1.05; }
.sub { color: var(--ink-soft); font-size: 13px; margin: 0; max-width: 640px; line-height: 1.7; }
.doc-count { color: var(--ink-soft); font-size: 13px; }
.kb-layout { display: grid; grid-template-columns: 300px minmax(0, 1fr); gap: 18px; align-items: start; }
.kb-nav { padding: 12px; max-height: 78vh; overflow-y: auto; }
.kb-search { width: 100%; padding: 8px 12px; border: 1px solid var(--line); border-radius: 10px; margin-bottom: 10px; }
.kb-cats { display: flex; gap: 4px; margin-bottom: 12px; flex-wrap: wrap; }
.kb-cats button { font-size: 11px; padding: 4px 10px; border-radius: 20px; border: 1px solid var(--line); background: transparent; color: var(--ink-soft); }
.kb-cats button.active { background: var(--accent); color: white; border-color: var(--accent); }
.kb-item { display: grid; gap: 3px; width: 100%; text-align: left; border: none; background: transparent; padding: 10px; border-radius: 10px; margin-bottom: 2px; }
.kb-item.active { background: rgba(15,107,92,.1); }
.kb-item strong { font-size: 12px; }
.kb-item small { color: var(--ink-soft); font-size: 11px; line-height: 1.5; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.kb-meta { font-size: 10px; color: var(--accent); }
.kb-auto-badge { display: inline-block; margin-right: 6px; padding: 1px 6px; border-radius: 999px; background: rgba(163,59,50,.1); color: var(--bad); font-size: 9px; font-weight: 600; }
.kb-author-badge { display: inline-block; margin-right: 6px; padding: 1px 6px; border-radius: 999px; background: rgba(0,0,0,.06); color: var(--ink-soft); font-size: 9px; font-weight: 600; }
.kb-stages-inline { font-size: 10px; color: var(--ink-soft); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.kb-stages-inline.untagged { color: var(--bad); opacity: .75; }
.kb-stage-bar { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; padding: 12px 24px; border-bottom: 1px solid var(--line); }
.kb-stage-bar small { color: var(--ink-soft); font-size: 11px; margin-right: 2px; }
.stage-chip { font-size: 11px; padding: 3px 10px; border-radius: 20px; border: 1px solid var(--line); background: transparent; color: var(--ink-soft); }
.stage-chip.on { background: var(--accent); border-color: var(--accent); color: #fff; }
.stage-chip:disabled { cursor: default; }
.stage-chip:disabled:not(.on) { opacity: .7; }
.stage-hint { font-size: 10px; color: var(--ink-soft); margin-left: 4px; }
.kb-subgenre { display: flex; align-items: center; gap: 6px; margin-left: auto; font-size: 10px; color: var(--ink-soft); }
.kb-subgenre input { width: 150px; padding: 3px 8px; border: 1px solid var(--line); border-radius: 8px; font-size: 11px; }
.kb-genre-filter { width: 100%; padding: 7px 10px; border: 1px solid var(--line); border-radius: 10px; margin-bottom: 10px; font-size: 12px; background: rgba(255,255,255,.7); color: var(--ink); }
.kb-empty, .kb-empty-state { color: var(--ink-soft); font-size: 12px; padding: 16px; text-align: center; }
.kb-content { overflow: hidden; }
.kb-content-head { display: flex; justify-content: space-between; align-items: center; padding: 18px 24px; border-bottom: 1px solid var(--line); gap: 12px; }
.kb-content-head h2 { margin: 0; font-family: var(--font-prose); font-size: 17px; }
.kb-title-row, .kb-actions { display: flex; align-items: center; gap: 10px; }
.kb-actions { flex-shrink: 0; }
.kb-badge { padding: 2px 7px; border: 1px solid var(--line); border-radius: 12px; color: var(--accent); font-size: 10px; white-space: nowrap; }
.kb-badge.edited { color: var(--ink-soft); }
.kb-path { font-size: 11px; color: var(--ink-soft); }
.kb-body { padding: 24px 30px; max-height: 72vh; overflow-y: auto; }
.kb-body h3 { font-family: var(--font-prose); margin: 20px 0 10px; }
.kb-body h3.h1 { font-size: 22px; }
.kb-body h3.h2 { font-size: 17px; border-bottom: 1px solid var(--line); padding-bottom: 6px; }
.kb-body h3.h3 { font-size: 14px; color: var(--accent); }
.kb-body p { color: var(--ink-soft); font-size: 13px; line-height: 1.75; margin: 8px 0; }
.kb-body ul { padding-left: 20px; }
.kb-body li { color: var(--ink-soft); font-size: 12px; line-height: 1.65; margin-bottom: 4px; }
.kb-table-wrap { overflow-x: auto; margin: 14px 0; }
.kb-body table { width: 100%; border-collapse: collapse; font-size: 12px; line-height: 1.65; }
.kb-body th, .kb-body td { border: 1px solid var(--line); padding: 8px 10px; text-align: left; vertical-align: top; min-width: 80px; }
.kb-body th { color: var(--ink); background: rgba(15,107,92,.05); font-weight: 600; }
.kb-body td { color: var(--ink-soft); }
.kb-code { padding: 12px; background: rgba(15,107,92,.05); border-radius: 8px; overflow-x: auto; font: 12px/1.7 ui-monospace, monospace; white-space: pre-wrap; }
.kb-body blockquote { margin: 12px 0; padding: 4px 14px; border-left: 3px solid var(--accent); color: var(--ink-soft); font-size: 12px; line-height: 1.65; }
.kb-editor { width: 100%; min-height: 66vh; border: none; padding: 24px 30px; font: 12px/1.75 ui-monospace, monospace; resize: vertical; background: transparent; outline: none; }
@media (max-width: 900px) { .kb-layout { grid-template-columns: minmax(0, 1fr); } .kb-nav { max-height: 300px; } }
</style>
