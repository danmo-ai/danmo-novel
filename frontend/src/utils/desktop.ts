/** True when UI runs inside Tauri (desktop shell). */
export function isTauriRuntime(): boolean {
  const w = window as Window & { __TAURI_INTERNALS__?: unknown; __TAURI__?: unknown }
  return Boolean(w.__TAURI_INTERNALS__ ?? w.__TAURI__)
}

/**
 * Resolve the API base URL for backend requests.
 * - VITE_API_BASE_URL (build-time) takes highest priority.
 * - In Tauri desktop the webview may load from a custom protocol where relative
 *   URLs cannot reach the Go backend, so fall back to localhost.
 * - Otherwise empty (same-origin, proxied by Vite).
 */
export function apiBaseUrl(): string {
  return import.meta.env.VITE_API_BASE_URL ?? (isTauriRuntime() ? 'http://127.0.0.1:7804' : '')
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
