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

/**
 * 标记当前是移动壳。
 *
 * 共享组件（UnitProse / UnitFlowActions 等）的紧凑形态必须由「哪个壳」决定，
 * 不能由窗口宽度决定：桌面版窗口被拖窄时不该突然换成手机布局，手机也不该
 * 因为横屏宽于 720px 就退回桌面形态。所以移动端专属规则统一写成
 * `html.shell-mobile ...`，只在这个入口里挂上这个类。
 */
function markMobileShell() {
  document.documentElement.classList.add('shell-mobile')
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
    markMobileShell()
    await initCore()
    createApp(App).use(createPinia()).use(router).mount('#app')
  } catch (error) {
    loading.textContent = error instanceof Error ? error.message : String(error)
    loading.setAttribute('role', 'alert')
  }
}

void bootstrap()
