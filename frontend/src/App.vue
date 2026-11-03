<script setup lang="ts">
import { onMounted, computed } from 'vue'
import { storeToRefs } from 'pinia'
import { useAppStore } from '@/stores/app'
import { useAppUpdater } from '@/composables/useAppUpdater'
import { waitForBackend } from '@/utils/desktop'

const store = useAppStore()
const { toast, error } = storeToRefs(store)
const {
  appVersion,
  status: updaterStatus,
  availableVersion,
  updateNotes,
  errorMessage: updaterError,
  downloadPercent,
  hasUpdate,
  isBusy: updaterBusy,
  initAppVersion,
  checkForUpdates,
  downloadAndInstallUpdate,
  startSilentUpdateCheck,
} = useAppUpdater()

// Brand shows only clean release tags (v1.2.3); dev build strings stay hidden.
const showVersion = computed(() => /^v?\d+\.\d+(\.\d+)?$/.test(appVersion.value.trim()))

/**
 * 版本号就是自升级入口：点一下检查，有更新时再点一下才开始装。
 *
 * 不做成「检查到就自动装」：装完要重启，作者可能正在写正文。所以第一次点击只
 * 检查，发现更新后用确认框把版本号和更新说明摆出来，由作者决定何时重启。
 */
const versionTitle = computed(() => {
  switch (updaterStatus.value) {
    case 'checking':
      return '正在检查更新…'
    case 'downloading':
      return downloadPercent.value != null
        ? `正在下载更新 ${downloadPercent.value}%`
        : '正在下载更新…'
    case 'installing':
      return '正在安装，完成后自动重启…'
    case 'available':
      return `发现新版本 ${availableVersion.value}，点击升级`
    case 'upToDate':
      return '已是最新版本，点击重新检查'
    case 'error':
      return updaterError.value || '检查更新失败'
    default:
      return '点击检查更新'
  }
})

async function onVersionClick() {
  // 浏览器里没有 Tauri updater，静默忽略比弹错误更有用（网页版本来就没有自升级）。
  if (updaterBusy.value) return

  if (hasUpdate.value) {
    const notes = updateNotes.value.trim()
    const detail = notes ? `\n\n更新内容：\n${notes}` : ''
    if (!window.confirm(`升级到 ${availableVersion.value}？${detail}\n\n更新会先下载再安装，完成后应用自动重启。`)) {
      return
    }
    try {
      store.note('正在下载更新…')
      await downloadAndInstallUpdate()
    } catch (e) {
      store.fail(e)
    }
    return
  }

  try {
    const found = await checkForUpdates()
    if (found) store.note(`发现新版本 ${availableVersion.value}，点击版本号升级`)
    else if (updaterStatus.value === 'upToDate') store.note('已是最新版本')
    else if (updaterStatus.value === 'error') store.fail(updaterError.value || '检查更新失败')
  } catch (e) {
    store.fail(e)
  }
}

onMounted(async () => {
  await waitForBackend()
  await initAppVersion()
  startSilentUpdateCheck()
})
</script>

<template>
  <div class="app-shell">
    <header class="topbar">
      <!-- 版本按钮不能放进 router-link：那样点「升级」会顺带跳回书架。 -->
      <div class="brand-group">
        <router-link to="/" class="brand">
          <span class="brand-mark">N</span>
          <span>Danmo Novel</span>
        </router-link>
        <!-- 自升级入口：有更新时变成高亮按钮 + 圆点，点击 → 确认 → 下载安装并重启。 -->
        <button
          v-if="showVersion"
          type="button"
          class="ver"
          :class="{ 'ver--busy': updaterBusy, 'ver--update': hasUpdate }"
          :title="versionTitle"
          :aria-label="versionTitle"
          :disabled="updaterBusy"
          @click="onVersionClick"
        >
          {{ appVersion }}
          <span v-if="updaterStatus === 'downloading' && downloadPercent != null" class="ver-pct">
            {{ downloadPercent }}%
          </span>
          <span v-else-if="hasUpdate" class="ver-dot" aria-hidden="true" />
        </button>
      </div>
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
.brand-group { display: flex; align-items: center; gap: 8px; min-width: 0; }
.brand { display: flex; align-items: center; gap: 10px; color: var(--ink); font-weight: 700; }
.brand-mark {
  width: 32px; height: 32px; border-radius: 10px; display: grid; place-items: center;
  background: var(--ink); color: #f7faf8; font-family: var(--font-prose);
}
.ver {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: 11px; font-weight: 600; color: var(--ink-soft);
  padding: 2px 6px; border-radius: 999px; border: 1px solid transparent;
  background: none; font-family: inherit; cursor: pointer; line-height: 1.5;
}
.ver:hover:not(:disabled) { border-color: var(--line, #d8dfda); color: var(--ink); }
.ver:disabled { cursor: progress; }
/* 有更新：用强调色 + 圆点，避免作者错过；但它不抢正文区域的注意力。 */
.ver--update { color: var(--accent); background: color-mix(in srgb, var(--accent) 10%, transparent); }
.ver--busy { color: var(--accent); }
.ver-dot {
  width: 6px; height: 6px; border-radius: 50%; background: var(--accent);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 25%, transparent);
}
.ver-pct { font-variant-numeric: tabular-nums; opacity: .8; }
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
