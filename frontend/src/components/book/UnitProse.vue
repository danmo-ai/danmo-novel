<script setup lang="ts">
import { computed, ref } from 'vue'
import type { UnitInfo } from '@/types'

const props = defineProps<{ unit: UnitInfo; raw: string; canWrite: boolean }>()
const emit = defineEmits<{ edit: [] }>()
const activeChapter = ref(0)
const copied = ref<number | null>(null)
const copiedAll = ref(false)

type Chapter = { chapter: number; title: string; body: string; words: number }
const chapters = computed<Chapter[]>(() => {
  const lines = (props.raw || '').replace(/\r\n/g, '\n').split('\n')
  const out: Chapter[] = []
  let cur: { chapter: number; title: string; body: string[] } | null = null
  const flush = () => {
    if (!cur) return
    const body = cur.body.join('\n').trim()
    out.push({ chapter: cur.chapter, title: cur.title, body, words: body.replace(/\s/g, '').length })
    cur = null
  }
  for (const line of lines) {
    const m = line.match(/^##\s*第\s*(\d+)\s*章\s*(.*)$/)
    if (m) {
      flush()
      cur = { chapter: Number(m[1]), title: m[2].trim(), body: [] }
      continue
    }
    if (!cur) continue
    if (line.trim() === '---') continue
    cur.body.push(line)
  }
  flush()
  return out
})

const current = computed(() => chapters.value[activeChapter.value] || chapters.value[0])
const totalWords = computed(() => chapters.value.reduce((s, c) => s + c.words, 0))
const paragraphs = computed(() => (current.value?.body || '').split(/\n{2,}/).map((p) => p.trim()).filter(Boolean))

function copyText(text: string) {
  if (navigator.clipboard?.writeText) return navigator.clipboard.writeText(text)
  const ta = document.createElement('textarea')
  ta.value = text
  ta.style.position = 'fixed'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  ta.select()
  document.execCommand('copy')
  document.body.removeChild(ta)
  return Promise.resolve()
}

async function copyChapter() {
  const c = current.value
  if (!c) return
  await copyText(`第${c.chapter}章 ${c.title}\n\n${c.body}`)
  copied.value = c.chapter
  setTimeout(() => { copied.value = null }, 1600)
}

async function copyAll() {
  const text = chapters.value.map((c) => `第${c.chapter}章 ${c.title}\n\n${c.body}`).join('\n\n')
  await copyText(text)
  copiedAll.value = true
  setTimeout(() => { copiedAll.value = false }, 1600)
}
</script>

<template>
  <section class="prose-view">
    <slot name="contract" />
    <header class="prose-head">
      <div>
        <small>{{ unit.unitId }} · 共 {{ chapters.length }} 章 · {{ totalWords.toLocaleString() }} 字</small>
        <h3>{{ unit.title || '单元正文' }}</h3>
      </div>
      <div class="prose-actions">
        <slot name="actions" />
        <button class="copy-all" @click="copyAll">{{ copiedAll ? '✓ 已复制全部' : '复制全部' }}</button>
        <button v-if="canWrite" @click="emit('edit')">编辑</button>
      </div>
    </header>

    <div class="chapter-tabs" role="tablist" aria-label="章节">
      <button v-for="(c, i) in chapters" :key="c.chapter" role="tab"
        :aria-selected="activeChapter === i || (!activeChapter && i === 0)"
        :class="{ active: (activeChapter === i) || (activeChapter === 0 && i === 0) }"
        @click="activeChapter = i">
        <span>第 {{ c.chapter }} 章</span><small>{{ c.words.toLocaleString() }} 字</small>
      </button>
    </div>

    <article v-if="current" class="chapter-card">
      <div class="chapter-title-row">
        <h2>第 {{ current.chapter }} 章<template v-if="current.title"> · {{ current.title }}</template></h2>
        <button class="copy-chapter" :class="{ done: copied === current.chapter }" @click="copyChapter">
          {{ copied === current.chapter ? '✓ 已复制' : '复制本章' }}
        </button>
      </div>
      <div class="chapter-body">
        <p v-for="(p, i) in paragraphs" :key="i">{{ p }}</p>
      </div>
      <footer class="chapter-foot">
        <span>{{ current.words.toLocaleString() }} 字</span>
        <div>
          <button v-if="activeChapter > 0" @click="activeChapter--">← 上一章</button>
          <button v-if="activeChapter < chapters.length - 1" @click="activeChapter++">下一章 →</button>
        </div>
      </footer>
    </article>
    <p v-else class="empty">正文尚未解析出章节。</p>
  </section>
</template>

<style scoped>
.prose-view { display: grid; gap: 20px; padding: 22px 24px 26px; }
.prose-head { display: flex; justify-content: space-between; align-items: flex-end; gap: 16px; padding-bottom: 2px; }
.prose-head small { color: var(--accent); font-size: 11px; font-weight: 600; letter-spacing: .03em; }
.prose-head h3 { margin: 6px 0 0; font-family: var(--font-prose); font-size: 24px; line-height: 1.3; }
.prose-actions { display: flex; gap: 8px; flex-shrink: 0; flex-wrap: wrap; justify-content: flex-end; align-items: center; max-width: min(100%, 520px); }

.chapter-tabs { display: flex; gap: 10px; overflow-x: auto; padding: 2px 2px 6px; }
.chapter-tabs button { display: grid; gap: 3px; padding: 10px 18px; border-radius: 12px; white-space: nowrap; border: 1px solid var(--line); background: rgba(255,255,255,.65); color: var(--ink-soft); text-align: center; flex-shrink: 0; }
.chapter-tabs button span { font-size: 13px; font-weight: 600; }
.chapter-tabs button small { font-size: 10px; opacity: .75; }
.chapter-tabs button.active { background: var(--accent); border-color: var(--accent); color: white; box-shadow: 0 4px 12px rgba(15,107,92,.25); }
.chapter-tabs button.active small { opacity: .85; }

.chapter-card { background: rgba(255,255,255,.7); border: 1px solid var(--line); border-radius: 16px; overflow: hidden; }
.chapter-title-row { display: flex; justify-content: space-between; align-items: center; gap: 14px; padding: 20px 28px; border-bottom: 1px solid var(--line); }
.chapter-title-row h2 { margin: 0; font-family: var(--font-prose); font-size: 22px; line-height: 1.35; }
.copy-chapter { flex-shrink: 0; }
.copy-chapter.done { color: var(--accent); border-color: var(--accent); }
.chapter-body { padding: 28px 34px; max-height: 66vh; overflow-y: auto; }
.chapter-body p { font-family: var(--font-prose); font-size: 17px; line-height: 2.05; color: var(--ink); margin: 0 0 1.3em; text-indent: 2em; }
.chapter-body p:last-child { margin-bottom: 0; }
.chapter-foot { display: flex; justify-content: space-between; align-items: center; padding: 14px 28px; border-top: 1px solid var(--line); color: var(--ink-soft); font-size: 12px; }
.chapter-foot div { display: flex; gap: 8px; }
.empty { color: var(--ink-soft); padding: 24px; }
@media (max-width: 720px) {
  .prose-view { padding: 16px 14px 20px; gap: 16px; }
  .chapter-body { padding: 20px; }
  .chapter-body p { font-size: 16px; }
  .chapter-title-row { padding: 16px 18px; }
  .prose-head { flex-direction: column; align-items: flex-start; }
}
</style>
