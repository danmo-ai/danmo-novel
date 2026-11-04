<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useAppStore } from '@/stores/app'
import { api } from '@/api/client'
import RelationGraph from '@/components/book/RelationGraph.vue'
import type { IdentitySegment } from '@/types'

const props = defineProps<{ saving: boolean }>()
const emit = defineEmits<{ save: [path: string, content: string]; openUnit: [unit: any]; rewrite: [spec: { targetPath: string; entityKind: string; title: string }] }>()
const store = useAppStore()
const selectedPath = ref('book-bible.md')
const selectedCast = ref('')
const editing = ref(false)
const draft = ref('')
const showSecrets = ref(false)
const navOpen = ref(false)
const activeGroup = ref('总纲派生')
const specialView = ref<'relations' | null>(null)
const book = computed(() => store.currentBook)

type Doc = { path: string; label: string; group: string; description: string; secret?: boolean; usage: string }
const definitions: Doc[] = [
  { path: 'book-bible.md', label: '作者意图与读者承诺', group: '总纲派生', description: '读者承诺、主角目标、故事引擎；重写携带总纲作依据', usage: '立项 · 重写带总纲' },
  { path: 'canon/world.md', label: '世界观与力量规则', group: '总纲派生', description: '力量/代价、势力地图、禁忌；重写携带总纲', usage: '立项 · 重写带总纲' },
  { path: 'canon/goldfinger.md', label: '金手指与资源', group: '总纲派生', description: '能力、代价、上限；重写携带总纲', usage: '立项 · 重写带总纲' },
  { path: 'canon/foreshadow-schedule.md', label: '伏笔登记表', group: '总纲派生', description: '伏笔的埋与收；总纲定卷、卷纲定单元、细纲定章。信息类伏笔即原揭示计划', usage: '立项 · 卷纲 · 细纲 · 正文 · 审校 · 重写带总纲' },
  { path: 'canon/platform-positioning.md', label: '平台与篇幅定位', group: '生产约束', description: '平台、目标读者、更新与开篇承诺', usage: '立项 · 可独立优化' },
  { path: 'canon/writing-rules.md', label: '写作规则', group: '生产约束', description: '单元与单章生产约束', usage: '正文 · 审校 · 可独立优化' },
  { path: 'canon/style-fingerprint.md', label: '风格指纹', group: '生产约束', description: 'POV、句段、对话、感官与反模式', usage: '正文 · 局部编辑 · 审校 · 可独立优化' },
  { path: 'canon/locked-terms.yaml', label: '锁词与合规红线', group: '作者底牌', description: '按卷解锁的真名/真相词与永久禁词', usage: '正文约束 · 定稿扫描 · 仅手工' },
  { path: 'canon/author-lore.md', label: '作者底牌与终局真相', group: '作者底牌', description: '未来反转、真实规则、角色秘密；写正文禁止加载', secret: true, usage: '立项 · 重写带总纲（正文/审校禁读）' },
  { path: 'continuity/state.yaml', label: '连续性状态账本', group: '连续性账本', description: '按单元段：位置、伤势、资源、目标、知情范围与伏笔状态；定稿时自动追加', usage: '正文 · 审校 · 细纲（应用自动读取）' },
  { path: 'novel-state.yaml', label: '书籍运行状态', group: '连续性账本', description: '当前阶段、题材、时间系统与已提交章节游标', usage: '流程控制 · 定稿回写' },
]
const docs = computed(() => definitions.map((d) => ({
  ...d,
  raw: d.path.includes('#open-loops') ? openLoops(book.value?.files[d.path.split('#')[0]] || '')
    : d.path.includes('#relations') ? relations(book.value?.files[d.path.split('#')[0]] || '')
      : book.value?.files[d.path] || '',
  exists: Boolean(book.value?.files[d.path.split('#')[0]]),
})))
type RuntimeDoc = Doc & { raw: string; exists: boolean }
const dynamicDocs = computed<RuntimeDoc[]>(() => [...summaries.value, ...commits.value].map((path) => ({
  path,
  label: path.includes('/summaries/') ? `卷摘要 · ${path.split('/').pop()?.replace('.md', '')}` : `定稿账本 · ${path.split('/').pop()?.replace('.md', '')}`,
  group: '连续性账本',
  description: path.includes('/summaries/') ? '逐章事件、状态变化、伏笔与钩子；续写只读上一章摘要' : '单元定稿执行日志：门禁、字数、锁词、修订；不默认给模型',
  usage: path.includes('/summaries/') ? '正文 · 审校 · 定稿回写' : '回溯排查（不进上下文）',
  raw: book.value?.files[path] || '', exists: true,
})))
const allDocs = computed(() => [...docs.value, ...dynamicDocs.value])
const groups = ['总纲派生', '生产约束', '人物与关系', '作者底牌', '连续性账本']
const summaries = computed(() => Object.keys(book.value?.files || {}).filter((p) => /^continuity\/summaries\/[^/]+\.md$/.test(p)).sort())
const commits = computed(() => Object.keys(book.value?.files || {}).filter((p) => /^continuity\/commits\/[^/]+\.md$/.test(p)).sort())
const currentDoc = computed(() => allDocs.value.find((d) => d.path === selectedPath.value) || docs.value[0])
const currentRaw = computed(() => selectedCast.value ? book.value?.cast.find((c) => c.stem === selectedCast.value)?.raw || '' : currentDoc.value?.raw || '')
const currentLabel = computed(() => specialView.value === 'relations' ? '人物关系图' : selectedCast.value ? currentCast.value?.name : currentDoc.value?.label)
function roleLabel(role?: string) {
  const r = (role || '').toLowerCase()
  if (['protagonist', 'hero', 'main'].includes(r)) return '主角'
  if (['antagonist', 'volume_antagonist', 'villain'].includes(r)) return '卷反派'
  return '常驻'
}
const currentFilePath = computed(() => selectedCast.value ? `canon/cast/${selectedCast.value}.md` : currentDoc.value?.path || '')
const currentCast = computed(() => book.value?.cast.find((c) => c.stem === selectedCast.value))
// 人物身份时间线：canon/identity-timeline.yaml 按 stem 过滤。
const castTimeline = computed(() => {
  if (!selectedCast.value) return []
  const segs = (book.value as any)?.identitySegments || []
  return segs.filter((s: any) => s.stem === selectedCast.value)
})
const currentUnitId = computed(() => book.value?.summary ? '' : '')
const missingCount = computed(() => docs.value.filter((d) => !d.exists).length)

