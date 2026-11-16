<script setup lang="ts">
/**
 * 单元流水线里的审校清单步：勾选可修问题、提交一次定向修订。
 *
 * 从旧审校面板抽出，供任务页的确认卡内嵌使用。勾选、章筛选、默认勾阻断项的
 * 规则完全沿用 useReview，不复制一份逻辑。
 */
import { computed, ref } from 'vue'
import { useAppStore } from '@/shared/stores/app'
import { useReview, type ReviewRow } from '@/shared/composables/useReview'
import { isUnfinishedFinding, VERDICT_LABEL } from '@/shared/utils/reviewState'
import type { Candidate, UnitInfo } from '@/shared/types'

const props = defineProps<{
  unitId: string
  busy?: boolean
  readOnly?: boolean
}>()
const emit = defineEmits<{
  revise: [unitId: string, issueIds: string[]]
  retrySplit: [unitId?: string]
  rejectUnit: [unitId: string, candidateIds: string[]]
}>()

const store = useAppStore()
const expanded = ref(false)

const {
  groups, picksOf, isPicked, toggle, issueIdsOf,
  chapterFilter, filterOf, visibleRows, canPick, selectableRows, toggleAll, allLabel,
  pickTitle, copyFinding, rejectedRevises, keyOf,
} = useReview({
  selectedUnit: computed<UnitInfo | null>(() => store.currentBook?.units.find((u) => u.unitId === props.unitId) ?? null),
  onlyUnit: computed(() => props.unitId),
  busy: computed(() => props.busy),
  readOnly: computed(() => props.readOnly),
})

const group = computed(() => groups.value.find((g) => g.unitId === props.unitId) ?? null)
const issueIds = computed(() => (group.value ? issueIdsOf(group.value) : []))
const pickedChapters = computed(() => {
  if (!group.value) return 0
  const set = new Set<number>()
  for (const row of group.value.rows) {
    if (canPick(row) && isPicked(row, props.unitId) && row.chapter) set.add(row.chapter)
  }
  return set.size
})
const rejected = computed(() => rejectedRevises.value.get(props.unitId))

function submit() {
  if (!group.value || !issueIds.value.length) return
  emit('revise', props.unitId, issueIds.value)
}
function severityLabel(f: ReviewRow) {
  if (isUnfinishedFinding(f)) return '不完整'
  return f.severity === 'blocking' ? '阻断' : '建议'
}
</script>

