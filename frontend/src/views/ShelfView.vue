<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { api } from '@/api/client'
import { seedSampleBook } from '@/api/demo'

const store = useAppStore()
const router = useRouter()
const showCreate = ref(false)

/**
 * 立项表单：基本信息 + 创意种子。
 *
 * 这两组字段都会写进 outline/idea.md 的「立项输入」，扩写与结构化立项都读它。
 * 空白项保持空白——模板不会替作者编造「平台」「读者」这类决定生产的事实。
 */
const form = ref({
  bookId: '',
  title: '',
  genre: '玄幻',
  subgenre: '',
  platform: '',
  audience: '',
  length: '',
  chapterChars: '',
  idea: '',
  tone: '',
  pleasure: '',
  protagonist: '',
  conflict: '',
  ending: '',
  taboos: '',
  references: '',
  pov: '',
})
const bookIdTouched = ref(false)
const seeding = ref(false)
const GENRES = ['玄幻', '仙侠', '都市', '悬疑', '言情']

onMounted(async () => {
  await store.loadBooks()
  await store.loadLLM().catch(() => {})
})

function slugify(s: string) {
  return s
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}
// 书名变化时同步一个可用的 book id；中文书名 slug 为空就退回时间戳，
// 作者仍可手改。手动改过之后就不再覆盖。
watch(() => form.value.title, (title) => {
  if (bookIdTouched.value) return
  const slug = slugify(title)
  form.value.bookId = slug || (title.trim() ? `book-${Date.now().toString(36).slice(-6)}` : '')
})

function openCreate() {
  showCreate.value = true
}

function resetForm() {
  form.value = {
    bookId: '', title: '', genre: '玄幻', subgenre: '', platform: '', audience: '',
    length: '', chapterChars: '', idea: '', tone: '', pleasure: '', protagonist: '',
    conflict: '', ending: '', taboos: '', references: '', pov: '',
  }
  bookIdTouched.value = false
}

async function createBook() {
  try {
    const res = await api.post<any>('/api/books', form.value)
    showCreate.value = false
    resetForm()
    await store.loadBooks()
    // 新书没有总纲，BookView 会落在「立项」工作台：先扩写创意再生成结构化立项。
    router.push(`/books/${res.summary.id}`)
  } catch (e) { store.fail(e) }
}

/**
 * 空书架时给一书本就绪的示例书。内容随 Go 二进制发布（总纲/设定/卷纲/细纲/正文），
 * 不调用模型——这是没有 API Key 的人（和审核员）唯一能看到完整成品的方式。
 */
async function openSample() {
  if (seeding.value) return
  seeding.value = true
  try {
    const { id } = await seedSampleBook()
    await store.loadBooks()
    router.push(`/books/${id}`)
  } catch (e) {
    store.fail(e)
  } finally {
    seeding.value = false
  }
}

function primaryLabel(b: any) {
  if (b.openCandidates) return '待审候选'
  return '继续创作'
}
</script>

