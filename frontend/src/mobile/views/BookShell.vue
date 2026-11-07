<script setup lang="ts">
import { computed, provide, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useBookWorkspace, type WorkspaceView } from '@/composables/useBookWorkspace'
import RunConfirmDialog from '@/components/book/RunConfirmDialog.vue'
import { BOOK_WORKSPACE } from '../workspace'

/**
 * 一本书的移动端外壳：持有工作台状态，子页只做展示。
 *
 * 这里也负责两件「层级」的事，桌面不需要：
 *   1. 还没有总纲的书直接落在立项页，否则作者看到的是空单元列表；
 *   2. 路由里的 unitId 与工作台选中的单元保持同步——两个方向都要映射，
 *      否则返回键/深链会让两处指向不同单元。
 */
const route = useRoute()
const router = useRouter()

const bookId = computed(() => String(route.params.id))
const view = ref<WorkspaceView>('write')
const ws = useBookWorkspace({ bookId, view })
provide(BOOK_WORKSPACE, ws)

const { store, selectedUnit, busy, primaryInfo, hasBookOutline, showSetupTab, primaryAction,
  confirmSpec, confirmTitle, confirmRun, defaultModelFor, runDrawer, runCacheOf } = ws

const unitIdInRoute = computed(() => (route.params.unitId ? String(route.params.unitId) : ''))

// 深链/返回键 → 选中单元
watch([unitIdInRoute, () => store.currentBook?.units], () => {
  const id = unitIdInRoute.value
  if (!id) return
  if (selectedUnit.value?.unitId === id) return
  const unit = store.currentBook?.units.find((u) => u.unitId === id)
  if (unit) selectedUnit.value = unit
}, { immediate: true })

// 选中单元 → 路由（只在单元详情这一层同步，避免列表页被强制跳走）
watch(selectedUnit, (unit) => {
  if (!unit) return
  const onUnitPage = route.name === 'unit'
  if (onUnitPage && unit.unitId !== unitIdInRoute.value) {
    router.replace(`/books/${bookId.value}/units/${unit.unitId}`)
  }
})

// 没有总纲 = 还没立项：单元列表必然是空的，直接带去立项页。
// 必须等书真的加载完再判断：初值 hasBookOutline 是 false，立即跳会把有总纲的书
// 也甩到立项页（而且是在路由刚进来的一瞬间）。
watch([hasBookOutline, () => route.name, () => store.currentBook], ([has, name, book]) => {
  if (!book) return
  if (has === false && name === 'book') router.replace(`/books/${bookId.value}/setup`)
})

function backToShelf() {
  router.push('/')
}

const title = computed(() => store.currentBook?.summary.title || '')
</script>

<template>
  <header class="book-head">
    <button class="back" type="button" aria-label="返回书架" @click="backToShelf">‹</button>
    <div class="head-main">
      <h1>{{ title || '加载中…' }}</h1>
      <small v-if="store.currentBook">{{ primaryInfo.label }}</small>
    </div>
    <button class="primary go" type="button" :disabled="busy" @click="primaryAction">
      {{ busy ? '启动中…' : '下一步' }}
    </button>
  </header>

  <nav class="subnav" aria-label="书内导航">
    <RouterLink :to="`/books/${bookId}`" class="subnav-item">单元</RouterLink>
    <RouterLink :to="`/books/${bookId}/bible`" class="subnav-item">设定</RouterLink>
    <RouterLink v-if="showSetupTab" :to="`/books/${bookId}/setup`" class="subnav-item">立项</RouterLink>
  </nav>

  <router-view />

  <!-- 运行确认与实时日志和桌面同一套：RunConfirmDialog 自带全屏移动布局。 -->
  <RunConfirmDialog
    v-if="confirmSpec"
    :spec="confirmSpec"
    :title="confirmTitle"
    :model-label="defaultModelFor(confirmSpec.kind === 'plan' || confirmSpec.kind === 'rewrite_entity' ? 'plan' : confirmSpec.kind)"
    @confirm="confirmRun"
    @cancel="confirmSpec = null"
  />

  <div v-if="runDrawer && store.activeRun" class="run-bar">
    <span>{{ store.activeRun.status }} · 调用 {{ store.activeRun.callsUsed }}/{{ store.activeRun.policy?.maxCalls || '—' }}</span>
    <span v-if="runCacheOf(store.activeRun).hasData" class="muted">缓存命中 {{ runCacheOf(store.activeRun).read.toLocaleString() }} token</span>
    <button type="button" @click="runDrawer = false">收起</button>
  </div>
</template>

<style scoped>
.book-head { display: flex; align-items: center; gap: 10px; padding: 2px 0 10px; }
.back {
  flex-shrink: 0; width: 40px; min-height: 40px; border: 0; background: transparent;
  font-size: 26px; line-height: 1; color: var(--accent); padding: 0;
}
.head-main { min-width: 0; flex: 1; display: grid; gap: 2px; }
h1 { margin: 0; font-family: var(--font-prose); font-size: 20px; line-height: 1.25; overflow-wrap: anywhere; }
.head-main small { color: var(--ink-soft); font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.go { flex-shrink: 0; }
.subnav { display: flex; gap: 4px; margin-bottom: 12px; border-bottom: 1px solid var(--line); }
.subnav-item {
  flex: 1; text-align: center; padding: 10px 0; font-size: 14px; color: var(--ink-soft);
  border-bottom: 2px solid transparent;
}
.subnav-item.router-link-exact-active { color: var(--accent); border-bottom-color: var(--accent); font-weight: 700; }
.run-bar {
  position: fixed; left: 12px; right: 12px; display: flex; align-items: center; gap: 10px;
  bottom: calc(60px + env(safe-area-inset-bottom));
  padding: 10px 12px; border-radius: 12px; background: var(--ink); color: white;
  font-size: 12px; z-index: 40;
}
.run-bar .muted { color: rgba(255,255,255,.72); }
.run-bar button {
  margin-left: auto; flex-shrink: 0; background: transparent; border-color: rgba(255,255,255,.35);
  color: white; min-height: 32px; padding: 2px 10px; font-size: 12px;
}
</style>
