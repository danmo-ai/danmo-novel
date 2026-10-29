<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { api } from '@/api/client'
import type { Candidate, UnitInfo } from '@/types'
import OutlineWorkspace from '@/components/book/OutlineWorkspace.vue'
import BibleWorkspace from '@/components/book/BibleWorkspace.vue'
import ReviewWorkspace from '@/components/book/ReviewWorkspace.vue'
import UnitProse from '@/components/book/UnitProse.vue'
import UnitContract from '@/components/book/UnitContract.vue'
import RunConfirmDialog from '@/components/book/RunConfirmDialog.vue'

const route = useRoute()
const store = useAppStore()
const bookId = computed(() => route.params.id as string)
const view = ref<'write' | 'outline' | 'bible' | 'review'>('write')
const selectedUnit = ref<UnitInfo | null>(null)
const editorPath = ref('')
const editorContent = ref('')
const saving = ref(false)
const busy = ref(false)
const modelId = ref('')
const runDrawer = ref(false)
const pollTimer = ref<number | null>(null)
const customInstruction = ref('')
const editingProse = ref(false)
const editingOutline = ref(false)
const isProseUnit = computed(() => Boolean(selectedUnit.value?.hasProse && selectedUnit.value.prosePath === editorPath.value))
const stageLabels: Record<string, string> = { plan: '规划', outline: '细纲', write: '写作', review: '审校', finalize: '定稿', revise: '修订', local_edit: '局部编辑', rewrite_entity: '重写优化' }
// 单元驱动工作流：proposed→细纲 → accepted→正文 → drafted→审校 → reviewed→定稿。
// 逐单元闭环推进，下一单元细纲才能读到最新连续性账本。
const FLOW_STEPS = [
  { key: 'outline', label: '细纲', desc: '单元合同' },
  { key: 'write', label: '正文', desc: '候选稿' },
  { key: 'review', label: '审校', desc: '本地+AI' },
  { key: 'finalize', label: '定稿', desc: '回写账本' },
]
const flowState = computed(() => {
  const u = selectedUnit.value
  if (!u) return { active: -1, done: -1 }
  const idx = { proposed: 0, accepted: 1, drafted: 2, reviewed: 3 }[u.status] ?? 0
  return { active: idx, done: u.status === 'reviewed' ? 3 : idx - 1 }
})
// 单元按卷分组；默认展开选中单元所在卷，其余卷可折叠。
const collapsedVolumes = ref<Set<number>>(new Set())
const volumeGroups = computed(() => {
  const units = store.currentBook?.units || []
  const groups: { volume: number; units: UnitInfo[]; reviewed: number; title: string }[] = []
  for (const u of units) {
    let g = groups.find((x) => x.volume === u.volume)
    if (!g) {
      const vol = store.currentBook?.volumes.find((v) => v.volume === u.volume)
      const title = vol?.raw?.split('\n').find((l) => /^#\s+/.test(l))?.replace(/^#\s+/, '') || ''
      g = { volume: u.volume, units: [], reviewed: 0, title }
      groups.push(g)
    }
    g.units.push(u)
    if (u.status === 'reviewed') g.reviewed++
  }
  return groups.sort((a, b) => a.volume - b.volume)
})
function toggleVolume(v: number) {
  const s = new Set(collapsedVolumes.value)
  if (s.has(v)) s.delete(v)
  else s.add(v)
  collapsedVolumes.value = s
}
function isCollapsed(v: number) {
  return collapsedVolumes.value.has(v)
}
const primaryInfo = computed(() => {
  const p = store.primary
  if (!p) return { label: '继续创作', description: '选择下一步', target: '', stage: '写作' }
  const target = p.unitId || `第 ${p.volume} 卷`
  if (p.kind === 'plan') {
    const approving = Boolean(store.currentBook?.volumes?.length)
    return { label: approving ? '批准卷纲' : '生成卷纲', description: approving ? '确认后写入人物与单元' : '生成本卷总纲与单元索引', target, stage: '规划' }
  }
  if (p.kind === 'outline') return { label: `生成 ${target} 细纲`, description: '第 1 步 · 补全单元写作合同（欲望/阻碍/选择/场面）', target, stage: '细纲' }
  if (p.kind === 'write') return { label: `写 ${target} 正文`, description: '第 2 步 · 按合同生成候选稿；定稿后才开下一单元', target, stage: '写作' }
  if (p.kind === 'review') return { label: `审校 ${target}`, description: '第 3 步 · 本地硬门 + 一次 AI 审校，通过后定稿', target, stage: '审校' }
  return { label: '继续创作', description: '执行下一步', target, stage: stageLabels[p.kind] || '写作' }
})

onMounted(async () => {
  await store.loadLLM().catch(() => {})
  await reload()
})
onUnmounted(() => { if (pollTimer.value) clearInterval(pollTimer.value) })

watch(selectedUnit, () => { editingProse.value = false; editingOutline.value = false; void openSelected() })
watch(view, () => {
  void openSelected()
})

async function reload() {
  await store.loadBook(bookId.value)
  const book = store.currentBook
  if (!book) return
  if (!modelId.value) modelId.value = defaultModelFor('write')
  if ((!selectedUnit.value || !book.units.some((u) => u.unitId === selectedUnit.value?.unitId)) && book.units.length) {
    selectedUnit.value = book.units[0]
  }
  if (!book.units.length) {
    editorPath.value = 'book-bible.md'
    editorContent.value = book.files['book-bible.md'] || ''
  } else if (selectedUnit.value) {
    await openSelected()
  }
}

function defaultModelFor(role: string) {
  const a = store.assignments.find((x) => x.role === role)
  if (a?.modelId) return a.modelId
  return store.models[0]?.id || ''
}

async function openSelected() {
  const u = selectedUnit.value
  if (!u) return
  if (u.hasProse && u.prosePath) {
    editorPath.value = u.prosePath
    editorContent.value = u.proseRaw || (await api.get<any>(`/api/books/${bookId.value}/file?path=${encodeURIComponent(u.prosePath)}`)).content
  } else {
    editorPath.value = u.outlinePath
    editorContent.value = u.outlineRaw || ''
  }
}

async function saveFile(path = editorPath.value, content = editorContent.value) {
  if (!path) return
  saving.value = true
  try {
    await api.put(`/api/books/${bookId.value}/file`, { path, content })
    store.note('已保存')
    await reload()
  } catch (e) { store.fail(e) } finally { saving.value = false }
}

const confirmSpec = ref<Record<string, any> | null>(null)
const confirmTitle = ref('')

function start(kind: string) {
  const book = store.currentBook
  if (!book || busy.value) return
  const spec: any = { kind, bookId: book.summary.id, baseRevision: book.revision }
  if (selectedUnit.value) spec.unitId = selectedUnit.value.unitId
  if (kind === 'outline' && store.primary?.volume) spec.volumeId = `v${String(store.primary.volume).padStart(2, '0')}`
  const target = selectedUnit.value ? `${selectedUnit.value.unitId} ${selectedUnit.value.title || ''}` : `第 ${store.primary?.volume || 1} 卷`
  confirmTitle.value = `${stageLabels[kind] || kind} · ${target.trim()}`
  confirmSpec.value = spec
}

function startRewrite(payload: { targetPath: string; entityKind: string; title: string }) {
  const book = store.currentBook
  if (!book || busy.value) return
  confirmTitle.value = `重写优化 · ${payload.title}`
  confirmSpec.value = {
    kind: 'rewrite_entity', bookId: book.summary.id, baseRevision: book.revision,
    targetPath: payload.targetPath, entityKind: payload.entityKind,
  }
}

async function confirmRun(payload: { instruction: string; knowledgeIds: string[] }) {
  const spec = confirmSpec.value
  confirmSpec.value = null
  if (!spec || busy.value) return
  if (payload.instruction) spec.instruction = payload.instruction
  if (payload.knowledgeIds?.length) spec.knowledgeIds = payload.knowledgeIds
  busy.value = true
  try {
    const run = await store.startRun(spec)
    runDrawer.value = true
    if (pollTimer.value) clearInterval(pollTimer.value)
    pollTimer.value = window.setInterval(async () => {
      try {
        const r = await store.pollRun(run.id)
        if (r.status !== 'running' && r.status !== 'pending') {
          if (pollTimer.value) clearInterval(pollTimer.value)
          pollTimer.value = null
          await store.loadCandidates(bookId.value)
          await store.loadRuns(bookId.value)
        }
      } catch (e) { store.fail(e) }
    }, 1200)
  } catch (e) { store.fail(e) } finally { busy.value = false }
}

async function primaryAction() {
  const p = store.primary
  if (!p) return
  if (p.unitId && store.currentBook) {
    const u = store.currentBook.units.find((x) => x.unitId === p.unitId)
    if (u) selectedUnit.value = u
  }
  await start(p.kind)
}

async function accept(c: Candidate) {
  try {
    await store.acceptCandidate(c.id, c.baseRevision)
    if (c.kind === 'review') view.value = 'review'
    await reload()
    await store.loadRuns(bookId.value)
  } catch (e) { store.fail(e) }
}
async function reject(c: Candidate) {
  await store.rejectCandidate(c.id)
  await store.loadCandidates(bookId.value)
}

async function enableWrite() {
  try {
    await api.post(`/api/books/${bookId.value}/enable-write`)
    await reload()
    store.note('已启用写入')
  } catch (e) { store.fail(e) }
}

async function localEdit(instruction: string) {
  if (!editorContent.value) return
  const spec = {
    kind: 'local_edit', bookId: bookId.value, baseRevision: store.currentBook?.revision, modelId: modelId.value,
    instruction,
    selection: { path: editorPath.value, text: editorContent.value, before: '', after: '' },
  }
  try {
    const run = await store.startRun(spec)
    runDrawer.value = true
    if (pollTimer.value) clearInterval(pollTimer.value)
    pollTimer.value = window.setInterval(async () => {
      const r = await store.pollRun(run.id)
      if (r.status !== 'running') {
        if (pollTimer.value) clearInterval(pollTimer.value)
        await store.loadCandidates(bookId.value)
      }
    }, 1200)
  } catch (e) { store.fail(e) }
}

function chapterText(c: Candidate) {
  return (c.chapters || []).map((ch) => `## 第${ch.chapter}章 ${ch.title || ''}\n\n${ch.body}`).join('\n\n---\n\n')
}
const statusText: Record<string, string> = {
  proposed: '待细纲', accepted: '待写', drafted: '草稿', reviewed: '定稿',
}
const castExpanded = ref(false)
const visibleCast = computed(() => {
  const all = store.currentBook?.cast || []
  return castExpanded.value ? all : all.slice(0, 6)
})
function castName(name: string) {
  // loader 已把姓名与说明分离；这里只做兜底清洗。
  return name.replace(/^#+\s*/, '').trim()
}
function castInitial(name: string) { return castName(name).slice(0, 1) || '?' }
function roleLabel(role?: string) {
  const r = (role || '').toLowerCase()
  if (['protagonist', 'hero', 'main'].includes(r)) return '主角'
  if (['antagonist', 'volume_antagonist', 'villain'].includes(r)) return '卷反派'
  return '常驻'
}
</script>

<template>
  <div v-if="store.currentBook" class="book-page">
    <div class="book-head">
      <div>
        <p class="crumb"><router-link to="/">书架</router-link> / {{ store.currentBook.summary.title }}</p>
        <h1>{{ store.currentBook.summary.title }}</h1>
      </div>
      <div class="book-head-actions">
        <button v-if="store.currentBook.summary.readOnly" @click="enableWrite">启用写入</button>
      </div>
    </div>

    <div class="primary-task panel">
      <div class="primary-task-copy">
        <span class="task-kicker">下一步 · {{ primaryInfo.target }}</span>
        <p>{{ primaryInfo.description }}</p>
      </div>
      <button class="primary" :disabled="busy" @click="primaryAction">{{ busy ? '正在启动…' : primaryInfo.label }}</button>
    </div>

    <div class="tabs" role="tablist" aria-label="创作主导航">
      <button role="tab" :aria-selected="view === 'write'" :class="{ active: view === 'write' }" @click="view = 'write'">写作</button>
      <button role="tab" :aria-selected="view === 'outline'" :class="{ active: view === 'outline' }" @click="view = 'outline'">大纲</button>
      <button role="tab" :aria-selected="view === 'bible'" :class="{ active: view === 'bible' }" @click="view = 'bible'">设定</button>
      <button role="tab" :aria-selected="view === 'review'" :class="{ active: view === 'review' }" @click="view = 'review'">审校</button>
    </div>

    <div class="workspace" :class="`view-${view}`">
      <aside v-if="view === 'write'" class="unit-tree panel">
        <div class="flow-guide">
          <div class="flow-steps">
            <template v-for="(s, i) in FLOW_STEPS" :key="s.key">
              <span class="flow-step" :class="{ active: flowState.active === i, done: flowState.done >= i }" :title="s.desc">{{ s.label }}</span>
              <span v-if="i < FLOW_STEPS.length - 1" class="flow-arrow">→</span>
            </template>
          </div>
          <p class="flow-note">逐单元闭环：一个单元走完细纲→正文→审校→定稿，再开下一个。定稿回写人物状态与伏笔，下一单元细纲才能读到最新事实。</p>
        </div>
        <div class="tree-head"><span>单元</span><span class="tree-count">{{ store.currentBook.units.length }}</span></div>
        <div v-for="g in volumeGroups" :key="g.volume" class="volume-group">
          <button class="volume-head" @click="toggleVolume(g.volume)">
            <span class="volume-caret">{{ isCollapsed(g.volume) ? '▸' : '▾' }}</span>
            <span class="volume-label">第 {{ g.volume }} 卷</span>
            <span class="volume-progress">{{ g.reviewed }}/{{ g.units.length }}</span>
          </button>
          <p v-if="g.title && !isCollapsed(g.volume)" class="volume-title">{{ g.title }}</p>
          <template v-if="!isCollapsed(g.volume)">
            <button v-for="u in g.units" :key="u.unitId"
              class="unit-row" :class="{ active: selectedUnit?.unitId === u.unitId }"
              @click="selectedUnit = u">
              <span class="dot" :data-status="u.status"></span>
              <span class="unit-main">
                <span class="unit-line"><span class="unit-id">{{ u.unitId }}</span><span class="unit-status" :data-status="u.status">{{ statusText[u.status] || u.status }}</span></span>
                <span class="unit-title">{{ u.title || '未完成单元合同' }}</span>
              </span>
            </button>
          </template>
        </div>
        <div v-if="!(store.currentBook.units || []).length" class="tree-empty">还没有单元。先规划一卷。</div>
      </aside>

      <OutlineWorkspace
        v-if="view === 'outline'"
        :saving="saving"
        :busy="busy"
        @open-unit="selectedUnit = $event; view = 'write'"
        @save="saveFile"
        @generate="start"
        @rewrite="startRewrite"
      />
      <BibleWorkspace
        v-else-if="view === 'bible'"
        :saving="saving"
        @save="saveFile"
        @open-unit="selectedUnit = $event; view = 'write'"
        @rewrite="startRewrite"
      />
      <ReviewWorkspace
        v-else-if="view === 'review'"
        :selected-unit="selectedUnit"
        :busy="busy"
        @select="selectedUnit = $event"
        @generate="start"
      />
      <section v-else class="editor-panel panel">
        <template v-if="isProseUnit && selectedUnit">
          <UnitProse v-if="!editingProse" :unit="selectedUnit" :raw="editorContent"
            :can-write="!store.currentBook.summary.readOnly" @edit="editingProse = true">
            <template #contract>
              <details class="inline-contract">
                <summary>单元合同 · 写正文时对照</summary>
                <UnitContract :unit="selectedUnit" compact />
              </details>
            </template>
          </UnitProse>
          <div v-else>
            <div class="editor-toolbar">
              <span class="path">{{ editorPath }}</span>
              <div class="editor-actions">
                <button @click="saveFile(); editingProse = false" :disabled="saving">{{ saving ? '保存中' : '保存' }}</button>
                <button @click="editingProse = false">取消</button>
              </div>
            </div>
            <textarea v-model="editorContent" class="prose-editor" spellcheck="false"></textarea>
          </div>
        </template>
        <template v-else-if="selectedUnit">
          <!-- 无正文单元：写作页直接生产/查看细纲 -->
          <div class="unit-work">
            <header class="unit-work-head">
              <div>
                <small>{{ selectedUnit.unitId }} · {{ statusText[selectedUnit.status] || selectedUnit.status }}<template v-if="selectedUnit.chapterFrom"> · 第 {{ selectedUnit.chapterFrom }}–{{ selectedUnit.chapterTo }} 章</template></small>
                <h3>{{ selectedUnit.title || '未完成单元合同' }}</h3>
              </div>
              <div class="unit-work-actions">
                <button v-if="!editingOutline" :disabled="busy" @click="start('outline')">生成 / 重写细纲</button>
                <button v-if="!editingOutline" @click="editingOutline = true">编辑细纲</button>
                <button v-if="!editingOutline && selectedUnit.status === 'accepted'" class="primary" :disabled="busy" @click="start('write')">写正文</button>
              </div>
            </header>
            <template v-if="!editingOutline">
              <UnitContract :unit="selectedUnit" />
              <details class="raw-outline">
                <summary>查看细纲源文件</summary>
                <pre>{{ editorContent }}</pre>
              </details>
            </template>
            <div v-else class="outline-edit">
              <div class="editor-toolbar">
                <span class="path">{{ selectedUnit.outlinePath }}</span>
                <div class="editor-actions">
                  <button @click="saveFile(); editingOutline = false" :disabled="saving">{{ saving ? '保存中' : '保存' }}</button>
                  <button @click="editingOutline = false">取消</button>
                </div>
              </div>
              <textarea v-model="editorContent" class="prose-editor" spellcheck="false"></textarea>
            </div>
          </div>
        </template>
        <template v-else>
          <div class="editor-toolbar">
            <span class="path">{{ editorPath }}</span>
            <div class="editor-actions">
              <button @click="saveFile()" :disabled="saving">{{ saving ? '保存中' : '保存' }}</button>
            </div>
          </div>
          <textarea v-model="editorContent" class="prose-editor" spellcheck="false"></textarea>
        </template>
      </section>

      <aside v-if="view === 'write'" class="context-panel">
        <section class="side-card panel">
          <h4>下一步</h4>
          <div class="next-step">
            <span class="next-kind">{{ stageLabels[store.primary?.kind || ''] || store.primary?.kind }}</span>
            <strong>{{ store.primary?.unitId || `第 ${store.primary?.volume} 卷` }}</strong>
          </div>
          <p class="side-note">细纲、人物、连续性事实按阶段自动挂载，作者底牌不进正文。</p>
        </section>
        <section class="side-card panel">
          <h4>本卷人物 <span class="side-count">{{ store.currentBook.cast?.length || 0 }}</span></h4>
          <div class="cast-list">
            <button class="cast-item" v-for="c in visibleCast" :key="c.stem" @click="view = 'bible'">
              <span class="cast-avatar">{{ castInitial(c.name) }}</span>
              <span class="cast-info">
                <strong>{{ castName(c.name) }}<i class="role-badge" :data-role="c.role">{{ roleLabel(c.role) }}</i></strong>
                <small v-if="c.tagline">{{ c.tagline }}</small>
              </span>
            </button>
          </div>
          <button v-if="(store.currentBook.cast?.length || 0) > 6" class="cast-more" @click="castExpanded = !castExpanded">
            {{ castExpanded ? '收起' : `查看全部 ${store.currentBook.cast?.length} 人` }}
          </button>
        </section>
        <section class="side-card panel side-actions">
          <button :disabled="busy" @click="start('outline')">写细纲</button>
          <button :disabled="busy" @click="start('plan')">规划</button>
        </section>
      </aside>
    </div>

    <section class="candidates">
      <h2>待审候选</h2>
      <p v-if="!store.candidates.length" class="muted">生成结果会先到这里，接受后才进入正文。</p>
      <article v-for="c in store.candidates" :key="c.id" class="candidate panel">
        <header>
          <div>
            <strong>{{ stageLabels[c.kind] || c.kind }} · {{ c.unitId || c.volumeId || c.targetPath }}</strong>
            <span class="muted"> · {{ new Date(c.createdAt).toLocaleString() }}</span>
          </div>
          <div>
            <button @click="reject(c)">拒绝</button>
            <button class="primary" @click="accept(c)">接受</button>
          </div>
        </header>
        <pre v-if="c.chapters" class="candidate-prose">{{ chapterText(c) }}</pre>
        <pre v-else-if="c.unitOutlines" class="candidate-prose">{{ JSON.stringify(c.unitOutlines, null, 2) }}</pre>
        <pre v-else-if="c.replacement" class="candidate-prose">{{ c.replacement }}</pre>
        <pre v-else-if="c.planMarkdown" class="candidate-prose">{{ c.planMarkdown.volume_outline || c.planMarkdown.book_outline }}</pre>
        <div v-if="c.findings?.length" class="candidate-findings">
          <div v-for="f in c.findings.slice(0, 6)" :key="f.id" class="finding" :class="f.severity">
            {{ f.title }} <span v-if="f.evidence">· {{ f.evidence }}</span>
          </div>
        </div>
      </article>
    </section>

    <RunConfirmDialog v-if="confirmSpec" :spec="confirmSpec" :title="confirmTitle"
      :model-label="defaultModelFor(confirmSpec.kind === 'plan' || confirmSpec.kind === 'rewrite_entity' ? 'plan' : confirmSpec.kind)"
      @confirm="confirmRun" @cancel="confirmSpec = null" />

    <div v-if="runDrawer" class="run-drawer panel">
      <header><strong>运行中心</strong><button @click="runDrawer = false">×</button></header>
      <div v-if="store.activeRun">
        <p>{{ store.activeRun.kind }} · {{ store.activeRun.status }}</p>
        <p class="muted">模型 {{ store.activeRun.modelId }} · 调用 {{ store.activeRun.callsUsed }}/{{ store.activeRun.policy?.maxCalls || '计算中' }}</p>
        <div v-for="c in store.activeRun.calls" :key="c.index" class="call-row">
          #{{ c.index }} {{ c.step }} · in {{ c.promptTokens }} / out {{ c.completionTokens }} · {{ c.durationMs }}ms
        </div>
        <p v-if="store.activeRun.error" class="error-text">{{ store.activeRun.error }}</p>
      </div>
      <div v-else class="muted">暂无运行</div>
    </div>
  </div>
</template>

<style scoped>
/* ---- 页头 ---- */
.book-head { display: flex; justify-content: space-between; align-items: flex-end; gap: 16px; padding: 4px 0 18px; }
.crumb { color: var(--ink-soft); font-size: 12px; margin: 0; }
h1 { font-family: var(--font-prose); margin: 6px 0 0; font-size: 30px; line-height: 1.25; }
.book-head-actions { display: flex; gap: 8px; align-items: center; flex-shrink: 0; }

/* ---- 主任务条 ---- */
.primary-task { display: flex; justify-content: space-between; align-items: center; gap: 20px; padding: 14px 16px 14px 20px; margin-bottom: 16px; border-color: rgba(15,107,92,.2); background: linear-gradient(120deg, rgba(15,107,92,.06), rgba(255,255,255,.75)); }
.primary-task-copy { min-width: 0; display: grid; gap: 3px; }
.task-kicker { color: var(--accent); font-size: 12px; font-weight: 700; }
.primary-task-copy p { margin: 0; color: var(--ink-soft); font-size: 12px; }
.primary-task .primary { flex-shrink: 0; padding: 10px 18px; }

/* ---- 主 Tab ---- */
.tabs { display: inline-flex; gap: 2px; margin-bottom: 18px; padding: 4px; border-radius: 12px; background: rgba(20,35,31,.05); }
.tabs button { border: none; background: transparent; color: var(--ink-soft); padding: 8px 22px; border-radius: 9px; font-weight: 600; }
.tabs button:hover:not(:disabled) { background: rgba(255,255,255,.7); color: var(--ink); }
.tabs button.active { background: white; color: var(--accent); box-shadow: 0 2px 8px rgba(20,35,31,.1); }

/* ---- 三栏工作区 ---- */
.workspace { display: grid; grid-template-columns: 236px minmax(0, 1fr) 252px; gap: 18px; align-items: start; }
.workspace:not(.view-write) { grid-template-columns: minmax(0, 1fr); }

/* 左：单元树 */
.unit-tree { padding: 10px; max-height: 74vh; overflow-y: auto; position: sticky; top: 12px; }
.tree-head { display: flex; justify-content: space-between; align-items: center; font-size: 12px; font-weight: 600; color: var(--ink-soft); padding: 6px 10px 10px; }
.tree-count { font-size: 11px; background: rgba(20,35,31,.06); padding: 1px 8px; border-radius: 20px; }
.unit-row { display: flex; align-items: flex-start; gap: 8px; width: 100%; border: none; background: transparent; padding: 9px 10px; border-radius: 10px; text-align: left; margin-bottom: 2px; }
.unit-row:hover:not(:disabled) { background: rgba(15,107,92,.06); color: inherit; }
.unit-row.active { background: rgba(15,107,92,.1); }
.unit-row.active .unit-id { color: var(--accent); }
.unit-main { display: grid; gap: 3px; min-width: 0; flex: 1; }
.unit-line { display: flex; justify-content: space-between; align-items: center; gap: 6px; }
.unit-id { font-size: 12px; font-weight: 600; white-space: nowrap; }
.unit-title { font-size: 11px; color: var(--ink-soft); line-height: 1.4; overflow: hidden; text-overflow: ellipsis; display: -webkit-box; -webkit-line-clamp: 1; -webkit-box-orient: vertical; }
.unit-status { font-size: 10px; padding: 1px 7px; border-radius: 20px; background: rgba(20,35,31,.06); color: var(--ink-soft); white-space: nowrap; flex-shrink: 0; }
.unit-status[data-status="reviewed"] { background: rgba(31,122,77,.12); color: var(--ok); }
.unit-status[data-status="drafted"] { background: rgba(161,92,18,.12); color: var(--warn); }
.unit-status[data-status="accepted"] { background: rgba(15,107,92,.12); color: var(--accent); }
.dot { width: 7px; height: 7px; border-radius: 50%; background: #9aa; flex-shrink: 0; margin-top: 5px; }
.dot[data-status="reviewed"] { background: var(--ok); }
.dot[data-status="drafted"] { background: var(--warn); }
.dot[data-status="accepted"] { background: var(--accent); }
.tree-empty { color: var(--ink-soft); font-size: 12px; padding: 16px 10px; }
/* 卷分组 */
.volume-group { margin-bottom: 6px; }
.volume-head { display: flex; align-items: center; gap: 6px; width: 100%; border: none; background: transparent; padding: 8px 10px 6px; border-radius: 8px; text-align: left; }
.volume-head:hover:not(:disabled) { background: rgba(15,107,92,.06); }
.volume-caret { color: var(--ink-soft); font-size: 11px; width: 12px; }
.volume-label { font-size: 12px; font-weight: 700; color: var(--ink); }
.volume-progress { margin-left: auto; font-size: 10px; color: var(--ok); background: rgba(31,122,77,.1); padding: 1px 8px; border-radius: 20px; }
.volume-title { margin: 0 10px 6px 28px; font-size: 10px; color: var(--ink-soft); line-height: 1.5; overflow: hidden; text-overflow: ellipsis; display: -webkit-box; -webkit-line-clamp: 1; -webkit-box-orient: vertical; }
.volume-group .unit-row { margin-left: 6px; }
/* 单元工作流引导 */
.flow-guide { padding: 10px 10px 14px; border-bottom: 1px solid var(--line); margin-bottom: 10px; }
.flow-steps { display: flex; align-items: center; gap: 3px; flex-wrap: wrap; }
.flow-step { font-size: 10px; padding: 3px 8px; border-radius: 20px; background: rgba(20,35,31,.05); color: var(--ink-soft); white-space: nowrap; }
.flow-step.active { background: var(--accent); color: white; font-weight: 700; }
.flow-step.done { background: rgba(31,122,77,.15); color: var(--ok); }
.flow-arrow { color: var(--ink-soft); font-size: 10px; opacity: .5; }
.flow-note { margin: 10px 0 0; font-size: 10px; color: var(--ink-soft); line-height: 1.7; }

/* 中：正文/编辑器 */
.editor-panel { min-height: 60vh; overflow: hidden; }
.editor-toolbar { display: flex; justify-content: space-between; align-items: center; gap: 10px; padding: 12px 16px; border-bottom: 1px solid var(--line); }
.path { font-size: 12px; color: var(--ink-soft); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.editor-actions { display: flex; gap: 8px; flex-wrap: wrap; flex-shrink: 0; }
.prose-editor {
  width: 100%; min-height: 64vh; border: none; resize: vertical; padding: 24px 28px;
  font-family: var(--font-prose); font-size: 17px; line-height: 1.9; color: var(--ink);
  background: transparent; outline: none;
}

/* 右：上下文侧栏 */
.context-panel { display: grid; gap: 14px; position: sticky; top: 12px; }
.side-card { padding: 16px; }
.side-card h4 { margin: 0 0 12px; font-size: 12px; font-weight: 700; color: var(--ink-soft); letter-spacing: .04em; display: flex; align-items: center; gap: 6px; }
.side-count { font-size: 10px; background: rgba(20,35,31,.06); padding: 1px 7px; border-radius: 20px; font-weight: 500; }
.next-step { display: flex; align-items: center; gap: 10px; }
.next-kind { font-size: 11px; padding: 3px 10px; border-radius: 8px; background: rgba(15,107,92,.1); color: var(--accent); font-weight: 600; flex-shrink: 0; }
.next-step strong { font-size: 14px; font-family: var(--font-prose); }
.side-note { margin: 12px 0 0; color: var(--ink-soft); font-size: 11px; line-height: 1.7; }
.cast-list { display: grid; gap: 2px; }
.cast-item { display: flex; align-items: center; gap: 10px; width: 100%; border: none; background: transparent; padding: 7px 6px; border-radius: 10px; text-align: left; }
.cast-item:hover:not(:disabled) { background: rgba(15,107,92,.06); }
.cast-avatar { width: 30px; height: 30px; border-radius: 50%; background: linear-gradient(135deg, rgba(15,107,92,.18), rgba(15,107,92,.06)); color: var(--accent); display: grid; place-items: center; font-size: 13px; font-weight: 700; font-family: var(--font-prose); flex-shrink: 0; }
.cast-info { display: grid; gap: 2px; min-width: 0; }
.cast-info strong { font-size: 13px; display: flex; align-items: center; gap: 6px; }
.role-badge { font-style: normal; font-size: 9px; padding: 1px 6px; border-radius: 6px; background: rgba(20,35,31,.06); color: var(--ink-soft); flex-shrink: 0; font-weight: 500; }
.role-badge[data-role="protagonist"] { background: rgba(15,107,92,.12); color: var(--accent); }
.role-badge[data-role="volume_antagonist"] { background: rgba(163,59,50,.1); color: var(--bad); }
.cast-info small { font-size: 10px; color: var(--ink-soft); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cast-more { margin-top: 10px; width: 100%; border: none; background: transparent; color: var(--accent); font-size: 12px; padding: 6px; }
.cast-more:hover:not(:disabled) { background: rgba(15,107,92,.06); }
.side-actions { display: grid; gap: 8px; }
.side-actions button { width: 100%; padding: 10px; }

/* 无正文单元：细纲工作区 */
.unit-work { padding: 24px 26px; display: grid; gap: 20px; }
.unit-work-head { display: flex; justify-content: space-between; align-items: flex-end; gap: 16px; }
.unit-work-head small { color: var(--accent); font-size: 11px; font-weight: 600; }
.unit-work-head h3 { margin: 6px 0 0; font-family: var(--font-prose); font-size: 24px; }
.unit-work-actions { display: flex; gap: 8px; flex-shrink: 0; flex-wrap: wrap; }
.raw-outline { border-top: 1px solid var(--line); padding-top: 4px; }
.raw-outline summary { padding: 10px 0; cursor: pointer; color: var(--ink-soft); font-size: 12px; }
.raw-outline pre { white-space: pre-wrap; font: 12px/1.7 ui-monospace, monospace; color: var(--ink-soft); background: rgba(255,255,255,.5); padding: 14px; border-radius: 10px; max-height: 320px; overflow: auto; }
.inline-contract { border-bottom: 1px solid var(--line); padding-bottom: 16px; }
.inline-contract summary { cursor: pointer; color: var(--accent); font-size: 12px; font-weight: 600; padding: 4px 0; }
.inline-contract[open] summary { margin-bottom: 14px; }

/* 待审候选 */
.candidates { margin-top: 28px; }
.candidates h2 { font-size: 17px; font-family: var(--font-prose); margin: 0 0 10px; }
.muted { color: var(--ink-soft); font-size: 12px; }
.candidate { padding: 18px; margin-bottom: 14px; }
.candidate header { display: flex; justify-content: space-between; align-items: center; gap: 10px; margin-bottom: 12px; }
.candidate header > div:last-child { display: flex; gap: 8px; flex-shrink: 0; }
.candidate-prose { white-space: pre-wrap; font-family: var(--font-prose); font-size: 14px; line-height: 1.9; max-height: 300px; overflow: auto; background: rgba(255,255,255,.5); padding: 16px; border-radius: 12px; border: 1px solid var(--line); margin: 0; }
.finding { padding: 8px 12px; margin: 8px 0 0; border-radius: 10px; background: rgba(161,92,18,.08); font-size: 12px; }
.finding.blocking { background: rgba(163,59,50,.1); color: var(--bad); }

/* 运行抽屉 */
.run-drawer { position: fixed; right: 24px; bottom: 24px; width: min(400px, 92vw); padding: 18px; z-index: 30; }
.run-drawer header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.run-drawer p { margin: 6px 0; font-size: 13px; }
.call-row { font-size: 12px; padding: 8px 0; border-top: 1px solid var(--line); color: var(--ink-soft); }
.error-text { color: var(--bad); font-size: 12px; }

@media (max-width: 1180px) {
  .workspace.view-write { grid-template-columns: 210px minmax(0, 1fr); }
  .context-panel { grid-column: 1 / -1; grid-template-columns: repeat(3, minmax(0, 1fr)); position: static; }
}
@media (max-width: 720px) {
  .workspace { grid-template-columns: minmax(0, 1fr); }
  .book-head { flex-direction: column; align-items: flex-start; gap: 10px; }
  .unit-tree { max-height: 240px; position: static; }
  .context-panel { grid-template-columns: minmax(0, 1fr); }
  .editor-panel { min-height: 0; }
  .prose-editor { min-height: 48vh; padding: 18px 16px; font-size: 16px; }
  .editor-toolbar { align-items: flex-start; flex-direction: column; }
  .candidate header { flex-direction: column; align-items: flex-start; }
  .run-drawer { right: 16px; bottom: 16px; }
  .primary-task { align-items: stretch; flex-direction: column; gap: 12px; }
  .primary-task .primary { width: 100%; }
}
</style>
