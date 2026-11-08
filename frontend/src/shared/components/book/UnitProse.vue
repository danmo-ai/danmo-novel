<script setup lang="ts">
import { computed, ref } from 'vue'
import type { UnitInfo } from '@/shared/types'

const props = defineProps<{
  unit: UnitInfo
  raw: string
  canWrite: boolean
  /** 连续阅读：按章连续渲染，不再一次只看一章。移动端更像读书。 */
  continuous?: boolean
}>()
const emit = defineEmits<{ edit: [chapter: number, title: string, raw: string] }>()
const activeChapter = ref(0)
const copied = ref<number | null>(null)

// 发布端单章超过这个字数通常要拆成上下两章。这里只在**展示层**标出建议切分点，
// 不写回正文：正文仍是模型的连续输出，切分是作者发布时的人工决定。
const SPLIT_HINT_CHARS = 5000

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

// splitInfo 给出「这一章该在哪一段之后切开」：段落边界中离全章字数一半最近的那个。
//
// 只在段落之间切，不切句子——句中断开会把对话或动作截成两半，发布时还得手工缝。
// 上下两半各自按同一口径（去掉空白）计字，作者能直接判断拆完是否还满足平台下限。
const splitInfo = computed<{ index: number; above: number; below: number } | null>(() => {
  const c = current.value
  const ps = paragraphs.value
  if (!c || c.words <= SPLIT_HINT_CHARS || ps.length < 2) return null
  const half = c.words / 2
  let acc = 0
  let index = -1
  let bestDiff = Infinity
  for (let i = 0; i < ps.length - 1; i++) {
    acc += ps[i].replace(/\s/g, '').length
    const diff = Math.abs(acc - half)
    if (diff < bestDiff) {
      bestDiff = diff
      index = i
    }
  }
  if (index < 0) return null
  let above = 0
  for (let i = 0; i <= index; i++) above += ps[i].replace(/\s/g, '').length
  return { index, above, below: Math.max(c.words - above, 0) }
})
// 模板里用扁平值，避免在每个属性表达式里重复判空。
const splitIndex = computed(() => splitInfo.value?.index ?? -1)
const splitLabel = computed(() => {
  const s = splitInfo.value
  return s ? `建议在此切成上下两章 · 上 ${s.above.toLocaleString()} 字 / 下 ${s.below.toLocaleString()} 字` : ''
})
const splitAria = computed(() => {
  const s = splitInfo.value
  return s ? `建议切分点，上 ${s.above} 字，下 ${s.below} 字` : ''
})

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

function editChapter() {
  const c = current.value
  if (!c) return
  emit('edit', c.chapter, c.title, `## 第${c.chapter}章${c.title ? ` ${c.title}` : ''}\n\n${c.body}`)
}

/**
 * 连续阅读时的当前章：由滚动位置决定（锚点），而不是 tab。
 * 切章会改变复制/编辑的目标，所以这里统一记当前所在章。
 */
const visibleChapter = computed(() => props.continuous ? chapters.value[activeChapter.value] || chapters.value[0] : current.value)

function chapterParagraphs(c: Chapter) {
  return (c?.body || '').split(/\n{2,}/).map((p) => p.trim()).filter(Boolean)
}
function chapterSplitIndex(c: Chapter): number {
  if (!c || c.words <= SPLIT_HINT_CHARS) return -1
  const ps = chapterParagraphs(c)
  if (ps.length < 2) return -1
  const half = c.words / 2
  let acc = 0, index = -1, bestDiff = Infinity
  for (let i = 0; i < ps.length - 1; i++) {
    acc += ps[i].replace(/\s/g, '').length
    const diff = Math.abs(acc - half)
    if (diff < bestDiff) { bestDiff = diff; index = i }
  }
  return index
}

async function copyChapterByNumber(chapter: number) {
  const c = chapters.value.find((x) => x.chapter === chapter)
  if (!c) return
  await copyText(`第${c.chapter}章 ${c.title}\n\n${c.body}`)
  copied.value = c.chapter
  setTimeout(() => { copied.value = null }, 1600)
}

