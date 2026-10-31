<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useAppStore } from '@/stores/app'
import { api } from '@/api/client'

type Doc = { id: string; title: string; summary: string; bytes: number; category: string; builtin: boolean; customized: boolean }
const store = useAppStore()
const docs = ref<Doc[]>([])
const selectedId = ref('')
const content = ref('')
const draft = ref('')
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
const filtered = computed(() => docs.value.filter((d) => {
  if (activeCategory.value !== 'all' && d.category !== activeCategory.value) return false
  if (search.value && !`${d.title}${d.summary}${d.id}`.toLowerCase().includes(search.value.toLowerCase())) return false
  return true
}))
const current = computed(() => docs.value.find((d) => d.id === selectedId.value))

onMounted(async () => {
  await load()
  if (docs.value.length) select(docs.value[0].id)
})

async function load() {
  docs.value = (await api.get<Doc[]>('/api/knowledge')) || []
}
async function select(id: string) {
  if (editing.value && !confirm('放弃未保存的修改？')) return
  selectedId.value = id
  editing.value = false
  const res = await api.get<{ content: string }>(`/api/knowledge/${id}`)
  content.value = res.content
  draft.value = res.content
}
async function save() {
  saving.value = true
  try {
    await api.put(`/api/knowledge/${selectedId.value}`, { content: draft.value })
    content.value = draft.value
    editing.value = false
    await load()
    store.note('已保存到本地资料库；内置原文仍保留')
  } catch (e) { store.fail(e) } finally { saving.value = false }
}
async function restore() {
  if (!current.value?.builtin || !confirm('恢复为当前版本的内置原文？你在本地对这篇文章的修改将被移除。')) return
  saving.value = true
  try {
    const res = await api.post<{ content: string }>(`/api/knowledge/${selectedId.value}/restore`)
    content.value = res.content
    draft.value = res.content
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
      <div>
        <p class="eyebrow">Knowledge Base</p>
        <h1>知识库</h1>
        <p class="sub">内置创作方法与题材指南随版本提供，本地编辑会形成个人副本。知识不是 canon，只有在创作确认窗口勾选后才作为「方法/风格参考」附加。</p>
      </div>
      <span class="doc-count">{{ docs.length }} 篇</span>
    </header>

    <div class="kb-layout">
      <aside class="kb-nav panel">
        <input v-model="search" class="kb-search" placeholder="搜索标题 / 摘要" />
        <div class="kb-cats">
          <button v-for="c in CATEGORIES" :key="c.id" :class="{ active: activeCategory === c.id }" @click="activeCategory = c.id">{{ c.label }}</button>
        </div>
        <button v-for="d in filtered" :key="d.id" class="kb-item" :class="{ active: selectedId === d.id }" @click="select(d.id)">
          <strong>{{ d.title }}</strong>
          <small>{{ d.summary || d.id }}</small>
          <span class="kb-meta">{{ d.category === 'genre' ? '题材' : d.category === 'general' ? '方法' : '自建' }} · {{ d.customized ? '已编辑' : d.builtin ? '内置' : '个人' }} · {{ (d.bytes / 1024).toFixed(1) }}KB</span>
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
              <button v-if="editing" @click="editing = false; draft = content">取消</button>
              <button class="primary" :disabled="saving" @click="editing ? save() : (editing = true)">{{ editing ? (saving ? '保存中' : '保存副本') : '编辑' }}</button>
            </div>
          </header>
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
.eyebrow { color: var(--accent); letter-spacing: 0.16em; text-transform: uppercase; font-size: 12px; margin: 0; }
.page-head h1 { font-family: var(--font-prose); font-size: clamp(28px, 3.5vw, 44px); margin: 8px 0 8px; line-height: 1.05; }
.sub { color: var(--ink-soft); font-size: 12px; margin: 0; max-width: 640px; line-height: 1.7; }
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
@media (max-width: 900px) { .kb-layout { grid-template-columns: 1fr; } .kb-nav { max-height: 300px; } }
</style>
