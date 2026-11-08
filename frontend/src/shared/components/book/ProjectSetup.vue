<script setup lang="ts">
import { computed, ref } from 'vue'
import { useAppStore } from '@/shared/stores/app'
import type { Candidate } from '@/shared/types'
import PlanCandidateDiff from '@/shared/components/book/PlanCandidateDiff.vue'

/**
 * 立项工作台：只服务「还没有总纲」的书。
 *
 * 职责边界很窄：作者输入 → 创意扩写（自由 Markdown）→ 提案确认 → 执行一次
 * 结构化立项（总纲 + 9 个设定 + 人物）。总纲一旦接受，这个页面就整体消失，
 * 后续的重新立项、范围化优化、单文件重写都在大纲 / 设定 Tab 完成——
 * 两个页面都能重写立项，作者就分不清该用哪个。
 *
 * 所有动作都只产出候选，接受才写盘。
 */
const props = defineProps<{ busy: boolean; saving: boolean }>()
const emit = defineEmits<{
  save: [path: string, content: string]
  idea: [instruction: string]
  plan: [mode: 'init' | 'reinit', volume?: number]
  accept: [candidate: Candidate]
  reject: [candidate: Candidate]
}>()

const store = useAppStore()
const book = computed(() => store.currentBook)
const files = computed(() => book.value?.files || {})
const readOnly = computed(() => Boolean(book.value?.summary.readOnly))

const ideaRaw = computed(() => files.value['outline/idea.md'] || '')
const hasIdea = computed(() => Boolean(ideaRaw.value.trim()))

/** 立项输入段：从「## 立项输入」到「## 一句话 idea」之前。 */
function section(raw: string, title: string, next: string[]): string {
  const lines = raw.split('\n')
  const start = lines.findIndex((l) => l.trim() === title)
  if (start < 0) return ''
  let end = lines.length
  for (let i = start + 1; i < lines.length; i++) {
    if (next.some((t) => lines[i].trim() === t)) { end = i; break }
  }
  return lines.slice(start + 1, end).join('\n').trim()
}
const intake = computed(() => section(ideaRaw.value, '## 立项输入', ['## 一句话 idea', '## 创意提案']))
const logline = computed(() => {
  const body = section(ideaRaw.value, '## 一句话 idea', ['## 创意提案'])
  return body.replace(/^>\s?/gm, '').trim()
})
const proposal = computed(() => {
  const body = section(ideaRaw.value, '## 创意提案', [])
  return body.replace(/<!--[\s\S]*?-->/g, '').trim()
})
const hasProposal = computed(() => Boolean(proposal.value))
const ideaCandidates = computed(() => store.candidates.filter((c) => c.kind === 'idea'))
const planCandidates = computed(() => store.candidates.filter((c) => c.kind === 'plan'))

// 设定覆盖：只统计结构化立项会产出的 9 个文件。它说明这次立项将写哪些文件。
const SETTING_PATHS = [
  'book-bible.md',
  'canon/world.md',
  'canon/goldfinger.md',
  'canon/platform-positioning.md',
  'canon/writing-rules.md',
  'canon/style-fingerprint.md',
  'canon/foreshadow-schedule.md',
  'canon/author-lore.md',
  'canon/locked-terms.yaml',
]
const settingRows = computed(() => SETTING_PATHS.map((path) => ({ path, exists: Boolean((files.value[path] || '').trim()) })))
const settingCount = computed(() => settingRows.value.filter((r) => r.exists).length)

const editingIdea = ref(false)
const ideaDraft = ref('')
function startEditIdea() {
  ideaDraft.value = ideaRaw.value
  editingIdea.value = true
}
function saveIdea() {
  emit('save', 'outline/idea.md', ideaDraft.value)
  editingIdea.value = false
}

const direction = ref('')
const DIRECTIONS = ['更黑暗', '更轻松', '换主角', '加强悬疑', '提高爽点密度', '降低设定复杂度']
function expandIdea() {
  emit('idea', direction.value.trim())
  direction.value = ''
}

function candidateFiles(c: Candidate) {
  return Object.keys(c.planSettings || {}).length + (c.planMarkdown?.book_outline ? 1 : 0) + (c.planMarkdown?.cast_json ? 1 : 0)
}
</script>

