<script setup lang="ts">
import { computed } from 'vue'
import type { UnitInfo } from '@/shared/types'

const props = defineProps<{
  unit: UnitInfo
  busy?: boolean
  readOnly?: boolean
  /** highlight the stage matching unit status */
  emphasize?: boolean
}>()
const emit = defineEmits<{ run: [kind: 'outline' | 'write' | 'review' | 'finalize'] }>()

const outlineLabel = computed(() => (props.unit.status === 'proposed' ? '细纲' : '重写细纲'))
const writeLabel = computed(() => (props.unit.hasProse ? '重写正文' : '正文'))
const reviewLabel = computed(() => (['reviewed', 'finalized'].includes(props.unit.status) ? '重新审校' : '审校'))
const finalizeLabel = computed(() => (props.unit.status === 'finalized' ? '重新定稿' : '定稿'))
const canWrite = computed(() => ['accepted', 'drafted', 'reviewed', 'finalized'].includes(props.unit.status))
const canReview = computed(() => Boolean(props.unit.hasProse))
const canFinalize = computed(() => Boolean(props.unit.hasProse) && props.unit.status !== 'proposed')
const primaryKind = computed(() => {
  if (props.unit.status === 'proposed') return 'outline'
  if (props.unit.status === 'accepted') return 'write'
  if (props.unit.status === 'drafted') return 'review'
  if (props.unit.status === 'reviewed' || props.unit.status === 'finalized') return 'finalize'
  return ''
})
</script>

<template>
  <div class="unit-flow-actions" role="group" :aria-label="`${unit.unitId} 生产与重做`">
    <button
      type="button"
      :class="{ primary: emphasize && primaryKind === 'outline' }"
      :disabled="busy || readOnly"
      :title="unit.status === 'proposed' ? '补全单元写作合同' : '重新生成单元合同（接受后覆盖）'"
      @click="emit('run', 'outline')"
    >{{ outlineLabel }}</button>
    <span class="flow-arrow" aria-hidden="true">→</span>
    <button
      type="button"
      :class="{ primary: emphasize && primaryKind === 'write' }"
      :disabled="busy || readOnly || !canWrite"
      :title="canWrite ? (unit.hasProse ? '重写正文：确认弹窗里可选「在原文上优化」或「重新生成」' : '按合同生成正文候选') : '先接受细纲后再写正文'"
      @click="emit('run', 'write')"
    >{{ writeLabel }}</button>
    <span class="flow-arrow" aria-hidden="true">→</span>
    <button
      type="button"
      :class="{ primary: emphasize && primaryKind === 'review' }"
      :disabled="busy || readOnly || !canReview"
      :title="canReview ? '本地硬门 + AI 审校' : '先有正文再审校'"
      @click="emit('run', 'review')"
    >{{ reviewLabel }}</button>
    <span class="flow-arrow" aria-hidden="true">→</span>
    <button
      type="button"
      :class="{ primary: emphasize && primaryKind === 'finalize' }"
      :disabled="busy || readOnly || !canFinalize"
      :title="canFinalize ? '定稿检查；接受后回写账本并将单元标为已定稿' : '先有正文再定稿'"
      @click="emit('run', 'finalize')"
    >{{ finalizeLabel }}</button>
  </div>
</template>

<style scoped>
/**
 * 流程条只有一种形态：按钮 + 箭头，靠 flex 自动换行。
 *
 * 这里刻意不留窄屏断点。桌面版不再适配手机宽度，按钮 + 箭头固定一种排布，
 * 空间不足时由 flex 自动换行，避免窗口拖窄就退化成手机布局。
 */
.unit-flow-actions { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; min-width: 0; }
.unit-flow-actions button { padding: 8px 12px; font-size: 12px; white-space: normal; }
.unit-flow-actions .flow-arrow { flex: 0 0 auto; color: var(--ink-soft); opacity: .55; font-size: 12px; }
</style>
