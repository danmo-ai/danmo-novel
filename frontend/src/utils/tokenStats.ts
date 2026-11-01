import type { CallRecord } from '@/types'

export interface CacheStat {
  /** 命中前缀缓存的 prompt token。 */
  read: number
  /** 写入缓存的 token（Anthropic 显式断点才有）。 */
  creation: number
  /** 计入缓存的 prompt 总量，即命中率分母。 */
  prompt: number
  /** 命中率 0–1；无数据时为 0。 */
  ratio: number
  /** 至少一条记录带缓存字段。老记录没有这些字段，要区分"0 命中"和"没数据"。 */
  hasData: boolean
}

const EMPTY: CacheStat = { read: 0, creation: 0, prompt: 0, ratio: 0, hasData: false }

/**
 * 汇总一批调用的缓存命中情况。
 *
 * 各家的 prompt token 都包含缓存部分（OpenAI 的 prompt_tokens 含
 * cached_tokens；Anthropic 侧我们在 adapter 里把 cache read/write 折算进了
 * PromptTokens），所以命中率统一用 read / promptTokens。
 */
export function cacheStat(calls: CallRecord[] | undefined | null): CacheStat {
  if (!calls?.length) return EMPTY
  let read = 0
  let creation = 0
  let prompt = 0
  let hasData = false
  for (const c of calls) {
    const r = c.cacheReadTokens || 0
    const w = c.cacheCreationTokens || 0
    if (r > 0 || w > 0) hasData = true
    read += r
    creation += w
    prompt += c.promptTokens || 0
  }
  return { read, creation, prompt, ratio: prompt > 0 ? read / prompt : 0, hasData }
}

/** 命中率的显示文本；无数据时给 "—" 而不是伪造 0%。 */
export function hitRateText(stat: CacheStat): string {
  return stat.hasData ? `${Math.round(stat.ratio * 100)}%` : '—'
}

/**
 * 单行调用的缓存摘要：`12,345 (78%)`。
 *
 * 修复重试这一行应接近 100%——repair 只追加在 payload 尾部，前缀不动。
 * 接近 0 说明有东西在字节层面改动了 prompt 前缀。
 */
export function callCacheText(c: CallRecord): string {
  const read = c.cacheReadTokens || 0
  if (!read) return '—'
  const pct = Math.round((read / Math.max(1, c.promptTokens || 0)) * 100)
  return `${read.toLocaleString()} (${pct}%)`
}
