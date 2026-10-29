import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: { '@': resolve(__dirname, 'src') },
    dedupe: ['vue'],
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
  build: {
    outDir: resolve(__dirname, '../out/frontend/dist'),
    emptyOutDir: true,
  },
})