watch(currentDoc, () => { if (currentDoc.value?.secret) showSecrets.value = false })
function openLoops(raw: string) {
  const match = raw.match(/## Open loops[\s\S]*?(?=\n## |$)/)
  return match?.[0] || '## Open loops\n\n尚未登记伏笔。'
}
function relations(raw: string) {
  const match = raw.match(/### Cast snapshot[\s\S]*?(?=\n### |\n## |$)/)
  return (match?.[0] || '### Cast snapshot\n\n尚无人物状态。') + '\n\n人物当前状态由定稿接受按 state_deltas 回写；单元剧情时钟见「人物与关系 → 单元剧情时钟」。'
}
function chooseDoc(path: string) { selectedPath.value = path; selectedCast.value = ''; specialView.value = null; editing.value = false; navOpen.value = false }
function chooseCast(stem: string) { selectedCast.value = stem; specialView.value = null; editing.value = false; navOpen.value = false }
function chooseGroup(group: string) {
  activeGroup.value = group
  specialView.value = null
  selectedCast.value = ''
  editing.value = false
  if (group === '人物与关系') {
    chooseSpecial('relations')
    activeGroup.value = group
  } else {
    const first = docs.value.find((d) => d.group === group)
    if (first) selectedPath.value = first.path
  }
}
function chooseSpecial(view: 'relations') { specialView.value = view; selectedCast.value = ''; editing.value = false; navOpen.value = false }
// ---- 结构化渲染：把 Markdown / YAML 实体渲染成卡片、表格、列表，而非原始源码 ----

type MDBlock =
  | { kind: 'heading'; level: number; text: string }
  | { kind: 'para'; text: string }
  | { kind: 'bullets'; items: string[] }
  | { kind: 'table'; headers: string[]; rows: string[][] }
  | { kind: 'quote'; text: string }

function parseMarkdown(raw: string): MDBlock[] {
  const blocks: MDBlock[] = []
  const lines = raw.split('\n')
  let bullets: string[] = []
  let para: string[] = []
  const flushPara = () => { if (para.length) { blocks.push({ kind: 'para', text: para.join(' ') }); para = [] } }
  const flushBullets = () => { if (bullets.length) { blocks.push({ kind: 'bullets', items: bullets }); bullets = [] } }
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    const t = line.trim()
    if (/^#{1,4}\s/.test(t)) {
      flushPara(); flushBullets()
      blocks.push({ kind: 'heading', level: t.match(/^#+/)![0].length, text: cleanInline(t.replace(/^#+\s*/, '')) })
      continue
    }
    if (t.startsWith('|')) {
      flushPara(); flushBullets()
      const tableLines: string[] = []
      while (i < lines.length && lines[i].trim().startsWith('|')) { tableLines.push(lines[i].trim()); i++ }
      i--
      const rows = tableLines
        .filter((l) => !/^\|?\s*:?-{2,}/.test(l))
        .map((l) => l.replace(/^\||\|$/g, '').split('|').map((c) => cleanInline(c.trim())))
      if (rows.length > 1) blocks.push({ kind: 'table', headers: rows[0], rows: rows.slice(1) })
      continue
    }
    if (/^[-*]\s+/.test(t)) {
      flushPara()
      bullets.push(cleanInline(t.replace(/^[-*]\s+/, '')))
      continue
    }
    if (t.startsWith('>')) {
      flushPara(); flushBullets()
      blocks.push({ kind: 'quote', text: cleanInline(t.replace(/^>\s*/, '')) })
      continue
    }
    if (!t) { flushPara(); flushBullets(); continue }
    para.push(cleanInline(t))
  }
  flushPara(); flushBullets()
  return blocks
}

function cleanInline(s: string) {
  return s.replace(/\*\*([^*]+)\*\*/g, '$1').replace(/`([^`]+)`/g, '$1').trim()
}

type StateField = { key: string; label: string; value: string }
type GateField = { key: string; value: string }

const STATE_LABELS: Record<string, string> = {
  title: '书名', genre: '题材', subgenre: '子题材', stage: '当前阶段',
  last_committed_ch: '已定稿章', next_action: '下一动作', active_unit: '活动单元',
  time_system: '时间系统', qc_profile: '质检档案', continuation_mode: '续写模式',
}
const STAGE_TEXT: Record<string, string> = {
  planning: '立项中', outlining: '细纲中', units: '单元生产', writing: '写作中', done: '完结',
}

function parseState(raw: string): { fields: StateField[]; gates: GateField[]; blockers: string[] } {
  const fields: StateField[] = []
  const gates: GateField[] = []
  const blockers: string[] = []
  let inGates = false
  for (const line of raw.split('\n')) {
    const m = line.match(/^([a-z_]+):\s*(.*)$/)
    if (m) {
      inGates = m[1] === 'gates'
      if (m[1] === 'gates' || m[1] === 'blockers') continue
      const label = STATE_LABELS[m[1]]
      if (!label) continue
      let v = m[2].replace(/^['"]|['"]$/g, '').trim()
      if (m[1] === 'stage') v = STAGE_TEXT[v] || v
      if (m[1] === 'continuation_mode') v = v === 'true' ? '开' : '关'
      fields.push({ key: m[1], label, value: v || '—' })
      continue
    }
    const g = line.match(/^\s{2}([a-z_]+):\s*(.+)$/)
    if (g && inGates) gates.push({ key: g[1], value: g[2].trim() })
    const b = line.match(/^\s*-\s*(.+)$/)
    if (b) blockers.push(b[1])
  }
  return { fields, gates, blockers }
}

type LockedTerm = { term: string; until: string }
function parseLockedTerms(raw: string): { locked: LockedTerm[]; compliance: string[]; aliases: { alias: string; real: string }[] } {
  const locked: LockedTerm[] = []
  const compliance: string[] = []
  const aliases: { alias: string; real: string }[] = []
  let section = ''
  for (const line of raw.split('\n')) {
    if (/^locked_until:/.test(line)) { section = 'locked'; continue }
    if (/^compliance:/.test(line)) { section = 'compliance'; continue }
    if (/^aliases:/.test(line)) { section = 'aliases'; continue }
    if (section === 'locked') {
      const m = line.match(/^\s{2}([^:]+):\s*(.+)$/)
      if (m) locked.push({ term: cleanInline(m[1].trim()), until: m[2].trim() })
    } else if (section === 'compliance') {
      const m = line.match(/^\s*-\s*(.+)$/)
      if (m) compliance.push(cleanInline(m[1]))
    } else if (section === 'aliases') {
      const m = line.match(/^\s{2}([^:]+):\s*(.+)$/)
      if (m) aliases.push({ alias: cleanInline(m[1].trim()), real: m[2].trim() })
    }
  }
  return { locked, compliance, aliases }
}

type RenderMode = 'state' | 'locked' | 'markdown'
const renderMode = computed<RenderMode>(() => {
  if (selectedCast.value) return 'markdown'
  const p = currentDoc.value?.path.split('#')[0] || ''
  if (p === 'novel-state.yaml') return 'state'
  if (p === 'canon/locked-terms.yaml') return 'locked'
  return 'markdown'
})
const mdBlocks = computed(() => {
  let blocks = parseMarkdown(currentRaw.value)
  if (selectedCast.value) {
    // 人物卡：H1 与卡片头姓名重复，去掉；status/role/tagline 元字段已在头部，正文隐藏。
    let skippedTitle = false
    blocks = blocks.filter((b) => {
      if (!skippedTitle && b.kind === 'heading' && b.level === 1 && b.text === currentCast.value?.name) {
        skippedTitle = true
        return false
      }
      if (b.kind === 'para' && /^`?(status|role|tagline)`?\s*[:：]/.test(b.text)) return false
      return true
    })
  }
  return blocks
})
const stateView = computed(() => parseState(book.value?.stateRaw || book.value?.files['novel-state.yaml'] || ''))
const lockedView = computed(() => parseLockedTerms(currentRaw.value))
function gateIcon(v: string) {
  return v === 'pass' ? '✓ 通过' : v === 'fail' ? '✗ 未过' : '○ 未知'
}

function startEdit() { draft.value = currentRaw.value; editing.value = true }

const promoting = ref(false)
const identityEditing = ref<{ action: 'append' | 'update' | 'close'; index: number; segment: IdentitySegment } | null>(null)
const identitySaving = ref(false)
function startIdentityEdit(index = -1) {
  if (!selectedCast.value) return
  const current = index >= 0 ? book.value?.identitySegments?.[index] : null
  identityEditing.value = {
    action: index >= 0 ? 'update' : 'append', index,
    segment: current ? { ...current } : { stem: selectedCast.value, roleTitle: '', org: '', fromUnit: book.value?.units[0]?.unitId || 'v01-U1', toUnit: '', note: '' },
  }
}
async function saveIdentity() {
  if (!book.value || !identityEditing.value || identitySaving.value) return
  identitySaving.value = true
  try {
    await api.post(`/api/books/${book.value.summary.id}/identity-segments`, { ...identityEditing.value, baseRevision: book.value.revision })
    await store.loadBook(book.value.summary.id)
    identityEditing.value = null
    store.note('职务身份已更新；后续写作与审校将使用生效段')
  } catch (e) { store.fail(e) } finally { identitySaving.value = false }
}
async function closeIdentity(index: number) {
  if (!book.value) return
  const segment = book.value.identitySegments?.[index]
  if (!segment) return
  identityEditing.value = { action: 'close', index, segment: { ...segment } }
}
async function promoteCast() {
  if (!selectedCast.value || !book.value) return
  if (!book.value.identitySegments?.some((s) => s.stem === selectedCast.value)) {
    store.note('请先在下方登记该人物的初始职务身份，再晋升 canon')
    startIdentityEdit()
    return
  }
  promoting.value = true
  try {
    await api.post(`/api/books/${book.value.summary.id}/cast/${selectedCast.value}/promote`, { initialIdentity: book.value.identitySegments.find((s) => s.stem === selectedCast.value), baseRevision: book.value.revision })
    await store.loadBook(book.value.summary.id)
    store.note(`${currentCast.value?.name} 已晋升 canon`)
  } catch (e) { store.fail(e) } finally { promoting.value = false }
}

const OPTIMIZE_BLOCKLIST = new Set([
  'continuity/state.yaml',
  'novel-state.yaml',
  'canon/locked-terms.yaml',
])
const canOptimize = computed(() => {
  if (selectedCast.value) return true
  const path = currentFilePath.value
  if (!path || path.includes('#') || OPTIMIZE_BLOCKLIST.has(path)) return false
  if (path.startsWith('continuity/')) return false
  return true
})
const optimizeLabel = computed(() => selectedCast.value ? '重写人物卡' : '重写设定')
const optimizeHint = computed(() => {
  if (selectedCast.value) return '携带总纲/卷纲/伏笔登记表/公开状态作依据；接受仅写本卡'
  const p = currentFilePath.value
  if (p === 'canon/foreshadow-schedule.md') return '携带总纲/卷纲/公开账本；正文按章注入本章该埋该收的条目'
  if (p === 'canon/world.md' || p === 'canon/goldfinger.md' || p === 'book-bible.md' || p === 'canon/author-lore.md') {
    return '总纲派生：携带总纲等依据；接受仅写本文件'
  }
  return '生产约束可独立优化；接受仅写本文件'
})

function requestIdentityRewrite() {
  emit('rewrite', { targetPath: 'canon/identity-timeline.yaml', entityKind: 'identity', title: `${currentCast.value?.name || '人物'} · 身份时间线` })
}

function requestRewrite() {
  const path = currentFilePath.value
  if (!path || path.includes('#') || !canOptimize.value) {
    store.note('当前项请手工编辑；连续性账本与锁词不提供 AI 优化')
    return
  }
  const kind = selectedCast.value ? 'cast'
    : path === 'book-bible.md' ? 'bible'
    : path.includes('volumes/') ? 'volume'
    : path === 'novel-state.yaml' ? 'state'
    : 'canon'
  emit('rewrite', { targetPath: path, entityKind: kind, title: currentLabel.value || path })
}
function save() {
  if (currentFilePath.value) emit('save', currentFilePath.value, draft.value)
  editing.value = false
}
async function scaffoldMissing() {
  if (!book.value) return
  try {
    await api.post(`/api/books/${book.value.summary.id}/scaffold-missing`)
    await store.loadBook(book.value.summary.id)
    store.note('已补齐缺失设定模板（已有内容未覆盖）')
  } catch (e) { store.fail(e) }
}
</script>

<template>
  <section class="bible-workspace">
    <div class="view-title">
      <div><p class="eyebrow">Story Bible</p><h2>设定与正史</h2><p>故事约束、作者底牌和读者事实分层管理，不把知识库教材塞给模型。</p></div>
      <div class="view-actions">
        <button v-if="missingCount" @click="scaffoldMissing">补齐 {{ missingCount }} 项模板</button>
        <button v-if="selectedCast && currentCast?.status === 'candidate'" class="promote-btn" :disabled="book?.summary.readOnly || promoting" @click="promoteCast">{{ promoting ? '晋升中…' : '晋升 canon' }}</button>
        <button v-if="!specialView && currentRaw && !editing && canOptimize" :disabled="book?.summary.readOnly" :title="optimizeHint" @click="requestRewrite">{{ optimizeLabel }}</button>
        <button v-if="!specialView" class="primary" :disabled="props.saving || (!currentDoc && !selectedCast)" @click="editing ? save() : startEdit()">{{ editing ? '保存更改' : '编辑当前' }}</button>
      </div>
    </div>

    <div class="coverage panel">
      <span><strong>{{ docs.filter(d => d.exists).length }}/{{ docs.length }}</strong> 设定与状态已建立</span>
      <span>{{ book?.cast?.length || 0 }} 人物</span><span>{{ summaries.length }} 卷摘要</span><span>{{ commits.length }} 定稿账本</span><span>关系/金手指在人物卡</span>
      <span v-if="missingCount" class="missing">缺 {{ missingCount }} 项</span>
    </div>

    <button class="mobile-nav-toggle panel" :aria-expanded="navOpen" @click="navOpen = !navOpen">
      <span>设定目录 · {{ currentLabel }}</span><span>{{ navOpen ? '收起' : '展开' }}</span>
    </button>
    <div class="bible-primary-tabs" role="tablist" aria-label="设定分类">
      <button v-for="group in groups" :key="group" role="tab" :aria-selected="activeGroup === group" :class="{ active: activeGroup === group }" @click="chooseGroup(group)">{{ group }}</button>
    </div>
    <div class="bible-layout">
      <nav class="bible-nav panel" :class="{ 'mobile-open': navOpen }">
        <template v-for="group in [activeGroup]" :key="group">
          <h3>{{ group }}</h3>
          <template v-if="group === '人物与关系'">
            <button :class="{ active: specialView === 'relations' }" @click="chooseSpecial('relations')"><span>人物关系图</span><small>正式表对方填 stem；关系锚点/与主角关系为虚线</small></button>
            <button v-for="c in book?.cast || []" :key="c.stem" :class="{ active: selectedCast === c.stem, candidate: c.status === 'candidate' }" @click="chooseCast(c.stem)">
              <span>{{ c.name }}<i class="cast-role" :data-role="c.role">{{ roleLabel(c.role) }}</i><i v-if="c.status === 'candidate'" class="cand-mark">候选</i></span>
              <small v-if="c.tagline">{{ c.tagline }}</small>
              <small v-else class="cast-stem">{{ c.stem }}</small>
            </button>
            <p v-if="!book?.cast?.length" class="nav-empty">批准立项后人物卡进入这里。</p>
          </template>
          <template v-else-if="group === '连续性账本'">
            <button v-for="d in docs.filter(x => x.group === group)" :key="d.path" :class="{ active: !selectedCast && currentDoc?.path === d.path }" @click="chooseDoc(d.path)">
              <span>{{ d.label }}<i v-if="!d.exists">未创建</i></span><small>{{ d.path }}</small>
            </button>
            <button v-for="path in summaries" :key="path" :class="{ active: selectedPath === path }" @click="chooseDoc(path)">
              <span>卷摘要 · {{ path.split('/').pop()?.replace('.md', '') }}</span><small>{{ path }}</small>
            </button>
            <button v-for="path in commits" :key="path" :class="{ active: selectedPath === path }" @click="chooseDoc(path)">
              <span>定稿账本 · {{ path.split('/').pop()?.replace('.md', '') }}</span><small>{{ path }}</small>
            </button>
          </template>
          <template v-else>
            <button v-for="d in docs.filter(x => x.group === group)" :key="d.path" :class="{ active: !selectedCast && currentDoc?.path === d.path }" @click="chooseDoc(d.path)">
              <span>{{ d.label }}<i v-if="!d.exists">未创建</i></span><small>{{ d.description }}</small>
              <small class="usage-tag">用于：{{ d.usage }}</small>
            </button>
          </template>
        </template>
      </nav>

      <article class="bible-content panel">
        <RelationGraph v-if="specialView === 'relations'" @open="chooseCast" />
        <template v-else>
        <header>
          <div>
            <template v-if="selectedCast">
              <h3 class="cast-title">{{ currentCast?.name }}<i class="cast-role big" :data-role="currentCast?.role">{{ roleLabel(currentCast?.role) }}</i>
                <i v-if="currentCast?.status === 'candidate'" class="candidate-badge">候选 · 待晋升</i>
              </h3>
              <p v-if="currentCast?.tagline" class="cast-tagline">{{ currentCast.tagline }}</p>
              <p class="usage-line">用于：细纲 · 正文 · 审校（压缩快照）· 定稿回写当前状态</p>
            </template>
            <template v-else>
              <h3>{{ currentLabel }}</h3><p>{{ currentDoc?.description }}</p>
              <p v-if="currentDoc?.usage" class="usage-line">用于：{{ currentDoc.usage }}</p>
            </template>
          </div>
          <span>{{ currentFilePath }}</span>
        </header>
        <div v-if="currentDoc?.secret && !showSecrets && !editing" class="secret-gate">
          <strong>作者底牌</strong><p>这些内容不会进入正文或审校上下文。展开前确认没有读者在场。</p>
          <button @click="showSecrets = true">显示作者底牌</button>
        </div>
        <template v-else>
          <!-- 人物身份时间线（单元边界对齐，单元内部身份恒定） -->
          <div v-if="selectedCast" class="identity-timeline">
            <div class="identity-heading"><div><small class="timeline-label">职务身份时间线 · 单元内部身份恒定，转换只在单元边界</small><p>规划接受即生效；定稿只确认实际状态，不重复修改这里。</p></div><div class="identity-actions"><button :disabled="book?.summary.readOnly" @click="requestIdentityRewrite">候选重写</button><button :disabled="book?.summary.readOnly" @click="startIdentityEdit()">{{ castTimeline.length ? '新增身份段' : '登记初始身份' }}</button></div></div>
            <p v-if="!castTimeline.length" class="identity-warning">尚未建立职务身份。正式写作前必须登记初始职务与机构。</p>
            <div class="identity-segs">
              <div v-for="(s, i) in castTimeline" :key="i" class="identity-seg" :class="{ current: !s.toUnit }">
                <span class="seg-range">{{ s.fromUnit }}<template v-if="s.toUnit"> → {{ s.toUnit }}</template><template v-else> → 至今</template></span>
                <strong>{{ s.roleTitle }}</strong>
                <small v-if="s.org">{{ s.org }}</small>
                <small v-if="s.note" class="identity-note">{{ s.note }}</small>
                <div class="identity-actions"><button :disabled="book?.summary.readOnly" @click="startIdentityEdit(i)">修正</button><button v-if="!s.toUnit" :disabled="book?.summary.readOnly" @click="closeIdentity(i)">结束当前段</button></div>
              </div>
            </div>
          </div>
          <div v-if="currentDoc && !currentDoc.exists && !editing" class="missing-doc">
            <h4>这项设定尚未创建</h4><p>{{ currentDoc.description }}</p><button @click="startEdit">新建并编辑</button>
          </div>
          <template v-else-if="!editing && currentRaw">
            <!-- 书籍运行状态：字段卡片 -->
            <div v-if="renderMode === 'state'" class="entity-view">
              <div class="state-grid">
                <div v-for="f in stateView.fields" :key="f.key" class="state-card">
                  <small>{{ f.label }}</small><strong>{{ f.value }}</strong>
                </div>
              </div>
              <div v-if="stateView.gates.length" class="gate-row">
                <span v-for="g in stateView.gates" :key="g.key" class="gate-chip" :data-state="g.value">{{ g.key }} · {{ gateIcon(g.value) }}</span>
              </div>
              <p v-if="!stateView.fields.length" class="muted-inline">尚未写入状态字段。</p>
            </div>
            <!-- 锁词：表格式 -->
            <div v-else-if="renderMode === 'locked'" class="entity-view">
              <section class="locked-section">
                <h4>按卷锁词 <small>到期卷之前正文禁用</small></h4>
                <table v-if="lockedView.locked.length" class="entity-table">
                  <thead><tr><th>锁词</th><th>解锁卷</th></tr></thead>
                  <tbody><tr v-for="t in lockedView.locked" :key="t.term"><td>{{ t.term }}</td><td>{{ t.until }}</td></tr></tbody>
                </table>
                <p v-else class="muted-inline">暂无锁词。</p>
              </section>
              <section class="locked-section">
                <h4>永久合规红线</h4>
                <ul v-if="lockedView.compliance.length" class="entity-list"><li v-for="c in lockedView.compliance" :key="c">{{ c }}</li></ul>
                <p v-else class="muted-inline">暂无红线。</p>
              </section>
              <section v-if="lockedView.aliases.length" class="locked-section">
                <h4>别名映射</h4>
                <table class="entity-table">
                  <thead><tr><th>公开称呼</th><th>真实指向</th></tr></thead>
                  <tbody><tr v-for="a in lockedView.aliases" :key="a.alias"><td>{{ a.alias }}</td><td>{{ a.real }}</td></tr></tbody>
                </table>
              </section>
            </div>
            <!-- Markdown 实体：结构化渲染 -->
            <div v-else class="entity-view">
              <template v-for="(b, i) in mdBlocks" :key="i">
                <h4 v-if="b.kind === 'heading'" class="md-heading" :class="`h${b.level}`">{{ b.text }}</h4>
                <p v-else-if="b.kind === 'para'" class="md-para">{{ b.text }}</p>
                <blockquote v-else-if="b.kind === 'quote'" class="md-quote">{{ b.text }}</blockquote>
                <ul v-else-if="b.kind === 'bullets'" class="entity-list">
                  <li v-for="(item, j) in b.items" :key="j">{{ item }}</li>
                </ul>
                <div v-else-if="b.kind === 'table'" class="table-wrap">
                  <table class="entity-table">
                    <thead><tr><th v-for="h in b.headers" :key="h">{{ h || '—' }}</th></tr></thead>
                    <tbody>
                      <tr v-for="(row, j) in b.rows" :key="j"><td v-for="(cell, k) in row" :key="k">{{ cell || '—' }}</td></tr>
                    </tbody>
                  </table>
                </div>
              </template>
              <p v-if="!mdBlocks.length" class="muted-inline">文件为空。</p>
            </div>
            <details class="raw-source">
              <summary>查看源文件</summary>
              <pre>{{ currentRaw }}</pre>
            </details>
          </template>
          <div v-else-if="!editing" class="empty-state"><h3>还没有内容</h3><p>可以新建模板，或批准立项候选后写入。</p></div>
          <textarea v-if="editing" v-model="draft" class="source-editor" spellcheck="false" />
          <button v-if="editing" class="cancel" @click="editing = false">取消编辑</button>
        </template>
        </template>
      </article>
    </div>
    <div v-if="identityEditing" class="identity-modal" @click.self="identityEditing = null">
      <form class="identity-dialog panel" @submit.prevent="saveIdentity">
        <header><div><small>{{ identityEditing.action === 'append' ? '规划身份变更' : identityEditing.action === 'update' ? '修正身份段' : '结束当前身份段' }}</small><h3>{{ currentCast?.name }}</h3></div><button type="button" @click="identityEditing = null">×</button></header>
        <p class="identity-dialog-note">身份只从指定单元开始生效；不会改写人物稳定人格或自动替正文认定变化。</p>
        <label>职务 / 公开身份<input v-model="identityEditing.segment.roleTitle" required :disabled="identityEditing.action === 'close'" placeholder="例如：重案队试用" /></label>
        <label>所属机构<input v-model="identityEditing.segment.org" required :disabled="identityEditing.action === 'close'" placeholder="例如：青江分局" /></label>
        <label>起始单元<select v-model="identityEditing.segment.fromUnit" :disabled="identityEditing.action !== 'append'"><option v-for="u in book?.units" :key="u.unitId" :value="u.unitId">{{ u.unitId }} · {{ u.title }}</option></select></label>
        <label v-if="identityEditing.action === 'close'">结束于单元<select v-model="identityEditing.segment.toUnit" required><option v-for="u in book?.units" :key="u.unitId" :value="u.unitId">{{ u.unitId }} · {{ u.title }}</option></select></label>
        <label v-else>变更原因 / 依据<input v-model="identityEditing.segment.note" placeholder="例如：调令生效" /></label>
        <p v-if="identityEditing.action === 'append'" class="identity-impact">将从 {{ identityEditing.segment.fromUnit }} 起使用「{{ identityEditing.segment.roleTitle || '新职务' }} · {{ identityEditing.segment.org || '新机构' }}」，并结束此前开放身份段。</p>
        <footer><button type="button" @click="identityEditing = null">取消</button><button class="primary" :disabled="identitySaving">{{ identitySaving ? '校验并保存中…' : '校验并保存' }}</button></footer>
      </form>
    </div>
  </section>
</template>

<style scoped>
.bible-workspace { display: grid; gap: 18px; }
.view-title { display: flex; justify-content: space-between; gap: 20px; align-items: flex-end; }
.view-title h2 { margin: 4px 0 6px; font-family: var(--font-prose); font-size: 28px; }
.view-title p { margin: 0; color: var(--ink-soft); font-size: 13px; }
.eyebrow { color: var(--accent) !important; font-size: 11px !important; letter-spacing: .12em; text-transform: uppercase; }
.view-actions { display: flex; gap: 8px; }
.coverage { display: flex; flex-wrap: wrap; gap: 20px; padding: 12px 16px; color: var(--ink-soft); font-size: 12px; }
.coverage strong { color: var(--accent); }
.coverage .missing { color: var(--warn); margin-left: auto; }
.bible-primary-tabs { display: flex; gap: 4px; border-bottom: 1px solid var(--line); overflow-x: auto; }
.bible-primary-tabs button { border: 0; border-bottom: 2px solid transparent; border-radius: 0; background: transparent; color: var(--ink-soft); padding: 10px 16px; white-space: nowrap; }
.bible-primary-tabs button.active { color: var(--accent); border-bottom-color: var(--accent); font-weight: 700; }
.bible-layout { display: grid; grid-template-columns: 300px minmax(0, 1fr); gap: 16px; align-items: start; }
.bible-nav { padding: 12px; max-height: 75vh; overflow: auto; }
.bible-nav h3 { margin: 12px 8px 6px; font-size: 11px; color: var(--ink-soft); text-transform: uppercase; letter-spacing: .08em; }
.bible-nav button { display: grid; gap: 4px; text-align: left; width: 100%; border: 0; background: transparent; padding: 10px; }
.bible-nav button.active { background: rgba(15,107,92,.1); color: var(--accent); }
.bible-nav small { color: var(--ink-soft); font-size: 10px; line-height: 1.5; }
.bible-nav i { font-style: normal; color: var(--warn); font-size: 10px; margin-left: 6px; }
.usage-tag { color: var(--accent) !important; opacity: .8; }
.usage-line { margin: 6px 0 0; color: var(--accent); font-size: 11px; }
.cast-role { font-style: normal; font-size: 9px; padding: 1px 6px; border-radius: 6px; background: rgba(20,35,31,.06); color: var(--ink-soft); margin-left: 8px; font-weight: 500; }
.cast-role[data-role="protagonist"] { background: rgba(15,107,92,.12); color: var(--accent); }
.cast-role[data-role="volume_antagonist"] { background: rgba(163,59,50,.1); color: var(--bad); }
.cast-role.big { font-size: 11px; padding: 2px 10px; vertical-align: middle; }
.cast-title { display: flex; align-items: center; }
.cast-tagline { margin: 6px 0 0; color: var(--ink-soft); font-size: 13px; }
.cast-stem { opacity: .6; }
.candidate-badge { font-style: normal; font-size: 10px; padding: 2px 8px; border-radius: 8px; background: rgba(161,92,18,.14); color: var(--warn); margin-left: 8px; font-weight: 600; }
.cand-mark { font-style: normal; font-size: 9px; padding: 0 5px; border-radius: 6px; background: rgba(161,92,18,.14); color: var(--warn); margin-left: 6px; }
.promote-btn { background: rgba(31,122,77,.12); color: var(--ok); border-color: rgba(31,122,77,.3); }
.bible-nav button.candidate { border-left: 2px solid var(--warn); }
.identity-timeline { padding: 16px 24px; border-bottom: 1px solid var(--line); background: rgba(15,107,92,.03); }
.timeline-label { color: var(--ink-soft); font-size: 11px; display: block; margin-bottom: 12px; }
.identity-segs { display: flex; flex-direction: column; gap: 8px; }
.identity-seg { display: flex; align-items: baseline; gap: 12px; padding: 8px 12px; border-left: 3px solid var(--line); }
.identity-seg.current { border-left-color: var(--accent); background: rgba(15,107,92,.06); border-radius: 0 8px 8px 0; }
.seg-range { font-size: 11px; color: var(--accent); font-family: ui-monospace, monospace; white-space: nowrap; }
.identity-seg strong { font-size: 13px; }
.identity-seg small { color: var(--ink-soft); font-size: 11px; }
.identity-heading { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; margin-bottom: 12px; }.identity-heading p { margin: 4px 0 0; color: var(--ink-soft); font-size: 11px; }.identity-heading .timeline-label { margin-bottom: 0; }.identity-warning { color: var(--warn); font-size: 12px; }.identity-actions { margin-left: auto; display: flex; gap: 8px; }.identity-actions button { font-size: 10px; padding: 3px 8px; }.identity-note { font-style: italic; }
.identity-modal { position: fixed; inset: 0; z-index: 35; display: grid; place-items: center; padding: 20px; background: rgba(20,35,31,.25); }.identity-dialog { width: min(500px, 100%); padding: 22px; display: grid; gap: 14px; }.identity-dialog header { display: flex; justify-content: space-between; }.identity-dialog h3 { margin: 4px 0 0; }.identity-dialog small { color: var(--accent); }.identity-dialog label { display: grid; gap: 6px; color: var(--ink-soft); font-size: 12px; }.identity-dialog input, .identity-dialog select { width: 100%; padding: 9px 10px; border: 1px solid var(--line); border-radius: 8px; background: var(--paper); color: var(--ink); }.identity-dialog footer { display: flex; justify-content: flex-end; gap: 8px; }.identity-dialog-note, .identity-impact { color: var(--ink-soft); font-size: 12px; line-height: 1.7; margin: 0; }
.nav-empty { color: var(--ink-soft); font-size: 12px; padding: 8px; }
.bible-content { overflow: hidden; padding-bottom: 16px; }
.bible-content > .relation-view, .bible-content > .timeline-view { padding: 20px; }
.bible-content > header { display: flex; justify-content: space-between; gap: 10px; align-items: center; padding: 16px 20px; border-bottom: 1px solid var(--line); }
.bible-content header h3 { margin: 0; font-family: var(--font-prose); }
.bible-content header p { margin: 4px 0 0; color: var(--ink-soft); font-size: 11px; }
.bible-content header > span { color: var(--ink-soft); font-size: 11px; max-width: 45%; word-break: break-all; }
.bible-content pre { white-space: pre-wrap; font: 14px/1.85 var(--font-ui); padding: 22px; margin: 0; max-height: 65vh; overflow: auto; }
/* 结构化实体渲染 */
.entity-view { padding: 20px 24px; max-height: 65vh; overflow: auto; }
.md-heading { font-family: var(--font-prose); margin: 20px 0 10px; color: var(--ink); }
.md-heading.h1 { font-size: 22px; }
.md-heading.h2 { font-size: 17px; border-bottom: 1px solid var(--line); padding-bottom: 6px; }
.md-heading.h3, .md-heading.h4 { font-size: 14px; color: var(--accent); }
.md-heading:first-child { margin-top: 0; }
.md-para { color: var(--ink-soft); font-size: 14px; line-height: 1.85; margin: 8px 0; }
.md-quote { margin: 10px 0; padding: 10px 14px; border-left: 3px solid var(--accent); background: rgba(15,107,92,.05); color: var(--ink); font-family: var(--font-prose); font-size: 15px; border-radius: 0 10px 10px 0; }
.entity-list { margin: 8px 0; padding-left: 20px; }
.entity-list li { color: var(--ink-soft); font-size: 13px; line-height: 1.8; margin-bottom: 4px; }
.table-wrap { overflow-x: auto; margin: 10px 0; }
.entity-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.entity-table th { text-align: left; color: var(--ink-soft); font-weight: 500; padding: 8px 10px; border-bottom: 1px solid var(--line); white-space: nowrap; font-size: 12px; }
.entity-table td { padding: 9px 10px; border-bottom: 1px solid var(--line); color: var(--ink-soft); line-height: 1.6; }
.entity-table td:first-child { color: var(--ink); font-weight: 600; }
.state-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 10px; }
.state-card { padding: 14px; border: 1px solid var(--line); border-radius: 12px; background: rgba(255,255,255,.5); }
.state-card small { display: block; color: var(--accent); font-size: 11px; margin-bottom: 4px; }
.state-card strong { font-size: 14px; font-family: var(--font-prose); word-break: break-all; }
.gate-row { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 14px; }
.gate-chip { padding: 5px 10px; border-radius: 8px; font-size: 11px; background: rgba(154,165,160,.12); color: var(--ink-soft); }
.gate-chip[data-state="pass"] { background: rgba(15,107,92,.1); color: var(--accent); }
.gate-chip[data-state="fail"] { background: rgba(163,59,50,.1); color: var(--bad); }
.locked-section { margin-bottom: 20px; }
.locked-section h4 { margin: 0 0 10px; font-family: var(--font-prose); font-size: 16px; }
.locked-section h4 small { color: var(--ink-soft); font-size: 11px; font-weight: 400; margin-left: 8px; }
.muted-inline { color: var(--ink-soft); font-size: 12px; }
.raw-source { border-top: 1px solid var(--line); margin: 18px 24px 0; }
.raw-source summary { padding: 12px 0; cursor: pointer; color: var(--ink-soft); font-size: 12px; }
.raw-source pre { padding: 4px 0 14px; font-size: 12px; }
.source-editor { width: calc(100% - 32px); min-height: 540px; margin: 16px; padding: 16px; resize: vertical; border: 1px solid var(--line); border-radius: 12px; font: 13px/1.7 ui-monospace, monospace; }
.cancel { margin-left: 16px; }
.empty-state, .missing-doc, .secret-gate { padding: 36px 24px; text-align: center; }
.missing-doc p, .secret-gate p { color: var(--ink-soft); line-height: 1.7; }
.secret-gate strong { font-family: var(--font-prose); font-size: 22px; }
.mobile-nav-toggle { display: none; }
@media (max-width: 900px) {
  .bible-layout { grid-template-columns: 1fr; }
  .bible-nav { display: none; max-height: 55vh; }
  .bible-nav.mobile-open { display: block; }
  .mobile-nav-toggle { display: flex; justify-content: space-between; align-items: center; padding: 12px 14px; text-align: left; }
  .view-title { flex-direction: column; align-items: flex-start; }
}
</style>
