<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ChapterDraft } from '@/shared/types'

const props = defineProps<{
  base?: ChapterDraft[]
  revised: ChapterDraft[]
}>()

type Seg = { kind: 'same' | 'add' | 'del'; text: string }
type Row = { chapter: number; title: string; segs: Seg[]; changed: boolean; added: number; removed: number }

// 句级 LCS：中文正文里"改了什么"通常落在句子上，句级比行级更贴合作者的阅读单位。
function splitSentences(text: string): string[] {
  const out: string[] = []
  let buf = ''
  for (const ch of text) {
    buf += ch
    if ('。！？；\n'.includes(ch)) {
      out.push(buf)
      buf = ''
    }
  }
  if (buf) out.push(buf)
  return out
}

function pushMerge(segs: Seg[], kind: Seg['kind'], text: string) {
  if (!text) return
  const last = segs[segs.length - 1]
  if (last && last.kind === kind) last.text += text
  else segs.push({ kind, text })
}

// 通用 LCS 差分：返回 same / del / add 交错序列。
function lcsOps(a: string[], b: string[]): Seg[] {
  const n = a.length
  const m = b.length
  const dp: number[][] = Array.from({ length: n + 1 }, () => new Array(m + 1).fill(0))
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1])
    }
  }
  const ops: Seg[] = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      pushMerge(ops, 'same', a[i])
      i++
      j++
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      pushMerge(ops, 'del', a[i])
      i++
    } else {
      pushMerge(ops, 'add', b[j])
      j++
    }
  }
  while (i < n) pushMerge(ops, 'del', a[i++])
  while (j < m) pushMerge(ops, 'add', b[j++])
  return ops
}

// 字级细化：句级 diff 会把整句重复一遍（"……说：七天了。"→"……说：五年了。"），
// 中文长句下读起来噪音很大。对成对的增删再做一次字级 LCS，只高亮真正改动的字。
const refineBudget = 400_000 // 超过就不细化，避免整章重写时卡住

function refineChangeBlocks(ops: Seg[]): Seg[] {
  const out: Seg[] = []
  let i = 0
  while (i < ops.length) {
    if (ops[i].kind === 'same') {
      pushMerge(out, 'same', ops[i].text)
      i++
      continue
    }
    let dels = ''
    let adds = ''
    while (i < ops.length && ops[i].kind !== 'same') {
      if (ops[i].kind === 'del') dels += ops[i].text
      else adds += ops[i].text
      i++
    }
    if (dels && adds && [...dels].length * [...adds].length <= refineBudget) {
      for (const s of lcsOps([...dels], [...adds])) pushMerge(out, s.kind, s.text)
    } else {
      pushMerge(out, 'del', dels)
      pushMerge(out, 'add', adds)
    }
  }
  return out
}

const rows = computed<Row[]>(() => {
  const baseByChapter = new Map<number, ChapterDraft>()
  for (const ch of props.base || []) baseByChapter.set(ch.chapter, ch)

  return props.revised.map((ch) => {
    const before = baseByChapter.get(ch.chapter)
    if (!before) {
      return { chapter: ch.chapter, title: ch.title, segs: [{ kind: 'add', text: ch.body }], changed: true, added: ch.body.length, removed: 0 }
    }
    if (before.body === ch.body) {
      return { chapter: ch.chapter, title: ch.title, segs: [{ kind: 'same', text: ch.body }], changed: false, added: 0, removed: 0 }
    }
    const segs = refineChangeBlocks(lcsOps(splitSentences(before.body), splitSentences(ch.body)))
    let added = 0
    let removed = 0
    for (const s of segs) {
      if (s.kind === 'add') added += s.text.length
      else if (s.kind === 'del') removed += s.text.length
    }
    return { chapter: ch.chapter, title: ch.title, segs, changed: true, added, removed }
  })
})

const changedRows = computed(() => rows.value.filter((r) => r.changed))
const unchangedRows = computed(() => rows.value.filter((r) => !r.changed))

// 默认只看改动章：未涉及的章逐字未变，铺开会把真正的修改淹没。
const showUnchanged = ref(false)
const visibleRows = computed(() => (showUnchanged.value ? rows.value : changedRows.value))

const totalAdded = computed(() => changedRows.value.reduce((n, r) => n + r.added, 0))
const totalRemoved = computed(() => changedRows.value.reduce((n, r) => n + r.removed, 0))
</script>

<template>
  <div class="chapter-diff">
    <div class="diff-head">
      <span class="diff-stat">
        <strong>{{ changedRows.length }}</strong> 章有改动
        <span v-if="totalAdded" class="add">+{{ totalAdded }} 字</span>
        <span v-if="totalRemoved" class="del">−{{ totalRemoved }} 字</span>
      </span>
      <label v-if="unchangedRows.length" class="diff-toggle">
        <input type="checkbox" v-model="showUnchanged" />
        显示未改动的 {{ unchangedRows.length }} 章
      </label>
    </div>

    <div v-if="!changedRows.length" class="diff-empty">
      修订没有产生任何改动。这份候选与现有正文逐字相同，接受不会改变正文。
    </div>

    <details v-for="r in visibleRows" :key="r.chapter" class="diff-chapter" :open="r.changed">
      <summary>
        <span class="ch-label">第 {{ r.chapter }} 章</span>
        <span class="ch-title">{{ r.title }}</span>
        <span v-if="r.changed" class="ch-flag">已改</span>
        <span v-else class="ch-flag unchanged">未改</span>
      </summary>
      <div class="diff-body">
        <template v-for="(s, i) in r.segs" :key="i">
          <span v-if="s.kind === 'same'" class="seg">{{ s.text }}</span>
          <del v-else-if="s.kind === 'del'" class="seg del">{{ s.text }}</del>
          <ins v-else class="seg add">{{ s.text }}</ins>
        </template>
      </div>
    </details>
  </div>
</template>

<style scoped>
.chapter-diff { display: grid; gap: 10px; }
.diff-head { display: flex; justify-content: space-between; align-items: center; gap: 12px; flex-wrap: wrap; font-size: 12px; color: var(--ink-soft); }
.diff-stat strong { font-size: 14px; color: var(--ink); }
.diff-stat .add { color: var(--ok); margin-left: 8px; }
.diff-stat .del { color: var(--bad); margin-left: 8px; }
.diff-toggle { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; }
.diff-empty { padding: 14px; border-radius: 10px; background: rgba(0,0,0,.03); font-size: 13px; color: var(--ink-soft); }
.diff-chapter { border: 1px solid var(--line); border-radius: 10px; overflow: hidden; }
.diff-chapter summary { display: flex; align-items: center; gap: 10px; padding: 10px 14px; cursor: pointer; background: rgba(255,255,255,.5); font-size: 13px; }
.ch-label { font-weight: 600; }
.ch-title { color: var(--ink-soft); }
.ch-flag { margin-left: auto; font-size: 11px; padding: 1px 8px; border-radius: 999px; background: rgba(163,59,50,.1); color: var(--bad); }
.ch-flag.unchanged { background: rgba(0,0,0,.05); color: var(--ink-soft); }
.diff-body { padding: 12px 14px; font-size: 13px; line-height: 1.9; white-space: pre-wrap; max-height: 420px; overflow: auto; }
.seg { color: var(--ink-soft); }
.seg.add, ins.add { color: var(--ok); background: rgba(31,122,77,.1); text-decoration: none; border-radius: 3px; }
.seg.del, del.del { color: var(--bad); background: rgba(163,59,50,.08); border-radius: 3px; }

</style>
