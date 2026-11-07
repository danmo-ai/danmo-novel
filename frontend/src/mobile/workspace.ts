import { inject, type InjectionKey } from 'vue'
import type { useBookWorkspace } from '@/composables/useBookWorkspace'

/**
 * 书内页共享同一个工作台实例。
 *
 * 单元列表和单元详情如果各自 useBookWorkspace()，每次进出详情都会重新拉一次书，
 * 而且「哪些候选待审、轮询到哪一步」会在两页之间各存一份。放在 BookShell 里
 * provide，子页 inject，行为和桌面单页一致。
 */
export type BookWorkspace = ReturnType<typeof useBookWorkspace>

export const BOOK_WORKSPACE: InjectionKey<BookWorkspace> = Symbol('book-workspace')

export function useInjectedWorkspace(): BookWorkspace {
  const ws = inject(BOOK_WORKSPACE)
  if (!ws) throw new Error('useInjectedWorkspace 必须在 BookShell 内使用')
  return ws
}
