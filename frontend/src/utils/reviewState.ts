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

/**
 * blocking 优先于 unfinished：已知硬伤可执行，审校不完整只是缺口。
 *
 * 判据必须与后端 domain.ReviewVerdict 完全一致，否则 UI 说"可通过"、服务端
 * 却拒绝通过，作者会卡在没有解释的报错上：
 *  - 复审范围内标记的历史欠账（outOfScope）不算阻断；
 *  - 单轮检出的 blocking 不算阻断（同一章两次调用给出的 blocking 完全不同，
 *    单轮即可阻断会让清单永远关不掉），要 2 轮以上；
 *  - roundsHit 缺省（旧数据、本地硬门）按原语义阻断，避免历史清单突然放行。
 */
export function reviewVerdict(findings: ReviewFinding[] = []): ReviewVerdict {
  let unfinished = false
  for (const f of findings) {
    if (isUnfinishedFinding(f)) {
      unfinished = true
      continue
    }
    if (f.severity !== 'blocking' || f.outOfScope) continue
    if (!f.roundsHit || f.roundsHit >= 2) return 'blocking'
  }
  return unfinished ? 'unfinished' : 'clean'
}

export const VERDICT_LABEL: Record<ReviewVerdict, string> = {
  clean: '可通过',
  blocking: '有阻断',
  unfinished: '审校不完整',
}
