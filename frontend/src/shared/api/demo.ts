import { api } from '@/shared/api/client'
import type { BookSnapshot } from '@/shared/types'

/**
 * 内置示例书。
 *
 * 内容不在这里：它随 Go 二进制一起发布（core/service/books/demo/），因为那些
 * 文件要过总纲/卷纲的结构校验，只有 Go 侧能用测试保证。前端只负责请求一次
 * 「把这本示例书写进库里」。
 *
 * 为什么需要它：应用的全部生成能力都要用户自己的模型 API Key。审核员没有 Key，
 * 空书架 + 一排点不动的按钮就是 App Store 4.2 说的「功能不完整」。
 *
 * 两个壳（桌面书架 / 手机书架）共用这一个调用，所以放在 api/ 而不是 mobile/。
 */
export async function seedSampleBook(): Promise<{ id: string; created: boolean }> {
  const res = await api.post<{ created: boolean; book: BookSnapshot }>('/api/books/demo')
  return { id: res.book.summary.id, created: res.created }
}
