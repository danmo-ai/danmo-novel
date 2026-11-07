import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'
import { existsSync, renameSync } from 'fs'

/**
 * 移动端入口文件叫 mobile.html（源码里要能一眼看出这是哪套壳），但 Tauri 的
 * frontendDist 固定按 index.html 找入口。Rollup 的 HTML 入口总是按源文件
 * basename 命名，所以只能构建后改名一次。
 */
function mobileEntryRename(): Plugin {
  return {
    name: 'danmo-mobile-entry-rename',
    closeBundle() {
      const dir = resolve(__dirname, '../out/frontend/mobile')
      const from = resolve(dir, 'mobile.html')
      if (existsSync(from)) renameSync(from, resolve(dir, 'index.html'))
    },
  }
}

/**
 * 一个工程，两个壳。
 *
 *   vite            → out/frontend/dist   桌面/浏览器（index.html + main.ts）
 *   vite --mode mobile → out/frontend/mobile 移动端（mobile.html + main.mobile.ts）
 *
 * 两个入口共享受理，但各自打包：桌面包里不会带移动页面，移动包里也不会
 * 带桌面 Tab 布局。`VITE_SHELL` 是构建期能力标记，用来断言「移动端没有自升级」
 * ——比在运行时嗅探 UA 可靠（见 utils/desktop.ts）。
 */
export default defineConfig(({ mode }) => {
  const mobile = mode === 'mobile'
  return {
    plugins: [vue(), ...(mobile ? [mobileEntryRename()] : [])],
    resolve: {
      alias: { '@': resolve(__dirname, 'src') },
      dedupe: ['vue'],
    },
    // input 的键就是产物文件名：移动端也产出 index.html，
    // 因为 Tauri 的 frontendDist 固定按 index.html 找入口。
    build: {
      outDir: resolve(__dirname, mobile ? '../out/frontend/mobile' : '../out/frontend/dist'),
      emptyOutDir: true,
      rollupOptions: {
        input: { index: resolve(__dirname, mobile ? 'mobile.html' : 'index.html') },
      },
    },
    define: {
      'import.meta.env.VITE_SHELL': JSON.stringify(mobile ? 'mobile' : 'desktop'),
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
  }
})
