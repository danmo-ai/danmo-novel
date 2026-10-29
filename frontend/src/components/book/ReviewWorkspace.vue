<script setup lang="ts">
import { computed } from 'vue'
import { useAppStore } from '@/stores/app'
import type { UnitInfo } from '@/types'

const props = defineProps<{ selectedUnit: UnitInfo | null; busy: boolean }>()
const emit = defineEmits<{ generate: [kind: string]; select: [unit: UnitInfo] }>()
const store = useAppStore()
const findings = computed(() => store.candidates.flatMap((c) => (c.findings || []).map((f) => ({ ...f, candidateId: c.id, unitId: c.unitId }))))
const reviewRuns = computed(() => store.runs.filter((r) => ['review', 'finalize', 'revise'].includes(r.kind)))
const latestRun = computed(() => reviewRuns.value[0])
const blocking = computed(() => findings.value.filter((f) => f.severity === 'blocking'))
const advisory = computed(() => findings.value.filter((f) => f.severity !== 'blocking'))
</script>

<template>
  <section class="review-workspace">
    <div class="view-title">
      <div><p class="eyebrow">Quality</p><h2>审校与定稿</h2><p>本地硬门和 AI 建议分开呈现；只有接受的修订才能进入正文。</p></div>
      <div class="review-actions">
        <button class="primary" :disabled="busy || !selectedUnit?.hasProse" @click="emit('generate', 'review')">审校 {{ selectedUnit?.unitId || '当前单元' }}</button>
        <button :disabled="busy || !selectedUnit?.hasProse" @click="emit('generate', 'finalize')">本地定稿检查</button>
      </div>
    </div>
    <div class="review-stats">
      <div class="stat panel"><strong>{{ blocking.length }}</strong><span>阻断问题</span></div>
      <div class="stat panel"><strong>{{ advisory.length }}</strong><span>风格建议</span></div>
      <div class="stat panel"><strong>{{ selectedUnit?.status === 'reviewed' ? '已定稿' : '未定稿' }}</strong><span>{{ selectedUnit?.unitId || '未选择单元' }}</span></div>
    </div>
    <div v-if="latestRun?.status === 'failed'" class="run-error panel">
      <strong>最近一次审校失败</strong>
      <p>{{ latestRun.error }}</p>
      <small>已按调用预算停止，可修复后重试；正文未改变。</small>
    </div>
    <div v-if="findings.length" class="review-groups">
      <section v-if="blocking.length" class="finding-group panel">
        <h3>阻断 · 需先处理</h3>
        <article v-for="f in blocking" :key="f.candidateId + f.id" class="finding blocking">
          <div class="finding-meta"><span>{{ f.source === 'local_gate' ? '本地硬门' : 'AI 审校' }}</span><span v-if="f.chapter">第 {{ f.chapter }} 章</span><span>{{ f.lens }}</span></div>
          <h4>{{ f.title }}</h4><p>{{ f.evidence }}</p><p v-if="f.suggestion" class="suggestion">建议：{{ f.suggestion }}</p>
        </article>
      </section>
      <section v-if="advisory.length" class="finding-group panel">
        <h3>建议 · 作者判断</h3>
        <article v-for="f in advisory" :key="f.candidateId + f.id" class="finding advisory">
          <div class="finding-meta"><span>{{ f.source === 'local_gate' ? '本地检查' : 'AI 审校' }}</span><span v-if="f.chapter">第 {{ f.chapter }} 章</span><span>{{ f.lens }}</span></div>
          <h4>{{ f.title }}</h4><p>{{ f.evidence }}</p><p v-if="f.suggestion" class="suggestion">建议：{{ f.suggestion }}</p>
        </article>
      </section>
    </div>
    <div v-else class="empty-state panel">
      <h3>当前没有待处理审校问题</h3>
      <p v-if="selectedUnit?.hasProse">点击「审校」生成一次有预算的检查；历史结果和问题会显示在这里。</p>
      <p v-else>先为当前单元写正文，再运行审校。</p>
    </div>
    <section v-if="reviewRuns.length" class="history panel">
      <h3>审校记录</h3>
      <div v-for="r in reviewRuns" :key="r.id" class="history-row">
        <span>{{ r.kind }} · {{ r.spec.unitId }}</span><span>{{ r.status }}</span><span>调用 {{ r.callsUsed }}/{{ r.policy?.maxCalls }}</span>
      </div>
    </section>
  </section>
</template>

<style scoped>
.review-workspace { display: grid; gap: 18px; }
.view-title { display: flex; justify-content: space-between; gap: 20px; align-items: flex-end; }
.view-title h2 { margin: 4px 0 6px; font-family: var(--font-prose); font-size: 28px; }
.view-title p { margin: 0; color: var(--ink-soft); font-size: 13px; }
.eyebrow { color: var(--accent) !important; font-size: 11px !important; letter-spacing: .12em; text-transform: uppercase; }
.review-actions { display: flex; gap: 8px; flex-wrap: wrap; }
.review-stats { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }
.stat { padding: 16px; display: grid; gap: 6px; }
.stat strong { font-size: 24px; font-family: var(--font-prose); }
.stat span { font-size: 12px; color: var(--ink-soft); }
.finding-group { padding: 18px; }
.finding-group h3 { margin: 0 0 14px; font-size: 15px; }
.finding { border-top: 1px solid var(--line); padding: 14px 0; }
.finding-meta { display: flex; gap: 8px; color: var(--ink-soft); font-size: 11px; }
.finding h4 { margin: 6px 0; font-size: 14px; }
.finding p { margin: 4px 0; color: var(--ink-soft); font-size: 13px; line-height: 1.7; }
.finding.blocking h4 { color: var(--bad); }
.suggestion { color: var(--accent) !important; }
.empty-state { padding: 32px; text-align: center; }
.run-error { padding: 16px; border-color: rgba(163,59,50,.3); }
.run-error strong { color: var(--bad); }
.run-error p { color: var(--ink-soft); font-size: 13px; }
.run-error small { color: var(--ink-soft); }
.history { padding: 18px; }
.history h3 { margin: 0 0 10px; font-size: 15px; }
.history-row { display: flex; justify-content: space-between; gap: 10px; border-top: 1px solid var(--line); padding: 10px 0; font-size: 12px; color: var(--ink-soft); }
@media (max-width: 720px) { .view-title { flex-direction: column; align-items: flex-start; } .review-stats { grid-template-columns: 1fr; } .history-row { flex-direction: column; } }
</style>
