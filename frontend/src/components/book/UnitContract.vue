<script setup lang="ts">
import { computed, ref } from 'vue'
import { useAppStore } from '@/stores/app'
import type { UnitInfo } from '@/types'

const props = defineProps<{ unit: UnitInfo; compact?: boolean }>()
const expanded = ref(false)
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
  const wt = scalar(r, 'word_target'); if (wt) items.push(`目标 ${wt} 字/章`)
  const day = scalar(r, 'story_day'); if (day && day !== 'null') items.push(`故事第 ${day} 天`)
  if (scalar(r, 'flashback') === 'true') items.push('闪回')
  const hook = section(r, 'next_hook').match(/type:\s*(.*)/)?.[1]?.trim().replace(/^['"]|['"]$/g, '')
  if (hook) items.push(`下一钩：${hook}`)
  return items
})
// 衔接说明是长文本，独立成段，不塞进元数据 chip。
const gapFromPrev = computed(() => scalar(raw.value, 'gap_from_prev'))
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

type Scene = { id: string; chapter: string; beat: string; who: string; where: string }
const scenes = computed<Scene[]>(() => {
  const text = section(raw.value, 'scenes')
  if (!text.trim()) return []
  const groups: string[][] = []
  let cur: string[] = []
  for (const line of text.split('\n')) {
    if (/^\s+-\s/.test(line)) { if (cur.length) groups.push(cur); cur = [line] }
    else if (cur.length) cur.push(line)
  }
  if (cur.length) groups.push(cur)
  return groups.map((g) => {
    const s = g.join('\n')
    const val = (k: string) => s.match(new RegExp(`(?:^|\\s)${k}:\\s*(.*)$`))?.[1]?.trim().replace(/^['"]|['"]$/g, '') || ''
    return { id: val('id') || val('scene'), chapter: val('chapter'), beat: val('beat') || val('scene'), who: val('who'), where: val('where') }
  })
})
const visibleScenes = computed(() => (props.compact && !expanded.value ? scenes.value.slice(0, 3) : scenes.value))
const hasContent = computed(() => fields.value.length > 0 || scenes.value.length > 0)
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
      <div v-if="scenes.length" class="contract-scenes">
        <small class="scenes-label">场面节拍 · {{ scenes.length }}</small>
        <div v-for="(s, i) in visibleScenes" :key="i" class="scene-row">
          <span class="scene-ch">第 {{ s.chapter || '?' }} 章</span>
          <span class="scene-beat"><strong v-if="s.id">{{ s.id }}</strong> {{ s.beat }}</span>
          <span v-if="s.where" class="scene-where">{{ s.where }}</span>
        </div>
        <button v-if="compact && scenes.length > 3" class="scenes-toggle" @click="expanded = !expanded">
          {{ expanded ? '收起场面' : `展开全部 ${scenes.length} 个场面` }}
        </button>
      </div>
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
.scenes-toggle { margin-top: 8px; border: none; background: transparent; color: var(--accent); font-size: 12px; padding: 4px 0; }
@media (max-width: 720px) { .contract-fields { grid-template-columns: 1fr; } .scene-where { display: none; } }
</style>
