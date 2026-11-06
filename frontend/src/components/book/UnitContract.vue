<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '@/stores/app'
import type { UnitInfo } from '@/types'

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
type Scene = { id: string; chapter: string; beat: string; who: string; where: string; goal: string; conflict: string; turn: string; outcome: string; info: string }

function parseChapters(text: string): ChapterView[] {
  const body = text.trim()
  if (!body || body === '[]') return []
  const chapters: ChapterView[] = []
  let current: ChapterView | null = null
  let currentScene: Scene | null = null
  for (const line of body.split('\n')) {
    const top = line.match(/^\s*-\s+chapter:\s*(\d+)/)
    const nested = line.match(/^\s+chapter:\s*(\d+)/)
    const field = line.match(/^\s+(goal|change|cut_hook|day_offset):\s*(.*)$/)
    if (top) {
      current = { chapter: top[1], goal: '', change: '', cutHook: '', dayOffset: '0', scenes: [] }
      chapters.push(current)
      currentScene = null
      continue
    }
    if (!current) continue
    if (field) {
      if (field[1] === 'goal') current.goal = field[2]
      if (field[1] === 'change') current.change = field[2]
      if (field[1] === 'cut_hook') current.cutHook = field[2]
      if (field[1] === 'day_offset') current.dayOffset = field[2]
      continue
    }
    if (nested) {
      currentScene = { id: '', chapter: nested[1], beat: '', who: '', where: '', goal: '', conflict: '', turn: '', outcome: '', info: '' }
      current.scenes.push(currentScene)
      continue
    }
    if (/^\s+goal:/.test(line) && currentScene) {
      const v = line.replace(/^\s+goal:\s*/, '').trim()
      currentScene.goal = v
      if (!currentScene.beat) currentScene.beat = v
      continue
    }
    if (/^\s+conflict:/.test(line) && currentScene) { currentScene.conflict = line.replace(/^\s+conflict:\s*/, '').trim(); continue }
    if (/^\s+turn:/.test(line) && currentScene) { currentScene.turn = line.replace(/^\s+turn:\s*/, '').trim(); continue }
    if (/^\s+outcome:/.test(line) && currentScene) { currentScene.outcome = line.replace(/^\s+outcome:\s*/, '').trim(); continue }
    if (/^\s+(must_land|reveal|withhold):/.test(line) && currentScene) {
      currentScene.info = (currentScene.info ? currentScene.info + ' · ' : '') + line.trim()
      continue
    }
  }
  return chapters
}

