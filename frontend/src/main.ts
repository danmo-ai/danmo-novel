import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import { initCore } from './api/bootstrap'
import './styles.css'

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
    await initCore()
    createApp(App).use(createPinia()).use(router).mount('#app')
  } catch (error) {
    loading.textContent = error instanceof Error ? error.message : String(error)
    loading.setAttribute('role', 'alert')
  }
}

void bootstrap()