<template>
  <section class="setup-workspace">
    <div class="view-title">
      <div>
        <p class="eyebrow">Setup</p>
        <h2>立项</h2>
        <p>先把灵感扩成作者确认过的创意提案，再生成总纲与设定。所有产出都是候选，接受才写盘。</p>
      </div>
      <div class="view-actions">
        <span class="cover-chip" :class="{ on: hasProposal }">创意提案 {{ hasProposal ? '已确认' : '待扩写' }}</span>
        <span class="cover-chip" :class="{ on: settingCount === SETTING_PATHS.length }">设定 {{ settingCount }}/{{ SETTING_PATHS.length }}</span>
      </div>
    </div>

    <article class="panel setup-card">
      <header>
        <div>
          <small>第 1 步 · 作者输入</small>
          <h3>立项输入与一句话 idea</h3>
        </div>
        <div class="card-actions">
          <button v-if="!editingIdea" :disabled="readOnly" @click="startEditIdea">编辑</button>
          <template v-else>
            <button @click="editingIdea = false">取消</button>
            <button class="primary" :disabled="props.saving || readOnly" @click="saveIdea">保存</button>
          </template>
        </div>
      </header>
      <div v-if="editingIdea" class="idea-editor">
        <p>直接改这份 Markdown；「创意提案」段由扩写生成，也可以手工编辑。</p>
        <textarea v-model="ideaDraft" spellcheck="false" />
      </div>
      <template v-else-if="hasIdea">
        <blockquote v-if="logline" class="logline">{{ logline }}</blockquote>
        <pre v-if="intake" class="intake">{{ intake }}</pre>
        <p v-else class="muted">还没有填写立项输入。先补上题材、平台、读者与核心爽点，扩写才有依据。</p>
      </template>
      <p v-else class="muted">尚无立项输入。重新创建这本书时填写表单，或直接编辑生成 outline/idea.md。</p>
    </article>

    <article class="panel setup-card">
      <header>
        <div>
          <small>第 2 步 · 自由扩写（不结构化）</small>
          <h3>创意提案</h3>
        </div>
        <div class="card-actions">
          <button class="primary" :disabled="props.busy || readOnly" @click="expandIdea">
            {{ hasProposal ? '重新扩写' : '扩写创意' }}
          </button>
        </div>
      </header>
      <div class="direction-row">
        <input v-model="direction" placeholder="这次想改什么（可选，如：加强悬疑、换主角）" />
        <button v-for="d in DIRECTIONS" :key="d" class="chip" @click="direction = d">{{ d }}</button>
      </div>
      <p class="muted">扩写只产出 Markdown 提案候选，接受后写入 outline/idea.md；不写总纲、设定或人物。</p>
      <pre v-if="proposal" class="proposal">{{ proposal }}</pre>
      <p v-else class="empty">还没有创意提案。扩写后先看方向是否成立，再进入结构化立项。</p>

      <div v-if="ideaCandidates.length" class="pending">
        <h4>待处理的提案候选</h4>
        <div v-for="c in ideaCandidates" :key="c.id" class="pending-row">
          <span>{{ new Date(c.createdAt).toLocaleString() }} · {{ (c.replacement || '').length }} 字</span>
          <div>
            <button @click="emit('reject', c)">拒绝</button>
            <button class="primary" @click="emit('accept', c)">接受提案</button>
          </div>
        </div>
      </div>
    </article>

    <article class="panel setup-card">
      <header>
        <div>
          <small>第 3 步 · 结构化立项</small>
          <h3>总纲、设定与人物</h3>
        </div>
        <div class="card-actions">
          <button
            class="primary"
            :disabled="props.busy || readOnly || !hasProposal"
            :title="hasProposal ? '一次生成全书总纲、9 个设定与人物（不含卷纲）' : '先扩写并接受创意提案'"
            @click="emit('plan', 'init')"
          >生成总纲与设定</button>
        </div>
      </header>
      <p class="muted">
        {{ hasProposal ? '以创意提案为第一依据，调用一次模型生成总纲 + 9 个设定 + 人物；不写卷纲，不播种单元。' : '需要先接受一份创意提案，结构化立项才有依据。' }}
      </p>
      <ul class="coverage">
        <li v-for="row in settingRows" :key="row.path" :class="{ on: row.exists }">
          <span class="dot" />
          <span class="path">{{ row.path }}</span>
        </li>
      </ul>

      <div v-if="planCandidates.length" class="pending">
        <h4>待处理的立项候选</h4>
        <div v-for="c in planCandidates" :key="c.id" class="plan-candidate">
          <div class="pending-row">
            <span>{{ c.mode === 'volume' ? '卷纲' : '立项' }} · {{ candidateFiles(c) }} 个文件 · {{ new Date(c.createdAt).toLocaleString() }}</span>
            <div>
              <button @click="emit('reject', c)">拒绝</button>
              <button class="primary" @click="emit('accept', c)">接受</button>
            </div>
          </div>
          <PlanCandidateDiff :candidate="c" :files="files" />
        </div>
      </div>
    </article>
  </section>
