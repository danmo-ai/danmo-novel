import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

/**
 * Vue 只服务桌面外壳：产物固定写入 out/frontend/dist，由 Tauri 的
 * frontendDist / index.html 加载。手机端是原生工程（native-ios /
 * native-android），不参与前端构建。
 */
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: { '@': resolve(__dirname, 'src') },
    dedupe: ['vue'],
  },
  build: {
    outDir: resolve(__dirname, '../out/frontend/dist'),
    emptyOutDir: true,
    rollupOptions: {
      input: { index: resolve(__dirname, 'index.html') },
    },
  },
  server: {
    host: '127.0.0.1',
    port: Number(process.env.DQ_FRONTEND_PORT || 5804),
    strictPort: true,
    proxy: {
      '/api': {
        target: `http://127.0.0.1:${process.env.DQ_BACKEND_PORT || 7804}`,
        changeOrigin: true,
      },
    },
  },
})
