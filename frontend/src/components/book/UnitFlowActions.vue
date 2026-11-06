<script setup lang="ts">
import { computed } from 'vue'
import type { UnitInfo } from '@/types'

const props = defineProps<{
  unit: UnitInfo
  busy?: boolean
  readOnly?: boolean
  /** highlight the stage matching unit status */
  emphasize?: boolean
}>()
const emit = defineEmits<{ run: [kind: 'outline' | 'write' | 'review' | 'finalize' | 'outline_review'] }>()

const outlineLabel = computed(() => (props.unit.status === 'proposed' ? '生成细纲' : '重写细纲'))
const writeLabel = computed(() => (props.unit.hasProse ? '重写正文' : '写正文'))
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
    <button type="button" :disabled="busy || readOnly || unit.status === 'proposed'" title="检查章间因果、父级兑现与状态合同；不自动改写" @click="emit('run', 'outline_review')">细纲检查</button>
    <button
      type="button"
      :class="{ primary: emphasize && primaryKind === 'write' }"
      :disabled="busy || readOnly || !canWrite"
      :title="canWrite ? (unit.hasProse ? '重写正文：确认弹窗里可选「在原文上优化」或「重新生成」' : '按合同生成正文候选') : '先接受细纲后再写正文'"
      @click="emit('run', 'write')"
    >{{ writeLabel }}</button>
    <button
      type="button"
      :class="{ primary: emphasize && primaryKind === 'review' }"
      :disabled="busy || readOnly || !canReview"
      :title="canReview ? '本地硬门 + AI 审校' : '先有正文再审校'"
      @click="emit('run', 'review')"
    >审校</button>
    <button
      type="button"
      :class="{ primary: emphasize && primaryKind === 'finalize' }"
      :disabled="busy || readOnly || !canFinalize"
      :title="canFinalize ? '定稿检查；接受后回写账本并将单元标为已定稿' : '先有正文再定稿'"
      @click="emit('run', 'finalize')"
    >{{ unit.status === 'finalized' ? '重跑定稿' : '定稿' }}</button>
  </div>
</template>

<style scoped>
.unit-flow-actions { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.unit-flow-actions button { padding: 8px 12px; font-size: 12px; }
</style>
