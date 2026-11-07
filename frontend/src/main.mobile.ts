import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './mobile/AppMobile.vue'
import router from './mobile/router'
import { initCore } from './api/bootstrap'
import './mobile/styles.css'

/**
 * 移动端入口。与桌面入口（main.ts）共用 api / stores / composables，
 * 只有外壳与路由不同：桌面是五个平铺 Tab，移动是「书架 → 单元列表 → 单元详情」。
 */

/**
 * 键盘避让：iOS 弹出键盘时 dvh 不会跟着收（Safari 会，WKWebView 不一定），
 * 结果正文编辑器的最后几行被键盘盖住。visualViewport 的高度才是"真正可见"的
 * 高度，把它写进 --vvh，编辑器按它算高度。
 */
function trackVisualViewport() {
  const vv = window.visualViewport
  if (!vv) return
  const apply = () => {
    document.documentElement.style.setProperty('--vvh', `${Math.round(vv.height)}px`)
  }
  apply()
  vv.addEventListener('resize', apply)
  vv.addEventListener('scroll', apply)
}

async function bootstrap() {
  const loading = document.querySelector('.core-loading') as HTMLDivElement | null
  if (!loading) throw new Error('应用容器未初始化')
  window.addEventListener('error', (event) => {
    loading.textContent = event.message || '页面脚本加载失败'
    loading.setAttribute('role', 'alert')
  })
  window.addEventListener('unhandledrejection', (event) => {
    loading.textContent = String(event.reason)
    loading.setAttribute('role', 'alert')
  })
  try {
    trackVisualViewport()
    await initCore()
    createApp(App).use(createPinia()).use(router).mount('#app')
  } catch (error) {
    loading.textContent = error instanceof Error ? error.message : String(error)
    loading.setAttribute('role', 'alert')
  }
}

void bootstrap()
