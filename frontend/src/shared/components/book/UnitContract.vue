<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '@/shared/stores/app'
import type { UnitInfo } from '@/shared/types'

const props = defineProps<{ unit: UnitInfo; compact?: boolean }>()
const store = useAppStore()

function scalar(raw: string, key: string): string {
  const m = raw.match(new RegExp(`^${key}:\\s*(.*)$`, 'm'))
  return (m?.[1] || '').trim().replace(/^['"]|['"]$/g, '')
}
function section(raw: string, key: string): string {
  return raw.match(new RegExp(`^${key}:\\s*\\n((?:[ \\t]+.*\\n?)*)`, 'm'))?.[1] || ''
}
function list(raw: string, key: string): string {
  return raw.match(new RegExp(`^${key}:\\s*\\[([^\\]]*)\\]`, 'm'))?.[1]?.replace(/['"]/g, '').trim() || ''
}

const raw = computed(() => props.unit.outlineRaw || '')
const fields = computed(() => {
  const r = raw.value
  const pairs: [string, string][] = [
    ['单元功能', scalar(r, 'function')],
    ['欲望', scalar(r, 'desire')],
    ['阻碍', scalar(r, 'obstacle')],
    ['关键选择', scalar(r, 'choice')],
    ['兑现', scalar(r, 'payoff')],
    ['读者爽点', scalar(r, 'pleasure')],
    ['禁忌', scalar(r, 'forbidden')],
  ]
  return pairs.filter(([, v]) => v)
})
const meta = computed(() => {
  const r = raw.value
  const items: string[] = []
  if (props.unit.chapterFrom) items.push(`第 ${props.unit.chapterFrom}–${props.unit.chapterTo} 章`)
  const pov = scalar(r, 'pov'); if (pov) items.push(`POV ${castName(pov)}`)
  // 字数只显示派生值：chapterTarget 是后端算好的单章目标，
  // 细纲里的 word_target 是单元总数，直接标成「字/章」会放大章数倍。
  const target = props.unit.chapterTarget
  if (target > 0) {
    const chapters = Math.max(1, (props.unit.chapterTo || 0) - (props.unit.chapterFrom || 0) + 1)
    items.push(`每章 ${target} 字 · 共约 ${(target * chapters).toLocaleString()} 字`)
  }
  const day = scalar(r, 'story_day'); if (day && day !== 'null') items.push(`故事第 ${day} 天`)
  // 间隔天数只有 gap_days 是权威字段；gap_from_prev 是承接说明（长文本）。
  // 早期生成把天数写进了承接说明（"1"），别让它在元数据里变成一句没头没尾的话。
  const gapDays = gapIntervalDays.value
  if (gapDays !== null) items.push(gapDays === 0 ? '紧接上一单元' : `距上一单元 ${gapDays} 天`)
  if (scalar(r, 'flashback') === 'true') items.push('闪回')
  const hook = section(r, 'next_hook').match(/type:\s*(.*)/)?.[1]?.trim().replace(/^['"]|['"]$/g, '')
  if (hook) items.push(`下一钩：${hook}`)
  return items
})
/**
 * 与上一单元的间隔天数。gap_days 是结构化字段（整数），优先使用；没有时退回到
 * gap_from_prev——但只在它确实是个纯数字时才算数（那种数据是生成时把天数写错
 * 了字段）。两者都没有就返回 null，不显示任何间隔信息。
 */
const gapIntervalDays = computed<number | null>(() => {
  const structured = scalar(raw.value, 'gap_days')
  const bare = isBareNumber(raw.value) ? scalar(raw.value, 'gap_from_prev') : ''
  const picked = structured || bare
  if (!picked) return null
  const n = Number(picked)
  return Number.isFinite(n) && n >= 0 ? n : null
})
/** 纯数字 = 天数，不是承接说明；这类值不能当长文本段落渲染。 */
function isBareNumber(value: string) {
  return /^\d+(?:\.\d+)?$/.test(value.trim())
}
// 衔接说明是长文本，独立成段，不塞进元数据 chip。
// 只在它真的是说明文字时才渲染：纯数字是字段写错（天数进了承接说明），
// 上面已按间隔天数显示，这里渲染成「衔接上一单元 1」只会是噪音。
const gapFromPrev = computed(() => {
  const v = scalar(raw.value, 'gap_from_prev')
  return isBareNumber(v) ? '' : v
})
const onStage = computed(() => list(raw.value, 'on_stage').split(',').map((s) => s.trim()).filter(Boolean))
const infoControl = computed(() => scalar(raw.value, 'info_control'))

function castName(stem: string) {
  const card = store.currentBook?.cast.find((c) => c.stem === stem)
  return card?.name || stem
}
// 本单元身份：identity-timeline 在 unit 处有效的 roleTitle。
function identityAt(stem: string): string {
  const segs = (store.currentBook as any)?.identitySegments || []
  const key = unitKey(props.unit.unitId)
  let best: any = null
  for (const s of segs) {
    if (s.stem !== stem) continue
    const from = unitKey(s.fromUnit)
    if (from > key) continue
    if (s.toUnit && unitKey(s.toUnit) < key) continue
    if (!best || from >= unitKey(best.fromUnit)) best = s
  }
  return best ? best.roleTitle : ''
}
function unitKey(id: string) {
  const m = (id || '').match(/v(\d+)-U(\d+)/i)
  return m ? Number(m[1]) * 1000 + Number(m[2]) : 0
}
const onStageIdentities = computed(() =>
  onStage.value.map((s) => ({ stem: s, name: castName(s), identity: identityAt(s) })).filter((x) => x.identity)
)

type ChapterView = { chapter: string; goal: string; change: string; cutHook: string; dayOffset: string; scenes: Scene[] }
type Scene = { id: string; chapter: string; goal: string; conflict: string; turn: string; outcome: string; who: string; where: string; info: string }

const indentOf = (line: string) => (line.match(/^\s*/)?.[0] || '').length
const unquote = (v: string) => v.trim().replace(/^['"]|['"]$/g, '')

/**
 * 读一个场面块里的字段。
 *
 * `scalar` 与 `list` 分开，是因为 must_land/reveal/withhold 既可以写成
 * `[a, b]` 也可以写成缩进列表；只取「冒号后那一段」会把 `[` 当成值显示。
 * 列表只收比键更深一层的 `- ` 行，避免把下一个键的列表项算进来。
 */
function parseSceneFields(lines: string[], fallbackChapter = ''): Scene {
  const scalar = (key: string): string => {
    for (const line of lines) {
      const m = line.match(new RegExp(`^\\s*${key}\\s*:\\s*(.*)$`))
      if (m) return unquote(m[1])
    }
    return ''
  }
  const list = (key: string): string[] => {
    const out: string[] = []
    let active = false
    let base = 0
    for (const line of lines) {
      const m = line.match(new RegExp(`^(\\s*)${key}\\s*:\\s*(.*)$`))
      if (m) {
        active = true
        base = m[1].length
        const inline = m[2].trim()
        if (inline && inline !== '[]') {
          out.push(...inline.replace(/^\[/, '').replace(/\]$/, '').split(',').map(unquote).filter(Boolean))
        }
        continue
      }
      if (!active) continue
      if (!line.trim()) continue
      const li = line.match(/^(\s*)-\s+(.*)$/)
      if (li && li[1].length > base) {
        out.push(unquote(li[2]))
        continue
      }
      active = false
    }
    return out
  }
  const whoList = list('who')
  const info = [
    ['must_land', list('must_land')],
    ['reveal', list('reveal')],
    ['withhold', list('withhold')],
  ]
    .filter(([, items]) => (items as string[]).length)
    .map(([key, items]) => `${key as string}: ${(items as string[]).join('、')}`)
    .join(' · ')
  return {
    id: scalar('id') || scalar('scene'),
    chapter: scalar('chapter') || fallbackChapter,
    goal: scalar('goal'),
    conflict: scalar('conflict'),
    turn: scalar('turn'),
    outcome: scalar('outcome'),
    who: whoList.length ? whoList.join('、') : scalar('who'),
    where: scalar('where') || scalar('when'),
    info,
  }
}

const hasSceneBody = (s: Scene) => Boolean(s.goal || s.conflict || s.turn || s.outcome)

/**
 * 解析章合同。
 *
 * 必须按缩进判定「这一条是不是章」：嵌套的 `      - chapter: 51` 也是
 * `- chapter:` 开头，用不锚定缩进的正则会把每个场面都当成新章，于是章
 * 重复出现、场面永远为空，场面里的 `goal:` 还会覆盖掉章自己的目标。
 * 章条目的缩进以第一个匹配为准，比它深一层的就是场面。
 */
function parseChapters(text: string): ChapterView[] {
  // 不能 trim() 左侧：块内第一行的缩进是判定「章 / 场面」的基准，裁掉它会让
  // 第一章缩进变成 0、其余章（同为 2 空格）全部落进场面分支。
  const body = text.replace(/\r/g, '').replace(/\s+$/, '')
  if (!body.trim() || body.trim() === '[]') return []
  const lines = body.split('\n')
  const chapterItemRe = /^(\s*)-\s+chapter\s*:\s*(\d+)\s*$/
  let chapterIndent = -1
  for (const line of lines) {
    const m = line.match(chapterItemRe)
    if (m) { chapterIndent = m[1].length; break }
  }
  if (chapterIndent < 0) return []

  const chapters: ChapterView[] = []
  const byNumber = new Map<string, ChapterView>()
  let current: ChapterView | null = null
  let sceneLines: string[] = []
  const flushScene = () => {
    if (current && sceneLines.length) {
      const scene = parseSceneFields(sceneLines, current.chapter)
      if (hasSceneBody(scene)) current.scenes.push(scene)
    }
    sceneLines = []
  }

  for (const line of lines) {
    if (!line.trim()) continue
    const indent = indentOf(line)
    const item = line.match(chapterItemRe)
    if (item && indent === chapterIndent) {
      flushScene()
      const existing = byNumber.get(item[2])
      if (existing) { current = existing; continue } // 同章号重复时合并，不再多显示一章
      current = { chapter: item[2], goal: '', change: '', cutHook: '', dayOffset: '0', scenes: [] }
      byNumber.set(item[2], current)
      chapters.push(current)
      continue
    }
    if (!current) continue
    if (indent > chapterIndent + 2) {
      // 场面区域：`- chapter:` 或 `- id:` 开启下一场，其余行归当前场。
      if (item || /^\s*-\s+(id|scene)\s*:/.test(line)) {
        flushScene()
        sceneLines = [line]
      } else if (sceneLines.length) {
        sceneLines.push(line)
      }
      continue
    }
    const field = line.match(/^\s*(goal|change|cut_hook|day_offset)\s*:\s*(.*)$/)
    if (!field) continue
    if (field[1] === 'goal') current.goal = unquote(field[2])
    if (field[1] === 'change') current.change = unquote(field[2])
    if (field[1] === 'cut_hook') current.cutHook = unquote(field[2])
    if (field[1] === 'day_offset') current.dayOffset = unquote(field[2])
  }
  flushScene()
  return chapters
}

/**
 * 解析单元级 `scenes:`。
 *
 * 旧书把场面平铺在单元上（同一个 mapping 里重复写 chapter/goal/conflict…），
 * 新版把场面嵌在 chapters[].scenes 下。两种都要能显示：只要出现下一个
 * `chapter:` 键就切块，块内字段用 parseSceneFields 读。
 */
function parseUnitScenes(text: string): Scene[] {
  const body = text.replace(/\r/g, '').replace(/\s+$/, '')
  if (!body.trim() || body.trim() === '[]') return []
  const blocks: string[][] = []
  let cur: string[] = []
  let hasChapter = false
  for (const line of body.split('\n')) {
    if (!line.trim()) continue
    const chapterKey = /^\s*chapter\s*:/.test(line)
    const itemStart = /^\s*-\s+(id|scene|chapter)\s*:/.test(line)
    if ((chapterKey && hasChapter) || (itemStart && cur.length)) {
      blocks.push(cur)
      cur = []
      hasChapter = false
    }
    cur.push(line)
    if (chapterKey) hasChapter = true
  }
  if (cur.length) blocks.push(cur)
  return blocks
    .map((block) => parseSceneFields(block))
    .filter((s) => s.chapter && hasSceneBody(s))
}

/** 单元级场面（旧书格式在这里；新版嵌在 chapters[].scenes 下）。 */
const unitScenes = computed<Scene[]>(() => parseUnitScenes(section(raw.value, 'scenes')))
/** 章合同 + 单元级场面归并：老书的四元组也要挂到对应章上显示。 */
const chapterPlans = computed(() => {
  const chapters = parseChapters(section(raw.value, 'chapters'))
  if (!chapters.length) return chapters
  const byNumber = new Map(chapters.map((c) => [c.chapter, c]))
  // 以「章」为单位决定用哪一份：章下有嵌套场面就整章用嵌套的，否则整章用
  // 单元级的那几条。以前按「章内有没有场面」逐条判断，结果每章只挂上第一场。
  const nestedChapters = new Set(chapters.filter((c) => c.scenes.length > 0).map((c) => c.chapter))
  for (const scene of unitScenes.value) {
    if (nestedChapters.has(scene.chapter)) continue
    const target = byNumber.get(scene.chapter)
    if (target) target.scenes.push(scene)
  }
  return chapters
})
const orphanSceneGroups = computed(() => {
  const known = new Set(chapterPlans.value.map((c) => c.chapter))
  const groups: { chapter: string; scenes: Scene[] }[] = []
  for (const scene of unitScenes.value) {
    if (known.has(scene.chapter)) continue
    let group = groups.find((g) => g.chapter === scene.chapter)
    if (!group) {
      group = { chapter: scene.chapter, scenes: [] }
      groups.push(group)
    }
    group.scenes.push(scene)
  }
  return groups
})
const hasContent = computed(() => fields.value.length > 0 || chapterPlans.value.length > 0 || unitScenes.value.length > 0)
</script>

<template>
  <div class="contract" :class="{ compact }">
    <div v-if="!hasContent" class="contract-empty">
      <p>这个单元还没有细纲合同。</p>
      <p class="hint">细纲会定义欲望、阻碍、关键选择、场面节拍和故事时钟——写正文前必须先补全。</p>
    </div>
    <template v-else>
      <div class="contract-meta">
        <span v-for="m in meta" :key="m" class="meta-chip">{{ m }}</span>
        <span v-for="s in onStage" :key="s" class="meta-chip stage-chip" :title="s">{{ castName(s) }}</span>
      </div>
      <p v-if="gapFromPrev" class="gap-note"><small>衔接上一单元</small>{{ gapFromPrev }}</p>
      <div v-if="onStageIdentities.length" class="identity-row">
        <span v-for="x in onStageIdentities" :key="x.stem" class="identity-chip" :title="`${x.name} 在本单元的职务身份`">{{ x.name }} · {{ x.identity }}</span>
      </div>
      <div class="contract-fields">
        <div v-for="[label, value] in fields" :key="label" class="contract-field">
          <small>{{ label }}</small><p>{{ value }}</p>
        </div>
      </div>
      <p v-if="infoControl" class="info-control"><small>信息控制</small>{{ infoControl }}</p>
      <div v-if="chapterPlans.length" class="contract-scenes chapter-plans">
        <small class="scenes-label">逐章推进 · {{ chapterPlans.length }} 章</small>
        <article v-for="chapter in chapterPlans" :key="chapter.chapter" class="chapter-plan">
          <header><span>第 {{ chapter.chapter }} 章</span><small>第 {{ chapter.dayOffset }} 日偏移</small></header>
          <p><strong>目标</strong>{{ chapter.goal }}</p>
          <p><strong>变化</strong>{{ chapter.change }}</p>
          <p><strong>断章</strong>{{ chapter.cutHook }}</p>
          <div v-if="chapter.scenes.length" class="chapter-scenes"><small>场面脉络 · {{ chapter.scenes.length }} 场</small>
            <div v-for="(s, i) in chapter.scenes" :key="i" class="scene-beat">
              <p class="scene-head"><strong>{{ s.id || `S${i + 1}` }}</strong><span v-if="s.who">{{ s.who }}</span><span v-if="s.where">{{ s.where }}</span></p>
              <p v-if="s.goal"><strong>目标</strong>{{ s.goal }}</p>
              <p v-if="s.conflict"><strong>阻力</strong>{{ s.conflict }}</p>
              <p v-if="s.turn"><strong>转折</strong>{{ s.turn }}</p>
              <p v-if="s.outcome"><strong>结果</strong>{{ s.outcome }}</p>
              <p v-if="s.info" class="scene-info">{{ s.info }}</p>
            </div>
          </div>
          <p v-else class="chapter-scenes-missing">本章没有场面脉络：写正文时只能临场决定路径。</p>
        </article>
      </div>
      <details v-if="orphanSceneGroups.length" class="contract-scenes legacy-scenes">
        <summary><span>未归章场面 · {{ orphanSceneGroups.reduce((n, g) => n + g.scenes.length, 0) }} 个</span><small>章号越界或章合同缺失</small></summary>
        <p class="legacy-note">这些场面的章号在本单元的章合同里找不到，因此没有显示在上面的逐章推进中。请检查细纲的 chapter_range 与 scenes[].chapter 是否一致。</p>
        <div v-for="group in orphanSceneGroups" :key="group.chapter" class="legacy-scene-group">
          <h5>第 {{ group.chapter || '?' }} 章</h5>
          <div v-for="(s, i) in group.scenes" :key="i" class="legacy-scene-row">
            <p v-if="s.goal"><strong>目标</strong>{{ s.goal }}</p>
            <p v-if="s.conflict"><strong>阻力</strong>{{ s.conflict }}</p>
            <p v-if="s.turn"><strong>转折</strong>{{ s.turn }}</p>
            <p v-if="s.outcome"><strong>结果</strong>{{ s.outcome }}</p>
          </div>
        </div>
      </details>
    </template>
  </div>
</template>

<style scoped>
.contract { display: grid; gap: 16px; }
.contract-empty { padding: 26px; text-align: center; color: var(--ink-soft); }
.contract-empty p { margin: 0 0 6px; font-size: 14px; }
.contract-empty .hint { font-size: 12px; line-height: 1.7; max-width: 420px; margin: 0 auto; }
.contract-meta { display: flex; flex-wrap: wrap; gap: 6px; }
.meta-chip { font-size: 11px; padding: 4px 10px; border-radius: 8px; background: rgba(20,35,31,.05); color: var(--ink-soft); }
.stage-chip { background: rgba(15,107,92,.08); color: var(--accent); }
.gap-note { margin: 0; padding: 10px 14px; background: rgba(15,107,92,.04); border-radius: 10px; font-size: 12px; line-height: 1.75; color: var(--ink-soft); }
.gap-note small { display: block; color: var(--accent); font-size: 10px; font-weight: 700; margin-bottom: 4px; }
.contract-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.contract-field { padding: 12px 14px; border: 1px solid var(--line); border-radius: 12px; background: rgba(255,255,255,.5); }
.contract-field small { color: var(--accent); font-size: 11px; font-weight: 600; }
.contract-field p { margin: 5px 0 0; font-size: 13px; line-height: 1.65; color: var(--ink-soft); }
.info-control { margin: 0; padding: 12px 14px; border-left: 3px solid var(--accent-2); background: rgba(196,92,38,.05); border-radius: 0 10px 10px 0; font-size: 13px; line-height: 1.7; color: var(--ink-soft); }
.info-control small { display: block; color: var(--accent-2); font-size: 11px; margin-bottom: 4px; }
.identity-row { display: flex; flex-wrap: wrap; gap: 6px; }
.identity-chip { font-size: 11px; padding: 4px 10px; border-radius: 8px; background: rgba(196,92,38,.1); color: var(--accent-2); font-weight: 600; }
.scenes-label { color: var(--ink-soft); font-size: 11px; font-weight: 600; display: block; margin-bottom: 8px; }
.scene-ch { color: var(--accent); font-size: 11px; white-space: nowrap; flex-shrink: 0; width: 52px; }
.scene-beat { color: var(--ink-soft); line-height: 1.6; flex: 1; }
.scene-beat strong { color: var(--ink); margin-right: 4px; }

.legacy-scenes summary { display: flex; align-items: baseline; gap: 10px; cursor: pointer; color: var(--accent); font-size: 12px; }.legacy-scenes summary small { color: var(--warn); font-size: 10px; }.legacy-note { color: var(--ink-soft); font-size: 11px; line-height: 1.65; max-width: 80ch; }.legacy-scene-group { margin-top: 12px; padding-top: 8px; border-top: 1px solid var(--line); }.legacy-scene-group h5 { color: var(--accent); font-size: 11px; margin: 0 0 6px; }
.chapter-plans { display: grid; gap: 10px; }.chapter-plan { border: 1px solid var(--line); border-radius: 10px; padding: 12px 14px; }.chapter-plan header { display: flex; justify-content: space-between; color: var(--accent); font-size: 12px; margin-bottom: 8px; }.chapter-plan header small { color: var(--ink-soft); }.chapter-plan p { color: var(--ink-soft); font-size: 12px; line-height: 1.65; margin: 6px 0; }.chapter-plan p strong { color: var(--ink); margin-right: 8px; font-weight: 600; }.chapter-scenes { margin-top: 10px; border-top: 1px solid var(--line); padding-top: 8px; }.chapter-scenes small { color: var(--accent); }.scene-beat { padding: 8px 0; border-bottom: 1px dashed var(--line); }.scene-beat:last-child { border-bottom: none; }.scene-beat p { margin: 3px 0; font-size: 12px; line-height: 1.6; }.scene-beat p strong { color: var(--accent); font-weight: 600; margin-right: 8px; font-size: 11px; }.scene-beat .scene-head { display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; color: var(--ink-soft); font-size: 11px; }.scene-beat .scene-head strong { color: var(--accent-2); }.scene-beat .scene-info { color: var(--ink-soft); font-size: 11px; }.chapter-scenes-missing { margin: 8px 0 0; color: var(--warn); font-size: 11px; }.legacy-scene-row { padding: 6px 0; border-bottom: 1px dashed var(--line); }.legacy-scene-row:last-child { border-bottom: none; }.legacy-scene-row p { margin: 2px 0; color: var(--ink-soft); font-size: 12px; line-height: 1.6; }.legacy-scene-row p strong { color: var(--accent); font-weight: 600; margin-right: 8px; font-size: 11px; }
html.shell-mobile .contract-fields { grid-template-columns: 1fr; }

</style>
