import { computed, getCurrentInstance, onBeforeUnmount, ref } from 'vue'

/**
 * 移动壳内部的「放得下双栏」判断。
 *
 * 这里刻意用「宽度够不够放两栏」而不是「是不是手机」。Apple 对 Duo 的要求
 * （developer.apple.com/iphone-duo/prepare）很明确：布局决策看 size class，
 * 不看设备型号、尺寸或方向。iPhone 应用在内屏就该有 iPad 式的双栏。
 *
 * 阈值 900 是「列表 240–300 + 正文至少 500」的下限，不是某个设备的宽度。
 * 与 html.shell-mobile 正交：
 *   shell-mobile  = 跑在哪个壳里（决定按钮形态、编辑器字号等）
 *   size-regular  = 壳内现在是紧凑还是常规（决定要不要双栏）
 * 桌面窗口拖窄不会被误当成手机，因为桌面没有 shell-mobile。
 */
const isRegular = ref(false)

let listeners = 0
let query: MediaQueryList | null = null

function apply() {
  if (!query) return
  isRegular.value = query.matches
  const root = document.documentElement
  root.classList.toggle('size-regular', query.matches)
  root.classList.toggle('size-compact', !query.matches)
}

export function useSizeClass() {
  // 可以在组件外调用（main.mobile.ts 在 createApp 之前）。这时没有生命周期
  // 钩子可用，监听器只注册一次并随模块常驻——它本来就该覆盖整个应用的生命周期。
  if (typeof window !== 'undefined') {
    if (listeners === 0) {
      query = window.matchMedia('(min-width: 900px)')
      query.addEventListener('change', apply)
      apply()
    }
    listeners += 1
  }

  if (getCurrentInstance()) {
    onBeforeUnmount(() => {
      listeners = Math.max(0, listeners - 1)
      if (listeners === 0 && query) {
        query.removeEventListener('change', apply)
        query = null
      }
    })
  }

  return {
    isRegular,
    isCompact: computed(() => !isRegular.value),
  }
}
