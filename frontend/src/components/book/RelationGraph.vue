<script setup lang="ts">
import { computed, ref } from 'vue'
import { useAppStore } from '@/stores/app'

const emit = defineEmits<{ open: [stem: string] }>()
const store = useAppStore()
const selected = ref('')

type Relation = { from: string; to: string; type: string; current: string; formal: boolean }
const book = computed(() => store.currentBook)
const cards = computed(() => book.value?.cast || [])
const normalizeRole = (role?: string) => {
  const r = (role || '').toLowerCase()
  if (['protagonist', 'hero', 'main'].includes(r)) return 'protagonist'
  if (['antagonist', 'volume_antagonist', 'villain'].includes(r)) return 'volume_antagonist'
  return 'recurring'
}
const protagonist = computed(() => cards.value.find((c) => normalizeRole(c.role) === 'protagonist'))
const relations = computed<Relation[]>(() => {
  const result: Relation[] = []
  const seen = new Set<string>()
  for (const card of cards.value) {
    const raw = card.raw || ''
    const section = raw.match(/##\s*(?:关系|人物关系)[^\n]*\n([\s\S]*?)(?=\n##\s|$)/)?.[1] || ''
    for (const line of section.split('\n')) {
      if (!line.trim().startsWith('|') || /^\s*\|?\s*:?-+/.test(line)) continue
      const cells = line.trim().replace(/^\||\|$/g, '').split('|').map((s) => s.trim())
      if (cells.length < 2 || ['对方', '角色', '人物'].includes(cells[0])) continue
      const target = cells[0].replace(/[`\s]/g, '')
      if (!cards.value.some((c) => c.stem === target)) continue
      const key = [card.stem, target].sort().join('|')
      if (seen.has(key)) continue
      seen.add(key)
      result.push({ from: card.stem, to: target, type: cells[1], current: cells[2] || '', formal: true })
    }
    if (!protagonist.value || card.stem === protagonist.value.stem) continue
    const text = raw.match(/(?:与主角关系|关键关系)[：:]\s*(.+)/)?.[1]
      || raw.match(/关系钩[：:]\s*(.+)/)?.[1]
    if (text) {
      result.push({ from: card.stem, to: protagonist.value.stem, type: '与主角', current: text, formal: false })
    }
  }
  return result
})
const nodes = computed(() => {
  const center = protagonist.value
  const others = cards.value.filter((c) => c.stem !== center?.stem)
  return [
    ...(center ? [{ card: center, x: 350, y: 260, center: true }] : []),
    ...others.map((card, i) => {
      const angle = -Math.PI / 2 + (i * 2 * Math.PI) / Math.max(1, others.length)
      return { card, x: 350 + Math.cos(angle) * 235, y: 260 + Math.sin(angle) * 185, center: false }
    }),
  ]
})
const nodeByStem = computed(() => new Map(nodes.value.map((n) => [n.card.stem, n])))
const links = computed(() => relations.value.map((r) => ({ ...r, a: nodeByStem.value.get(r.from), b: nodeByStem.value.get(r.to) })).filter((r) => r.a && r.b))
const selectedCard = computed(() => cards.value.find((c) => c.stem === selected.value))
function name(stem: string) {
  return cards.value.find((c) => c.stem === stem)?.name || stem
}
function choose(stem: string) { selected.value = selected.value === stem ? '' : stem }
</script>

<template>
  <section class="relation-view">
    <div class="relation-head">
      <div><h3>人物关系图</h3><p>实线为正式关系表；虚线为人物卡中的文字关系，需补全关系表。</p><p v-if="!protagonist" class="graph-warning">未识别 protagonist 主角，暂按人物总览排列；文字关系暂不连边。</p></div>
      <span>{{ relations.length }} 条关系 · {{ cards.length }} 人物</span>
    </div>
    <div v-if="!cards.length" class="empty">还没有人物卡。</div>
    <div v-else class="graph-layout">
      <div class="graph-canvas">
        <svg viewBox="0 0 700 520" role="img" aria-label="人物关系图">
          <g v-for="(link, i) in links" :key="i">
            <line :x1="link.a!.x" :y1="link.a!.y" :x2="link.b!.x" :y2="link.b!.y" :class="['relation-edge', { informal: !link.formal }]">
              <title>{{ name(link.from) }} → {{ name(link.to) }} · {{ link.current }}</title>
            </line>
          </g>
          <g v-for="node in nodes" :key="node.card.stem" class="graph-node" :class="{ center: node.center, selected: selected === node.card.stem }" @click="choose(node.card.stem)">
            <circle :cx="node.x" :cy="node.y" :r="node.center ? 34 : 26" />
            <text :x="node.x" :y="node.y + (node.center ? 52 : 43)" text-anchor="middle">{{ name(node.card.stem) }}</text>
            <title>{{ node.card.role }}</title>
          </g>
        </svg>
        <div class="legend"><span><i class="solid"></i>正式关系表</span><span><i class="dashed"></i>文字关系</span></div>
      </div>
      <aside class="relation-detail panel">
        <template v-if="selectedCard">
          <h4>{{ name(selectedCard.stem) }}</h4>
          <p v-if="selectedCard.tagline" class="card-tagline">{{ selectedCard.tagline }}</p>
          <button @click="emit('open', selectedCard.stem)">打开人物卡</button>
          <div v-for="r in relations.filter(x => x.from === selected || x.to === selected)" :key="r.from + r.to" class="relation-item">
            <strong>{{ name(r.from) }} ↔ {{ name(r.to) }}</strong><p>{{ r.current }}</p><small>{{ r.formal ? '正式关系表' : '待补关系表' }}</small>
          </div>
        </template>
        <div v-else class="empty"><p>点击人物节点查看关系。</p><p v-if="!relations.length">还没有关系。请在人物卡添加「关系」表，或填写「与主角关系」。</p></div>
      </aside>
    </div>
  </section>
</template>

<style scoped>
.relation-view { display: grid; gap: 16px; }
.relation-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.relation-head h3 { margin: 0 0 4px; }
.relation-head p, .relation-head > span { margin: 0; color: var(--ink-soft); font-size: 12px; }
.relation-head .graph-warning { color: var(--warn); margin-top: 6px; }
.graph-layout { display: grid; grid-template-columns: minmax(0, 1fr) 260px; gap: 16px; align-items: start; }
.graph-canvas { background: var(--panel); border: 1px solid var(--line); border-radius: 18px; overflow: hidden; }
.graph-canvas svg { display: block; width: 100%; height: auto; }
.relation-edge { stroke: var(--accent); stroke-width: 2; opacity: .65; }
.relation-edge.informal { stroke-dasharray: 5 5; opacity: .45; }
.graph-node { cursor: pointer; }
.graph-node circle { fill: #dce9e4; stroke: var(--accent); stroke-width: 2; }
.graph-node.center circle { fill: var(--accent); }
.graph-node.selected circle { stroke: var(--accent-2); stroke-width: 4; }
.graph-node text { fill: var(--ink); font-size: 12px; }
.legend { display: flex; gap: 16px; padding: 12px 16px; color: var(--ink-soft); font-size: 11px; }
.legend span { display: flex; gap: 6px; align-items: center; }
.legend i { display: inline-block; width: 20px; border-top: 2px solid var(--accent); }
.legend i.dashed { border-top-style: dashed; }
.relation-detail { padding: 16px; max-height: 520px; overflow: auto; }
.relation-detail h4 { font-family: var(--font-prose); font-size: 22px; margin: 0 0 6px; }
.card-tagline { margin: 0 0 12px; color: var(--ink-soft); font-size: 12px; line-height: 1.6; }
.relation-detail small { color: var(--ink-soft); }
.relation-item { border-top: 1px solid var(--line); margin-top: 14px; padding-top: 12px; }
.relation-item strong { font-size: 12px; }
.relation-item p { color: var(--ink-soft); font-size: 12px; line-height: 1.6; }
.empty { color: var(--ink-soft); padding: 16px; }
@media (max-width: 800px) { .graph-layout { grid-template-columns: 1fr; } .relation-head { flex-direction: column; } }
</style>
