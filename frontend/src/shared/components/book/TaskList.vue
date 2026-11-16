<script setup lang="ts">
/**
 * 任务页：本书唯一的确认界面。待确认、进行中、操作记录融合成一条时间线——
 * LLM 调用是分钟级的，作者回来时问的是「什么在等我」。
 *
 * 所有 kind 都在这里确认（细纲/正文/审校清单/修订/定稿/卷纲/重写设定），
 * 单元页只负责生产与阅读。
 */
import { computed, ref } from 'vue'
import { useAppStore } from '@/shared/stores/app'
import { buildTaskItems } from '@/shared/utils/taskItems'
import type { Candidate } from '@/shared/types'
import TaskConfirmCard from './TaskConfirmCard.vue'

const props = defineProps<{ busy?: boolean }>()
const emit = defineEmits<{
  accept: [c: Candidate]
  reject: [c: Candidate]
  repair: [c: Candidate]
  revise: [unitId: string, issueIds: string[]]
  retrySplit: [unitId?: string]
  rejectUnit: [unitId: string, candidateIds: string[]]
  openUnit: [unitId: string]
}>()

const store = useAppStore()
const showHistory = ref(false)

const items = computed(() => buildTaskItems(store.candidates, store.runs, store.activeRun))
const needsAction = computed(() => items.value.filter((i) => i.section === 'needsAction'))
const running = computed(() => items.value.filter((i) => i.section === 'running'))
const closed = computed(() => items.value.filter((i) => i.section === 'closed'))

const reviewTotals = computed(() => {
  let blocking = 0
  let findings = 0
  for (const c of store.candidates) {
    if (c.kind !== 'review') continue
    for (const f of c.findings || []) {
      findings += 1
      const hits = f.roundsHit ?? 0
      if (f.severity === 'blocking' && (hits === 0 || hits >= 2)) blocking += 1
    }
  }
  return { blocking, findings }
})
</script>

<template>
  <section class="task-page">
    <div class="tp-head">
      <div>
        <p class="eyebrow">Tasks</p>
        <h2>任务与确认</h2>
        <p class="sub">生成结果都在这里确认；失败与取消也留在同一条记录里。</p>
      </div>
      <div class="tp-totals">
        <span v-if="reviewTotals.findings"><strong>{{ reviewTotals.blocking }}</strong> 阻断</span>
        <span v-if="reviewTotals.findings"><strong>{{ reviewTotals.findings }}</strong> 总问题</span>
      </div>
    </div>

    <p v-if="store.continuity?.units" class="tp-continuity">
      连续性状态：{{ store.continuity.lastUnit }} 之后
      <template v-if="store.continuity.afterCh"> · 已定稿到第 {{ store.continuity.afterCh }} 章</template>
      · {{ store.continuity.cast }} 人 · {{ store.continuity.openLoops }} 条未回收伏笔
    </p>
    <p v-else class="tp-continuity muted">连续性状态未建立：定稿第一个单元后自动生成。</p>

    <div v-if="!items.length" class="empty panel">
      <h3>还没有任务</h3>
      <p>到写作页对单元发起生成后，任务与待确认结果会出现在这里。</p>
    </div>

    <template v-else>
      <div v-if="!needsAction.length && !running.length" class="tp-none panel">当前没有待确认的结果。</div>

      <section v-if="needsAction.length" class="tp-section">
        <h3>待确认 · {{ needsAction.length }}</h3>
        <TaskConfirmCard v-for="item in needsAction" :key="item.id" :item="item" :busy="busy"
          @accept="(c) => emit('accept', c)" @reject="(c) => emit('reject', c)"
          @repair="(c) => emit('repair', c)"
          @revise="(u, ids) => emit('revise', u, ids)"
          @retry-split="(u) => emit('retrySplit', u)"
          @reject-unit="(u, ids) => emit('rejectUnit', u, ids)"
          @open-unit="(u) => emit('openUnit', u)" />
      </section>

      <section v-if="running.length" class="tp-section">
        <h3>进行中 · {{ running.length }}</h3>
        <TaskConfirmCard v-for="item in running" :key="item.id" :item="item" :busy="busy"
          @accept="(c) => emit('accept', c)" @reject="(c) => emit('reject', c)"
          @repair="(c) => emit('repair', c)"
          @revise="(u, ids) => emit('revise', u, ids)"
          @retry-split="(u) => emit('retrySplit', u)"
          @reject-unit="(u, ids) => emit('rejectUnit', u, ids)"
          @open-unit="(u) => emit('openUnit', u)" />
      </section>

      <details v-if="closed.length" class="tp-history panel" :open="showHistory"
        @toggle="showHistory = ($event.target as HTMLDetailsElement).open">
        <summary><strong>操作记录</strong><span class="muted"> · {{ closed.length }} 条</span></summary>
        <div class="tp-history-body">
          <TaskConfirmCard v-for="item in closed.slice(0, 40)" :key="item.id" :item="item" :busy="busy"
            @accept="(c) => emit('accept', c)" @reject="(c) => emit('reject', c)"
            @repair="(c) => emit('repair', c)"
            @revise="(u, ids) => emit('revise', u, ids)"
            @retry-split="(u) => emit('retrySplit', u)"
            @reject-unit="(u, ids) => emit('rejectUnit', u, ids)"
            @open-unit="(u) => emit('openUnit', u)" />
        </div>
      </details>
    </template>
  </section>
</template>

<style scoped>
.task-page { display: grid; gap: 16px; }
.tp-head { display: flex; justify-content: space-between; align-items: flex-end; gap: 20px; }
.tp-head h2 { margin: 4px 0 6px; font-family: var(--font-prose); font-size: 26px; }
.eyebrow { margin: 0; color: var(--accent); font-size: 11px; letter-spacing: .12em; text-transform: uppercase; }
.sub { margin: 0; color: var(--ink-soft); font-size: 13px; }
.tp-totals { display: flex; gap: 18px; flex-shrink: 0; font-size: 12px; color: var(--ink-soft); }
.tp-totals strong { font-size: 20px; font-family: var(--font-prose); color: var(--ink); }
.tp-continuity { margin: 0; font-size: 12px; color: var(--ink-soft); }
.tp-section { display: grid; gap: 12px; }
.tp-section h3 { margin: 0; font-size: 14px; color: var(--ink-soft); }
.tp-none { padding: 14px 16px; font-size: 13px; color: var(--ink-soft); }
.empty { padding: 32px; text-align: center; }
.empty h3 { margin: 0 0 6px; }
.empty p { margin: 0; color: var(--ink-soft); font-size: 13px; }
.tp-history { padding: 14px 16px; }
.tp-history summary { cursor: pointer; font-size: 14px; }
.tp-history-body { display: grid; gap: 12px; margin-top: 12px; }
.muted { color: var(--ink-soft); font-size: 12px; }
</style>
