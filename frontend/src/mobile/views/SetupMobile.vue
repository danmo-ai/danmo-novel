<script setup lang="ts">
import ProjectSetup from '@/components/book/ProjectSetup.vue'
import { useInjectedWorkspace } from '../workspace'

/**
 * 手机立项页：直接复用桌面的立项工作台。
 *
 * 立项只有一条路径（填创意 → 扩写提案 → 确认 → 结构化立项），没有
 * 「同时看好几块」的需要，所以桌面的单列表现在手机上就是对的布局，
 * 没必要再写一份。唯一的差别是动作按钮在手机上要占满一行。
 */
const {
  store, busy, saving, saveFile, startIdea, startPlan, accept, reject,
} = useInjectedWorkspace()
</script>

<template>
  <ProjectSetup
    v-if="store.currentBook"
    :busy="busy"
    :saving="saving"
    @save="saveFile"
    @idea="startIdea"
    @plan="(mode, volume) => startPlan(mode, volume)"
    @accept="accept"
    @reject="reject"
  />
</template>

<style scoped>
/* 立项表单里的按钮组在手机上是主操作，必须占满一行且够高。
   .view-title 换行与 .setup-card 内边距已经写在 ProjectSetup 自己的窄屏规则里，
   这里不重复。 */
:deep(.view-actions),
:deep(.card-actions) { display: grid; gap: 8px; }
:deep(.view-actions button),
:deep(.card-actions button) { width: 100%; min-height: 44px; }
</style>
