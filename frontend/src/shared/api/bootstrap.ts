import { isTauriRuntime, waitForBackend } from '@/shared/utils/desktop'

/**
 * The desktop webview talks to the Go sidecar over HTTP on 127.0.0.1:7804.
 * The sidecar is spawned by the Tauri shell, so a first launch may still be
 * binding the port when the webview mounts; wait for health before rendering.
 */
export async function initCore(): Promise<void> {
  if (!isTauriRuntime()) return
  if (!(await waitForBackend())) {
    throw new Error('本地内核启动超时，请重新打开应用')
  }
}