<template>
  <section class="shelf-hero">
    <div>
      <p class="eyebrow">Workspace</p>
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
      <p>从一句灵感开始新建一本书；或者先打开示例书，看看一本完整的小书长什么样——总纲、设定、卷纲、细纲和正文都已写好，不调用模型。</p>
      <div class="empty-actions">
        <button class="primary" @click="openCreate">新建一本书</button>
        <button :disabled="seeding" @click="openSample">{{ seeding ? '正在写入示例…' : '打开示例书' }}</button>
      </div>
    </div>
  </section>

  <div v-if="showCreate" class="modal-mask" @click.self="showCreate = false">
    <div class="modal panel">
      <h2>立项</h2>
        <p class="form-note">先填基础信息与创意种子。创建后进入立项工作台：先扩写创意提案，确认后再生成总纲与设定。</p>

        <h3 class="group-head">基本信息</h3>
        <div class="form-grid">
          <label class="span2">书名<input v-model="form.title" placeholder="书名" /></label>
          <label>书 ID（英文/数字/连字符）<input v-model="form.bookId" placeholder="my-novel" @input="bookIdTouched = true" /></label>
          <label>题材
            <select v-model="form.genre">
              <option v-for="g in GENRES" :key="g">{{ g }}</option>
            </select>
          </label>
          <label>子题材<input v-model="form.subgenre" placeholder="如：刑侦探案 / 年代刑侦" /></label>
          <label>平台 / 频道<input v-model="form.platform" placeholder="如：番茄 · 男频" /></label>
          <label>目标读者<input v-model="form.audience" placeholder="如：18–30 男性读者" /></label>
          <label>预计篇幅<input v-model="form.length" placeholder="如：80 万字 / 3 卷" /></label>
          <label>单章目标字数<input v-model="form.chapterChars" placeholder="如：3000" /></label>
        </div>

        <h3 class="group-head">创意种子</h3>
        <div class="form-grid">
          <label class="span2">一句话 idea<textarea v-model="form.idea" rows="2" placeholder="主角＋困境＋目标＋核心冲突" /></label>
          <label>基调<input v-model="form.tone" placeholder="如：冷硬、克制、黑色幽默" /></label>
          <label>核心爽点 / 读者承诺<input v-model="form.pleasure" placeholder="读者每次翻页得到什么" /></label>
          <label class="span2">主角种子<input v-model="form.protagonist" placeholder="身份、欲望、伤口、不会越过的底线" /></label>
          <label class="span2">主要对抗<input v-model="form.conflict" placeholder="持续制造压力的力量" /></label>
          <label>结局倾向<input v-model="form.ending" placeholder="方向与代价，不写真相" /></label>
          <label>POV<input v-model="form.pov" placeholder="如：第三人称限知" /></label>
          <label class="span2">禁忌 / 红线<input v-model="form.taboos" placeholder="内容、时代、平台边界" /></label>
          <label class="span2">参考作品<input v-model="form.references" placeholder="只作风格与结构参考" /></label>
        </div>

        <div class="modal-actions">
          <button @click="showCreate = false">取消</button>
          <button class="primary" :disabled="!form.title.trim() || !form.bookId.trim()" @click="createBook">创建并进入立项</button>
        </div>
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
.form-note { margin: 0; font-size: 12px; color: var(--ink-soft); line-height: 1.7; }
.group-head { margin: 4px 0 0; font-size: 12px; color: var(--accent); letter-spacing: .06em; }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px 12px; }
.form-grid .span2 { grid-column: 1 / -1; }
.form-grid textarea { padding: 9px 12px; border-radius: 10px; border: 1px solid var(--line); font: inherit; resize: vertical; }
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
.empty-actions { display: flex; justify-content: center; gap: 10px; flex-wrap: wrap; }
.modal-mask { position: fixed; inset: 0; background: rgba(20,35,31,0.3); display: grid; place-items: center; z-index: 40; overflow: auto; padding: 24px; }
.modal { width: min(680px, 96vw); padding: 24px; display: grid; gap: 12px; max-height: 92vh; overflow: auto; }
.modal label { display: grid; gap: 6px; font-size: 13px; color: var(--ink-soft); }
.modal input, .modal select { padding: 10px 12px; border-radius: 10px; border: 1px solid var(--line); }
.modal-actions { display: flex; justify-content: flex-end; gap: 10px; margin-top: 4px; }
@media (max-width: 720px) {
  .shelf-hero { align-items: flex-start; flex-direction: column; padding-top: 24px; }
  .tool-row { flex-wrap: wrap; }
  .tool-row input { flex-basis: 100%; }
  .book-grid { grid-template-columns: minmax(0, 1fr); }
  .book-card { min-width: 0; width: 100%; }
  .book-cover { min-width: 0; }
  .book-cover h3 { overflow-wrap: anywhere; }
  .book-meta { flex-wrap: wrap; }
  .book-meta > span { min-width: 0; }
  .book-meta .pill { white-space: normal; }
  .form-grid { grid-template-columns: minmax(0, 1fr); }
  .modal-mask { padding: max(12px, env(safe-area-inset-top)) 12px max(12px, env(safe-area-inset-bottom)); place-items: center; }
  .modal { width: 100%; max-width: 100%; min-width: 0; max-height: calc(100dvh - max(12px, env(safe-area-inset-top)) - max(12px, env(safe-area-inset-bottom))); margin: 0; padding: 18px 16px; border-radius: 14px; overflow-x: hidden; }
  .modal > * { min-width: 0; }
  .modal-actions { position: sticky; bottom: -18px; margin: 4px -16px -18px; padding: 12px 16px max(12px, env(safe-area-inset-bottom)); background: var(--paper); border-top: 1px solid var(--line); }
  .modal-actions button { flex: 1; min-width: 0; }
}
</style>
