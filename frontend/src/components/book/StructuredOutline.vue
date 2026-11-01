<script setup lang="ts">
import { computed, ref } from 'vue'
import type { MarkdownEntity, StructuredMarkdown, UnitInfo } from '@/types'

const props = defineProps<{
  document: StructuredMarkdown
  units?: UnitInfo[]
  compact?: boolean
  saving?: boolean
  readOnly?: boolean
}>()
const emit = defineEmits<{
  editField: [payload: { kind: string; entityId: string; field: string; value: string; label: string }]
  openUnit: [unit: UnitInfo]
}>()
const expanded = ref<Set<string>>(new Set())
const book = computed(() => props.document.protocol === 'book-outline/v1')
const roots = computed(() => props.document.entities.filter((e) => ['故事线', '承诺', '约束'].includes(e.kind)))
const arcs = computed(() => props.document.entities.filter((e) => e.kind === '分卷'))
const beats = computed(() => props.document.entities.filter((e) => ['节拍', '转折'].includes(e.kind)))
const seeds = computed(() => props.document.entities.filter((e) => e.kind === '单元'))
const handoff = computed(() => props.document.entities.find((e) => e.kind === '卷末交接'))
const rootFields = computed(() => props.document.root.fields.filter((f) => !['schema_version', 'book_id', 'volume_id'].includes(f.name)))

