<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useAppStore } from '@/stores/app'
import { api } from '@/api/client'

type Doc = { id: string; title: string; summary: string; bytes: number; category: string }
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
    store.note('已保存')
  } catch (e) { store.fail(e) } finally { saving.value = false }
}
const rendered = computed(() => renderMarkdown(content.value))
function renderMarkdown(raw: string) {
  const blocks: { kind: string; text: string; items?: string[] }[] = []
  let list: string[] = []
  const flush = () => { if (list.length) { blocks.push({ kind: 'ul', text: '', items: list }); list = [] } }
  for (const line of raw.split('\n')) {
    const t = line.trim()
    if (/^#{1,3}\s/.test(t)) { flush(); blocks.push({ kind: 'h' + t.match(/^#+/)![0].length, text: t.replace(/^#+\s*/, '') }); continue }
    if (/^[-*]\s+/.test(t)) { list.push(t.replace(/^[-*]\s+/, '')); continue }
    flush()
    if (t) blocks.push({ kind: 'p', text: t })
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
        <p class="sub">作者的方法与题材参考资料。不是 canon，不会自动进入上下文——在创作确认窗口勾选后作为「方法/风格参考」附加。</p>
      </div>
      <span class="doc-count">{{ docs.length }} 篇</span>
    </header>

    <div class="kb-layout">
      <aside class="kb-nav panel">
        <input v-model="search" class="kb-search" placeholder="搜索标题 / 内容" />
        <div class="kb-cats">
          <button v-for="c in CATEGORIES" :key="c.id" :class="{ active: activeCategory === c.id }" @click="activeCategory = c.id">{{ c.label }}</button>
        </div>
        <button v-for="d in filtered" :key="d.id" class="kb-item" :class="{ active: selectedId === d.id }" @click="select(d.id)">
          <strong>{{ d.title }}</strong>
          <small>{{ d.summary || d.id }}</small>
          <span class="kb-meta">{{ d.category === 'genre' ? '题材' : d.category === 'general' ? '方法' : '自建' }} · {{ (d.bytes / 1024).toFixed(1) }}KB</span>
        </button>
        <p v-if="!filtered.length" class="kb-empty">没有匹配的文章。</p>
      </aside>

      <article class="kb-content panel">
        <template v-if="current">
          <header class="kb-content-head">
            <div><h2>{{ current.title }}</h2><span class="kb-path">knowledge/{{ current.id }}.md</span></div>
            <button class="primary" :disabled="saving" @click="editing ? save() : (editing = true)">{{ editing ? (saving ? '保存中' : '保存') : '编辑' }}</button>
          </header>
          <textarea v-if="editing" v-model="draft" class="kb-editor" spellcheck="false" />
          <div v-else class="kb-body">
            <template v-for="(b, i) in rendered" :key="i">
              <h3 v-if="b.kind.startsWith('h')" :class="b.kind">{{ b.text }}</h3>
              <p v-else-if="b.kind === 'p'">{{ b.text }}</p>
              <ul v-else-if="b.kind === 'ul'"><li v-for="(it, j) in b.items" :key="j">{{ it }}</li></ul>
            </template>
          </div>
        </template>
        <div v-else class="kb-empty-state"><h3>选择一篇文章</h3><p>左侧选择要查看或编辑的知识文章。</p></div>
      </article>
    </div>
  </section>
</template>

<style scoped>
.page-head { display: flex; justify-content: space-between; align-items: flex-end; gap: 20px; padding: 4px 0 20px; }
.eyebrow { color: var(--accent); font-size: 11px; letter-spacing: .12em; text-transform: uppercase; margin: 0; }
.page-head h1 { font-family: var(--font-prose); font-size: 30px; margin: 4px 0 8px; }
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
.kb-item strong { font-size: 13px; }
.kb-item small { color: var(--ink-soft); font-size: 11px; line-height: 1.5; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.kb-meta { font-size: 10px; color: var(--accent); }
.kb-empty, .kb-empty-state { color: var(--ink-soft); font-size: 12px; padding: 16px; text-align: center; }
.kb-content { overflow: hidden; }
.kb-content-head { display: flex; justify-content: space-between; align-items: center; padding: 18px 24px; border-bottom: 1px solid var(--line); gap: 12px; }
.kb-content-head h2 { margin: 0; font-family: var(--font-prose); font-size: 22px; }
.kb-path { font-size: 11px; color: var(--ink-soft); }
.kb-body { padding: 24px 30px; max-height: 72vh; overflow-y: auto; }
.kb-body h3 { font-family: var(--font-prose); margin: 20px 0 10px; }
.kb-body h3.h1 { font-size: 22px; }
.kb-body h3.h2 { font-size: 17px; border-bottom: 1px solid var(--line); padding-bottom: 6px; }
.kb-body h3.h3 { font-size: 14px; color: var(--accent); }
.kb-body p { color: var(--ink-soft); font-size: 14px; line-height: 1.85; margin: 8px 0; }
.kb-body ul { padding-left: 20px; }
.kb-body li { color: var(--ink-soft); font-size: 13px; line-height: 1.8; margin-bottom: 4px; }
.kb-editor { width: 100%; min-height: 66vh; border: none; padding: 24px 30px; font: 13px/1.8 ui-monospace, monospace; resize: vertical; background: transparent; outline: none; }
@media (max-width: 900px) { .kb-layout { grid-template-columns: 1fr; } .kb-nav { max-height: 300px; } }
</style>