const chapterPlans = computed(() => parseChapters(section(raw.value, 'chapters')))
/** 解析 scenes：支持 `- id:/chapter:` 列表，也支持扁平重复的 chapter/beat/who 块。 */
function parseScenes(text: string): Scene[] {
  const body = text.trim()
  if (!body || body === '[]') return []
  const groups: string[][] = []
  let cur: string[] = []
  const flush = () => { if (cur.length) { groups.push(cur); cur = [] } }
  // who 下的人物 stem（无冒号），不是场面条目。
  const isWhoStem = (line: string) => /^\s+-\s+[a-z][a-z0-9-]*\s*$/i.test(line)
  // 场面顶层键（行首，可带 list dash）；勿用全局 /chapter/ 以免误伤 beat 正文。
  const topKey = (line: string) => line.match(/^\s*(?:-\s+)?(id|scene|chapter|beat|purpose)\s*:/i)?.[1]?.toLowerCase() || ''
  const hasTopKey = (lines: string[], key: string) => lines.some((l) => topKey(l) === key)

  for (const line of body.split('\n')) {
    if (!line.trim()) continue
    if (isWhoStem(line)) {
      if (cur.length) cur.push(line)
      continue
    }
    const key = topKey(line)
    // 同一场面不应重复 chapter/id/beat；再出现则开启下一场面（扁平格式）。
    if (key && hasTopKey(cur, key)) {
      flush()
      cur = [line]
      continue
    }
    // 列表项 `- id:` / `- chapter:` 也开启新场面。
    if (key && /^\s+-\s+/.test(line) && cur.length) {
      flush()
      cur = [line]
      continue
    }
    if (cur.length) cur.push(line)
    else if (key || /^\s+(?:when|where|who|pov)\s*:/i.test(line)) cur = [line]
  }
  flush()

  return groups.map((g) => {
    const s = g.map((l) => l.replace(/^\s+-\s+/, '  ')).join('\n')
    const val = (k: string) => s.match(new RegExp(`^\\s*${k}\\s*:\\s*(.*)$`, 'im'))?.[1]?.trim().replace(/^['"]|['"]$/g, '') || ''
    const whoLines = g.filter(isWhoStem).map((l) => l.replace(/^\s+-\s+/, '').trim())
    const whoInline = val('who')
    // must_land/reveal/withhold 是多行数组或单行列表，单行 val() 只能抓到 "["，
    // 交给章合同解析（parseChapters）展示，这里不重复。
    const raw = g.join('\n')
    const info = ['must_land', 'reveal', 'withhold']
      .map((k) => {
        const m = raw.match(new RegExp(`^\\s+${k}:\\s*\\[(.*)\\]`, 'm'))
        return m ? `${k}: [${m[1]}]` : ''
      })
      .filter(Boolean)
      .join(' · ')
    return {
      id: val('id') || val('scene'),
      chapter: val('chapter'),
      beat: val('beat') || val('purpose'),
      who: whoLines.length ? whoLines.join('、') : whoInline,
      where: val('where') || val('when'),
      goal: val('goal'),
      conflict: val('conflict'),
      turn: val('turn'),
      outcome: val('outcome'),
      info,
    }
  }).filter((s) => s.chapter || s.beat || s.id)
}
const scenes = computed<Scene[]>(() => parseScenes(section(raw.value, 'scenes')))
const sceneGroups = computed(() => {
  const groups: { chapter: string; scenes: Scene[] }[] = []
  for (const scene of scenes.value) {
    let group = groups.find((g) => g.chapter === scene.chapter)
    if (!group) {
      group = { chapter: scene.chapter, scenes: [] }
      groups.push(group)
    }
    group.scenes.push(scene)
  }
  return groups
})
const hasContent = computed(() => fields.value.length > 0 || chapterPlans.value.length > 0 || scenes.value.length > 0)
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
              <p v-if="s.goal"><strong>目标</strong>{{ s.goal }}</p>
              <p v-if="s.conflict"><strong>阻力</strong>{{ s.conflict }}</p>
              <p v-if="s.turn"><strong>转折</strong>{{ s.turn }}</p>
              <p v-if="s.outcome"><strong>结果</strong>{{ s.outcome }}</p>
              <p v-if="s.info" class="scene-info">{{ s.info }}</p>
            </div>
          </div>
        </article>
      </div>
      <details v-if="!chapterPlans.length && sceneGroups.length" class="contract-scenes legacy-scenes">
        <summary><span>行动参考 · {{ scenes.length }} 个场面</span><small>旧格式，非章合同</small></summary>
        <p class="legacy-note">当前单元仍是旧场面表。章合同需在后续 Agent 迁移中生成；以下仅作为行动参考，不代表每章的目标、变化与断章钩。</p>
        <div v-for="group in sceneGroups" :key="group.chapter" class="legacy-scene-group">
          <h5>第 {{ group.chapter || '?' }} 章</h5>
          <div v-for="(s, i) in group.scenes" :key="i" class="scene-row">
            <span class="scene-beat"><strong v-if="s.id">{{ s.id }}</strong>{{ s.beat || '（无行动说明）' }}</span>
            <span v-if="s.where" class="scene-where">{{ s.where }}</span>
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
.scene-row { display: flex; align-items: baseline; gap: 12px; padding: 8px 0; border-bottom: 1px dashed var(--line); font-size: 13px; }
.scene-row:last-of-type { border-bottom: none; }
.scene-ch { color: var(--accent); font-size: 11px; white-space: nowrap; flex-shrink: 0; width: 52px; }
.scene-beat { color: var(--ink-soft); line-height: 1.6; flex: 1; }
.scene-beat strong { color: var(--ink); margin-right: 4px; }
.scene-where { color: var(--ink-soft); font-size: 11px; flex-shrink: 0; }
.legacy-scenes summary { display: flex; align-items: baseline; gap: 10px; cursor: pointer; color: var(--accent); font-size: 12px; }.legacy-scenes summary small { color: var(--warn); font-size: 10px; }.legacy-note { color: var(--ink-soft); font-size: 11px; line-height: 1.65; max-width: 80ch; }.legacy-scene-group { margin-top: 12px; padding-top: 8px; border-top: 1px solid var(--line); }.legacy-scene-group h5 { color: var(--accent); font-size: 11px; margin: 0 0 6px; }
.chapter-plans { display: grid; gap: 10px; }.chapter-plan { border: 1px solid var(--line); border-radius: 10px; padding: 12px 14px; }.chapter-plan header { display: flex; justify-content: space-between; color: var(--accent); font-size: 12px; margin-bottom: 8px; }.chapter-plan header small { color: var(--ink-soft); }.chapter-plan p { color: var(--ink-soft); font-size: 12px; line-height: 1.65; margin: 6px 0; }.chapter-plan p strong { color: var(--ink); margin-right: 8px; font-weight: 600; }.chapter-scenes { margin-top: 10px; border-top: 1px solid var(--line); padding-top: 8px; }.chapter-scenes small { color: var(--accent); }.scene-beat { padding: 6px 0; border-bottom: 1px dashed var(--line); }.scene-beat:last-child { border-bottom: none; }.scene-beat p { margin: 3px 0; font-size: 12px; line-height: 1.6; }.scene-beat p strong { color: var(--accent); font-weight: 600; margin-right: 8px; font-size: 11px; }.scene-beat .scene-info { color: var(--ink-soft); font-size: 11px; }
@media (max-width: 720px) { .contract-fields { grid-template-columns: 1fr; } .scene-where { display: none; } }
</style>