const labels: Record<string, string> = {
  title: '标题', logline: '一句话故事', purpose: '结构职责', goal: '卷级问题', dramatic_question: '卷级问题',
  chapter_range: '章范围', central_conflict: '核心冲突', stakes: '失败代价', entry_state: '开卷状态', exit_state: '卷末状态',
  description: '描述', kind: '类型', opening_state: '起点', ending_state: '终点', thread_moves: '故事线迁移',
  deliverables: '结果兑现', open_questions: '保留问题', next_volume_entry: '下一卷入口', cast: '本卷人物',
  objective: '目标', obstacle: '阻碍', attempt: '尝试', setback: '方法缺口', choice: '关键选择', cost: '代价',
  outcome: '不可逆结果', entry_cause: '前置原因', exit_result: '离场结果', state_changes: '状态迁移',
  thread_ids: '故事线', event_ids: '关联事件', reveal_ids: '可揭示', forbidden_reveal_ids: '禁止提前揭示', next_hook: '下一钩',
  event: '事件', cause: '原因', effect: '影响', chapter_window: '落点范围',
  clock_start_day: '开场日', clock_end_day: '结束日', clock_gap_days: '最小间隔', clock_deadline: '关键死线', timeline_id: '时间线',
}
function fieldLabel(key: string) { return labels[key] || key }
function display(entity: MarkdownEntity, key: string) { return entity.fields.find((f) => f.name === key)?.value || '' }
function edit(entity: MarkdownEntity | null, field: string, value: string) {
  if (props.readOnly) return
  emit('editField', { kind: entity?.kind || 'root', entityId: entity?.id || '', field, value, label: `${entity?.id || '总纲'} · ${fieldLabel(field)}` })
}
function toggle(id: string) {
  const next = new Set(expanded.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  expanded.value = next
}
function unitFor(id: string) { return props.units?.find((u) => u.unitId === id) }
function clockLabel(entity: MarkdownEntity) {
  const start = display(entity, 'clock_start_day')
  const end = display(entity, 'clock_end_day')
  if (!start || !end) return ''
  const gap = display(entity, 'clock_gap_days') || '0'
  return `故事第 ${start}–${end} 日 · 间隔≥${gap}日`
}
function nextAction(id: string) {
  const unit = unitFor(id)
  if (!unit) return '待生成细纲'
  return ({ proposed: '生成细纲', accepted: '写正文', drafted: '审校正文', reviewed: '查看定稿' } as Record<string, string>)[unit.status] || '进入生产'
}
function status(id: string) {
  const value = unitFor(id)?.status || 'proposed'
  return ({ proposed: '待细纲', accepted: '待写', drafted: '草稿', reviewed: '定稿' } as Record<string, string>)[value] || value
}
</script>

<template>
  <div class="structured-outline" :class="{ compact }">
    <header class="outline-header">
      <div><small>{{ book ? '全书结构' : '本卷结构' }}</small><h3>{{ document.title }}</h3></div>
      <span class="protocol">{{ document.protocol }}</span>
    </header>
    <div v-if="document.diagnostics?.length" class="diagnostics">
      <strong>结构错误</strong><p v-for="d in document.diagnostics" :key="`${d.path}-${d.line}`">第 {{ d.line }} 行 · {{ d.path }}：{{ d.message }}</p>
    </div>
    <section class="root-contract">
      <button v-for="field in rootFields" :key="field.name" class="contract-field" :disabled="readOnly" @click="edit(null, field.name, field.value)">
        <small>{{ fieldLabel(field.name) }}</small><span>{{ field.value }}</span><i v-if="!readOnly">编辑</i>
      </button>
    </section>

    <section v-if="roots.length" class="entity-section">
      <h4>读者承诺与故事线</h4>
      <div class="thread-grid">
        <article v-for="entity in roots" :key="entity.id" class="thread-card">
          <header><strong>{{ entity.id }}</strong><span>{{ display(entity, 'kind') }}</span></header>
          <p>{{ display(entity, 'description') }}</p>
          <div class="state-transition"><span>{{ display(entity, 'opening_state') }}</span><b>→</b><span>{{ display(entity, 'ending_state') }}</span></div>
          <button class="text-action" :disabled="readOnly" @click="edit(entity, 'description', display(entity, 'description'))">编辑故事线</button>
        </article>
      </div>
    </section>

    <section v-if="arcs.length" class="entity-section">
      <h4>分卷契约</h4>
      <div class="arc-list">
        <article v-for="arc in arcs" :key="arc.id" class="arc-card">
          <header><div><small>{{ arc.id }} · {{ display(arc, 'chapter_range') }}</small><h4>{{ display(arc, 'title') }}</h4></div><span>{{ display(arc, 'dramatic_question') }}</span></header>
          <p class="purpose">{{ display(arc, 'purpose') }}</p>
          <div class="arc-state"><div><small>开卷</small>{{ display(arc, 'entry_state') }}</div><b>→</b><div><small>卷末</small>{{ display(arc, 'exit_state') }}</div></div>
          <dl><div><dt>结果兑现</dt><dd>{{ display(arc, 'deliverables') }}</dd></div><div><dt>保留问题</dt><dd>{{ display(arc, 'open_questions') }}</dd></div></dl>
          <footer><button class="text-action" :disabled="readOnly" @click="edit(arc, 'purpose', display(arc, 'purpose'))">编辑卷职责</button><button class="text-action" :disabled="readOnly" @click="edit(arc, 'deliverables', display(arc, 'deliverables'))">编辑兑现</button></footer>
        </article>
      </div>
    </section>

    <section v-if="beats.length" class="entity-section">
      <h4>卷级节拍</h4>
      <div class="beat-list"><article v-for="beat in beats" :key="beat.id"><small>{{ beat.id }} · {{ display(beat, 'chapter_window') }}</small><strong>{{ display(beat, 'event') }}</strong><p>{{ display(beat, 'cause') }} → {{ display(beat, 'effect') }}</p></article></div>
    </section>

    <section v-if="seeds.length" class="entity-section">
      <div class="unit-index-heading"><div><h4>单元索引 <span>{{ seeds.length }}</span></h4><p>每个单元是一个因果闭环；时间是索引的合同字段。点击卡片展开，或按下一步进入生产。</p></div><small>目标 → 选择 → 结果</small></div>
      <div class="seed-list">
        <article v-for="seed in seeds" :key="seed.id" class="seed-card">
          <div class="seed-summary">
            <button class="seed-main" @click="toggle(seed.id)">
              <span class="seed-title"><small>{{ seed.id }} · {{ display(seed, 'chapter_range') }}</small><strong>{{ display(seed, 'title') }}</strong></span>
              <span class="seed-goal"><small>目标</small>{{ display(seed, 'objective') }}</span>
              <span class="seed-choice"><small>选择</small>{{ display(seed, 'choice') }}</span>
              <span class="seed-outcome"><small>主结果</small>{{ display(seed, 'outcome') }}</span>
              <span class="seed-clock"><small>剧情时钟</small>{{ clockLabel(seed) || '待锚定' }}</span>
            </button>
            <div class="seed-actions"><span class="unit-status">{{ status(seed.id) }}</span><button v-if="unitFor(seed.id)" @click="emit('openUnit', unitFor(seed.id)!)">{{ nextAction(seed.id) }}</button><button v-else disabled>待播种</button></div>
          </div>
          <div v-if="expanded.has(seed.id)" class="seed-detail">
            <div class="causal-chain"><span>{{ display(seed, 'entry_cause') }}</span><b>→</b><span>{{ display(seed, 'objective') }}</span><b>→</b><span>{{ display(seed, 'choice') }}</span><b>→</b><span>{{ display(seed, 'outcome') }}</span></div>
            <dl><div><dt>阻碍</dt><dd>{{ display(seed, 'obstacle') }}</dd></div><div><dt>尝试与缺口</dt><dd>{{ display(seed, 'attempt') }}；{{ display(seed, 'setback') }}</dd></div><div><dt>代价</dt><dd>{{ display(seed, 'cost') }}</dd></div><div><dt>状态迁移</dt><dd>{{ display(seed, 'state_changes') }}</dd></div><div><dt>下一钩</dt><dd>{{ display(seed, 'next_hook') }}</dd></div><div v-if="display(seed, 'clock_deadline')"><dt>关键死线</dt><dd>{{ display(seed, 'clock_deadline') }}</dd></div></dl>
            <footer><button class="text-action" :disabled="readOnly" @click="edit(seed, 'outcome', display(seed, 'outcome'))">编辑结果</button><button v-if="unitFor(seed.id)" class="text-action" @click="emit('openUnit', unitFor(seed.id)!)">进入单元生产</button></footer>
          </div>
        </article>
      </div>
    </section>
    <section v-if="handoff" class="handoff"><small>卷末交接</small><p>{{ display(handoff, 'deliverables') }}</p><p>{{ display(handoff, 'next_volume_entry') }}</p></section>
  </div>
</template>

<style scoped>
.structured-outline { display: grid; gap: 22px; }
.outline-header { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; }
.outline-header small, .contract-field small, .arc-state small, .handoff small { display: block; color: var(--accent); font-size: 11px; margin-bottom: 4px; }
.outline-header h3 { margin: 0; font-family: var(--font-prose); font-size: 23px; }
.protocol { font: 10px ui-monospace, monospace; color: var(--ink-soft); white-space: nowrap; }
.root-contract { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.contract-field { position: relative; text-align: left; border: 1px solid var(--line); border-radius: 12px; background: rgba(255,255,255,.45); padding: 12px 14px; }
.contract-field span { color: var(--ink-soft); font-size: 13px; line-height: 1.75; white-space: pre-wrap; }
.contract-field i { position: absolute; top: 10px; right: 12px; opacity: 0; color: var(--accent); font-size: 10px; font-style: normal; }
.contract-field:hover i { opacity: 1; }
.entity-section > h4 { margin: 0 0 10px; font-size: 14px; }
.entity-section > h4 span { color: var(--ink-soft); font-size: 11px; font-weight: 400; }
.thread-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(230px, 1fr)); gap: 12px; }
.thread-card, .arc-card { border: 1px solid var(--line); border-radius: 14px; padding: 15px; background: rgba(255,255,255,.4); }
.thread-card header, .arc-card header { display: flex; justify-content: space-between; align-items: flex-start; gap: 10px; }
.thread-card header span { color: var(--ink-soft); font-size: 10px; }
.thread-card p, .arc-card p { color: var(--ink-soft); font-size: 12px; line-height: 1.7; }
.state-transition, .arc-state { display: grid; grid-template-columns: 1fr auto 1fr; gap: 10px; align-items: center; color: var(--ink-soft); font-size: 12px; line-height: 1.65; }
.state-transition b, .arc-state b { color: var(--accent); }
.text-action { border: 0; background: transparent; color: var(--accent); padding: 8px 0 0; font-size: 11px; }
.arc-list { display: grid; gap: 12px; }
.arc-card header h4 { margin: 3px 0 0; font-family: var(--font-prose); font-size: 19px; }
.arc-card header small { color: var(--accent); }
.arc-card header > span { max-width: 45%; color: var(--ink-soft); font-size: 12px; line-height: 1.7; }
.arc-state { padding: 12px; background: rgba(15,107,92,.045); border-radius: 10px; }
dl { display: grid; gap: 9px; margin: 12px 0 0; }
dl div { display: grid; grid-template-columns: 86px 1fr; gap: 10px; }
dt { color: var(--accent); font-size: 11px; } dd { margin: 0; color: var(--ink-soft); font-size: 12px; line-height: 1.7; white-space: pre-wrap; }
.arc-card footer, .seed-detail footer { display: flex; gap: 18px; }
.beat-list { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 10px; }
.beat-list article { border-left: 2px solid var(--accent); padding: 4px 12px; }
.beat-list small { display: block; color: var(--ink-soft); margin-bottom: 5px; }
.beat-list strong { font-size: 12px; } .beat-list p { color: var(--ink-soft); font-size: 12px; line-height: 1.65; }
.seed-list { display: grid; gap: 8px; }
.seed-card { border: 1px solid var(--line); border-radius: 12px; overflow: hidden; }
.unit-index-heading { display: flex; justify-content: space-between; align-items: flex-end; gap: 16px; margin-bottom: 12px; }.unit-index-heading h4 { margin: 0; }.unit-index-heading h4 span { color: var(--ink-soft); font-size: 11px; font-weight: 400; }.unit-index-heading p { margin: 5px 0 0; color: var(--ink-soft); font-size: 11px; line-height: 1.65; }.unit-index-heading > small { color: var(--accent); white-space: nowrap; }
.seed-summary { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: stretch; background: rgba(255,255,255,.35); }
.seed-main { display: grid; grid-template-columns: minmax(135px, .7fr) minmax(160px, 1.15fr) minmax(160px, 1.15fr) minmax(160px, 1.1fr) minmax(155px, .85fr); gap: 14px; padding: 14px 16px; border: 0; text-align: left; background: transparent; min-width: 0; }
.seed-main > span { min-width: 0; font-size: 12px; line-height: 1.65; color: var(--ink-soft); }
.seed-main small { display: block; color: var(--accent); font-size: 10px; margin-bottom: 4px; }
.seed-title small { color: var(--accent); }.seed-title strong { font-size: 14px; color: var(--ink); }
.seed-clock { border-left: 1px solid var(--line); padding-left: 14px; }
.seed-actions { display: flex; flex-direction: column; justify-content: center; align-items: flex-end; gap: 8px; padding: 12px 14px; border-left: 1px solid var(--line); min-width: 94px; }
.seed-actions button { font-size: 11px; white-space: nowrap; padding: 5px 9px; }
.seed-summary small { display: block; color: var(--accent); font-size: 10px; margin-bottom: 4px; }
.seed-summary strong { font-size: 14px; } .seed-outcome { color: var(--ink-soft); font-size: 12px; line-height: 1.65; }
.unit-status { font-size: 10px; color: var(--accent); white-space: nowrap; }
.seed-detail { padding: 15px; border-top: 1px solid var(--line); }
.causal-chain { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; color: var(--ink-soft); font-size: 12px; line-height: 1.65; }
.causal-chain b { color: var(--accent); }
.handoff { padding: 14px; background: rgba(15,107,92,.05); border-radius: 12px; }
.handoff p { color: var(--ink-soft); font-size: 12px; }
.diagnostics { padding: 14px; border: 1px solid var(--warn); border-radius: 12px; color: var(--warn); font-size: 12px; }
.compact { max-height: 620px; overflow: auto; padding-right: 8px; }
@media (max-width: 1050px) { .seed-main { grid-template-columns: repeat(2, minmax(0, 1fr)); } .seed-title { grid-column: 1 / -1; } .seed-clock { border-left: 0; padding-left: 0; } }
@media (max-width: 720px) { .root-contract { grid-template-columns: 1fr; } .arc-card header { flex-direction: column; } .arc-card header > span { max-width: none; } .seed-summary { grid-template-columns: 1fr; } .seed-main { grid-template-columns: 1fr; } .seed-actions { flex-direction: row; justify-content: flex-start; align-items: center; border-left: 0; border-top: 1px solid var(--line); } .arc-state { grid-template-columns: 1fr; } .arc-state b { transform: rotate(90deg); } .unit-index-heading { align-items: flex-start; flex-direction: column; } }
</style>
