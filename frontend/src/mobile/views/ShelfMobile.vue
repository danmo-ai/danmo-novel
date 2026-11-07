<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { api } from '@/api/client'
import { seedSampleBook } from '../demoBook'

/**
 * 手机书架：列表 + 一个三字段的新建表单。
 *
 * 桌面那个 15 字段的立项表在手机上要滚四屏，作者会直接放弃。手机只问
 * 「书名 / 题材 / 一句话 idea」，其余字段留下来到立项工作台里补——
 * 那些内容本来也要在扩写提案时再想一遍。
 */
const store = useAppStore()
const router = useRouter()

const showCreate = ref(false)
const creating = ref(false)
const seeding = ref(false)
const form = ref({ title: '', genre: '玄幻', idea: '' })
const bookId = ref('')
const bookIdTouched = ref(false)
const GENRES = ['玄幻', '仙侠', '都市', '悬疑', '言情']

/**
 * 首次启动是这一页唯一需要"解释自己"的时刻。
 *
 * 应用的全部生成能力都要用户自己的模型 Key，所以新装的人（和审核员）打开
 * 看到的是空书架 + 一堆点不动的按钮——这正是 App Store 4.2 说的「功能不完整」。
 * 因此空书架时先给两条明确的路：看示例书（不需要 Key），或者先配 Key。
 */
const brandNew = computed(() => store.books.length === 0)
const hasProvider = computed(() => store.providers.length > 0)

onMounted(async () => {
  await Promise.all([
    store.loadBooks(),
    store.loadLLM().catch(() => {}),
  ])
})

async function seedSample() {
  if (seeding.value) return
  seeding.value = true
  try {
    const id = await seedSampleBook()
    await store.loadBooks()
    router.push(`/books/${id}/bible`)
  } catch (e) {
    store.fail(e)
  } finally {
    seeding.value = false
  }
}

function slugify(s: string) {
  return s.toLowerCase().trim().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')
}

// 中文书名 slug 为空时退回时间戳，保证 id 可用又不必让作者自己想。
watch(() => form.value.title, (title) => {
  if (bookIdTouched.value) return
  const slug = slugify(title)
  bookId.value = slug || (title.trim() ? `book-${Date.now().toString(36).slice(-6)}` : '')
})

function openCreate() {
  form.value = { title: '', genre: '玄幻', idea: '' }
  bookId.value = ''
  bookIdTouched.value = false
  showCreate.value = true
}

async function createBook() {
  if (creating.value) return
  creating.value = true
  try {
    const res = await api.post<any>('/api/books', {
      bookId: bookId.value,
      title: form.value.title,
      genre: form.value.genre,
      idea: form.value.idea,
    })
    showCreate.value = false
    await store.loadBooks()
    // 新书没有总纲，BookShell 会把作者直接带到立项页。
    router.push(`/books/${res.summary.id}`)
  } catch (e) {
    store.fail(e)
  } finally {
    creating.value = false
  }
}

function open(id: string) {
  router.push(`/books/${id}`)
}
</script>

