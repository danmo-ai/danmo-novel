/**
 * 设定页（Story Bible）的视图模型：文档清单、人物、身份时间线、结构化解析。
 *
 * 解析函数（Markdown / 状态 YAML / 锁词）本身是纯函数，移动端要渲染同一份内容，
 * 所以它们属于这里而不是组件。壳层只决定目录怎么摆、详情怎么进。
 */
import { computed, ref, watch } from 'vue'
import { useAppStore } from '@/shared/stores/app'
import { api } from '@/shared/api/client'
import type { IdentitySegment } from '@/shared/types'

export type BibleDoc = { path: string; label: string; group: string; description: string; secret?: boolean; usage: string }
export type RuntimeDoc = BibleDoc & { raw: string; exists: boolean }

export function useBible(opts: {
  onSave?: (path: string, content: string) => void
  onRewrite?: (spec: { targetPath: string; entityKind: string; title: string }) => void
} = {}) {
  const store = useAppStore()

  const selectedPath = ref('book-bible.md')
  const selectedCast = ref('')
  const editing = ref(false)
  const draft = ref('')
  const showSecrets = ref(false)
  const activeGroup = ref('总纲派生')
  const specialView = ref<'relations' | null>(null)
  const book = computed(() => store.currentBook)

  const definitions: BibleDoc[] = [
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

  function openLoops(raw: string) {
    const match = raw.match(/## Open loops[\s\S]*?(?=\n## |$)/)
    return match?.[0] || '## Open loops\n\n尚未登记伏笔。'
  }
  function relations(raw: string) {
    const match = raw.match(/### Cast snapshot[\s\S]*?(?=\n### |\n## |$)/)
    return (match?.[0] || '### Cast snapshot\n\n尚无人物状态。') + '\n\n人物当前状态由定稿接受按 state_deltas 回写；单元剧情时钟见「人物与关系 → 单元剧情时钟」。'
  }

  const summaries = computed(() => Object.keys(book.value?.files || {}).filter((p) => /^continuity\/summaries\/[^/]+\.md$/.test(p)).sort())
  const commits = computed(() => Object.keys(book.value?.files || {}).filter((p) => /^continuity\/commits\/[^/]+\.md$/.test(p)).sort())

  const docs = computed(() => definitions.map((d) => ({
    ...d,
    raw: d.path.includes('#open-loops') ? openLoops(book.value?.files[d.path.split('#')[0]] || '')
      : d.path.includes('#relations') ? relations(book.value?.files[d.path.split('#')[0]] || '')
        : book.value?.files[d.path] || '',
    exists: Boolean(book.value?.files[d.path.split('#')[0]]),
  })))

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
  const missingCount = computed(() => docs.value.filter((d) => !d.exists).length)

  watch(currentDoc, () => { if (currentDoc.value?.secret) showSecrets.value = false })

  function chooseDoc(path: string) { selectedPath.value = path; selectedCast.value = ''; specialView.value = null; editing.value = false }
  function chooseCast(stem: string) { selectedCast.value = stem; specialView.value = null; editing.value = false }
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
  function chooseSpecial(view: 'relations') { specialView.value = view; selectedCast.value = ''; editing.value = false }

  // ---- 结构化渲染：把 Markdown / YAML 实体渲染成卡片、表格、列表，而非原始源码 ----
  type MDBlock =
    | { kind: 'heading'; level: number; text: string }
    | { kind: 'para'; text: string }
    | { kind: 'bullets'; items: string[] }
    | { kind: 'table'; headers: string[]; rows: string[][] }
    | { kind: 'quote'; text: string }

  function cleanInline(s: string) {
    return s.replace(/\*\*([^*]+)\*\*/g, '$1').replace(/`([^`]+)`/g, '$1').trim()
  }

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
    opts.onRewrite?.({ targetPath: 'canon/identity-timeline.yaml', entityKind: 'identity', title: `${currentCast.value?.name || '人物'} · 身份时间线` })
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
    opts.onRewrite?.({ targetPath: path, entityKind: kind, title: currentLabel.value || path })
  }
  function save() {
    if (currentFilePath.value) opts.onSave?.(currentFilePath.value, draft.value)
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

  return {
    store, book, groups, docs, dynamicDocs, allDocs, summaries, commits, missingCount,
    selectedPath, selectedCast, currentDoc, currentRaw, currentLabel, currentFilePath, currentCast, castTimeline,
    editing, draft, showSecrets, activeGroup, specialView,
    identityEditing, identitySaving, promoting,
    renderMode, mdBlocks, stateView, lockedView,
    canOptimize, optimizeLabel, optimizeHint,
    roleLabel, chooseDoc, chooseCast, chooseGroup, chooseSpecial,
    gateIcon, startEdit, save, scaffoldMissing,
    startIdentityEdit, saveIdentity, closeIdentity, promoteCast,
    requestRewrite, requestIdentityRewrite,
    // 供移动端复用的纯函数
    parseMarkdown, parseState, parseLockedTerms, cleanInline,
  }
}
