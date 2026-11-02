import type { ReviewFinding } from '@/types'

/**
 * 这两条不是"意见"，是"这一章没审完"的标记：输出撞到预算被截断，或分章
 * 审校里该章调用失败。它们是 advisory——截断的产物判不出严重度——但绝不能
 * 因此被读成"没有阻断，可以过"。后端 domain.ReviewVerdict 用的是同一套判据。
 */
const UNFINISHED_IDS = new Set(['review-truncated', 'review-incomplete'])

export type ReviewVerdict = 'clean' | 'blocking' | 'unfinished'

export function isUnfinishedFinding(f: ReviewFinding) {
  return UNFINISHED_IDS.has(f.id)
}

/** blocking 优先于 unfinished：已知硬伤可执行，审校不完整只是缺口。 */
export function reviewVerdict(findings: ReviewFinding[] = []): ReviewVerdict {
  let unfinished = false
  for (const f of findings) {
    if (isUnfinishedFinding(f)) {
      unfinished = true
      continue
    }
    if (f.severity === 'blocking') return 'blocking'
  }
  return unfinished ? 'unfinished' : 'clean'
}

export const VERDICT_LABEL: Record<ReviewVerdict, string> = {
  clean: '可通过',
  blocking: '有阻断',
  unfinished: '审校不完整',
}
