<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '@/stores/app'
import type { UnitInfo } from '@/types'

const props = defineProps<{ volume: number }>()
const emit = defineEmits<{ open: [unit: UnitInfo] }>()
const store = useAppStore()
const book = computed(() => store.currentBook)
const units = computed(() => (book.value?.units || []).filter((u) => u.volume === props.volume))

function scalar(raw: string, key: string) {
  const v = raw.match(new RegExp(`^${key}:\\s*(.*)$`, 'm'))?.[1]?.trim() || ''
  // 去掉行内 YAML 注释（"  # ..."）再剥引号。
  return v.replace(/\s+#.*$/, '').replace(/^['"]|['"]$/g, '').trim()
}
const events = computed(() => units.value.map((u) => {
  const raw = u.outlineRaw || ''
  const range = raw.match(/chapter_range:\s*(?:\[(\d+),\s*(\d+)\]|\n\s*-\s*(\d+)\n\s*-\s*(\d+))/)
  const unit = u.chapterFrom > 0 || !range ? u : { ...u, chapterFrom: Number(range[1] || range[3]), chapterTo: Number(range[2] || range[4]) }
  return {
    unit,
    day: scalar(raw, 'story_day'),
    // gap_days 才是天数；gap_from_prev 是承接说明长文本，纯数字是历史字段写错的数据。
    gap: gapLabel(scalar(raw, 'gap_days'), scalar(raw, 'gap_from_prev')),
    label: scalar(raw, 'time_label'),
    flashback: scalar(raw, 'flashback') === 'true',
    title: u.title || scalar(raw, 'title_working') || '未完成单元',
    summary: scalar(raw, 'function'),
    missing: !scalar(raw, 'story_day') && !scalar(raw, 'time_label'),
  }
}))
/**
 * 卡片页脚的间隔标记。只显示能读懂的东西：结构化 gap_days 优先；没有时只在
 * gap_from_prev 是纯数字（历史数据把天数写进了这个字段）时当作间隔，其余情况
 * 一律不显示——把「衔接说明 1」这种半截话铺在时间轴上只会误导。
 */
function gapLabel(gapDays: string, gapFromPrev: string) {
  const days = gapDays || (/^\d+$/.test(gapFromPrev.trim()) ? gapFromPrev : '')
  if (!days) return ''
  return Number(days) === 0 ? '紧接上一单元' : `间隔 ${days} 天`
}
function dayLabel(e: any) {
  // 优先 story_day（短、机读）；time_label 仅作短标签补充，长说明不进 marker。
  if (e.day && e.day !== 'null') return `故事第 ${e.day} 天`
  if (e.label && [...e.label].length <= 24) return e.label
  return '时间待补'
}
function unitStatus(status: string) {
  return ({ proposed: '待细纲', accepted: '待写', drafted: '草稿', reviewed: '已审校', finalized: '定稿' } as Record<string, string>)[status] || status
}
</script>

<template>
  <section class="timeline-view">
    <div class="timeline-head">
      <div><h3>单元剧情时钟</h3><p>单元索引的时间视角：story_day / gap_from_prev / flashback，不虚构日期。点击单元跳到写作页。</p></div>
    </div>
    <div v-if="!events.length" class="empty">当前卷还没有单元。</div>
    <div v-else class="timeline">
      <article v-for="(event, i) in events" :key="event.unit.unitId" class="timeline-event" :class="{ missing: event.missing, flashback: event.flashback }">
        <div class="time-marker"><span>{{ dayLabel(event) }}</span><i></i></div>
        <button class="event-card" @click="emit('open', event.unit)">
          <div class="event-top"><strong>{{ event.unit.unitId }}</strong><span>{{ unitStatus(event.unit.status) }}</span></div>
          <h4>{{ event.title }}</h4><p>{{ event.summary }}</p>
          <footer><span>第 {{ event.unit.chapterFrom }}–{{ event.unit.chapterTo }} 章</span><span v-if="event.gap">{{ event.gap }}</span><span v-if="event.flashback">闪回</span></footer>
        </button>
      </article>
    </div>
    <p class="timeline-note">灰色节点表示单元剧情时钟字段待补；琥珀色表示闪回。点击单元进入细纲编辑。</p>
  </section>
</template>

<style scoped>
.timeline-view { display: grid; gap: 18px; }
.timeline-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.timeline-head h3 { margin: 0 0 4px; }
.timeline-head p { margin: 0; color: var(--ink-soft); font-size: 12px; }
.timeline-head select { padding: 8px 10px; border: 1px solid var(--line); border-radius: 10px; background: white; }
.timeline { position: relative; display: grid; gap: 0; }
.timeline::before { content: ''; position: absolute; left: 126px; top: 18px; bottom: 18px; width: 2px; background: rgba(15,107,92,.18); }
.timeline-event { display: grid; grid-template-columns: 112px minmax(0, 1fr); gap: 30px; position: relative; padding-bottom: 16px; }
.time-marker { text-align: right; padding-top: 18px; position: relative; }
.time-marker span { color: var(--accent); font-size: 12px; font-weight: 600; }
.time-marker i { position: absolute; right: -24px; top: 22px; width: 12px; height: 12px; border-radius: 50%; background: var(--accent); border: 3px solid var(--paper); }
.timeline-event.missing .time-marker i { background: #9aa5a0; }
.timeline-event.flashback .time-marker i { background: var(--accent-2); }
.event-card { text-align: left; padding: 16px 18px; border-radius: 16px; background: var(--panel); border: 1px solid var(--line); }
.event-card:hover { border-color: var(--accent); }
.event-top { display: flex; justify-content: space-between; color: var(--accent); font-size: 11px; }
.event-top span { color: var(--ink-soft); }
.event-card h4 { margin: 8px 0 4px; font-family: var(--font-prose); font-size: 18px; }
.event-card p { color: var(--ink-soft); font-size: 13px; line-height: 1.7; }
.event-card footer { display: flex; gap: 12px; color: var(--ink-soft); font-size: 11px; border-top: 1px solid var(--line); padding-top: 10px; }
.timeline-note { color: var(--ink-soft); font-size: 11px; }
.empty { color: var(--ink-soft); padding: 20px; }
@media (max-width: 600px) {
  .timeline-head { flex-direction: column; }
  .timeline::before { left: 7px; }
  .timeline-event { grid-template-columns: 1fr; gap: 8px; padding-left: 24px; }
  .time-marker { text-align: left; padding-top: 0; }
  .time-marker i { left: -23px; right: auto; top: 3px; }
}
</style>
