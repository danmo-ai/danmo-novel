/** True when UI runs inside Tauri (desktop shell). */
export function isTauriRuntime(): boolean {
  const w = window as Window & { __TAURI_INTERNALS__?: unknown; __TAURI__?: unknown }
  return Boolean(w.__TAURI_INTERNALS__ ?? w.__TAURI__)
}

/**
 * 是否跑在移动外壳里。
 *
 * 用构建期标记而不是 UA：UA 会被系统语言、iPadOS 的桌面模式、以及未来的
 * 浏览器改动影响，而「这个包是给 iOS/Android 打的」在构建时就是确定的。
 * 移动能力清单（capabilities/mobile.json）里没有 updater，所以任何自升级
 * 调用都会失败——判断必须在这里就断言，不能等调用报错。
 */
export function isMobileShell(): boolean {
  return import.meta.env.VITE_SHELL === 'mobile'
}

/** 自升级只在桌面外壳里存在；网页版与移动端都没有。 */
export function hasUpdater(): boolean {
  return isTauriRuntime() && !isMobileShell()
}

/**
 * Resolve the API base URL for backend requests.
 * - VITE_API_BASE_URL (build-time) takes highest priority.
 * - In Tauri desktop the webview may load from a custom protocol where relative
 *   URLs cannot reach the Go backend, so fall back to localhost.
 * - Otherwise empty (same-origin, proxied by Vite).
 */
export function apiBaseUrl(): string {
  return import.meta.env.VITE_API_BASE_URL ?? window.__DANMO_API_BASE__ ?? (isTauriRuntime() ? 'http://127.0.0.1:7804' : '')
}

/** Wait until the local Go backend accepts HTTP (desktop first-launch race). */
export async function waitForBackend(opts?: {
  timeoutMs?: number
  intervalMs?: number
}): Promise<boolean> {
  if (!isTauriRuntime()) return true

  const timeoutMs = opts?.timeoutMs ?? 45_000
  const intervalMs = opts?.intervalMs ?? 250
  const url = `${apiBaseUrl()}/api/system/health`
  const deadline = Date.now() + timeoutMs

  while (Date.now() < deadline) {
    try {
      const res = await fetch(url, { method: 'GET', cache: 'no-store' })
      if (res.ok) return true
    } catch {
      /* connection refused */
    }
    await new Promise((r) => setTimeout(r, intervalMs))
  }
  return false
}