function editChapterByNumber(chapter: number) {
  const c = chapters.value.find((x) => x.chapter === chapter)
  if (!c) return
  emit('edit', c.chapter, c.title, `## 第${c.chapter}章${c.title ? ` ${c.title}` : ''}\n\n${c.body}`)
}

function scrollToChapter(chapter: number) {
  // 定位条只是锚点，不改变阅读位置之外的状态；复制/编辑按当前章记。
  activeChapter.value = chapters.value.findIndex((c) => c.chapter === chapter)
  const el = document.getElementById(`ch-${chapter}`)
  if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' })
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
        <div class="production-actions"><slot name="actions" /></div>
      </div>
    </header>

    <div class="chapter-tabs" role="tablist" aria-label="章节">
      <button v-for="(c, i) in chapters" :key="c.chapter" role="tab"
        :aria-selected="activeChapter === i || (!activeChapter && i === 0)"
        :class="{ active: (activeChapter === i) || (activeChapter === 0 && i === 0) }"
        @click="activeChapter = i; if (continuous) scrollToChapter(c.chapter)">
        <span>第 {{ c.chapter }} 章</span><small>{{ c.words.toLocaleString() }} 字</small>
      </button>
    </div>

    <!-- 连续阅读：所有章按顺序铺开，复制/编辑仍然只作用于所在章。 -->
    <div v-if="continuous" class="continuous">
      <article v-for="c in chapters" :key="c.chapter" :id="`ch-${c.chapter}`" class="chapter-card">
        <div class="chapter-title-row">
          <h2>第 {{ c.chapter }} 章<template v-if="c.title"> · {{ c.title }}</template></h2>
          <div class="chapter-actions">
            <button class="copy-chapter" :class="{ done: copied === c.chapter }" @click="copyChapterByNumber(c.chapter)">
              {{ copied === c.chapter ? '✓ 已复制' : '复制' }}
            </button>
            <button v-if="canWrite" @click="editChapterByNumber(c.chapter)">编辑</button>
          </div>
        </div>
        <div class="chapter-body">
          <template v-for="(p, i) in chapterParagraphs(c)" :key="i">
            <p>{{ p }}</p>
            <div v-if="i === chapterSplitIndex(c)" class="split-mark" role="separator">
              <span class="split-line" />
              <small>建议在此切成上下两章</small>
              <span class="split-line" />
            </div>
          </template>
        </div>
        <footer class="chapter-foot"><span>{{ c.words.toLocaleString() }} 字</span></footer>
      </article>
    </div>

    <article v-if="!continuous && current" class="chapter-card">
      <div class="chapter-title-row">
        <h2>第 {{ current.chapter }} 章<template v-if="current.title"> · {{ current.title }}</template></h2>
        <div class="chapter-actions">
          <button class="copy-chapter" :class="{ done: copied === current.chapter }" @click="copyChapter">
            {{ copied === current.chapter ? '✓ 已复制' : '复制' }}
          </button>
          <button v-if="canWrite" @click="editChapter">编辑</button>
        </div>
      </div>
      <div class="chapter-body">
        <template v-for="(p, i) in paragraphs" :key="i">
          <p>{{ p }}</p>
          <div v-if="i === splitIndex" class="split-mark" role="separator" :aria-label="splitAria">
            <span class="split-line" />
            <small>{{ splitLabel }}</small>
            <span class="split-line" />
          </div>
        </template>
      </div>
      <footer class="chapter-foot">
        <span>
          {{ current.words.toLocaleString() }} 字
          <em v-if="splitInfo" class="split-note">超过 {{ SPLIT_HINT_CHARS.toLocaleString() }} 字 · 正文中已标建议切分点</em>
        </span>
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
.production-actions { display: contents; }
.chapter-actions { display: flex; gap: 8px; flex-shrink: 0; }

.chapter-tabs { display: flex; gap: 10px; overflow-x: auto; padding: 2px 2px 6px; }
.chapter-tabs button { display: grid; gap: 3px; padding: 10px 18px; border-radius: 12px; white-space: nowrap; border: 1px solid var(--line); background: rgba(255,255,255,.65); color: var(--ink-soft); text-align: center; flex-shrink: 0; }
.chapter-tabs button span { font-size: 13px; font-weight: 600; }
.chapter-tabs button small { font-size: 10px; opacity: .75; }
.chapter-tabs button.active { background: var(--accent); border-color: var(--accent); color: white; }
.chapter-tabs button.active small { opacity: .85; }

.chapter-card { background: rgba(255,255,255,.7); border: 1px solid var(--line); border-radius: 16px; overflow: hidden; }
.chapter-title-row { display: flex; justify-content: space-between; align-items: center; gap: 14px; padding: 20px 28px; border-bottom: 1px solid var(--line); }
.chapter-title-row h2 { margin: 0; font-family: var(--font-prose); font-size: 22px; line-height: 1.35; }
.copy-chapter { flex-shrink: 0; }
.copy-chapter.done { color: var(--accent); border-color: var(--accent); }
.chapter-body { padding: 28px 34px; max-height: 66vh; overflow-y: auto; }
.chapter-body p { font-family: var(--font-prose); font-size: 17px; line-height: 2.05; color: var(--ink); margin: 0 0 1.3em; text-indent: 2em; }
.chapter-body p:last-child { margin-bottom: 0; }
/* 建议切分点：展示层的辅助线，不是正文的一部分——不参与复制、不写回文件。 */
.split-mark { display: flex; align-items: center; gap: 12px; margin: 1.9em 0; }
.split-mark small { color: var(--ink-soft); font-size: 11px; letter-spacing: .02em; text-align: center; }
.split-line { flex: 1 1 24px; min-width: 16px; height: 0; border-top: 1px dashed var(--line); }
.split-note { margin-left: 10px; padding: 2px 7px; border-radius: 6px; background: rgba(15,107,92,.08); color: var(--accent); font-size: 10px; font-style: normal; }
.chapter-foot { display: flex; justify-content: space-between; align-items: center; padding: 14px 28px; border-top: 1px solid var(--line); color: var(--ink-soft); font-size: 12px; }
.chapter-foot div { display: flex; gap: 8px; }
.empty { color: var(--ink-soft); padding: 24px; }
.continuous { display: grid; gap: 18px; }
.continuous .chapter-card { scroll-margin-top: 8px; }
html.shell-mobile .prose-view { padding: 16px 12px 20px; gap: 16px; min-width: 0; }
html.shell-mobile .prose-head { display: grid; grid-template-columns: minmax(0, 1fr); gap: 14px; align-items: stretch; }
html.shell-mobile .prose-head > div:first-child { min-width: 0; }
html.shell-mobile .prose-head h3 { font-size: 22px; overflow-wrap: anywhere; }
html.shell-mobile .prose-actions { display: grid; grid-template-columns: minmax(0, 1fr); gap: 10px; width: 100%; max-width: none; }
html.shell-mobile .production-actions { display: block; width: 100%; min-width: 0; }
html.shell-mobile .chapter-tabs { gap: 8px; padding: 2px 0 8px; scrollbar-width: none; scroll-snap-type: x proximity; overscroll-behavior-inline: contain; }
html.shell-mobile .chapter-tabs::-webkit-scrollbar { display: none; }
html.shell-mobile .chapter-tabs button { min-height: 52px; padding: 8px 14px; scroll-snap-align: start; }
html.shell-mobile .chapter-card { border-radius: 12px; }
html.shell-mobile .chapter-title-row { padding: 14px 16px; gap: 10px; align-items: flex-start; }
html.shell-mobile .chapter-title-row h2 { font-size: 19px; min-width: 0; }
html.shell-mobile .copy-chapter { min-height: 44px; padding: 6px 10px; font-size: 12px; }
html.shell-mobile .chapter-body { max-height: none; padding: 20px 18px; overflow: visible; }
html.shell-mobile .chapter-body p { font-size: 17px; line-height: 1.95; }
html.shell-mobile .split-mark { gap: 8px; }
html.shell-mobile .split-mark small { font-size: 10px; line-height: 1.7; }
html.shell-mobile .split-line { flex: 1 1 8px; min-width: 8px; }
html.shell-mobile .chapter-foot { padding: 12px 16px; flex-wrap: wrap; gap: 8px; }
html.shell-mobile .chapter-foot button { min-height: 44px; }

</style>
