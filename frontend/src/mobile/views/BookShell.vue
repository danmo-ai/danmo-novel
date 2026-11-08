<script setup lang="ts">
import { computed, provide, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useBookWorkspace, type WorkspaceView } from '@/shared/composables/useBookWorkspace'
import RunConfirmDialog from '@/shared/components/book/RunConfirmDialog.vue'
import { useSizeClass } from '../useSizeClass'
import UnitListMobile from './UnitListMobile.vue'
import UnitDetailMobile from './UnitDetailMobile.vue'
import { BOOK_WORKSPACE } from '../workspace'

/**
 * 一本书的移动端外壳：持有工作台状态，子页只做展示。
 *
 * 常规宽度（内屏/展开态/横屏）下，单元列表和单元详情同时出现：左边是卷/单元
 * 目录，右边是选中单元的细纲/正文/审校。这正是 Duo 展开后该有的形态，
 * 也顺带让折痕落在两栏中间，而不是压在正文上。
 *
 * 紧凑宽度保持原来的「列表 → 详情」push 导航，行为与之前一致。
 */
const route = useRoute()
const router = useRouter()
const { isRegular } = useSizeClass()

const bookId = computed(() => String(route.params.id))
const view = ref<WorkspaceView>('write')
const ws = useBookWorkspace({ bookId, view })
provide(BOOK_WORKSPACE, ws)

const { store, selectedUnit, busy, primaryInfo, hasBookOutline, showSetupTab, primaryAction,
  confirmSpec, confirmTitle, confirmRun, defaultModelFor, runDrawer, runCacheOf } = ws

const unitIdInRoute = computed(() => (route.params.unitId ? String(route.params.unitId) : ''))

// 常规宽度时，单元列表和详情必须同时挂载，否则左栏点选会卸载右栏、右栏的选中
// 又会把左栏替换掉，两栏永远互相挤掉一个。
const showSplit = computed(() => isRegular.value && (route.name === 'book' || route.name === 'unit'))
const detailUnitId = computed(() => selectedUnit.value?.unitId || '')

// 深链/返回键 → 选中单元
watch([unitIdInRoute, () => store.currentBook?.units], () => {
  const id = unitIdInRoute.value
  if (!id) return
  if (selectedUnit.value?.unitId === id) return
  const unit = store.currentBook?.units.find((u) => u.unitId === id)
  if (unit) selectedUnit.value = unit
}, { immediate: true })

// 选中单元 → 路由。
// 常规宽度下也同步到 /units/:unitId，但用 replace：切单元不是「进入下一级」，
// 不应该在历史栈里留下几十条单元切换记录，否则返回键要按很多次才能出这本书。
watch(selectedUnit, (unit) => {
  if (!unit) return
  const onUnitPage = route.name === 'unit'
  if (onUnitPage && unit.unitId !== unitIdInRoute.value) {
    router.replace(`/books/${bookId.value}/units/${unit.unitId}`)
  }
})

// 从列表回到双栏时，把「当前选中单元」恢复成路由里的单元，避免右栏空白。
watch(showSplit, (split) => {
  if (!split) return
  if (route.name === 'unit') return
  if (selectedUnit.value) {
    router.replace(`/books/${bookId.value}/units/${selectedUnit.value.unitId}`)
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
  // 原来这里是 router.push('/')，会让系统返回键变成「再前进一次到这本书」。
  // 有历史栈就退；没有（冷启动直接进书）就替换到书架。
  if (window.history.state?.back) router.back()
  else router.replace('/')
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

  <!-- 常规宽度：左栏单元列表 + 右栏单元详情，两栏各自滚动。 -->
  <div v-if="showSplit" class="split">
    <aside class="split-list">
      <UnitListMobile />
    </aside>
    <section class="split-detail">
      <UnitDetailMobile v-if="detailUnitId" :unit-id="detailUnitId" />
      <div v-else class="split-empty">从左边选择一个单元开始。</div>
    </section>
  </div>

  <!-- 紧凑宽度：列表/详情仍是两个路由层级。 -->
  <router-view v-else />

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
  flex-shrink: 0; width: 44px; min-height: 44px; border: 0; background: transparent;
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

/* 双栏：列表固定在左，正文固定在右。折痕落在中缝上时，两栏各自仍在自己的半屏里。 */
.split {
  display: grid;
  grid-template-columns: minmax(240px, 300px) minmax(0, 1fr);
  gap: 16px;
  align-items: stretch;
  min-height: calc(var(--vvh, 100dvh) - 190px);
}
.split-list {
  position: sticky; top: 0; max-height: calc(var(--vvh, 100dvh) - 200px);
  overflow-y: auto; overscroll-behavior: contain; -webkit-overflow-scrolling: touch;
  padding-right: 4px;
}
.split-detail { min-width: 0; }
.split-empty { padding: 40px 20px; color: var(--ink-soft); font-size: 13px; text-align: center; }
.split-detail :deep(.md-p),
.split-detail :deep(.table-wrap) { max-width: 720px; }

.run-bar {
  position: fixed; left: 12px; right: 12px; display: flex; align-items: center; gap: 10px;
  bottom: calc(60px + env(safe-area-inset-bottom));
  padding: 10px 12px; border-radius: 12px; background: var(--ink); color: white;
  font-size: 12px; z-index: 40;
}
.run-bar .muted { color: rgba(255,255,255,.72); }
.run-bar button {
  margin-left: auto; flex-shrink: 0; background: transparent; border-color: rgba(255,255,255,.35);
  color: white; min-height: 44px; padding: 2px 10px; font-size: 12px;
}
</style>
