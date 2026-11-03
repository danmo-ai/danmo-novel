<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Candidate } from '@/types'

/**
 * 立项候选的逐文件变更预览。
 *
 * 接受立项是一次多文件覆盖，而界面上原来只有一个文件名列表——作者无法判断
 * 「这次优化到底改了什么」。这里把候选与当前 canon 做一次行级比较，并把
 * 被 preservePaths 排除的文件单独标出来（它们不会写入）。
 */
const props = defineProps<{
  candidate: Candidate
  /** 当前书籍文件（相对路径 → 内容）。 */
  files: Record<string, string>
}>()

type Row = { path: string; current: string; next: string; status: 'new' | 'changed' | 'same' | 'kept' }

function castFiles(raw?: string): { path: string; next: string }[] {
  if (!raw) return []
  try {
    const list = JSON.parse(raw) as { stem?: string; cardMarkdown?: string }[]
    return list
      .filter((c) => c.stem)
      .map((c) => ({ path: `canon/cast/${c.stem}.md`, next: c.cardMarkdown || `# ${c.stem}\n` }))
  } catch {
    return []
  }
}

const preserve = computed(() => new Set((props.candidate as any).preservePaths || []))

const rows = computed<Row[]>(() => {
  const c = props.candidate
  const out: Row[] = []
  const push = (path: string, next: string) => {
    const current = props.files[path] || ''
    const kept = preserve.value.has(path)
    out.push({
      path,
      current,
      next,
      status: kept ? 'kept' : !current.trim() ? 'new' : next === current ? 'same' : 'changed',
    })
  }
  if (c.mode === 'volume') {
    if (c.planMarkdown?.volume_outline) push(`outline/volumes/${c.volumeId || 'v01'}.md`, c.planMarkdown.volume_outline)
    return out
  }
  if (c.planMarkdown?.book_outline) push('outline/book_outline.md', c.planMarkdown.book_outline)
  for (const [path, body] of Object.entries(c.planSettings || {})) push(path, body)
  for (const row of castFiles(c.planMarkdown?.cast_json)) push(row.path, row.next)
  return out
})

const counts = computed(() => ({
  changed: rows.value.filter((r) => r.status === 'changed').length,
  added: rows.value.filter((r) => r.status === 'new').length,
  same: rows.value.filter((r) => r.status === 'same').length,
  kept: rows.value.filter((r) => r.status === 'kept').length,
}))

const STATUS_LABEL: Record<Row['status'], string> = {
  new: '新增',
  changed: '修改',
  same: '未变',
  kept: '保留（不写）',
}

type Seg = { kind: 'same' | 'add' | 'del'; text: string }

// 行级 LCS。立项文件都在几百行以内，直接 DP 足够；超过行数上限就退化成
// 「整体替换」，避免一次打开候选卡片就卡住界面。
const MAX_DIFF_LINES = 1200

function lineDiff(before: string, after: string): Seg[] {
  const a = before.split('\n')
  const b = after.split('\n')
  if (a.length + b.length > MAX_DIFF_LINES) {
    return [
      ...(before ? [{ kind: 'del' as const, text: before }] : []),
      ...(after ? [{ kind: 'add' as const, text: after }] : []),
    ]
  }
  const n = a.length
  const m = b.length
  const dp: number[][] = Array.from({ length: n + 1 }, () => new Array(m + 1).fill(0))
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1])
    }
  }
  const ops: Seg[] = []
  const push = (kind: Seg['kind'], text: string) => {
    const last = ops[ops.length - 1]
    if (last && last.kind === kind) last.text += '\n' + text
    else ops.push({ kind, text })
  }
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      push('same', a[i])
      i++
      j++
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      push('del', a[i++])
    } else {
      push('add', b[j++])
    }
  }
  while (i < n) push('del', a[i++])
  while (j < m) push('add', b[j++])
  return ops
}

// 默认只展开有改动的文件：一份 9 设定 + 人物 + 总纲的候选，全展开读完就没法判断了。
const expanded = ref<Set<string>>(new Set(rows.value.filter((r) => r.status === 'changed' || r.status === 'new').map((r) => r.path)))
function toggle(path: string) {
  const s = new Set(expanded.value)
  if (s.has(path)) s.delete(path)
  else s.add(path)
  expanded.value = s
}
function diffOf(row: Row) {
  return lineDiff(row.current, row.next)
}
</script>

<template>
  <div class="plan-diff">
    <div class="diff-stats">
      <span v-if="counts.changed"><strong>{{ counts.changed }}</strong> 个修改</span>
      <span v-if="counts.added"><strong>{{ counts.added }}</strong> 个新增</span>
      <span v-if="counts.same" class="muted">{{ counts.same }} 个未变</span>
      <span v-if="counts.kept" class="muted">{{ counts.kept }} 个保留不写</span>
    </div>
    <div class="diff-files">
      <article v-for="row in rows" :key="row.path" class="diff-file" :class="row.status">
        <button class="diff-file-head" @click="toggle(row.path)">
          <span class="diff-path">{{ row.path }}</span>
          <span class="diff-tag">{{ STATUS_LABEL[row.status] }}</span>
        </button>
        <div v-if="expanded.has(row.path)" class="diff-body">
          <p v-if="row.status === 'kept'" class="kept-note">该文件在「接受时保留」中，候选内容不会写入。</p>
          <p v-else-if="row.status === 'same'" class="kept-note">与当前版本逐字相同。</p>
          <pre v-else class="diff-pre"><template v-for="(seg, i) in diffOf(row)" :key="i"><span :class="seg.kind">{{ seg.kind === 'add' ? '+ ' : seg.kind === 'del' ? '- ' : '  ' }}{{ seg.text }}
</span></template></pre>
        </div>
      </article>
    </div>
  </div>
</template>

<style scoped>
.plan-diff { display: grid; gap: 8px; }
.diff-stats { display: flex; gap: 12px; font-size: 12px; color: var(--ink-soft); }
.diff-stats strong { color: var(--accent); }
.diff-files { display: grid; gap: 6px; }
.diff-file { border: 1px solid var(--line); border-radius: 10px; overflow: hidden; background: rgba(255, 255, 255, .4); }
.diff-file.kept { opacity: .7; }
.diff-file-head { display: flex; justify-content: space-between; gap: 10px; width: 100%; padding: 8px 12px; border: 0; background: transparent; text-align: left; font-size: 12px; }
.diff-path { font-family: ui-monospace, monospace; word-break: break-all; }
.diff-tag { flex: none; color: var(--ink-soft); }
.diff-file.new .diff-tag { color: var(--ok); }
.diff-file.changed .diff-tag { color: var(--warn); }
.diff-body { border-top: 1px solid var(--line); padding: 8px 12px; }
.kept-note { margin: 0; font-size: 11px; color: var(--ink-soft); }
.diff-pre { margin: 0; max-height: 340px; overflow: auto; font: 12px/1.7 ui-monospace, monospace; white-space: pre-wrap; }
.diff-pre .add { color: #1b7a4b; background: rgba(27, 122, 75, .08); }
.diff-pre .del { color: #b4451f; background: rgba(180, 69, 31, .08); }
.diff-pre .same { color: var(--ink-soft); }
</style>