<template>
  <div v-if="group" class="findings-editor">
    <div class="fe-summary">
      <span class="fe-verdict" :data-verdict="group.verdict">
        {{ group.verdict === 'blocking' ? `${group.blocking} 阻断` : VERDICT_LABEL[group.verdict] }}
      </span>
      <span class="muted">{{ group.rows.length }} 条 · {{ group.chapters.length }} 章 · {{ group.candidateIds.length }} 份清单</span>
      <button class="ghost fe-reject" :disabled="busy || readOnly" @click="emit('rejectUnit', unitId, group.candidateIds)">拒绝清单</button>
    </div>

    <div v-if="group.verdict === 'unfinished'" class="fe-note warn">
      <strong>审校不完整</strong>
      <span class="muted">有章节的输出被预算截断或调用失败，结论覆盖不全。先重新审校，再决定修还是拒绝。</span>
      <button class="ghost" :disabled="busy || readOnly" @click="emit('retrySplit', unitId)">重新审校</button>
    </div>

    <div v-if="rejected" class="fe-note bad">
      <strong>上一次自动修复已丢弃</strong>
      <span class="muted">{{ rejected.reason }}</span>
      <small class="muted">正文未改变。调整下面的勾选后再点「自动修复」。</small>
    </div>

    <details class="fe-list" :open="expanded || group.rows.length <= 6" @toggle="expanded = ($event.target as HTMLDetailsElement).open">
      <summary>问题清单 · {{ group.rows.length }} 条</summary>
      <div class="fe-tools">
        <button class="ghost" :disabled="busy || readOnly || !selectableRows(group).length" @click="toggleAll(group)">
          {{ allLabel(group) }}
        </button>
        <select v-if="group.chapters.length > 1" :value="filterOf(unitId)"
          @change="chapterFilter[unitId] = ($event.target as HTMLSelectElement).value === 'all' ? 'all' : Number(($event.target as HTMLSelectElement).value)">
          <option value="all">全部章节</option>
          <option v-for="ch in group.chapters" :key="ch" :value="ch">第 {{ ch }} 章</option>
        </select>
        <span v-if="group.unfinished" class="muted">{{ group.unfinished }} 条审校不完整，不参与勾选</span>
        <span v-if="group.unitLevel" class="muted">{{ group.unitLevel }} 条不属于正文</span>
      </div>
      <article v-for="f in visibleRows(group)" :key="keyOf(f)" class="fe-row" :class="f.severity">
        <label class="fe-pick" :title="pickTitle(f)">
          <input type="checkbox" :disabled="!canPick(f) || busy || readOnly"
            :checked="isPicked(f, unitId)" @change="toggle(f, unitId)" />
        </label>
        <div class="fe-body">
          <div class="fe-meta">
            <span>{{ f.source === 'local_gate' ? '本地硬门' : 'AI 审校' }}</span>
            <span>{{ f.chapter ? `第 ${f.chapter} 章` : '整单元' }}</span>
            <span v-if="f.lens">{{ f.lens }}</span>
            <span v-if="(f.roundsHit || 0) > 1" class="fe-hits">×{{ f.roundsHit }}</span>
            <span class="fe-sev">{{ severityLabel(f) }}</span>
            <button class="fe-copy" @click="copyFinding(f)">复制</button>
          </div>
          <h5>{{ f.title }}</h5>
          <p v-if="f.evidence" class="muted">{{ f.evidence }}</p>
          <p v-if="f.suggestion" class="fe-suggest">建议：{{ f.suggestion }}</p>
        </div>
      </article>
    </details>

    <div class="fe-submit">
      <span class="muted">{{ issueIds.length ? `将修复 ${issueIds.length} 条 · 涉及 ${pickedChapters} 章` : '先勾选要修的问题' }}</span>
      <button class="primary" :disabled="busy || readOnly || !issueIds.length" @click="submit">
        {{ busy ? '运行中…' : '自动修复' }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.findings-editor { display: grid; gap: 8px; }
.fe-summary { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.fe-verdict { font-size: 11px; font-weight: 600; padding: 2px 8px; border-radius: 999px; background: rgba(15,107,92,.1); color: var(--accent); }
.fe-verdict[data-verdict='blocking'] { background: rgba(163,59,50,.12); color: var(--bad); }
.fe-verdict[data-verdict='unfinished'] { background: rgba(176,124,28,.14); color: var(--warn, #8a5a00); }
.fe-reject { margin-left: auto; }
.muted { color: var(--ink-soft); font-size: 12px; }
.fe-note { display: grid; gap: 4px; padding: 10px 12px; border-radius: 10px; justify-items: start; font-size: 12px; }
.fe-note.warn { background: rgba(176,124,28,.08); border: 1px solid rgba(176,124,28,.25); }
.fe-note.bad { background: rgba(163,59,50,.06); border: 1px solid rgba(163,59,50,.18); }
.fe-list { border: 1px solid var(--line); border-radius: 10px; padding: 8px 12px; }
.fe-list summary { cursor: pointer; font-size: 13px; font-weight: 600; }
.fe-tools { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; padding: 8px 0; }
.fe-tools select { padding: 4px 8px; border: 1px solid var(--line); border-radius: 8px; font-size: 12px; }
.fe-row { display: flex; gap: 10px; border-top: 1px solid var(--line); padding: 10px 0; }
.fe-pick { flex: none; padding-top: 2px; }
.fe-body { flex: 1; min-width: 0; }
.fe-meta { display: flex; gap: 8px; flex-wrap: wrap; color: var(--ink-soft); font-size: 11px; }
.fe-sev { font-weight: 600; }
.fe-row.blocking .fe-sev { color: var(--bad); }
.fe-hits { padding: 0 6px; border-radius: 999px; background: rgba(163,59,50,.1); color: var(--bad); font-weight: 600; }
.fe-copy { margin-left: auto; border: none; background: none; color: var(--accent); font-size: 11px; cursor: pointer; }
.fe-body h5 { margin: 5px 0; font-size: 13px; }
.fe-row.blocking h5 { color: var(--bad); }
.fe-body p { margin: 3px 0; font-size: 12px; line-height: 1.7; }
.fe-suggest { color: var(--accent); }
.fe-submit { display: flex; align-items: center; gap: 10px; justify-content: flex-end; }
.ghost { background: transparent; border: 1px solid var(--line); color: var(--ink-soft); font-size: 12px; padding: 4px 10px; border-radius: 8px; cursor: pointer; }
</style>