</template>

<style scoped>
.setup-workspace { display: grid; gap: 16px; }
.view-title { display: flex; justify-content: space-between; gap: 20px; align-items: flex-end; flex-wrap: wrap; }
.view-title h2 { margin: 4px 0 6px; font-family: var(--font-prose); font-size: 28px; }
.view-title p { margin: 0; color: var(--ink-soft); font-size: 13px; max-width: 640px; }
.eyebrow { color: var(--accent) !important; font-size: 11px !important; letter-spacing: .12em; }
.view-actions { display: flex; gap: 6px; flex-wrap: wrap; }
.cover-chip { font-size: 11px; padding: 3px 10px; border-radius: 20px; border: 1px solid var(--line); color: var(--ink-soft); }
.cover-chip.on { border-color: var(--accent); color: var(--accent); }
.setup-card { padding: 18px 20px; display: grid; gap: 12px; }
.setup-card header { display: flex; justify-content: space-between; gap: 12px; align-items: flex-start; flex-wrap: wrap; }
.setup-card h3 { margin: 2px 0 0; font-family: var(--font-prose); font-size: 18px; }
.setup-card small { color: var(--accent); font-size: 11px; }
.card-actions { display: flex; gap: 6px; }
.logline { margin: 0; padding: 10px 14px; border-left: 3px solid var(--accent); background: rgba(15, 107, 92, .06); font-family: var(--font-prose); font-size: 15px; }
.intake, .proposal { margin: 0; white-space: pre-wrap; font: 13px/1.8 var(--font-ui); color: var(--ink); max-height: 420px; overflow: auto; }
.proposal { border-left: 2px solid var(--line); padding-left: 12px; }
.idea-editor textarea { width: 100%; min-height: 320px; padding: 14px; border: 1px solid var(--line); border-radius: 10px; font: 13px/1.7 ui-monospace, monospace; resize: vertical; }
.idea-editor p { margin: 0 0 8px; color: var(--ink-soft); font-size: 12px; }
.direction-row { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
.direction-row input { flex: 1 1 260px; padding: 8px 10px; border-radius: 9px; border: 1px solid var(--line); }
.chip { font-size: 11px; padding: 3px 9px; border-radius: 20px; border: 1px solid var(--line); background: transparent; color: var(--ink-soft); }
.muted { margin: 0; color: var(--ink-soft); font-size: 12px; line-height: 1.7; }
.empty { padding: 22px; text-align: center; color: var(--ink-soft); font-size: 12px; border: 1px dashed var(--line); border-radius: 10px; margin: 0; }
.pending { display: grid; gap: 8px; border-top: 1px solid var(--line); padding-top: 12px; }
.pending h4 { margin: 0; font-size: 12px; color: var(--ink-soft); }
.pending-row { display: flex; justify-content: space-between; gap: 10px; align-items: center; font-size: 12px; }
.pending-row > div { display: flex; gap: 6px; }
.plan-candidate { display: grid; gap: 8px; padding: 10px; border: 1px solid var(--line); border-radius: 10px; }
.coverage { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 4px 12px; }
.coverage li { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--ink-soft); }
.coverage li.on { color: var(--ink); }
.coverage .dot { width: 6px; height: 6px; border-radius: 50%; background: var(--line); flex: none; }
.coverage li.on .dot { background: var(--ok); }
.coverage .path { font-family: ui-monospace, monospace; word-break: break-all; }
html.shell-mobile .view-title { align-items: flex-start; flex-direction: column; }
html.shell-mobile .setup-card { padding: 14px; }

</style>
