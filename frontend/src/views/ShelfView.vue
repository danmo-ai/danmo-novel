<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { api } from '@/api/client'

const store = useAppStore()
const router = useRouter()
const importPath = ref('')
const showCreate = ref(false)
const form = ref({ bookId: '', title: '', genre: '玄幻' })
const importReadOnly = ref(true)
const createTab = ref<'new' | 'import'>('new')

onMounted(async () => {
  await store.loadBooks()
  await store.loadLLM().catch(() => {})
})

function openCreate() {
  showCreate.value = true
}

async function createBook() {
  try {
    const res = await api.post<any>('/api/books', form.value)
    showCreate.value = false
    form.value = { bookId: '', title: '', genre: '玄幻' }
    await store.loadBooks()
    router.push(`/books/${res.summary.id}`)
  } catch (e) { store.fail(e) }
}

async function importBook() {
  if (!importPath.value) return
  try {
    const res = await api.post<any>('/api/books/import', { path: importPath.value, readOnly: importReadOnly.value })
    importPath.value = ''
    showCreate.value = false
    await store.loadBooks()
    store.note('已导入书籍')
    if (res?.summary?.id) router.push(`/books/${res.summary.id}`)
  } catch (e) { store.fail(e) }
}

function primaryLabel(b: any) {
  if (b.openCandidates) return '待审候选'
  return '继续创作'
}
</script>

<template>
  <section class="shelf-hero">
    <div>
      <p class="eyebrow">创作工作台</p>
      <h1>你的书，<br />从一句灵感到定稿。</h1>
      <p class="sub">受控生成 · 候选确认 · 多模型写作</p>
    </div>
    <div class="hero-actions">
      <button class="primary" @click="openCreate">新建一本书</button>
    </div>
  </section>

  <section class="book-grid">
    <button v-for="b in store.books" :key="b.id" class="book-card" @click="router.push(`/books/${b.id}`)">
      <div class="book-cover">
        <span class="genre">{{ b.genre }}</span>
        <h3>{{ b.title }}</h3>
      </div>
      <div class="book-meta">
        <span>{{ b.unitCount }} 单元 · {{ b.reviewedCount }} 定稿</span>
        <span class="pill" :class="{ warn: b.openCandidates }">{{ primaryLabel(b) }}</span>
      </div>
    </button>
    <div v-if="!store.books.length" class="empty panel">
      <h2>书架还是空的</h2>
      <p>新建一本书，或导入 Danmo Work 的小说目录。</p>
      <button class="primary" @click="openCreate">开始</button>
    </div>
  </section>

  <div v-if="showCreate" class="modal-mask" @click.self="showCreate = false">
    <div class="modal panel">
      <div class="modal-tabs">
        <button :class="{ active: createTab === 'new' }" @click="createTab = 'new'">新建</button>
        <button :class="{ active: createTab === 'import' }" @click="createTab = 'import'">导入 Danmo Work 书</button>
      </div>
      <template v-if="createTab === 'new'">
        <h2>立项</h2>
        <label>书 ID（英文/数字/连字符）<input v-model="form.bookId" placeholder="my-novel" /></label>
        <label>书名<input v-model="form.title" placeholder="书名" /></label>
        <label>题材
          <select v-model="form.genre">
            <option>玄幻</option><option>都市</option><option>悬疑</option><option>言情</option>
          </select>
        </label>
        <div class="modal-actions">
          <button @click="showCreate = false">取消</button>
          <button class="primary" :disabled="!form.bookId" @click="createBook">创建</button>
        </div>
      </template>
      <template v-else>
        <h2>导入 Danmo Work 书目录</h2>
        <p class="import-note">仅支持 Danmo Work 的小说目录（含 <code>novel-state.yaml</code> / <code>canon</code> / <code>outline</code> 的 <code>novel/&lt;book-id&gt;</code>）。字段结构不同的第三方书籍无法导入。</p>
        <label>目录路径<input v-model="importPath" placeholder="/Users/…/files/novel/<book-id>" /></label>
        <label class="check"><input v-model="importReadOnly" type="checkbox" /> 只读导入（不写回源目录，推荐）</label>
        <div class="modal-actions">
          <button @click="showCreate = false">取消</button>
          <button class="primary" :disabled="!importPath" @click="importBook">导入</button>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.shelf-hero {
  display: flex; justify-content: space-between; align-items: flex-end; gap: 24px;
  padding: 40px 4px 32px;
}
.eyebrow { color: var(--accent); letter-spacing: 0.16em; text-transform: uppercase; font-size: 12px; }
h1 { font-family: var(--font-prose); font-size: clamp(32px, 4.5vw, 56px); line-height: 1.05; margin: 8px 0 12px; }
.sub { color: var(--ink-soft); margin: 0; }
.check { display: flex; gap: 6px; align-items: center; color: var(--ink-soft); font-size: 12px; }
.modal-tabs { display: flex; gap: 4px; border-bottom: 1px solid var(--line); margin: -6px -6px 4px; padding: 0 6px; }
.modal-tabs button { border: none; border-bottom: 2px solid transparent; border-radius: 0; background: transparent; color: var(--ink-soft); padding: 8px 14px; }
.modal-tabs button.active { color: var(--accent); border-bottom-color: var(--accent); font-weight: 700; }
.import-note { margin: 0; font-size: 12px; color: var(--ink-soft); line-height: 1.7; }
.import-note code { background: rgba(20,35,31,.06); padding: 1px 5px; border-radius: 5px; font-size: 11px; }
.book-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 18px; }
.book-card {
  text-align: left; padding: 0; overflow: hidden; border-radius: 18px; border: 1px solid var(--line);
  background: var(--panel); box-shadow: var(--shadow);
}
.book-cover {
  min-height: 170px; padding: 18px; display: flex; flex-direction: column; justify-content: space-between;
  background: linear-gradient(145deg, #0f6b5c 0%, #143d33 70%, #c45c26 130%); color: white;
}
.book-cover h3 { font-family: var(--font-prose); font-size: 24px; margin: 0; }
.genre { opacity: 0.85; font-size: 12px; }
.book-meta { display: flex; justify-content: space-between; gap: 8px; padding: 12px 14px; font-size: 12px; color: var(--ink-soft); }
.pill { color: var(--accent); font-weight: 600; }
.pill.warn { color: var(--warn); }
.empty { padding: 32px; grid-column: 1 / -1; text-align: center; }
.modal-mask { position: fixed; inset: 0; background: rgba(20,35,31,0.3); display: grid; place-items: center; z-index: 40; }
.modal { width: min(420px, 92vw); padding: 24px; display: grid; gap: 14px; }
.modal label { display: grid; gap: 6px; font-size: 13px; color: var(--ink-soft); }
.modal input, .modal select { padding: 10px 12px; border-radius: 10px; border: 1px solid var(--line); }
.modal-actions { display: flex; justify-content: flex-end; gap: 10px; }
@media (max-width: 720px) {
  .shelf-hero { align-items: flex-start; flex-direction: column; padding-top: 24px; }
  .tool-row { flex-wrap: wrap; }
  .tool-row input { flex-basis: 100%; }
  .book-grid { grid-template-columns: 1fr; }
}
</style>
