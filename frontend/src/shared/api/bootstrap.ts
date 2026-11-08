import { isTauriRuntime, waitForBackend } from '@/shared/utils/desktop'
import { httpTransport, nativeTransport, setTransport } from './transport'

export async function initCore(): Promise<void> {
  if (!isTauriRuntime()) return
  const { invoke } = await import('@tauri-apps/api/core')
  const info = await Promise.race([
    invoke<{ transport: 'ffi' | 'http'; apiBase?: string }>('plugin:localcore|bootstrap'),
    new Promise<never>((_, reject) => window.setTimeout(() => reject(new Error('本地内核初始化超时，请重新启动应用')), 15_000)),
  ])
  if (info.transport === 'ffi') {
    setTransport(nativeTransport)
    return
  }
  if (info.apiBase) window.__DANMO_API_BASE__ = info.apiBase
  setTransport(httpTransport)
  if (!(await waitForBackend())) {
    throw new Error('本地内核启动超时，请重新打开应用')
  }
}
