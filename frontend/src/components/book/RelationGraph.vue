<script setup lang="ts">
import { computed, ref } from 'vue'
import { useAppStore } from '@/stores/app'
import { isProtagonist, parseCastRelations } from '@/utils/castRelations'

const emit = defineEmits<{ open: [stem: string] }>()
const store = useAppStore()
const selected = ref('')

const book = computed(() => store.currentBook)
const cards = computed(() => book.value?.cast || [])
const protagonist = computed(() => cards.value.find((c) => isProtagonist(c.role)))
const relations = computed(() => parseCastRelations(cards.value))
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
      <div>
        <h3>人物关系图</h3>
        <p>实线为正式「## 关系」表（对方填 stem）；虚线为关系锚点 / 与主角关系等文字关系。</p>
        <p v-if="!protagonist" class="graph-warning">未识别 protagonist 主角，暂按人物总览排列。</p>
      </div>
      <span>{{ relations.length }} 条关系 · {{ cards.length }} 人物</span>
    </div>
    <div v-if="!cards.length" class="empty">还没有人物卡。</div>
    <div v-else class="graph-layout">
      <div class="graph-canvas">
        <svg viewBox="0 0 700 520" role="img" aria-label="人物关系图">
          <g v-for="(link, i) in links" :key="i">
            <line :x1="link.a!.x" :y1="link.a!.y" :x2="link.b!.x" :y2="link.b!.y" :class="['relation-edge', { informal: !link.formal }]">
              <title>{{ name(link.from) }} → {{ name(link.to) }} · {{ link.current || link.type }}</title>
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
      <!--
        手机屏幕放不下 700×520 的力导向图：等比缩到 378px 后节点文字只剩 6px，
        名字全部糊成一团。窄屏改为可读的人物列表，选中后仍走右侧/下方详情。
      -->
      <ul class="cast-graph-list">
        <li v-for="card in cards" :key="card.stem">
          <button :class="{ selected: selected === card.stem }" @click="choose(card.stem)">
            <span class="cast-name">{{ name(card.stem) }}<i v-if="isProtagonist(card.role)" class="cast-badge">主角</i></span>
            <small>{{ relations.filter((r) => r.from === card.stem || r.to === card.stem).length }} 条关系</small>
          </button>
        </li>
      </ul>
      <aside class="relation-detail panel">
        <template v-if="selectedCard">
          <h4>{{ name(selectedCard.stem) }}</h4>
          <p v-if="selectedCard.tagline" class="card-tagline">{{ selectedCard.tagline }}</p>
          <button @click="emit('open', selectedCard.stem)">打开人物卡</button>
          <div v-for="r in relations.filter(x => x.from === selected || x.to === selected)" :key="r.from + r.to" class="relation-item">
            <strong>{{ name(r.from) }} ↔ {{ name(r.to) }}</strong>
            <p>{{ r.type }}<template v-if="r.current"> · {{ r.current }}</template></p>
            <small>{{ r.formal ? '正式关系表' : '文字关系' }}</small>
          </div>
        </template>
        <div v-else class="empty">
          <p>点击人物节点查看关系。</p>
          <p v-if="!relations.length">还没有关系。请在人物卡添加「## 关系」表（对方填 stem），或写「关系锚点 / 与主角关系」。</p>
        </div>
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
@media (max-width: 800px) { .graph-layout { grid-template-columns: minmax(0, 1fr); } .relation-head { flex-direction: column; } }
.cast-graph-list { display: none; }
@media (max-width: 800px) {
  .graph-canvas { display: none; }
  .cast-graph-list { display: grid; gap: 8px; margin: 0; padding: 0; list-style: none; }
  .cast-graph-list button {
    display: flex; align-items: center; justify-content: space-between; gap: 10px;
    width: 100%; padding: 12px 14px; text-align: left; font-size: 14px;
  }
  .cast-graph-list button.selected { border-color: var(--accent); color: var(--accent); }
  .cast-graph-list .cast-name { display: inline-flex; align-items: center; gap: 6px; font-weight: 600; }
  .cast-badge {
    font-style: normal; font-size: 10px; font-weight: 600; padding: 1px 6px; border-radius: 6px;
    background: rgba(15,107,92,.12); color: var(--accent);
  }
  .cast-graph-list small { color: var(--ink-soft); font-size: 12px; flex-shrink: 0; }
  .relation-detail { max-height: none; }
}
</style>
