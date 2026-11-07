<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useAppStore } from '@/stores/app'
import { waitForBackend } from '@/utils/desktop'

/**
 * 移动端外壳：只有底部 Tab 和全局提示。
 *
 * 没有自升级：iOS/Android 的更新由商店负责，客户端去拉 GitHub 上的
 * latest.json 既装不上，也会成为审核问题。桌面入口（App.vue）才有版本按钮。
 */
const store = useAppStore()
const { toast, error } = storeToRefs(store)
const route = useRoute()
const router = useRouter()

const bookId = computed(() => (route.params.id ? String(route.params.id) : ''))
const tabs = computed(() => {
  const items = [{ key: 'shelf', label: '书架', to: '/' }]
  // 没进书时「书」这一项没有落点，直接不显示，避免点了回到书架。
  if (bookId.value) items.push({ key: 'book', label: '书', to: `/books/${bookId.value}` })
  items.push({ key: 'runs', label: '日志', to: '/runs' })
  items.push({ key: 'settings', label: '设置', to: '/settings' })
  return items
})

function isActive(key: string) {
  if (key === 'shelf') return route.name === 'shelf'
  if (key === 'book') return ['book', 'unit', 'bible', 'setup'].includes(String(route.name))
  if (key === 'runs') return route.name === 'runs'
  return route.name === 'settings'
}

function go(to: string) {
  if (route.fullPath !== to) router.push(to)
}

onMounted(async () => {
  await waitForBackend()
})
</script>

<template>
  <div class="shell">
    <main class="scroll">
      <router-view />
    </main>

    <nav class="tabbar safe-bottom" aria-label="应用导航">
      <button
        v-for="t in tabs"
        :key="t.key"
        type="button"
        class="tab"
        :class="{ active: isActive(t.key) }"
        :aria-current="isActive(t.key) ? 'page' : undefined"
        @click="go(t.to)"
      >
        {{ t.label }}
      </button>
    </nav>

    <div v-if="toast" class="toast">{{ toast }}</div>
    <div v-if="error" class="toast error">{{ error }}</div>
  </div>
</template>

<style scoped>
.shell { display: flex; flex-direction: column; height: 100dvh; }
.scroll {
  flex: 1; min-height: 0; overflow-y: auto; overscroll-behavior: contain;
  -webkit-overflow-scrolling: touch;
  padding: max(10px, env(safe-area-inset-top)) 14px 0;
}
.tabbar {
  display: grid; grid-auto-flow: column; grid-auto-columns: 1fr;
  gap: 2px; padding: 6px 8px 0; background: rgba(247, 250, 248, .94);
  border-top: 1px solid var(--line); backdrop-filter: blur(12px);
}
.tab {
  border: 0; background: transparent; border-radius: 12px;
  min-height: 44px; font-size: 13px; color: var(--ink-soft);
}
.tab.active { color: var(--accent); font-weight: 700; background: rgba(15, 107, 92, .08); }
.toast {
  position: fixed; left: 16px; right: 16px;
  bottom: calc(64px + env(safe-area-inset-bottom));
  background: var(--ink); color: white; padding: 10px 14px; border-radius: 12px; z-index: 50;
  font-size: 13px; line-height: 1.5;
}
.toast.error { background: var(--bad); }
</style>