<template>
  <header class="page-head">
    <div>
      <p class="eyebrow">Workspace</p>
      <h1>书架</h1>
    </div>
    <button class="primary" @click="openCreate">新建</button>
  </header>

  <ul v-if="store.books.length" class="book-list">
    <li v-for="b in store.books" :key="b.id">
      <button class="book-row panel" @click="open(b.id)">
        <span class="book-main">
          <strong>{{ b.title }}</strong>
          <small>{{ b.genre }} · {{ b.unitCount }} 单元 · {{ b.reviewedCount }} 定稿</small>
        </span>
        <span v-if="b.openCandidates" class="badge">待审</span>
      </button>
    </li>
  </ul>

  <div v-else class="empty panel">
    <h2>书架还是空的</h2>
    <p>这个应用在本机写作、本机存稿，生成需要你自己的模型 API Key。</p>
    <div class="welcome-actions">
      <button class="primary" type="button" :disabled="seeding" @click="seedSample">
        {{ seeding ? '正在写入示例…' : '先看一本示例书' }}
      </button>
      <button type="button" @click="router.push('/settings?tab=providers')">
        {{ hasProvider ? '检查模型设置' : '配置模型 API Key' }}
      </button>
      <button type="button" @click="openCreate">从自己的灵感开始</button>
    </div>
    <p class="welcome-note">
      示例书只包含创意提案与设定，不调用任何模型；看完可以直接删掉，或照着它走一遍立项。
    </p>
  </div>

  <div v-if="showCreate" class="sheet-mask" @click.self="showCreate = false">
    <form class="sheet panel" @submit.prevent="createBook">
      <header>
        <h2>新建一本书</h2>
        <button type="button" class="close" @click="showCreate = false">×</button>
      </header>
      <label>书名<input v-model="form.title" placeholder="书名" autocomplete="off" /></label>
      <label>书 ID（英文/数字/连字符）
        <input v-model="bookId" placeholder="my-novel" @input="bookIdTouched = true" />
      </label>
      <label>题材
        <select v-model="form.genre">
          <option v-for="g in GENRES" :key="g">{{ g }}</option>
        </select>
      </label>
      <label>一句话 idea
        <textarea v-model="form.idea" rows="3" placeholder="主角＋困境＋目标＋核心冲突" />
      </label>
      <p class="note">其余字段（平台、读者、篇幅、基调等）在立项工作台里补，扩展提案时本来也要再想一遍。</p>
      <div class="sheet-actions">
        <button type="button" @click="showCreate = false">取消</button>
        <button class="primary" type="submit" :disabled="creating || !form.title.trim() || !bookId.trim()">
          {{ creating ? '创建中…' : '创建' }}
        </button>
      </div>
    </form>
  </div>
</template>

<style scoped>
.page-head { display: flex; align-items: flex-end; justify-content: space-between; gap: 12px; padding: 8px 0 14px; }
.eyebrow { margin: 0; color: var(--accent); font-size: 11px; letter-spacing: .14em; text-transform: uppercase; }
h1 { margin: 4px 0 0; font-family: var(--font-prose); font-size: 28px; line-height: 1.2; }
.book-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 10px; }
.book-row {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  width: 100%; padding: 14px 16px; text-align: left;
}
.book-main { display: grid; gap: 4px; min-width: 0; }
.book-main strong { font-family: var(--font-prose); font-size: 17px; }
.book-main small { color: var(--ink-soft); font-size: 12px; }
.badge { flex-shrink: 0; font-size: 11px; padding: 2px 8px; border-radius: 999px; background: rgba(161,92,18,.14); color: var(--warn); }
.empty { padding: 26px 18px; text-align: left; }
.empty h2 { margin: 0 0 8px; font-family: var(--font-prose); font-size: 20px; }
.empty p { margin: 0 0 14px; color: var(--ink-soft); font-size: 13px; line-height: 1.7; }
.welcome-actions { display: grid; gap: 8px; }
.welcome-actions button { width: 100%; min-height: 46px; }
.welcome-note { margin: 14px 0 0 !important; font-size: 11px !important; }
.sheet-mask {
  position: fixed; inset: 0; z-index: 60; display: flex; align-items: flex-end;
  background: rgba(20,35,31,.32);
}
.sheet {
  width: 100%; display: grid; gap: 12px; border-radius: 18px 18px 0 0;
  padding: 18px 16px calc(18px + env(safe-area-inset-bottom));
  max-height: 92dvh; overflow-y: auto;
}
.sheet header { display: flex; align-items: center; justify-content: space-between; }
.sheet h2 { margin: 0; font-family: var(--font-prose); font-size: 20px; }
.close { min-width: 44px; min-height: 44px; border: 0; background: transparent; font-size: 22px; line-height: 1; }
.sheet label { display: grid; gap: 6px; font-size: 13px; color: var(--ink-soft); }
.sheet textarea { padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; background: rgba(255,255,255,.85); }
.note { margin: 0; color: var(--ink-soft); font-size: 12px; line-height: 1.7; }
.sheet-actions { display: flex; gap: 10px; }
.sheet-actions button { flex: 1; }
</style>
