<script setup lang="ts">
import { onMounted } from 'vue'
import { storeToRefs } from 'pinia'
import { useAppStore } from '@/stores/app'
import { useAppUpdater } from '@/composables/useAppUpdater'
import { waitForBackend } from '@/utils/desktop'

const store = useAppStore()
const { toast, error } = storeToRefs(store)
const { appVersion, hasUpdate, availableVersion, startSilentUpdateCheck, initAppVersion } = useAppUpdater()

onMounted(async () => {
  await waitForBackend()
  await initAppVersion()
  startSilentUpdateCheck()
})
</script>

<template>
  <div class="app-shell">
    <header class="topbar">
      <router-link to="/" class="brand">
        <span class="brand-mark">N</span>
        <span>Danmo Novel</span>
        <span v-if="appVersion" class="ver">{{ appVersion }}</span>
        <span v-if="hasUpdate" class="ver update" :title="`新版本 ${availableVersion}`">↑</span>
      </router-link>
      <nav class="topnav" aria-label="应用导航">
        <router-link to="/">书架</router-link>
        <router-link to="/knowledge">知识库</router-link>
        <router-link to="/runs">模型日志</router-link>
        <router-link to="/settings">设置</router-link>
      </nav>
    </header>
    <main class="app-main">
      <router-view />
    </main>
    <div v-if="toast" class="toast">{{ toast }}</div>
    <div v-if="error" class="toast error">{{ error }}</div>
  </div>
</template>

<style scoped>
.topbar {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 24px; gap: 16px;
}
.brand { display: flex; align-items: center; gap: 10px; color: var(--ink); font-weight: 700; }
.brand-mark {
  width: 32px; height: 32px; border-radius: 10px; display: grid; place-items: center;
  background: var(--ink); color: #f7faf8; font-family: var(--font-prose);
}
.ver { font-size: 11px; font-weight: 600; color: var(--ink-soft); }
.ver.update { color: var(--accent); }
.topnav { display: flex; gap: 16px; }
.topnav a { color: var(--ink-soft); }
.topnav a.router-link-active { color: var(--accent); font-weight: 700; }
.app-main { padding: 0 24px 40px; }
@media (max-width: 720px) {
  .topbar { padding: 12px 16px; }
  .app-main { padding: 0 16px 32px; }
  .topnav { gap: 12px; }
  .brand { font-size: 14px; }
}
.toast {
  position: fixed; bottom: 20px; left: 50%; transform: translateX(-50%);
  background: var(--ink); color: white; padding: 10px 16px; border-radius: 12px; z-index: 50;
}
.toast.error { background: var(--bad); }
</style>
